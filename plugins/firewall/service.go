package firewall

import (
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
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

	out, err := s.runCmd("--zone=" + name + " --list-all")
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
	zone = s.resolveZone(zone)

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--remove-service="+service); err != nil {
		return fmt.Errorf("remove service: %w", err)
	}
	return s.reload()
}

// AddRichRule adds a rich rule permanently.
func (s *Service) AddRichRule(zone, rule string) error {
	if strings.TrimSpace(rule) == "" {
		return fmt.Errorf("rule cannot be empty")
	}
	zone = s.resolveZone(zone)

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--add-rich-rule="+rule); err != nil {
		return fmt.Errorf("add rich rule: %w", err)
	}
	return s.reload()
}

// RemoveRichRule removes a rich rule permanently.
func (s *Service) RemoveRichRule(zone, rule string) error {
	if strings.TrimSpace(rule) == "" {
		return fmt.Errorf("rule cannot be empty")
	}
	zone = s.resolveZone(zone)

	if _, err := s.runCmd("--permanent", "--zone="+zone, "--remove-rich-rule="+rule); err != nil {
		return fmt.Errorf("remove rich rule: %w", err)
	}
	return s.reload()
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

func (s *Service) validatePort(port string) error {
	if !portRe.MatchString(port) {
		return fmt.Errorf("invalid port: %s (expected number or range like 8080-8090)", port)
	}
	return nil
}

func (s *Service) validateProtocol(protocol string) error {
	if !protocolRe.MatchString(protocol) {
		return fmt.Errorf("invalid protocol: %s (expected tcp or udp)", protocol)
	}
	return nil
}

// checkPanelPort prevents removing the panel's own port.
func (s *Service) checkPanelPort(port, protocol string) error {
	if s.panelPort != "" && port == s.panelPort && protocol == "tcp" {
		return fmt.Errorf("cannot remove panel port %s/tcp — this would lock you out", port)
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
