package php

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ── Extension name validation ──

// validExtensionNameRe allows alphanumeric names with underscores, matching PHP extension naming.
var validExtensionNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// ValidateExtensionName checks if a single extension name is safe for Dockerfile interpolation.
func ValidateExtensionName(name string) error {
	if !validExtensionNameRe.MatchString(name) {
		return fmt.Errorf("invalid extension name: %q (must be alphanumeric with underscores)", name)
	}
	return nil
}

// ValidateExtensionNames validates a list of extension names.
func ValidateExtensionNames(names []string) error {
	for _, name := range names {
		if err := ValidateExtensionName(name); err != nil {
			return err
		}
	}
	return nil
}

// ── Path validation ──

// ValidateRootPath ensures the path is under /var/www/ and has no traversal.
func ValidateRootPath(path string) error {
	if path == "" {
		return fmt.Errorf("root path cannot be empty")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("root path cannot contain '..'")
	}
	cleaned := filepath.Clean(path)
	if !strings.HasPrefix(cleaned, "/var/www/") {
		return fmt.Errorf("root path must be under /var/www/")
	}
	// Reject Caddyfile-breaking characters.
	if strings.ContainsAny(cleaned, " \t\n\r{}\"'`;#$\\") {
		return fmt.Errorf("root path contains invalid characters")
	}
	return nil
}

// ValidateWorkerScript ensures the worker script is a relative .php path.
var validWorkerScriptRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,255}\.php$`)

func ValidateWorkerScript(script string) error {
	if script == "" {
		return nil // optional
	}
	if strings.Contains(script, "..") {
		return fmt.Errorf("worker script cannot contain '..'")
	}
	if strings.HasPrefix(script, "/") {
		return fmt.Errorf("worker script must be a relative path")
	}
	if !validWorkerScriptRe.MatchString(script) {
		return fmt.Errorf("invalid worker script: %q", script)
	}
	return nil
}

// ── FastCGI validation ──

// validFastCGIRe matches host:port format.
var validFastCGIRe = regexp.MustCompile(`^[a-zA-Z0-9.-]+:\d{1,5}$`)

// ValidatePHPFastCGI checks that the address is in host:port format.
func ValidatePHPFastCGI(addr string) error {
	if addr == "" {
		return fmt.Errorf("PHP FastCGI address cannot be empty")
	}
	if !validFastCGIRe.MatchString(addr) {
		return fmt.Errorf("invalid PHP FastCGI address: %q (expected host:port)", addr)
	}
	return nil
}

// ── FPM PM mode validation ──

// ValidatePMMode checks the process manager mode.
func ValidatePMMode(pm string) error {
	switch pm {
	case "dynamic", "static", "ondemand", "":
		return nil
	default:
		return fmt.Errorf("invalid PM mode: %q (must be dynamic, static, or ondemand)", pm)
	}
}

// ── php.ini custom directives validation ──

// dangerousPHPDirectives are php.ini directives blocked for security.
var dangerousPHPDirectives = []string{
	"auto_prepend_file",
	"auto_append_file",
	"extension",
	"zend_extension",
	"sendmail_path",
}

// ValidateCustomDirectives checks php.ini custom directives for dangerous entries.
func ValidateCustomDirectives(directives string) error {
	if directives == "" {
		return nil
	}
	for i, line := range strings.Split(directives, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, d := range dangerousPHPDirectives {
			if strings.HasPrefix(lower, d+"=") || strings.HasPrefix(lower, d+" ") || lower == d {
				return fmt.Errorf("line %d: directive %q is blocked for security reasons", i+1, d)
			}
		}
	}
	return nil
}

// ── Memory limit validation ──

// validMemoryLimitRe matches Docker memory format like "256m", "1g", "512M".
var validMemoryLimitRe = regexp.MustCompile(`^[1-9]\d{0,5}[mMgG]$`)

// ValidateMemoryLimit checks the Docker memory limit format.
func ValidateMemoryLimit(limit string) error {
	if limit == "" {
		return nil
	}
	if !validMemoryLimitRe.MatchString(limit) {
		return fmt.Errorf("invalid memory limit: %q (expected format like 256m, 1g)", limit)
	}
	return nil
}
