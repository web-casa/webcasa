package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const serviceTemplate = `[Unit]
Description=Web.Casa Project: {{.Name}}
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory={{.WorkDir}}
ExecStart={{.StartCommand}}
Restart=on-failure
RestartSec=5
{{range .EnvLines}}Environment={{.}}
{{end}}
# Logging
StandardOutput=append:{{.LogFile}}
StandardError=append:{{.LogFile}}

[Install]
WantedBy=multi-user.target
`

var svcTmpl = template.Must(template.New("service").Parse(serviceTemplate))

// ProcessManager manages project processes via systemd.
type ProcessManager struct {
	logDir string
}

// NewProcessManager creates a new process manager.
func NewProcessManager(logDir string) *ProcessManager {
	return &ProcessManager{logDir: logDir}
}

// ServiceName returns the systemd service name for a project.
func ServiceName(projectID uint) string {
	return fmt.Sprintf("webcasa-project-%d", projectID)
}

// Install creates and enables a systemd service for the project.
func (pm *ProcessManager) Install(project *Project, workDir string) error {
	name := ServiceName(project.ID)
	unitPath := filepath.Join("/etc/systemd/system", name+".service")

	// Prepare env lines
	var envLines []string
	for _, ev := range project.EnvVarList {
		envLines = append(envLines, fmt.Sprintf("%s=%s", ev.Key, ev.Value))
	}
	if project.Port > 0 {
		envLines = append(envLines, fmt.Sprintf("PORT=%d", project.Port))
	}

	// Runtime log file
	runtimeLog := filepath.Join(pm.logDir, fmt.Sprintf("project_%d", project.ID), "runtime.log")
	os.MkdirAll(filepath.Dir(runtimeLog), 0755)

	data := struct {
		Name         string
		WorkDir      string
		StartCommand string
		EnvLines     []string
		LogFile      string
	}{
		Name:         project.Name,
		WorkDir:      workDir,
		StartCommand: resolveStartCommand(project.StartCommand, workDir),
		EnvLines:     envLines,
		LogFile:      runtimeLog,
	}

	f, err := os.Create(unitPath)
	if err != nil {
		return fmt.Errorf("create service file: %w", err)
	}
	defer f.Close()

	if err := svcTmpl.Execute(f, data); err != nil {
		return fmt.Errorf("render service template: %w", err)
	}

	// Reload systemd and enable service
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	return systemctl("enable", name)
}

// Start starts the project's systemd service.
func (pm *ProcessManager) Start(projectID uint) error {
	return systemctl("start", ServiceName(projectID))
}

// Stop stops the project's systemd service.
func (pm *ProcessManager) Stop(projectID uint) error {
	return systemctl("stop", ServiceName(projectID))
}

// Restart restarts the project's systemd service.
func (pm *ProcessManager) Restart(projectID uint) error {
	return systemctl("restart", ServiceName(projectID))
}

// Uninstall stops, disables, and removes the systemd service.
func (pm *ProcessManager) Uninstall(projectID uint) error {
	name := ServiceName(projectID)
	systemctl("stop", name)
	systemctl("disable", name)
	unitPath := filepath.Join("/etc/systemd/system", name+".service")
	os.Remove(unitPath)
	return systemctl("daemon-reload")
}

// IsRunning checks if the project's systemd service is active.
func (pm *ProcessManager) IsRunning(projectID uint) bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", ServiceName(projectID))
	return cmd.Run() == nil
}

// RuntimeLogPath returns the path to the runtime log file.
func (pm *ProcessManager) RuntimeLogPath(projectID uint) string {
	return filepath.Join(pm.logDir, fmt.Sprintf("project_%d", projectID), "runtime.log")
}

// ReadRuntimeLog reads the last N lines of the runtime log.
func (pm *ProcessManager) ReadRuntimeLog(projectID uint, lines int) (string, error) {
	logPath := pm.RuntimeLogPath(projectID)
	if _, err := os.Stat(logPath); err != nil {
		return "", nil
	}
	cmd := exec.Command("tail", "-n", fmt.Sprintf("%d", lines), logPath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s (%w)", strings.Join(args, " "), string(out), err)
	}
	return nil
}

// resolveStartCommand makes relative paths absolute.
func resolveStartCommand(cmd, workDir string) string {
	if strings.HasPrefix(cmd, "./") {
		return filepath.Join(workDir, cmd[2:])
	}
	return cmd
}
