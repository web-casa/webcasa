package appstore

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// RenderCompose replaces {{variable}} placeholders in a Compose template
// with user-supplied form values and built-in variables.
func RenderCompose(template string, formValues map[string]string, builtins map[string]string) string {
	result := template

	// Replace built-in variables first
	for k, v := range builtins {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}

	// Replace form values (env variable names)
	for k, v := range formValues {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}

	return result
}

// RenderEnvFile generates a .env file from form values.
func RenderEnvFile(formValues map[string]string, builtins map[string]string) string {
	var lines []string

	// Built-in variables first
	for k, v := range builtins {
		lines = append(lines, fmt.Sprintf("%s=%s", k, v))
	}

	// User-provided values
	for k, v := range formValues {
		lines = append(lines, fmt.Sprintf("%s=%s", k, v))
	}

	return strings.Join(lines, "\n") + "\n"
}

// GenerateRandomValue generates a crypto-random hex string of the given length.
func GenerateRandomValue(length int) string {
	if length <= 0 {
		length = 32
	}
	// Each byte = 2 hex chars, so we need length/2 bytes (round up)
	byteLen := (length + 1) / 2
	b := make([]byte, byteLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

// ValidateFormValues validates user input against form field definitions.
func ValidateFormValues(fields []FormField, values map[string]string) error {
	for _, f := range fields {
		v, exists := values[f.EnvVariable]

		// Skip random fields — they're auto-generated
		if f.Type == "random" {
			continue
		}

		if f.Required && (!exists || strings.TrimSpace(v) == "") {
			return fmt.Errorf("field %q (%s) is required", f.Label, f.EnvVariable)
		}

		if !exists || v == "" {
			continue
		}

		// Regex validation
		if f.Regex != "" {
			re, err := regexp.Compile(f.Regex)
			if err == nil && !re.MatchString(v) {
				msg := f.PatternError
				if msg == "" {
					msg = fmt.Sprintf("does not match pattern %s", f.Regex)
				}
				return fmt.Errorf("field %q: %s", f.Label, msg)
			}
		}

		// Length/numeric range
		if f.Min != nil && len(v) < *f.Min {
			return fmt.Errorf("field %q must be at least %d characters", f.Label, *f.Min)
		}
		if f.Max != nil && len(v) > *f.Max {
			return fmt.Errorf("field %q must be at most %d characters", f.Label, *f.Max)
		}
	}

	return nil
}

// FillRandomFields generates random values for "random" type fields
// and adds them to the values map.
func FillRandomFields(fields []FormField, values map[string]string) {
	for _, f := range fields {
		if f.Type != "random" {
			continue
		}
		// Don't overwrite if user provided a value
		if v, ok := values[f.EnvVariable]; ok && v != "" {
			continue
		}
		length := 32
		if f.Min != nil && *f.Min > 0 {
			length = *f.Min
		}
		values[f.EnvVariable] = GenerateRandomValue(length)
	}
}

// FillDefaults fills default values for fields that the user didn't provide.
func FillDefaults(fields []FormField, values map[string]string) {
	for _, f := range fields {
		if _, ok := values[f.EnvVariable]; ok {
			continue
		}
		if f.Default != nil {
			values[f.EnvVariable] = fmt.Sprintf("%v", f.Default)
		}
	}
}

// SanitizeCompose cleans up a Runtipi-format compose file for standalone use:
//   - Removes tipi_main_network references (services & top-level)
//   - Strips traefik.* and runtipi.* labels
func SanitizeCompose(compose string) string {
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(compose))
	skipTopNetwork := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip traefik and runtipi labels
		if strings.HasPrefix(trimmed, "traefik.") || strings.HasPrefix(trimmed, "runtipi.") {
			continue
		}

		// Skip "- tipi_main_network" service-level network reference
		if trimmed == "- tipi_main_network" {
			continue
		}

		// Skip top-level "tipi_main_network:" block and its children
		if skipTopNetwork {
			if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
				continue
			}
			skipTopNetwork = false
		}
		if trimmed == "tipi_main_network:" {
			skipTopNetwork = true
			continue
		}

		out = append(out, line)
	}

	// Clean up empty networks/labels sections
	result := strings.Join(out, "\n")
	result = cleanEmptySection(result, "networks:")
	result = cleanEmptySection(result, "labels:")
	return result
}

// cleanEmptySection removes a YAML section that has no content after it
// (only whitespace/empty lines before the next same-level or higher key).
func cleanEmptySection(content, sectionKey string) string {
	lines := strings.Split(content, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == sectionKey {
			// Check if next non-empty line is at same or lower indent (section is empty)
			indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
			j := i + 1
			empty := true
			for j < len(lines) {
				if strings.TrimSpace(lines[j]) == "" {
					j++
					continue
				}
				childIndent := len(lines[j]) - len(strings.TrimLeft(lines[j], " \t"))
				if childIndent > indent {
					empty = false
				}
				break
			}
			if empty {
				i++ // skip empty section header
				continue
			}
		}
		out = append(out, lines[i])
		i++
	}
	return strings.Join(out, "\n")
}

// getSystemTimezone reads the system timezone (e.g. "Asia/Shanghai").
func getSystemTimezone() string {
	// Try /etc/timezone first (Debian/Ubuntu)
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(data)); tz != "" {
			return tz
		}
	}
	// Try reading the symlink /etc/localtime (RHEL/CentOS)
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		// e.g. /usr/share/zoneinfo/Asia/Shanghai → Asia/Shanghai
		if idx := strings.Index(target, "zoneinfo/"); idx >= 0 {
			return target[idx+len("zoneinfo/"):]
		}
	}
	return "UTC"
}
