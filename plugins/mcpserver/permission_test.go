package mcpserver

import (
	"context"
	"testing"
)

func TestCheckPermission(t *testing.T) {
	cases := []struct {
		name  string
		perms string
		scope string
		allow bool
	}{
		// Ordinary scopes: wildcard and exact grants work; empty denies.
		{"empty denies", "", "hosts:read", false},
		{"empty array denies", "[]", "hosts:read", false},
		{"wildcard grants ordinary read", `["*"]`, "hosts:read", true},
		{"wildcard grants ordinary write", `["*"]`, "hosts:write", true},
		{"exact grant", `["hosts:write"]`, "hosts:write", true},
		{"category wildcard grants", `["hosts:*"]`, "hosts:write", true},
		{"unrelated scope denied", `["hosts:read"]`, "deploy:write", false},
		{"read scope can't write", `["hosts:read"]`, "hosts:write", false},

		// Dangerous scopes: a bare "*" must NOT grant them.
		{"wildcard does NOT grant system:write", `["*"]`, "system:write", false},
		{"wildcard does NOT grant files:write", `["*"]`, "files:write", false},
		{"wildcard does NOT grant docker:write", `["*"]`, "docker:write", false},
		{"wildcard does NOT grant cronjob:write", `["*"]`, "cronjob:write", false},

		// Dangerous scopes require an explicit grant.
		{"explicit system:write granted", `["system:write"]`, "system:write", true},
		{"explicit category wildcard granted", `["system:*"]`, "system:write", true},
		{"explicit files:write granted", `["files:write"]`, "files:write", true},
		{"mixed wildcard + explicit danger", `["*","system:write"]`, "system:write", true},
		// system:read is not in the dangerous set, so "*" still grants it.
		{"wildcard grants system:read", `["*"]`, "system:read", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ContextWithPermissions(context.Background(), tc.perms)
			err := checkPermission(ctx, tc.scope)
			if tc.allow && err != nil {
				t.Fatalf("expected allow, got error: %v", err)
			}
			if !tc.allow && err == nil {
				t.Fatalf("expected deny, got allow")
			}
		})
	}
}
