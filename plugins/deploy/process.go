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
{{end}}{{if .MemoryMax}}MemoryMax={{.MemoryMax}}
{{end}}{{if .CPUQuota}}CPUQuota={{.CPUQuota}}
{{end}}# Logging
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
	return pm.installService(ServiceName(project.ID), project, workDir, project.Port)
}

// InstallStaging creates a staging systemd service with a custom port for zero-downtime deployment.
func (pm *ProcessManager) InstallStaging(project *Project, workDir string, port int) error {
	return pm.installService(ServiceName(project.ID)+"-staging", project, workDir, port)
}

// PromoteStaging stops the main service, removes it, renames the staging service, and reloads systemd.
func (pm *ProcessManager) PromoteStaging(projectID uint) error {
	mainName := ServiceName(projectID)
	stagingName := mainName + "-staging"

	// Stop and remove old main service
	systemctl("stop", mainName)
	systemctl("disable", mainName)
	mainUnitPath := filepath.Join("/etc/systemd/system", mainName+".service")
	os.Remove(mainUnitPath)

	// Rename staging service file to main
	stagingUnitPath := filepath.Join("/etc/systemd/system", stagingName+".service")
	os.Rename(stagingUnitPath, mainUnitPath)

	// Reload systemd to recognize the renamed file
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}

	// Enable the service under its canonical name
	return systemctl("enable", mainName)
}

// CleanupStaging removes a staging service if it exists (on failure).
func (pm *ProcessManager) CleanupStaging(projectID uint) {
	stagingName := ServiceName(projectID) + "-staging"
	systemctl("stop", stagingName)
	systemctl("disable", stagingName)
	unitPath := filepath.Join("/etc/systemd/system", stagingName+".service")
	os.Remove(unitPath)
	systemctl("daemon-reload")
}

// StartStaging starts the staging service.
func (pm *ProcessManager) StartStaging(projectID uint) error {
	return systemctl("start", ServiceName(projectID)+"-staging")
}

// IsStagingRunning checks if the staging service is active.
func (pm *ProcessManager) IsStagingRunning(projectID uint) bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", ServiceName(projectID)+"-staging")
	return cmd.Run() == nil
}

// installService is the internal implementation for creating a systemd service unit.
func (pm *ProcessManager) installService(serviceName string, project *Project, workDir string, port int) error {
	unitPath := filepath.Join("/etc/systemd/system", serviceName+".service")

	// Prepare env lines
	var envLines []string
	for _, ev := range project.EnvVarList {
		envLines = append(envLines, fmt.Sprintf("%s=%s", ev.Key, ev.Value))
	}
	if port > 0 {
		envLines = append(envLines, fmt.Sprintf("PORT=%d", port))
	}

	// Runtime log file
	runtimeLog := filepath.Join(pm.logDir, fmt.Sprintf("project_%d", project.ID), "runtime.log")
	os.MkdirAll(filepath.Dir(runtimeLog), 0755)

	// Resource limits
	var memoryMax, cpuQuota string
	if project.MemoryLimit > 0 {
		memoryMax = fmt.Sprintf("%dM", project.MemoryLimit)
	}
	if project.CPULimit > 0 {
		cpuQuota = fmt.Sprintf("%d%%", project.CPULimit)
	}

	data := struct {
		Name         string
		WorkDir      string
		StartCommand string
		EnvLines     []string
		LogFile      string
		MemoryMax    string
		CPUQuota     string
	}{
		Name:         project.Name,
		WorkDir:      workDir,
		StartCommand: resolveStartCommand(project.StartCommand, workDir),
		EnvLines:     envLines,
		LogFile:      runtimeLog,
		MemoryMax:    memoryMax,
		CPUQuota:     cpuQuota,
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
	return systemctl("enable", serviceName)
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

// ExtraProcessServiceName returns the systemd service name for an extra process instance.
func ExtraProcessServiceName(projectID uint, procName string, instance int) string {
	// Sanitize name: lowercase, replace spaces with dashes
	safe := strings.ToLower(strings.ReplaceAll(procName, " ", "-"))
	return fmt.Sprintf("webcasa-project-%d-%s-%d", projectID, safe, instance)
}

// InstallExtraProcess creates and enables systemd services for an extra process (one per instance).
func (pm *ProcessManager) InstallExtraProcess(project *Project, proc *ExtraProcess, workDir string) error {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(project.ID, proc.Name, i)
		unitPath := filepath.Join("/etc/systemd/system", svcName+".service")

		var envLines []string
		for _, ev := range project.EnvVarList {
			envLines = append(envLines, fmt.Sprintf("%s=%s", ev.Key, ev.Value))
		}

		runtimeLog := filepath.Join(pm.logDir, fmt.Sprintf("project_%d", project.ID), fmt.Sprintf("proc_%s_%d.log", proc.Name, i))
		os.MkdirAll(filepath.Dir(runtimeLog), 0755)

		data := struct {
			Name         string
			WorkDir      string
			StartCommand string
			EnvLines     []string
			LogFile      string
			MemoryMax    string
			CPUQuota     string
		}{
			Name:         fmt.Sprintf("%s - %s (#%d)", project.Name, proc.Name, i),
			WorkDir:      workDir,
			StartCommand: resolveStartCommand(proc.Command, workDir),
			EnvLines:     envLines,
			LogFile:      runtimeLog,
		}

		f, err := os.Create(unitPath)
		if err != nil {
			return fmt.Errorf("create extra process service file: %w", err)
		}
		if err := svcTmpl.Execute(f, data); err != nil {
			f.Close()
			return fmt.Errorf("render extra process service template: %w", err)
		}
		f.Close()
	}

	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(project.ID, proc.Name, i)
		if err := systemctl("enable", svcName); err != nil {
			return err
		}
	}
	return nil
}

// StartExtraProcess starts all instances of an extra process.
func (pm *ProcessManager) StartExtraProcess(projectID uint, proc *ExtraProcess) error {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(projectID, proc.Name, i)
		if err := systemctl("start", svcName); err != nil {
			return err
		}
	}
	return nil
}

// StopExtraProcess stops all instances of an extra process.
func (pm *ProcessManager) StopExtraProcess(projectID uint, proc *ExtraProcess) error {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(projectID, proc.Name, i)
		if err := systemctl("stop", svcName); err != nil {
			return err
		}
	}
	return nil
}

// RestartExtraProcess restarts all instances of an extra process.
func (pm *ProcessManager) RestartExtraProcess(projectID uint, proc *ExtraProcess) error {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(projectID, proc.Name, i)
		if err := systemctl("restart", svcName); err != nil {
			return err
		}
	}
	return nil
}

// UninstallExtraProcess stops, disables, and removes all instances of an extra process.
func (pm *ProcessManager) UninstallExtraProcess(projectID uint, proc *ExtraProcess) {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(projectID, proc.Name, i)
		systemctl("stop", svcName)
		systemctl("disable", svcName)
		unitPath := filepath.Join("/etc/systemd/system", svcName+".service")
		os.Remove(unitPath)
	}
	systemctl("daemon-reload")
}

// IsExtraProcessRunning checks if the first instance of an extra process is active.
func (pm *ProcessManager) IsExtraProcessRunning(projectID uint, proc *ExtraProcess) bool {
	svcName := ExtraProcessServiceName(projectID, proc.Name, 1)
	cmd := exec.Command("systemctl", "is-active", "--quiet", svcName)
	return cmd.Run() == nil
}

// resolveStartCommand makes relative paths absolute.
func resolveStartCommand(cmd, workDir string) string {
	if strings.HasPrefix(cmd, "./") {
		return filepath.Join(workDir, cmd[2:])
	}
	return cmd
}
