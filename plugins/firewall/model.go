package firewall

// FirewallStatus represents the overall firewalld status.
type FirewallStatus struct {
	Installed   bool     `json:"installed"`
	Running     bool     `json:"running"`
	DefaultZone string   `json:"default_zone"`
	Version     string   `json:"version"`
	Zones       []string `json:"zones"`
}

// ZoneInfo represents a firewalld zone with its rules.
type ZoneInfo struct {
	Name       string   `json:"name"`
	Target     string   `json:"target"`
	Interfaces []string `json:"interfaces"`
	Sources    []string `json:"sources"`
	Services   []string `json:"services"`
	Ports      []string `json:"ports"`
	RichRules  []string `json:"rich_rules"`
	Active     bool     `json:"active"`
}

// AddPortRequest is the input for adding a port rule.
type AddPortRequest struct {
	Zone     string `json:"zone"`
	Port     string `json:"port" binding:"required"`
	Protocol string `json:"protocol" binding:"required"`
}

// AddServiceRequest is the input for adding a service rule.
type AddServiceRequest struct {
	Zone    string `json:"zone"`
	Service string `json:"service" binding:"required"`
}

// AddRichRuleRequest is the input for adding a rich rule.
type AddRichRuleRequest struct {
	Zone string `json:"zone"`
	Rule string `json:"rule" binding:"required"`
}
