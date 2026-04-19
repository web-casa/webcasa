package deploy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/web-casa/webcasa/plugins/deploy/builders"
)

// DockerRunner manages Docker containers for project deployment.
type DockerRunner struct{}

// NewDockerRunner creates a new DockerRunner.
func NewDockerRunner() *DockerRunner {
	return &DockerRunner{}
}

// ImageName returns the Docker image name for a project.
func (r *DockerRunner) ImageName(projectID uint) string {
	return fmt.Sprintf("webcasa-project-%d", projectID)
}

// ImageTag returns the full image:tag string for a build.
func (r *DockerRunner) ImageTag(projectID uint, buildNum int) string {
	return fmt.Sprintf("webcasa-project-%d:%d", projectID, buildNum)
}

// ContainerName returns the Docker container name for a project.
func (r *DockerRunner) ContainerName(projectID uint) string {
	return fmt.Sprintf("webcasa-project-%d", projectID)
}

// StagingContainerName returns the staging container name for zero-downtime deployment.
func (r *DockerRunner) StagingContainerName(projectID uint) string {
	return fmt.Sprintf("webcasa-project-%d-staging", projectID)
}

// BuildImage runs the appropriate builder in the given directory and streams output to the logWriter.
// buildType can be: dockerfile, nixpacks, paketo, railpack, static, auto, or "" (legacy = dockerfile).
// Returns the image tag on success.
func (r *DockerRunner) BuildImage(ctx context.Context, dir string, projectID uint, buildNum int, logWriter *LogWriter, buildType ...string) (string, error) {
	imageTag := r.ImageTag(projectID, buildNum)

	// Determine builder type.
	bt := ""
	if len(buildType) > 0 {
		bt = buildType[0]
	}
	if bt == "auto" || bt == "" {
		bt = builders.DetectBuilder(dir)
	}

	logWriter.Write([]byte(fmt.Sprintf("==> Building with %s: %s\n", bt, imageTag)))

	binName, args, err := builders.BuildCommand(bt, dir, imageTag)
	if err != nil {
		return "", fmt.Errorf("builder setup failed: %w", err)
	}

	cmd := exec.CommandContext(ctx, binName, args...)
	cmd.Dir = dir
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s build failed: %w", bt, err)
	}

	logWriter.Write([]byte(fmt.Sprintf("==> Image built: %s\n", imageTag)))
	return imageTag, nil
}

// BuildImageWithTag builds an image with an explicitly-supplied tag rather
// than the projectID/buildNum-derived one. Used by the preview deploy flow
// where each preview gets its own tag (`webcasa-preview-<id>`) so old preview
// images can be pruned independently of the main project image.
func (r *DockerRunner) BuildImageWithTag(ctx context.Context, dir, imageTag string, logWriter *LogWriter, buildType string) error {
	bt := buildType
	if bt == "auto" || bt == "" {
		bt = builders.DetectBuilder(dir)
	}
	if logWriter != nil {
		logWriter.Write([]byte(fmt.Sprintf("==> Building with %s: %s\n", bt, imageTag)))
	}
	binName, args, err := builders.BuildCommand(bt, dir, imageTag)
	if err != nil {
		return fmt.Errorf("builder setup failed: %w", err)
	}
	cmd := exec.CommandContext(ctx, binName, args...)
	cmd.Dir = dir
	if logWriter != nil {
		cmd.Stdout = logWriter
		cmd.Stderr = logWriter
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s build failed: %w", bt, err)
	}
	if logWriter != nil {
		logWriter.Write([]byte(fmt.Sprintf("==> Image built: %s\n", imageTag)))
	}
	return nil
}

// RunOptions holds optional settings for running a container.
type RunOptions struct {
	MemoryLimitMB int // 0 = unlimited
	CPULimitPct   int // percentage (100 = 1 core), 0 = unlimited
}

// Run starts a container from the given image with port mapping and environment variables.
// It stops and removes any existing container with the same name first.
func (r *DockerRunner) Run(ctx context.Context, projectID uint, imageTag string, port int, envVars []EnvVar, opts ...RunOptions) (string, error) {
	containerName := r.ContainerName(projectID)
	return r.RunWithName(ctx, containerName, imageTag, port, envVars, opts...)
}

// RunStaging starts a staging container (doesn't stop the main container).
func (r *DockerRunner) RunStaging(ctx context.Context, projectID uint, imageTag string, port int, envVars []EnvVar, opts ...RunOptions) (string, error) {
	containerName := r.StagingContainerName(projectID)
	// Clean up any leftover staging container
	r.StopAndRemove(containerName)
	return r.runContainer(ctx, containerName, imageTag, port, envVars, false, opts...)
}

// RunWithName starts a container with the given name, stopping/removing existing container first.
func (r *DockerRunner) RunWithName(ctx context.Context, containerName string, imageTag string, port int, envVars []EnvVar, opts ...RunOptions) (string, error) {
	// Stop and remove existing container if any.
	r.StopAndRemove(containerName)
	return r.runContainer(ctx, containerName, imageTag, port, envVars, false, opts...)
}

// runContainer is the internal method that creates and starts a container.
func (r *DockerRunner) runContainer(ctx context.Context, containerName string, imageTag string, port int, envVars []EnvVar, _ bool, opts ...RunOptions) (string, error) {

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--restart", "unless-stopped",
	}

	// Resource limits
	if len(opts) > 0 {
		opt := opts[0]
		if opt.MemoryLimitMB > 0 {
			args = append(args, "--memory", fmt.Sprintf("%dm", opt.MemoryLimitMB))
		}
		if opt.CPULimitPct > 0 {
			// Convert percentage to fractional CPUs: 100% = 1.0 CPU, 200% = 2.0 CPU
			cpus := float64(opt.CPULimitPct) / 100.0
			args = append(args, "--cpus", fmt.Sprintf("%.2f", cpus))
		}
	}

	// Port mapping: host port → container port (same port for simplicity)
	if port > 0 {
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", port, port))
	}

	// Environment variables
	for _, ev := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", ev.Key, ev.Value))
	}
	// Always pass PORT env var
	if port > 0 {
		args = append(args, "-e", fmt.Sprintf("PORT=%d", port))
	}

	args = append(args, imageTag)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker run failed: %s: %w", out.String(), err)
	}

	containerID := strings.TrimSpace(out.String())
	return containerID, nil
}

// StopAndRemove stops and removes a container by name. Errors are ignored (container may not exist).
func (r *DockerRunner) StopAndRemove(containerName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	exec.CommandContext(ctx, "docker", "stop", containerName).Run()
	exec.CommandContext(ctx, "docker", "rm", "-f", containerName).Run()
}

// Stop stops a running container by name.
func (r *DockerRunner) Stop(containerName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "stop", containerName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker stop: %s: %w", string(out), err)
	}
	return nil
}

// Start starts a stopped container by name.
func (r *DockerRunner) Start(containerName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "start", containerName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker start: %s: %w", string(out), err)
	}
	return nil
}

// Remove removes a container by name (force).
func (r *DockerRunner) Remove(containerName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker rm: %s: %w", string(out), err)
	}
	return nil
}

// Rename renames a container.
func (r *DockerRunner) Rename(oldName, newName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "rename", oldName, newName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker rename: %s: %w", string(out), err)
	}
	return nil
}

// IsRunning checks if a container is running.
func (r *DockerRunner) IsRunning(containerName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerName).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// Logs returns the last N lines of container logs.
func (r *DockerRunner) Logs(containerName string, tail int) (string, error) {
	if tail <= 0 {
		tail = 100
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "logs", "--tail", fmt.Sprintf("%d", tail), containerName)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("docker logs: %w", err)
	}
	return buf.String(), nil
}

// ExtraProcessContainerName returns the Docker container name for an extra process instance.
func (r *DockerRunner) ExtraProcessContainerName(projectID uint, procName string, instance int) string {
	safe := strings.ToLower(strings.ReplaceAll(procName, " ", "-"))
	return fmt.Sprintf("webcasa-project-%d-%s-%d", projectID, safe, instance)
}

// RunExtraProcess starts containers for an extra process (one per instance).
// Uses the project's Docker image with a custom command override.
func (r *DockerRunner) RunExtraProcess(ctx context.Context, projectID uint, imageTag string, proc ExtraProcess, envVars []EnvVar) error {
	for i := 1; i <= proc.Instances; i++ {
		containerName := r.ExtraProcessContainerName(projectID, proc.Name, i)
		// Clean up existing container
		r.StopAndRemove(containerName)

		args := []string{
			"run", "-d",
			"--name", containerName,
			"--restart", "unless-stopped",
		}

		for _, ev := range envVars {
			args = append(args, "-e", fmt.Sprintf("%s=%s", ev.Key, ev.Value))
		}

		// Override entrypoint with bash -c for command
		args = append(args, "--entrypoint", "bash")
		args = append(args, imageTag, "-c", proc.Command)

		cmd := exec.CommandContext(ctx, "docker", args...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker run extra process %s#%d: %s: %w", proc.Name, i, out.String(), err)
		}
	}
	return nil
}

// StopExtraProcess stops and removes all containers for an extra process.
func (r *DockerRunner) StopExtraProcess(projectID uint, proc ExtraProcess) {
	for i := 1; i <= proc.Instances; i++ {
		containerName := r.ExtraProcessContainerName(projectID, proc.Name, i)
		r.StopAndRemove(containerName)
	}
}

// RestartExtraProcess restarts all containers for an extra process.
func (r *DockerRunner) RestartExtraProcess(projectID uint, proc ExtraProcess) error {
	for i := 1; i <= proc.Instances; i++ {
		containerName := r.ExtraProcessContainerName(projectID, proc.Name, i)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, "docker", "restart", containerName)
		if out, err := cmd.CombinedOutput(); err != nil {
			cancel()
			return fmt.Errorf("docker restart extra process %s#%d: %s: %w", proc.Name, i, string(out), err)
		}
		cancel()
	}
	return nil
}

// IsExtraProcessRunning checks if the first instance of an extra process container is running.
func (r *DockerRunner) IsExtraProcessRunning(projectID uint, proc ExtraProcess) bool {
	containerName := r.ExtraProcessContainerName(projectID, proc.Name, 1)
	return r.IsRunning(containerName)
}

// LogsStream streams container logs to a scanner (for real-time streaming).
func (r *DockerRunner) LogsStream(ctx context.Context, containerName string) (*bufio.Scanner, *exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", "--tail", "100", containerName)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = cmd.Stdout // combine stderr into stdout

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	scanner := bufio.NewScanner(stdout)
	return scanner, cmd, nil
}
