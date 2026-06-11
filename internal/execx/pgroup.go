// Package execx contains os/exec helpers that are safer than the standard
// library defaults for WebCasa's use cases. Specifically, every long-running
// shell pipeline we spawn (bash -c 'curl | bash', project build commands,
// kopia install, firewall streaming, etc.) needs a "kill the whole process
// tree on cancel" semantic that exec.CommandContext does not provide: the
// stdlib only sends SIGKILL to the PID it started, leaving downstream
// pipeline children orphaned to init. This package installs the process
// group plumbing once so callers don't repeatedly get it wrong.
package execx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// DefaultWaitDelay is how long we wait for a cancelled process group to
// exit cleanly after the initial SIGKILL lands before Go's cmd.Wait gives
// up. Tuned conservatively: most pipelines drain in milliseconds, but a
// stuck child with open file descriptors can take longer.
const DefaultWaitDelay = 5 * time.Second

// CommandContext is a drop-in replacement for exec.CommandContext that puts
// the spawned process in its own process group and installs a Cancel hook
// that SIGKILLs the entire group (pid = -pgid) when the context fires. This
// is the behaviour most callers actually want for bash/sh pipelines, since
// `bash -c "A | B"` creates two children, and killing only the outer bash
// leaves A and B orphaned.
//
// The returned *exec.Cmd behaves like any other Cmd — callers still need to
// configure Stdin/Stdout/Stderr/Env/Dir as usual. WaitDelay is pre-set to
// DefaultWaitDelay so cmd.Wait doesn't hang forever if the group doesn't
// die in response to SIGKILL.
//
// Linux/Unix only — on platforms without syscall.SysProcAttr.Setpgid this
// would need a build-tagged fallback. WebCasa targets EL9/EL10 so that is
// intentional.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative PID signals every process in the group.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		// Race: the process may have exited between the ctx firing and
		// the Kill syscall landing. ESRCH ("no such process") or the
		// stdlib's os.ErrProcessDone both mean "already gone". Translate
		// to ErrProcessDone so cmd.Wait returns the real exit status
		// instead of propagating a spurious kill-failed error. Matches
		// the stdlib default Cancel hook (exec.CommandContext →
		// Process.Kill filters this same case internally).
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = DefaultWaitDelay
	return cmd
}

// BashContext is a convenience wrapper that runs `bash -c <script>` under
// CommandContext. Callers that previously used exec.Command("bash", "-c",
// script) without context binding can migrate by swapping the constructor
// and threading a context through — the kill-group semantics then come for
// free. `script` is passed to bash as a single argument so normal shell
// quoting/escaping rules apply; callers are still responsible for not
// concatenating untrusted input into it.
func BashContext(ctx context.Context, script string) *exec.Cmd {
	return CommandContext(ctx, "bash", "-c", script)
}

// sandboxOnce memoises whether the host can run transient commands under a
// systemd sandbox (see SandboxAvailable).
var (
	sandboxOnce      sync.Once
	sandboxSupported bool
)

// SandboxAvailable reports whether SandboxBashContext will actually sandbox a
// command (vs. fall back to a plain bash run). It is true only when:
//
//   - the systemd-run binary is on PATH,
//   - the systemd system manager is running (/run/systemd/system exists), and
//   - we are uid 0.
//
// All three are required because DynamicUser sandboxing is created as a
// transient unit on the *system* bus, which only works when systemd is the
// init system and we have privilege to ask it. In a Docker deployment none of
// this holds — but there the WebCasa process already runs as a non-root user
// (see Dockerfile), so arbitrary commands are not root-privileged to begin
// with and the plain fallback is acceptable.
func SandboxAvailable() bool {
	sandboxOnce.Do(func() {
		if os.Geteuid() != 0 {
			return
		}
		if _, err := os.Stat("/run/systemd/system"); err != nil {
			return
		}
		if _, err := exec.LookPath("systemd-run"); err != nil {
			return
		}
		sandboxSupported = true
	})
	return sandboxSupported
}

// SandboxBashContext runs `bash -c <script>` inside a locked-down, transient
// systemd scope when the host supports it (see SandboxAvailable), otherwise it
// degrades to BashContext.
//
// The sandbox drops privileges to an ephemeral DynamicUser and applies:
//
//	NoNewPrivileges=yes   no setuid/capability escalation
//	ProtectSystem=strict  entire filesystem hierarchy mounted read-only
//	ProtectHome=yes       /home, /root, /run/user made inaccessible
//	PrivateTmp=yes        private /tmp scratch (so writes have somewhere to go)
//	PrivateDevices=yes    no access to physical block/char devices
//	MemoryMax / TasksMax   resource caps to blunt fork bombs / OOM
//	RuntimeMaxSec          hard wall-clock kill matching the caller timeout
//
// This is the real privilege boundary for arbitrary command execution — the
// denylist in coreapi.RunCommand is only a speed bump. The trade-off is that a
// sandboxed command runs read-only as a nobody-class user: it can inspect the
// system but cannot read root-only files (e.g. full journald) or mutate state.
// Privileged or mutating operations must go through dedicated, explicitly
// audited APIs/tools, not the arbitrary-command path.
//
// runtimeMaxSec bounds the unit's wall-clock lifetime; pass the same timeout
// the caller uses for its context so a wedged sandbox can't outlive it.
func SandboxBashContext(ctx context.Context, script string, runtimeMaxSec int) *exec.Cmd {
	if !SandboxAvailable() {
		return BashContext(ctx, script)
	}
	if runtimeMaxSec <= 0 {
		runtimeMaxSec = 120
	}
	args := []string{
		"--pipe", "--wait", "--collect", "--quiet",
		"-p", "DynamicUser=yes",
		"-p", "NoNewPrivileges=yes",
		"-p", "ProtectSystem=strict",
		"-p", "ProtectHome=yes",
		"-p", "PrivateTmp=yes",
		"-p", "PrivateDevices=yes",
		"-p", "MemoryMax=512M",
		"-p", "TasksMax=128",
		"-p", "RuntimeMaxSec=" + strconv.Itoa(runtimeMaxSec),
		"--", "/bin/bash", "-c", script,
	}
	return CommandContext(ctx, "systemd-run", args...)
}
