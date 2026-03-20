package caddy

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// domainRegex matches valid domain names (with optional wildcard prefix and port).
var domainRegex = regexp.MustCompile(`^(\*\.)?[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*(\:\d{1,5})?$`)

// ValidateDomain checks if a domain name is safe for Caddyfile injection.
func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	if len(domain) > 253 {
		return fmt.Errorf("domain too long (max 253 chars)")
	}
	// Reject characters that could break Caddyfile syntax
	if strings.ContainsAny(domain, " \t\n\r{}\"'`;#$\\") {
		return fmt.Errorf("domain contains invalid characters")
	}
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format: %s", domain)
	}
	return nil
}

// ValidateUpstream checks if an upstream address is safe for Caddyfile injection.
func ValidateUpstream(addr string) error {
	if addr == "" {
		return fmt.Errorf("upstream address cannot be empty")
	}
	// Reject Caddyfile-breaking characters
	if strings.ContainsAny(addr, " \t\n\r{}\"'`;#$\\") {
		return fmt.Errorf("upstream address contains invalid characters")
	}

	// Allow http:// or https:// prefixed URLs
	clean := addr
	if strings.HasPrefix(clean, "http://") {
		clean = strings.TrimPrefix(clean, "http://")
	} else if strings.HasPrefix(clean, "https://") {
		clean = strings.TrimPrefix(clean, "https://")
	}

	// Should be host:port or just host
	host, port, err := net.SplitHostPort(clean)
	if err != nil {
		// Might be host without port
		host = clean
		port = ""
	}

	// Validate host part
	if host == "" {
		return fmt.Errorf("upstream host cannot be empty")
	}
	if len(host) > 253 {
		return fmt.Errorf("upstream host too long")
	}

	// Validate port if present
	if port != "" {
		if len(port) > 5 {
			return fmt.Errorf("invalid port number")
		}
	}

	return nil
}

// ValidateIPRange checks if an IP range is safe for Caddyfile injection.
func ValidateIPRange(ipRange string) error {
	if ipRange == "" {
		return fmt.Errorf("IP range cannot be empty")
	}
	// Reject Caddyfile-breaking characters
	if strings.ContainsAny(ipRange, " \t\n\r{}\"'`;#$\\") {
		return fmt.Errorf("IP range contains invalid characters")
	}

	// Try parsing as CIDR
	if strings.Contains(ipRange, "/") {
		_, _, err := net.ParseCIDR(ipRange)
		if err != nil {
			return fmt.Errorf("invalid CIDR notation: %s", ipRange)
		}
		return nil
	}

	// Try parsing as plain IP
	if net.ParseIP(ipRange) == nil {
		return fmt.Errorf("invalid IP address: %s", ipRange)
	}
	return nil
}

// ValidateCaddyValue checks that a string is safe for embedding in a Caddyfile.
// It rejects newlines, braces, quotes, and backslashes that could alter structure
// or break quoted directives (e.g. header values rendered as "...").
func ValidateCaddyValue(label, value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, "\n\r{}\"\\") {
		return fmt.Errorf("%s contains characters that could break Caddyfile syntax", label)
	}
	return nil
}

// SanitizeCustomDirectives validates custom directives to prevent Caddyfile injection.
// It rejects lines that could close/open blocks unexpectedly.
func SanitizeCustomDirectives(directives string) error {
	if directives == "" {
		return nil
	}

	lines := strings.Split(directives, "\n")
	braceDepth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Count braces to detect block manipulation.
		// Check after EACH closing brace so that "} {" on the same line
		// (which nets to zero) is still caught.
		for _, ch := range trimmed {
			switch ch {
			case '{':
				braceDepth++
			case '}':
				braceDepth--
				if braceDepth < 0 {
					return fmt.Errorf("line %d: unbalanced closing brace — cannot close parent block", i+1)
				}
			}
		}
	}

	// All opened braces must be closed
	if braceDepth != 0 {
		return fmt.Errorf("unbalanced braces in custom directives (depth: %d)", braceDepth)
	}

	return nil
}
