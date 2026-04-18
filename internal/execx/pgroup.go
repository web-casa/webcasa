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
	"os/exec"
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
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
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
