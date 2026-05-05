package deploy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// nixpacksInstallScript is the official one-liner from
// https://nixpacks.com/docs/install (works on glibc Linux). It writes
// the binary to /usr/local/bin/nixpacks. We invoke it via `bash` so
// the script can use bashisms; we don't pipe through curl-to-shell
// directly to keep the chain auditable in panel logs.
//
// The script needs root to write to /usr/local/bin. The panel is
// expected to run as root (per install.sh defaults); a non-root
// install will surface in the SSE error stream as `permission denied`.
const nixpacksInstallScript = `set -e
echo "[nixpacks] downloading installer"
curl -fsSL https://nixpacks.com/install.sh -o /tmp/nixpacks-install.sh
chmod +x /tmp/nixpacks-install.sh
echo "[nixpacks] running installer (writes /usr/local/bin/nixpacks)"
/tmp/nixpacks-install.sh
rm -f /tmp/nixpacks-install.sh
echo "[nixpacks] verifying"
nixpacks --version
echo "[nixpacks] done"
`

// nixpacksInstallMu serializes install attempts so two admins clicking
// at the same time don't race the installer script.
var nixpacksInstallMu sync.Mutex

// NixpacksStatus describes whether nixpacks CLI is installed + its version.
type NixpacksStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"` // populated when Installed=true
	Path      string `json:"path,omitempty"`    // resolved binary path
}

// CheckNixpacks resolves the CLI and reads its --version. Returns a
// non-installed status without an error if the binary isn't on PATH.
func CheckNixpacks() NixpacksStatus {
	path, err := exec.LookPath("nixpacks")
	if err != nil {
		return NixpacksStatus{Installed: false}
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		// Binary exists but version probe failed — still report as
		// installed; the build path will surface the real error.
		return NixpacksStatus{Installed: true, Path: path}
	}
	return NixpacksStatus{
		Installed: true,
		Path:      path,
		Version:   strings.TrimSpace(string(out)),
	}
}

// InstallNixpacks runs the official installer script and streams its
// output line-by-line via writeSSE. writeEvent is called with event
// type "done" + final status JSON, or "error" + message.
//
// Mirrors plugins/backup/kopia_installer.go's contract so the
// frontend SSE consumer can be reused as-is for nixpacks.
func InstallNixpacks(ctx context.Context, writeSSE func(string), writeEvent func(event, data string)) {
	if !nixpacksInstallMu.TryLock() {
		writeEvent("error", "another install is already in progress")
		return
	}
	defer nixpacksInstallMu.Unlock()

	// Skip work if already installed; report success.
	if CheckNixpacks().Installed {
		writeSSE("[nixpacks] already installed; skipping")
		writeEvent("done", `{"installed": true}`)
		return
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", nixpacksInstallScript)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeEvent("error", fmt.Sprintf("stdout pipe: %v", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		writeEvent("error", fmt.Sprintf("stderr pipe: %v", err))
		return
	}
	if err := cmd.Start(); err != nil {
		writeEvent("error", fmt.Sprintf("install start: %v", err))
		return
	}

	// Stream stdout + stderr in parallel — installer writes progress
	// to both. Use one goroutine per stream + a sync.WaitGroup so we
	// don't lose late lines after Wait().
	var wg sync.WaitGroup
	streamReader := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		// Default 64 KiB buffer is fine for installer logs.
		for sc.Scan() {
			writeSSE(sc.Text())
		}
	}
	wg.Add(2)
	go streamReader(stdout)
	go streamReader(stderr)
	wg.Wait()

	err = cmd.Wait()
	if err != nil {
		writeEvent("error", fmt.Sprintf("install exited %v", err))
		return
	}

	// Re-check so the response carries the now-installed version.
	status := CheckNixpacks()
	writeEvent("done", fmt.Sprintf(`{"installed": %v, "version": %q}`, status.Installed, status.Version))
}
