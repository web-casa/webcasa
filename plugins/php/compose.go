package php

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GenerateFPMCompose creates docker-compose.yml for a PHP-FPM runtime.
func GenerateFPMCompose(rt *PHPRuntime, timezone string) string {
	image := rt.CustomImage
	if image == "" {
		if v := FindVersion(rt.Version, RuntimeFPM); v != nil {
			image = v.Image
		} else {
			image = fmt.Sprintf("php:%s-fpm-alpine", rt.Version)
		}
	}

	buildSection := ""
	if rt.CustomImage != "" {
		buildSection = fmt.Sprintf(`    build:
      context: .
      dockerfile: Dockerfile
    image: %s
`, rt.CustomImage)
	} else {
		buildSection = fmt.Sprintf("    image: %s\n", image)
	}

	return fmt.Sprintf(`services:
  php-fpm:
%s    container_name: %s
    restart: unless-stopped
    ports:
      - "127.0.0.1:%d:9000"
    volumes:
      - /var/www:/var/www
      - ./conf.d/99-webcasa.ini:/usr/local/etc/php/conf.d/99-webcasa.ini:ro
      - ./php-fpm.d/zz-webcasa.conf:/usr/local/etc/php-fpm.d/zz-webcasa.conf:ro
    deploy:
      resources:
        limits:
          memory: %s
    environment:
      - TZ=%s
    labels:
      webcasa.plugin: php
      webcasa.runtime: "%s"
`, buildSection, rt.ContainerName, rt.Port, rt.MemoryLimit, timezone, rt.ContainerName)
}

// GenerateFrankenCompose creates docker-compose.yml for a FrankenPHP site container.
func GenerateFrankenCompose(site *PHPSite, timezone, customImage, memoryLimit string) string {
	if memoryLimit == "" {
		memoryLimit = "256m"
	}
	image := customImage
	if image == "" {
		if v := FindVersion(site.PHPVersion, RuntimeFranken); v != nil {
			image = v.Image
		} else {
			image = fmt.Sprintf("dunglas/frankenphp:latest-php%s-alpine", site.PHPVersion)
		}
	}

	buildSection := ""
	if customImage != "" {
		buildSection = fmt.Sprintf(`    build:
      context: .
      dockerfile: Dockerfile
    image: %s
`, customImage)
	} else {
		buildSection = fmt.Sprintf("    image: %s\n", image)
	}

	return fmt.Sprintf(`services:
  frankenphp:
%s    container_name: %s
    restart: unless-stopped
    ports:
      - "127.0.0.1:%d:80"
    volumes:
      - %s:/app
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - ./conf.d/99-webcasa.ini:/usr/local/etc/php/conf.d/99-webcasa.ini:ro
    deploy:
      resources:
        limits:
          memory: %s
    environment:
      - TZ=%s
      - SERVER_NAME=:80
    labels:
      webcasa.plugin: php
      webcasa.site: "%s"
`, buildSection, site.ContainerName, site.Port, site.RootPath, memoryLimit, timezone, site.Name)
}

// GenerateFrankenCaddyfile creates the internal Caddyfile for a FrankenPHP container.
func GenerateFrankenCaddyfile(site *PHPSite) string {
	if site.WorkerMode && site.WorkerScript != "" {
		return fmt.Sprintf(`:80 {
	root * /app
	php_server {
		worker /app/%s
	}
}
`, site.WorkerScript)
	}
	return `:80 {
	root * /app
	php_server
}
`
}

// GenerateFPMDockerfile creates a Dockerfile for a PHP-FPM runtime with extensions.
// Extensions must be validated before calling this function.
func GenerateFPMDockerfile(version string, extensions []string) (string, error) {
	if err := ValidateExtensionNames(extensions); err != nil {
		return "", fmt.Errorf("extension validation: %w", err)
	}
	baseImage := fmt.Sprintf("php:%s-fpm-alpine", version)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("FROM %s\n\n", baseImage))

	// Collect system dependencies.
	deps := collectDeps(extensions)
	if len(deps) > 0 {
		sb.WriteString(fmt.Sprintf("RUN apk add --no-cache %s\n\n", strings.Join(deps, " ")))
	}

	// Separate core and PECL extensions.
	var coreExts, peclExts []string
	for _, ext := range extensions {
		info := findExtension(ext)
		if info != nil && info.PECL {
			peclExts = append(peclExts, ext)
		} else {
			coreExts = append(coreExts, ext)
		}
	}

	// GD needs special configure step.
	hasGD := false
	var filteredCore []string
	for _, ext := range coreExts {
		if ext == "gd" {
			hasGD = true
		} else {
			filteredCore = append(filteredCore, ext)
		}
	}
	if hasGD {
		sb.WriteString("RUN docker-php-ext-configure gd --with-freetype --with-jpeg\n")
		filteredCore = append([]string{"gd"}, filteredCore...)
	}

	if len(filteredCore) > 0 {
		sb.WriteString(fmt.Sprintf("RUN docker-php-ext-install %s\n", strings.Join(filteredCore, " ")))
	}

	for _, ext := range peclExts {
		sb.WriteString(fmt.Sprintf("RUN pecl install %s && docker-php-ext-enable %s\n", ext, ext))
	}

	return sb.String(), nil
}

// GenerateFrankenDockerfile creates a Dockerfile for a FrankenPHP container with extensions.
// Extensions must be validated before calling this function.
func GenerateFrankenDockerfile(version string, extensions []string) (string, error) {
	if err := ValidateExtensionNames(extensions); err != nil {
		return "", fmt.Errorf("extension validation: %w", err)
	}
	baseImage := fmt.Sprintf("dunglas/frankenphp:latest-php%s-alpine", version)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("FROM %s\n\n", baseImage))

	if len(extensions) > 0 {
		// FrankenPHP images use install-php-extensions script.
		sb.WriteString(fmt.Sprintf("RUN install-php-extensions %s\n", strings.Join(extensions, " ")))
	}

	return sb.String(), nil
}

// sanitizeIniValue strips newlines and carriage returns from a php.ini value
// to prevent directive injection via structured config fields.
func sanitizeIniValue(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// GeneratePHPIni renders a php.ini configuration file from structured config.
// CustomDirectives are validated before being written.
func GeneratePHPIni(cfg PHPIniConfig) (string, error) {
	if err := ValidateCustomDirectives(cfg.CustomDirectives); err != nil {
		return "", fmt.Errorf("custom directives: %w", err)
	}
	// Sanitize all string fields to prevent newline injection.
	cfg.MemoryLimit = sanitizeIniValue(cfg.MemoryLimit)
	cfg.UploadMaxFilesize = sanitizeIniValue(cfg.UploadMaxFilesize)
	cfg.PostMaxSize = sanitizeIniValue(cfg.PostMaxSize)
	cfg.ErrorReporting = sanitizeIniValue(cfg.ErrorReporting)
	cfg.OpcacheMemory = sanitizeIniValue(cfg.OpcacheMemory)
	cfg.DateTimezone = sanitizeIniValue(cfg.DateTimezone)
	var lines []string
	lines = append(lines, "; Generated by WebCasa PHP Manager")
	lines = append(lines, "")

	lines = append(lines, "; Resource Limits")
	if cfg.MemoryLimit != "" {
		lines = append(lines, fmt.Sprintf("memory_limit = %s", cfg.MemoryLimit))
	}
	if cfg.MaxExecutionTime > 0 {
		lines = append(lines, fmt.Sprintf("max_execution_time = %d", cfg.MaxExecutionTime))
	}
	if cfg.MaxInputTime > 0 {
		lines = append(lines, fmt.Sprintf("max_input_time = %d", cfg.MaxInputTime))
	}
	if cfg.MaxInputVars > 0 {
		lines = append(lines, fmt.Sprintf("max_input_vars = %d", cfg.MaxInputVars))
	}

	lines = append(lines, "")
	lines = append(lines, "; Upload")
	if cfg.UploadMaxFilesize != "" {
		lines = append(lines, fmt.Sprintf("upload_max_filesize = %s", cfg.UploadMaxFilesize))
	}
	if cfg.PostMaxSize != "" {
		lines = append(lines, fmt.Sprintf("post_max_size = %s", cfg.PostMaxSize))
	}
	if cfg.MaxFileUploads > 0 {
		lines = append(lines, fmt.Sprintf("max_file_uploads = %d", cfg.MaxFileUploads))
	}

	lines = append(lines, "")
	lines = append(lines, "; Error Handling")
	if cfg.DisplayErrors {
		lines = append(lines, "display_errors = On")
	} else {
		lines = append(lines, "display_errors = Off")
	}
	if cfg.ErrorReporting != "" {
		lines = append(lines, fmt.Sprintf("error_reporting = %s", cfg.ErrorReporting))
	}
	if cfg.LogErrors {
		lines = append(lines, "log_errors = On")
	} else {
		lines = append(lines, "log_errors = Off")
	}

	lines = append(lines, "")
	lines = append(lines, "; Session")
	if cfg.SessionGcMaxlifetime > 0 {
		lines = append(lines, fmt.Sprintf("session.gc_maxlifetime = %d", cfg.SessionGcMaxlifetime))
	}

	lines = append(lines, "")
	lines = append(lines, "; OPcache")
	if cfg.OpcacheEnable {
		lines = append(lines, "opcache.enable = 1")
	} else {
		lines = append(lines, "opcache.enable = 0")
	}
	if cfg.OpcacheMemory != "" {
		lines = append(lines, fmt.Sprintf("opcache.memory_consumption = %s", cfg.OpcacheMemory))
	}
	if cfg.OpcacheMaxFiles > 0 {
		lines = append(lines, fmt.Sprintf("opcache.max_accelerated_files = %d", cfg.OpcacheMaxFiles))
	}
	if cfg.OpcacheRevalidate >= 0 {
		lines = append(lines, fmt.Sprintf("opcache.revalidate_freq = %d", cfg.OpcacheRevalidate))
	}

	if cfg.DateTimezone != "" {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("date.timezone = %s", cfg.DateTimezone))
	}

	if cfg.CustomDirectives != "" {
		lines = append(lines, "")
		lines = append(lines, "; Custom Directives")
		lines = append(lines, cfg.CustomDirectives)
	}

	return strings.Join(lines, "\n") + "\n", nil
}

// GenerateFPMPoolConf renders a PHP-FPM pool configuration file.
func GenerateFPMPoolConf(cfg FPMPoolConfig) (string, error) {
	if err := ValidatePMMode(cfg.PM); err != nil {
		return "", err
	}
	var lines []string
	lines = append(lines, "; Generated by WebCasa PHP Manager")
	lines = append(lines, "[www]")

	pm := cfg.PM
	if pm == "" {
		pm = "dynamic"
	}
	lines = append(lines, fmt.Sprintf("pm = %s", pm))
	if cfg.MaxChildren > 0 {
		lines = append(lines, fmt.Sprintf("pm.max_children = %d", cfg.MaxChildren))
	}

	switch pm {
	case "dynamic":
		if cfg.StartServers > 0 {
			lines = append(lines, fmt.Sprintf("pm.start_servers = %d", cfg.StartServers))
		}
		if cfg.MinSpareServers > 0 {
			lines = append(lines, fmt.Sprintf("pm.min_spare_servers = %d", cfg.MinSpareServers))
		}
		if cfg.MaxSpareServers > 0 {
			lines = append(lines, fmt.Sprintf("pm.max_spare_servers = %d", cfg.MaxSpareServers))
		}
	case "ondemand":
		if cfg.IdleTimeout > 0 {
			lines = append(lines, fmt.Sprintf("pm.process_idle_timeout = %ds", cfg.IdleTimeout))
		}
	}

	if cfg.MaxRequests > 0 {
		lines = append(lines, fmt.Sprintf("pm.max_requests = %d", cfg.MaxRequests))
	}

	return strings.Join(lines, "\n") + "\n", nil
}

// parseExtensions deserializes a JSON extension list.
func parseExtensions(extJSON string) []string {
	if extJSON == "" {
		return nil
	}
	var exts []string
	if err := json.Unmarshal([]byte(extJSON), &exts); err != nil {
		return nil
	}
	return exts
}

// collectDeps gathers Alpine system dependencies for the given extensions.
func collectDeps(extensions []string) []string {
	seen := make(map[string]bool)
	var deps []string
	for _, ext := range extensions {
		info := findExtension(ext)
		if info == nil {
			continue
		}
		for _, d := range info.Deps {
			if !seen[d] {
				seen[d] = true
				deps = append(deps, d)
			}
		}
	}
	return deps
}

// findExtension looks up extension info from CommonExtensions.
func findExtension(name string) *ExtensionInfo {
	for _, e := range CommonExtensions {
		if e.Name == name {
			return &e
		}
	}
	return nil
}
