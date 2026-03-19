package firewall

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var (
	portRe     = regexp.MustCompile(`^\d+(-\d+)?$`)
	protocolRe = regexp.MustCompile(`^(tcp|udp)$`)
	serviceRe  = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	zoneRe     = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// Service wraps firewall-cmd operations.
type Service struct {
	logger    *slog.Logger
	panelPort string // panel's own port to protect
}

// NewService creates a firewall Service.
func NewService(logger *slog.Logger, panelPort string) *Service {
	return &Service{logger: logger, panelPort: panelPort}
}

// Status returns the firewalld status.
func (s *Service) Status() (*FirewallStatus, error) {
	status := &FirewallStatus{}

	// Check if firewall-cmd exists.
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		return status, nil
	}
	status.Installed = true

	// Check if running.
	out, err := s.runCmd("--state")
	if err != nil {
		return status, nil // not running
	}
	status.Running = strings.TrimSpace(out) == "running"

	if !status.Running {
		return status, nil
	}

	// Version.
	if v, err := s.runCmd("--version"); err == nil {
		status.Version = strings.TrimSpace(v)
	}

	// Default zone.
	if z, err := s.runCmd("--get-default-zone"); err == nil {
		status.DefaultZone = strings.TrimSpace(z)
	}

	// All zones.
	if z, err := s.runCmd("--get-zones"); err == nil {
		status.Zones = strings.Fields(z)
	}

	return status, nil
}

// ListZones returns all zones with their details.
func (s *Service) ListZones() ([]ZoneInfo, error) {
	status, err := s.Status()
	if err != nil || !status.Running {
		return nil, fmt.Errorf("firewalld is not running")
	}

	// Get active zones.
	activeMap := make(map[string]bool)
	if out, err := s.runCmd("--get-active-zones"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "interfaces:") && !strings.HasPrefix(line, "sources:") {
				activeMap[line] = true
			}
		}
	}

	var zones []ZoneInfo
	for _, name := range status.Zones {
		z, err := s.GetZone(name)
		if err != nil {
			continue
		}
		z.Active = activeMap[name]
		zones = append(zones, *z)
	}
	return zones, nil
}

// GetZone returns detailed info for a specific zone.
func (s *Service) GetZone(name string) (*ZoneInfo, error) {
	if !zoneRe.MatchString(name) {
		return nil, fmt.Errorf("invalid zone name")
	}

	out, err := s.runCmd("--zone="+name, "--list-all")
	if err != nil {
		return nil, fmt.Errorf("get zone %s: %w", name, err)
	}

	return s.parseZoneOutput(name, out), nil
}

// AddPort adds a port rule permanently.
func (s *Service) AddPort(zone, port, protocol string) error {
	if err := s.validatePort(port); err != nil {
		return err
	}
	if err := s.validateProtocol(protocol); err != nil {
		return err
	}
	zone = s.resolveZone(zone)
	if err := s.validateZone(zone); err != nil {
		return err
	}

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--add-port="+port+"/"+protocol); err != nil {
		return fmt.Errorf("add port: %w", err)
	}
	return s.reload()
}

// RemovePort removes a port rule permanently.
func (s *Service) RemovePort(zone, port, protocol string) error {
	if err := s.validatePort(port); err != nil {
		return err
	}
	if err := s.validateProtocol(protocol); err != nil {
		return err
	}
	if err := s.checkPanelPort(port, protocol); err != nil {
		return err
	}
	zone = s.resolveZone(zone)
	if err := s.validateZone(zone); err != nil {
		return err
	}

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--remove-port="+port+"/"+protocol); err != nil {
		return fmt.Errorf("remove port: %w", err)
	}
	return s.reload()
}

// AddService adds a service rule permanently.
func (s *Service) AddService(zone, service string) error {
	if !serviceRe.MatchString(service) {
		return fmt.Errorf("invalid service name")
	}
	zone = s.resolveZone(zone)
	if err := s.validateZone(zone); err != nil {
		return err
	}

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--add-service="+service); err != nil {
		return fmt.Errorf("add service: %w", err)
	}
	return s.reload()
}

// RemoveService removes a service rule permanently.
func (s *Service) RemoveService(zone, service string) error {
	if !serviceRe.MatchString(service) {
		return fmt.Errorf("invalid service name")
	}
	if err := s.checkProtectedService(service); err != nil {
		return err
	}
	zone = s.resolveZone(zone)
	if err := s.validateZone(zone); err != nil {
		return err
	}

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--remove-service="+service); err != nil {
		return fmt.Errorf("remove service: %w", err)
	}
	return s.reload()
}

// AddRichRule adds a rich rule permanently.
func (s *Service) AddRichRule(zone, rule string) error {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return fmt.Errorf("rule cannot be empty")
	}
	if err := s.validateRichRule(rule); err != nil {
		return err
	}
	zone = s.resolveZone(zone)
	if err := s.validateZone(zone); err != nil {
		return err
	}

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--add-rich-rule="+rule); err != nil {
		return fmt.Errorf("add rich rule: %w", err)
	}
	return s.reload()
}

// RemoveRichRule removes a rich rule permanently.
func (s *Service) RemoveRichRule(zone, rule string) error {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return fmt.Errorf("rule cannot be empty")
	}
	zone = s.resolveZone(zone)
	if err := s.validateZone(zone); err != nil {
		return err
	}

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--remove-rich-rule="+rule); err != nil {
		return fmt.Errorf("remove rich rule: %w", err)
	}
	return s.reload()
}

// StartFirewalld starts the firewalld service and ensures essential ports are open.
func (s *Service) StartFirewalld() error {
	cmd := exec.Command("systemctl", "start", "firewalld")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start firewalld: %s: %s", err, strings.TrimSpace(string(out)))
	}

	// Ensure panel port and essential services are allowed to prevent lockout.
	if s.panelPort != "" {
		s.runCmd("--permanent", "--add-port="+s.panelPort+"/tcp")
	}
	for _, svc := range []string{"ssh", "http", "https"} {
		s.runCmd("--permanent", "--add-service="+svc)
	}
	s.runCmd("--reload")

	return nil
}

// AvailableServices returns all known firewalld service names.
func (s *Service) AvailableServices() ([]string, error) {
	out, err := s.runCmd("--get-services")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// Reload reloads firewalld configuration.
func (s *Service) Reload() error {
	return s.reload()
}

// ── internal helpers ──

func (s *Service) runCmd(args ...string) (string, error) {
	cmd := exec.Command("firewall-cmd", args...)
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		s.logger.Debug("firewall-cmd failed", "args", args, "output", result, "err", err)
		return result, fmt.Errorf("%s: %s", err, result)
	}
	return result, nil
}

func (s *Service) reload() error {
	if _, err := s.runCmd("--reload"); err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	return nil
}

func (s *Service) resolveZone(zone string) string {
	if zone == "" {
		if z, err := s.runCmd("--get-default-zone"); err == nil {
			return strings.TrimSpace(z)
		}
		return "public"
	}
	return zone
}

func (s *Service) validateZone(zone string) error {
	if !zoneRe.MatchString(zone) {
		return fmt.Errorf("invalid zone name: %s", zone)
	}
	return nil
}

func (s *Service) validatePort(port string) error {
	if !portRe.MatchString(port) {
		return fmt.Errorf("invalid port: %s (expected number or range like 8080-8090)", port)
	}
	// Validate numeric range 1-65535.
	parts := strings.SplitN(port, "-", 2)
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("port %s out of range (1-65535)", p)
		}
	}
	if len(parts) == 2 {
		lo, _ := strconv.Atoi(parts[0])
		hi, _ := strconv.Atoi(parts[1])
		if lo >= hi {
			return fmt.Errorf("invalid port range: start (%d) must be less than end (%d)", lo, hi)
		}
	}
	return nil
}

func (s *Service) validateProtocol(protocol string) error {
	if !protocolRe.MatchString(protocol) {
		return fmt.Errorf("invalid protocol: %s (expected tcp or udp)", protocol)
	}
	return nil
}

// validateRichRule ensures a rich rule starts with "rule" and contains no
// shell-dangerous characters. firewall-cmd itself validates syntax, but we
// reject obviously malicious input early.
func (s *Service) validateRichRule(rule string) error {
	if !strings.HasPrefix(rule, "rule ") {
		return fmt.Errorf("rich rule must start with 'rule '")
	}
	for _, ch := range rule {
		if ch == ';' || ch == '|' || ch == '&' || ch == '`' || ch == '$' || ch == '\n' || ch == '\r' {
			return fmt.Errorf("rich rule contains forbidden character: %q", ch)
		}
	}
	return nil
}

// checkPanelPort prevents removing the panel's own port (exact or range).
func (s *Service) checkPanelPort(port, protocol string) error {
	if s.panelPort == "" || protocol != "tcp" {
		return nil
	}
	if port == s.panelPort {
		return fmt.Errorf("cannot remove panel port %s/tcp — this would lock you out", port)
	}
	// Check if panelPort falls within a range like "39900-39999".
	if strings.Contains(port, "-") {
		parts := strings.SplitN(port, "-", 2)
		if len(parts) == 2 {
			lo, errLo := strconv.Atoi(parts[0])
			hi, errHi := strconv.Atoi(parts[1])
			pp, errPP := strconv.Atoi(s.panelPort)
			if errLo == nil && errHi == nil && errPP == nil && pp >= lo && pp <= hi {
				return fmt.Errorf("cannot remove port range %s/tcp — it contains panel port %s", port, s.panelPort)
			}
		}
	}
	return nil
}

// checkProtectedService prevents removing SSH (remote access lockout).
func (s *Service) checkProtectedService(service string) error {
	if service == "ssh" {
		return fmt.Errorf("cannot remove SSH service — this would lock you out of remote access")
	}
	return nil
}

// parseZoneOutput parses `firewall-cmd --zone=X --list-all` output.
func (s *Service) parseZoneOutput(name, output string) *ZoneInfo {
	z := &ZoneInfo{Name: name}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "target:") {
			z.Target = strings.TrimSpace(strings.TrimPrefix(line, "target:"))
		} else if strings.HasPrefix(line, "interfaces:") {
			z.Interfaces = splitNonEmpty(strings.TrimPrefix(line, "interfaces:"))
		} else if strings.HasPrefix(line, "sources:") {
			z.Sources = splitNonEmpty(strings.TrimPrefix(line, "sources:"))
		} else if strings.HasPrefix(line, "services:") {
			z.Services = splitNonEmpty(strings.TrimPrefix(line, "services:"))
		} else if strings.HasPrefix(line, "ports:") {
			z.Ports = splitNonEmpty(strings.TrimPrefix(line, "ports:"))
		} else if strings.HasPrefix(line, "rich rules:") {
			// Rich rules can span multiple lines; capture the rest.
			raw := strings.TrimSpace(strings.TrimPrefix(line, "rich rules:"))
			if raw != "" {
				z.RichRules = append(z.RichRules, raw)
			}
		} else if strings.HasPrefix(line, "rule ") {
			// Continuation of rich rules section.
			z.RichRules = append(z.RichRules, line)
		}
	}

	if z.Interfaces == nil {
		z.Interfaces = []string{}
	}
	if z.Sources == nil {
		z.Sources = []string{}
	}
	if z.Services == nil {
		z.Services = []string{}
	}
	if z.Ports == nil {
		z.Ports = []string{}
	}
	if z.RichRules == nil {
		z.RichRules = []string{}
	}

	return z
}

func splitNonEmpty(s string) []string {
	var result []string
	for _, f := range strings.Fields(s) {
		if f != "" {
			result = append(result, f)
		}
	}
	return result
}

// InstallFirewalld installs firewalld, enables and starts the service.
// Progress is streamed via writeSSE (log lines) and writeEvent (done/error signals).
func (s *Service) InstallFirewalld(writeSSE func(string), writeEvent func(string, string)) {
	// Check if already installed.
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		writeSSE("firewalld is already installed")
		writeEvent("done", "ok")
		return
	}

	osFamily := detectOSFamily()
	writeSSE("Detected OS family: " + osFamily)

	var installCmd string
	switch osFamily {
	case "rhel":
		installCmd = "dnf install -y firewalld"
	case "debian":
		installCmd = "apt-get update && apt-get install -y firewalld"
	default:
		writeSSE("ERROR: Unsupported OS family: " + osFamily)
		writeEvent("error", "Unsupported OS family")
		return
	}

	writeSSE("Installing firewalld...")
	if !s.streamCmd(installCmd, writeSSE) {
		writeEvent("error", "Installation failed")
		return
	}

	// Enable and start.
	writeSSE("Enabling and starting firewalld...")
	if !s.streamCmd("systemctl enable --now firewalld", writeSSE) {
		writeEvent("error", "Failed to start firewalld")
		return
	}

	// Whitelist essential ports to avoid lockout.
	writeSSE("Adding essential firewall rules...")

	// Panel port.
	if s.panelPort != "" {
		if _, err := s.runCmd("--permanent", "--add-port="+s.panelPort+"/tcp"); err != nil {
			writeSSE("WARNING: failed to add panel port " + s.panelPort + "/tcp: " + err.Error())
		} else {
			writeSSE("  ✓ Allowed port " + s.panelPort + "/tcp (panel)")
		}
	}

	// Essential services: SSH (remote access), HTTP/HTTPS (Caddy proxy).
	for _, svc := range []string{"ssh", "http", "https"} {
		if _, err := s.runCmd("--permanent", "--add-service="+svc); err != nil {
			writeSSE("WARNING: failed to add service " + svc + ": " + err.Error())
		} else {
			writeSSE("  ✓ Allowed service " + svc)
		}
	}

	// Reload to apply permanent rules.
	if _, err := s.runCmd("--reload"); err != nil {
		writeSSE("WARNING: firewalld reload failed: " + err.Error())
	}

	// Verify.
	if v, err := s.runCmd("--version"); err == nil {
		writeSSE("firewalld installed successfully: " + strings.TrimSpace(v))
		writeEvent("done", "ok")
	} else {
		writeSSE("ERROR: firewall-cmd not found after installation")
		writeEvent("error", "firewall-cmd not found after install")
	}
}

// streamCmd runs a shell command and streams stdout/stderr line by line.
// Returns true if the command exits successfully.
func (s *Service) streamCmd(shellCmd string, writeSSE func(string)) bool {
	cmd := exec.Command("bash", "-c", shellCmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeSSE("ERROR: " + err.Error())
		return false
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		writeSSE("ERROR: " + err.Error())
		return false
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			writeSSE(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		writeSSE("ERROR: command failed: " + err.Error())
		return false
	}
	return true
}

// detectOSFamily reads /etc/os-release to determine the OS family.
func detectOSFamily() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	content := strings.ToLower(string(data))

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id_like=") {
			val := strings.Trim(strings.TrimPrefix(line, "id_like="), "\"")
			if strings.Contains(val, "debian") || strings.Contains(val, "ubuntu") {
				return "debian"
			}
			if strings.Contains(val, "rhel") || strings.Contains(val, "fedora") || strings.Contains(val, "centos") {
				return "rhel"
			}
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id=") {
			val := strings.Trim(strings.TrimPrefix(line, "id="), "\"")
			switch val {
			case "debian", "ubuntu", "linuxmint", "pop", "kali", "deepin":
				return "debian"
			case "rhel", "centos", "fedora", "rocky", "almalinux", "ol", "amzn":
				return "rhel"
			}
		}
	}
	return "unknown"
}
