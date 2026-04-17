package monitoring

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestSendWebhook_SSRFBlocked is a regression test for the "HIGH pattern
// extension" audit: admin-configured AlertRule.NotifyURL previously went
// straight to http.Client.Post with no SSRF check. Matches the fix
// delivered to internal/notify/notifier.go (Phase 4 v2.0) + plugins/deploy
// git-poll target validation (v0.11 Phase 4 codex fix).
func TestSendWebhook_SSRFBlocked(t *testing.T) {
	a := &Alerter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	history := &AlertHistory{
		RuleName:  "test",
		Metric:    "cpu",
		Value:     99,
		Threshold: 90,
		Message:   "cpu high",
	}

	cases := []string{
		"http://127.0.0.1/alert",
		"http://localhost/alert",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/alert",
		"ftp://example.com/alert", // non-http scheme
	}
	for _, url := range cases {
		err := a.sendWebhook(url, history)
		if err == nil {
			t.Errorf("URL %q should have been blocked by SSRF guard, got nil error", url)
			continue
		}
		if !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "unsupported") && !strings.Contains(err.Error(), "scheme") {
			t.Errorf("URL %q: expected SSRF-related error, got %v", url, err)
		}
	}
}

// TestSendWebhook_PrivateRFC1918Allowed verifies the fix permits self-hosted
// internal webhooks (e.g. a private Gitea / Alertmanager instance on a LAN).
// Mirrors the policy in internal/notify/ssrf.go. We exercise the URL check
// only — the actual outbound request is permitted to fail since we're not
// running a server at 10.0.0.5 in tests.
func TestSendWebhook_PrivateRFC1918Allowed(t *testing.T) {
	a := &Alerter{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	history := &AlertHistory{RuleName: "test", Metric: "cpu", Value: 1, Threshold: 0}

	err := a.sendWebhook("http://10.0.0.5/alert", history)
	if err == nil {
		// Unlikely: means an actual server responded. Test still passes.
		return
	}
	if strings.Contains(err.Error(), "blocked") {
		t.Errorf("RFC1918 private IP 10.0.0.5 must not be blocked; got: %v", err)
	}
	// Any other error (connection refused, timeout) is expected and fine.
}
