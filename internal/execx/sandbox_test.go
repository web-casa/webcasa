package execx

import (
	"context"
	"strings"
	"testing"
)

// TestSandboxBashContext_Fallback verifies that when the host can't sandbox
// (the normal case under `go test`: either non-root or no systemd), we still
// get a runnable plain bash command rather than an error or a systemd-run
// invocation.
func TestSandboxBashContext_Fallback(t *testing.T) {
	if SandboxAvailable() {
		t.Skip("host supports systemd sandbox; fallback path not exercised here")
	}
	cmd := SandboxBashContext(context.Background(), "echo hi", 30)
	if cmd == nil {
		t.Fatal("nil command")
	}
	if !strings.HasSuffix(cmd.Path, "bash") && cmd.Args[0] != "bash" {
		t.Fatalf("expected a bash fallback, got args %v", cmd.Args)
	}
	// The fallback must actually run.
	if err := cmd.Run(); err != nil {
		t.Fatalf("fallback command failed to run: %v", err)
	}
}

// TestSandboxBashContext_Args verifies the sandbox wrapper, when active, builds
// a systemd-run invocation that drops privileges and bounds runtime. We don't
// require systemd here — we just assert the arg construction is correct by
// exercising the builder through a fake-available path is not possible without
// refactor, so this test documents the expected hardening flags via the
// fallback-aware contract: when SandboxAvailable() is true the first arg is the
// systemd-run binary and DynamicUser is requested.
func TestSandboxBashContext_ActiveShape(t *testing.T) {
	if !SandboxAvailable() {
		t.Skip("systemd sandbox not available in this environment")
	}
	cmd := SandboxBashContext(context.Background(), "echo hi", 45)
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{"DynamicUser=yes", "NoNewPrivileges=yes", "ProtectSystem=strict", "RuntimeMaxSec=45"} {
		if !strings.Contains(joined, want) {
			t.Errorf("sandbox args missing %q; got %s", want, joined)
		}
	}
}
