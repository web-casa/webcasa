package notify

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// dangerousHosts are hostnames that should never be used as webhook targets.
var dangerousHosts = []string{
	"localhost",
	"127.0.0.1",
	"0.0.0.0",
	"::1",
	"[::1]",
	"metadata.google.internal",
	"metadata.internal",
}

// ValidateWebhookURL checks a URL for SSRF vulnerabilities.
// Blocks: non-HTTP schemes, loopback IPs, link-local/metadata IPs.
// Allows: private network IPs (10.x, 172.16-31.x, 192.168.x) for self-hosted setups.
func ValidateWebhookURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL is empty")
	}

	// Layer 1: Parse URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Layer 2: Scheme whitelist
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q: only http and https are allowed", u.Scheme)
	}

	// Layer 3: Dangerous hostnames
	hostname := strings.ToLower(u.Hostname())
	for _, blocked := range dangerousHosts {
		if hostname == blocked {
			return fmt.Errorf("blocked hostname: %s", hostname)
		}
	}
	if strings.HasSuffix(hostname, ".internal") {
		return fmt.Errorf("blocked hostname: %s (*.internal)", hostname)
	}

	// Layer 4: IP address blacklist (check literal IP)
	ip := net.ParseIP(hostname)
	if ip != nil {
		if err := checkBlockedIP(ip); err != nil {
			return err
		}
	}

	// Layer 5: Resolve hostname and check resolved IPs (prevents DNS rebinding)
	if ip == nil {
		resolved, err := net.LookupHost(hostname)
		if err == nil {
			for _, addr := range resolved {
				rip := net.ParseIP(addr)
				if rip != nil {
					if err := checkBlockedIP(rip); err != nil {
						return fmt.Errorf("blocked: %s resolves to %s (%w)", hostname, addr, err)
					}
				}
			}
		}
	}

	return nil
}

// SafeDialContext is a custom dialer that validates resolved IPs at connection time,
// preventing DNS rebinding attacks (TOCTOU between ValidateWebhookURL and actual request).
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// Resolve and validate each IP.
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip != nil {
			if err := checkBlockedIP(ip); err != nil {
				return nil, fmt.Errorf("SSRF blocked at dial: %s resolves to %s: %w", host, ipStr, err)
			}
		}
	}

	// Dial using the first resolved IP.
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
}

// checkBlockedIP returns an error if the IP is in a blocked range.
func checkBlockedIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("blocked IP: %s (loopback)", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("blocked IP: %s (link-local)", ip)
	}
	// Block 169.254.0.0/16 (AWS/GCP metadata endpoint)
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 169 && ip4[1] == 254 {
		return fmt.Errorf("blocked IP: %s (metadata endpoint)", ip)
	}
	return nil
}
