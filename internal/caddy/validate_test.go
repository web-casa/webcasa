package caddy

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ValidateDomain
// ---------------------------------------------------------------------------

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		// --- Valid domains ---
		{name: "simple domain", domain: "example.com", wantErr: false},
		{name: "subdomain", domain: "sub.example.com", wantErr: false},
		{name: "wildcard domain", domain: "*.example.com", wantErr: false},
		{name: "domain with port", domain: "example.com:8080", wantErr: false},
		{name: "deep subdomain", domain: "a.b.c.d.example.com", wantErr: false},
		{name: "wildcard with port", domain: "*.example.com:443", wantErr: false},
		{name: "hyphenated domain", domain: "my-site.example.com", wantErr: false},
		{name: "numeric subdomain", domain: "123.example.com", wantErr: false},
		{name: "single label with port", domain: "localhost:3000", wantErr: false},
		{name: "single char label", domain: "a", wantErr: false}, // regex allows single-char labels

		// --- Invalid domains ---
		{name: "empty string", domain: "", wantErr: true},
		{name: "too long (>253 chars)", domain: strings.Repeat("a", 254), wantErr: true},
		{name: "contains space", domain: "example .com", wantErr: true},
		{name: "contains tab", domain: "example\t.com", wantErr: true},
		{name: "contains newline", domain: "example.com\nmalicious", wantErr: true},
		{name: "contains carriage return", domain: "example.com\rmalicious", wantErr: true},
		{name: "caddyfile placeholder injection", domain: "{inject}", wantErr: true},
		{name: "caddyfile block injection open", domain: "example.com{", wantErr: true},
		{name: "caddyfile block injection close", domain: "example.com}", wantErr: true},
		{name: "import injection with semicolon", domain: `"; import malicious`, wantErr: true},
		{name: "double quote", domain: `"example.com"`, wantErr: true},
		{name: "single quote", domain: "'example.com'", wantErr: true},
		{name: "backtick", domain: "`example.com`", wantErr: true},
		{name: "semicolon", domain: "example.com;", wantErr: true},
		{name: "hash comment", domain: "example.com#comment", wantErr: true},
		{name: "dollar sign", domain: "$example.com", wantErr: true},
		{name: "backslash", domain: `example\.com`, wantErr: true},
		{name: "trailing dot", domain: "example.com.", wantErr: true},
		{name: "leading dot", domain: ".example.com", wantErr: true},
		{name: "double dot", domain: "example..com", wantErr: true},
		{name: "underscore", domain: "ex_ample.com", wantErr: true},
		{name: "label starts with hyphen", domain: "-example.com", wantErr: true},
		{name: "label ends with hyphen", domain: "example-.com", wantErr: true},
		{name: "just a wildcard", domain: "*", wantErr: true},
		{name: "wildcard mid-label", domain: "sub.*.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDomain(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDomain(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateUpstream
// ---------------------------------------------------------------------------

func TestValidateUpstream(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		// --- Valid upstreams ---
		{name: "localhost with port", addr: "localhost:3000", wantErr: false},
		{name: "IPv4 with port", addr: "192.168.1.1:8080", wantErr: false},
		{name: "http prefix", addr: "http://backend:3000", wantErr: false},
		{name: "https prefix", addr: "https://api.example.com", wantErr: false},
		{name: "IPv6 loopback with port", addr: "[::1]:8080", wantErr: false},
		{name: "http with IPv4 and port", addr: "http://127.0.0.1:9000", wantErr: false},
		{name: "https with port", addr: "https://backend:443", wantErr: false},
		{name: "plain hostname", addr: "backend", wantErr: false},
		{name: "hostname with port", addr: "my-service:5000", wantErr: false},
		{name: "http with IP no port", addr: "http://10.0.0.1", wantErr: false},

		// --- Invalid upstreams ---
		{name: "empty string", addr: "", wantErr: true},
		{name: "contains space", addr: "localhost :3000", wantErr: true},
		{name: "contains tab", addr: "localhost\t:3000", wantErr: true},
		{name: "command injection semicolon", addr: "; rm -rf /", wantErr: true},
		{name: "caddyfile placeholder", addr: "{evil}", wantErr: true},
		{name: "newline injection", addr: "upstream\ninjection", wantErr: true},
		{name: "carriage return injection", addr: "upstream\rinjection", wantErr: true},
		{name: "double quote injection", addr: `"localhost:3000"`, wantErr: true},
		{name: "single quote injection", addr: "'localhost:3000'", wantErr: true},
		{name: "backtick injection", addr: "`localhost:3000`", wantErr: true},
		{name: "hash comment injection", addr: "localhost:3000#comment", wantErr: true},
		{name: "dollar sign injection", addr: "$backend:3000", wantErr: true},
		{name: "backslash injection", addr: `localhost\:3000`, wantErr: true},
		{name: "block open brace", addr: "localhost{", wantErr: true},
		{name: "block close brace", addr: "localhost}", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUpstream(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUpstream(%q) error = %v, wantErr %v", tt.addr, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateIPRange
// ---------------------------------------------------------------------------

func TestValidateIPRange(t *testing.T) {
	tests := []struct {
		name    string
		ipRange string
		wantErr bool
	}{
		// --- Valid IP ranges ---
		{name: "CIDR /24", ipRange: "192.168.1.0/24", wantErr: false},
		{name: "plain IPv4", ipRange: "10.0.0.1", wantErr: false},
		{name: "IPv6 loopback", ipRange: "::1", wantErr: false},
		{name: "IPv6 CIDR", ipRange: "2001:db8::/32", wantErr: false},
		{name: "catch-all CIDR", ipRange: "0.0.0.0/0", wantErr: false},
		{name: "IPv4 /32 host", ipRange: "10.0.0.1/32", wantErr: false},
		{name: "link-local IPv4", ipRange: "169.254.0.0/16", wantErr: false},
		{name: "full IPv6", ipRange: "fe80::1", wantErr: false},
		{name: "IPv6 catch-all", ipRange: "::/0", wantErr: false},

		// --- Invalid IP ranges ---
		{name: "empty string", ipRange: "", wantErr: true},
		{name: "contains space", ipRange: "192.168.1.0 /24", wantErr: true},
		{name: "not an IP", ipRange: "not-an-ip", wantErr: true},
		{name: "CIDR prefix too large /33", ipRange: "192.168.1.0/33", wantErr: true},
		{name: "semicolon injection", ipRange: "; malicious", wantErr: true},
		{name: "caddyfile placeholder", ipRange: "{evil}", wantErr: true},
		{name: "contains tab", ipRange: "10.0.0.1\t", wantErr: true},
		{name: "contains newline", ipRange: "10.0.0.1\n", wantErr: true},
		{name: "contains carriage return", ipRange: "10.0.0.1\r", wantErr: true},
		{name: "double quote", ipRange: `"10.0.0.1"`, wantErr: true},
		{name: "single quote", ipRange: "'10.0.0.1'", wantErr: true},
		{name: "backtick", ipRange: "`10.0.0.1`", wantErr: true},
		{name: "hash comment", ipRange: "10.0.0.1#comment", wantErr: true},
		{name: "dollar sign", ipRange: "$10.0.0.1", wantErr: true},
		{name: "backslash", ipRange: `10.0.0.1\`, wantErr: true},
		{name: "word slash word", ipRange: "abc/24", wantErr: true},
		{name: "negative prefix", ipRange: "10.0.0.0/-1", wantErr: true},
		{name: "IPv6 prefix too large /129", ipRange: "::1/129", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIPRange(tt.ipRange)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIPRange(%q) error = %v, wantErr %v", tt.ipRange, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SanitizeCustomDirectives
// ---------------------------------------------------------------------------

func TestSanitizeCustomDirectives(t *testing.T) {
	tests := []struct {
		name       string
		directives string
		wantErr    bool
	}{
		// --- Valid directives ---
		{name: "empty string", directives: "", wantErr: false},
		{name: "simple header directive", directives: `header X-Custom "value"`, wantErr: false},
		{name: "multi-line directives", directives: "header X-A value-a\nheader X-B value-b", wantErr: false},
		{name: "balanced braces single block", directives: "route {\n    respond 200\n}", wantErr: false},
		{name: "nested balanced braces", directives: "route {\n    handle {\n        respond 200\n    }\n}", wantErr: false},
		{name: "comment lines", directives: "# this is a comment\nheader X-A value", wantErr: false},
		{name: "empty lines mixed", directives: "\n\nheader X-A value\n\n", wantErr: false},
		{name: "only comments and blanks", directives: "# comment 1\n\n# comment 2", wantErr: false},
		{name: "multiple balanced blocks", directives: "route {\n    respond 200\n}\nhandle {\n    respond 404\n}", wantErr: false},
		{name: "brace on same line", directives: "route { respond 200 }", wantErr: false},

		// --- Invalid directives ---
		{name: "unbalanced closing brace escapes parent", directives: "}", wantErr: true},
		{name: "unbalanced opening brace", directives: "route {", wantErr: true},
		{name: "close parent then inject", directives: "}\nmalicious\n{", wantErr: true},
		{name: "extra closing brace after block", directives: "route {\n    respond 200\n}\n}", wantErr: true},
		{name: "deep nesting with unmatched open", directives: "a {\n  b {\n    c {\n  }\n}", wantErr: true},
		{name: "close then open on same line", directives: "} {", wantErr: true},
		{name: "close then open on separate lines", directives: "}\n{", wantErr: true},
		{name: "multiple unbalanced opens", directives: "a {\nb {", wantErr: true},
		{name: "single close brace in middle", directives: "header X-A value\n}\nheader X-B value", wantErr: true},
		{name: "triple close brace", directives: "a {\n}\n}\n}", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SanitizeCustomDirectives(tt.directives)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizeCustomDirectives(%q) error = %v, wantErr %v", tt.directives, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Edge-case: Boundary length for ValidateDomain
// ---------------------------------------------------------------------------

func TestValidateDomain_BoundaryLength(t *testing.T) {
	// Exactly 253 chars: should be valid if domain format is correct.
	// Build a domain with multiple labels that totals exactly 253 characters.
	// label63 (63) + "." (1) + label61 (61) + "." (1) + label61 (61) + "." (1) +
	// label61 (61) + "." (1) + "com" (3) = 63 + 1 + 61 + 1 + 61 + 1 + 61 + 1 + 3 = 253
	label63 := strings.Repeat("a", 63)
	label61 := strings.Repeat("b", 61)
	domain253 := label63 + "." + label61 + "." + label61 + "." + label61 + ".com"

	if len(domain253) != 253 {
		t.Fatalf("test setup: expected 253 chars, got %d", len(domain253))
	}

	if err := ValidateDomain(domain253); err != nil {
		t.Errorf("ValidateDomain with exactly 253 chars should succeed, got error: %v", err)
	}

	// 254 chars: should fail
	domain254 := "x" + domain253
	if len(domain254) != 254 {
		t.Fatalf("test setup: expected 254 chars, got %d", len(domain254))
	}

	if err := ValidateDomain(domain254); err == nil {
		t.Errorf("ValidateDomain with 254 chars should fail, but got nil")
	}
}

// ---------------------------------------------------------------------------
// Edge-case: ValidateUpstream with empty host after scheme strip
// ---------------------------------------------------------------------------

func TestValidateUpstream_EmptyHostAfterScheme(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{name: "http:// only", addr: "http://"},
		{name: "https:// only", addr: "https://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUpstream(tt.addr)
			if err == nil {
				t.Errorf("ValidateUpstream(%q) should return error for empty host after scheme strip", tt.addr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Edge-case: ValidateUpstream host length limit
// ---------------------------------------------------------------------------

func TestValidateUpstream_HostTooLong(t *testing.T) {
	longHost := strings.Repeat("a", 254) + ":8080"
	err := ValidateUpstream(longHost)
	if err == nil {
		t.Errorf("ValidateUpstream with host >253 chars should fail, but got nil")
	}
}

// ---------------------------------------------------------------------------
// Edge-case: SanitizeCustomDirectives with only whitespace lines
// ---------------------------------------------------------------------------

func TestSanitizeCustomDirectives_WhitespaceOnly(t *testing.T) {
	err := SanitizeCustomDirectives("   \n\t\n   ")
	if err != nil {
		t.Errorf("SanitizeCustomDirectives with whitespace-only lines should succeed, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Edge-case: SanitizeCustomDirectives deeply nested balanced
// ---------------------------------------------------------------------------

func TestSanitizeCustomDirectives_DeepNested(t *testing.T) {
	// 10 levels of nesting, all balanced
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString(strings.Repeat("  ", i))
		b.WriteString("block {\n")
	}
	for i := 9; i >= 0; i-- {
		b.WriteString(strings.Repeat("  ", i))
		b.WriteString("}\n")
	}

	err := SanitizeCustomDirectives(b.String())
	if err != nil {
		t.Errorf("SanitizeCustomDirectives with 10-level balanced nesting should succeed, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Edge-case: ValidateIPRange boundary CIDR prefixes
// ---------------------------------------------------------------------------

func TestValidateIPRange_BoundaryCIDR(t *testing.T) {
	tests := []struct {
		name    string
		ipRange string
		wantErr bool
	}{
		{name: "IPv4 /0 minimum", ipRange: "0.0.0.0/0", wantErr: false},
		{name: "IPv4 /32 maximum", ipRange: "10.0.0.1/32", wantErr: false},
		{name: "IPv4 /33 over maximum", ipRange: "10.0.0.1/33", wantErr: true},
		{name: "IPv6 /0 minimum", ipRange: "::/0", wantErr: false},
		{name: "IPv6 /128 maximum", ipRange: "::1/128", wantErr: false},
		{name: "IPv6 /129 over maximum", ipRange: "::1/129", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIPRange(tt.ipRange)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIPRange(%q) error = %v, wantErr %v", tt.ipRange, err, tt.wantErr)
			}
		})
	}
}
