package appstore

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
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
			if err != nil {
				return fmt.Errorf("field %q: invalid validation pattern", f.Label)
			}
			if !re.MatchString(v) {
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
// and adds them to the values map. Supports "encoding" field: "base64" for base64 output.
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
		if f.Encoding == "base64" {
			values[f.EnvVariable] = GenerateRandomBase64Value(length)
		} else {
			values[f.EnvVariable] = GenerateRandomValue(length)
		}
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

// relabelHostBindMount appends `:Z` to host-path bind mount specs so the
// container's container_t SELinux domain can read/write them on EL9/EL10
// enforcing hosts. Without this, ${APP_DATA_DIR}/data:/data style mounts
// hit "permission denied" at first write (verified on the v0.12 Phase 5
// VPS smoke test — see docs/selinux.md scenario 1).
//
// inVolumes tells the function the line is positioned inside a `volumes:`
// block — this is required because `host:container` is the shape used by
// both volume binds AND port mappings, and we must not append :Z to a
// `- ${APP_PORT}:9443` entry. The caller (SanitizeCompose) tracks YAML
// context as it scans line-by-line.
//
// Skips, in addition to the inVolumes guard:
//   - Named volumes (no leading / or ${ — Podman creates these and labels
//     them container_file_t automatically).
//   - Spec already containing :Z, :z, :ro, or :rw — avoid double-suffix
//     or fighting the operator's intent.
//   - The Podman/Docker socket bind mount — relabeling the socket file
//     breaks it (the socket keeps its var_run_t label; access is granted
//     via setsebool container_manage_cgroup in install.sh).
func relabelHostBindMount(line string, inVolumes bool) string {
	if !inVolumes {
		return line
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "- ") {
		return line
	}
	stripped := strings.TrimPrefix(trimmed, "- ")

	// Detect and remember whether the spec was quoted so we can re-quote
	// after appending :Z. Earlier versions appended :Z after the closing
	// quote, producing `- "host:container":Z` which is a YAML list item
	// whose scalar ends at the closing quote — the :Z then looks like a
	// flow-style key and compose parsers reject the file. Seven apps in
	// the 269-entry catalogue use the quoted form (archivebox, linkstack,
	// mixpost-pro, nitter, photoprism, searxng, zipline).
	var quote byte
	if len(stripped) >= 2 {
		if stripped[0] == '"' && stripped[len(stripped)-1] == '"' {
			quote = '"'
			stripped = stripped[1 : len(stripped)-1]
		} else if stripped[0] == '\'' && stripped[len(stripped)-1] == '\'' {
			quote = '\''
			stripped = stripped[1 : len(stripped)-1]
		}
	}

	parts := strings.Split(stripped, ":")
	if len(parts) < 2 {
		return line
	}
	host := parts[0]
	if !strings.HasPrefix(host, "/") && !strings.HasPrefix(host, "${") {
		return line
	}
	if strings.Contains(host, "docker.sock") || strings.Contains(host, "podman.sock") {
		return line
	}
	// Pseudo-filesystems and device nodes — Podman explicitly refuses to
	// relabel these (`SELinux relabeling of /dev is not allowed`), and
	// /sys / /proc would be equally inappropriate to touch. Any path
	// rooted at one of these is left bare. Verified on Phase 5 VPS Round
	// 3 with the gladys app, which mounts /dev:/dev — :Z made
	// `podman create` fail with exit 0 but Status=Created and no logs.
	for _, prefix := range []string{"/dev", "/sys", "/proc", "/run/udev"} {
		if host == prefix || strings.HasPrefix(host, prefix+"/") {
			return line
		}
	}
	last := parts[len(parts)-1]
	for _, opt := range []string{"Z", "z", "ro", "rw"} {
		if last == opt {
			return line
		}
	}

	// Rebuild the line preserving indent + quote state. We must find where
	// the spec ends in the ORIGINAL line (after the "- " and optional
	// opening quote) and insert :Z there — editing `line` directly avoids
	// losing any indentation the caller had.
	prefix := line[:len(line)-len(trimmed)] + "- "
	if quote != 0 {
		return prefix + string(quote) + stripped + ":Z" + string(quote)
	}
	return line + ":Z"
}

// SanitizeCompose cleans up a Runtipi-format compose file for standalone use:
//   - Removes tipi_main_network references (services & top-level)
//   - Strips traefik.* and runtipi.* labels
//   - Auto-appends :Z to host bind mounts so SELinux container_t can write
//     to them on EL9/EL10 enforcing hosts (skips named volumes + sockets)
//   - Auto-adds `security_opt: ['label=disable']` to services that bind-mount
//     docker.sock / podman.sock (Round-1 VPS finding: SELinux silently
//     denies container_t access to var_run_t sockets via dontaudit, no
//     remediation possible without per-container label disable). See
//     docs/selinux.md scenario 4.
func SanitizeCompose(compose string) string {
	return injectSocketLabelDisable(sanitizeStripAndRelabel(compose))
}

// sanitizeStripAndRelabel runs the line-by-line strip + bind-mount relabel
// pass. Split out from SanitizeCompose so the two-pass orchestration is
// readable; not for external use.
func sanitizeStripAndRelabel(compose string) string {
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(compose))
	skipTopNetwork := false
	// volumesIndent is the indent (in space columns) of the current
	// `volumes:` key. Lines deeper than this indent are bind specs that
	// the relabel function should consider. Reset to -1 once we exit
	// the block (line at <= indent that isn't a child item).
	volumesIndent := -1

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Track entry into / exit from a `volumes:` block.
		if trimmed == "volumes:" {
			volumesIndent = indent
		} else if volumesIndent >= 0 && trimmed != "" && indent <= volumesIndent {
			volumesIndent = -1
		}
		inVolumes := volumesIndent >= 0 && indent > volumesIndent

		// Skip traefik and runtipi labels (both YAML mapping and list formats)
		// Mapping:  traefik.enable: true
		// List:     - traefik.enable=true
		// List:     - "traefik.enable=true"
		if strings.HasPrefix(trimmed, "traefik.") || strings.HasPrefix(trimmed, "runtipi.") {
			continue
		}
		stripped := strings.TrimPrefix(trimmed, "- ")
		stripped = strings.Trim(stripped, "\"'")
		if strings.HasPrefix(stripped, "traefik.") || strings.HasPrefix(stripped, "runtipi.") {
			continue
		}

		// Skip "- tipi_main_network" or "- tipi-main-network" service-level network reference
		if trimmed == "- tipi_main_network" || trimmed == "- tipi-main-network" {
			continue
		}

		// Skip top-level "tipi_main_network:" or "tipi-main-network:" block and its children
		if skipTopNetwork {
			if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
				continue
			}
			skipTopNetwork = false
		}
		if trimmed == "tipi_main_network:" || trimmed == "tipi-main-network:" {
			skipTopNetwork = true
			continue
		}

		// Append :Z to host bind mounts (no-op for named volumes / sockets
		// / lines outside a volumes: block such as ports).
		out = append(out, relabelHostBindMount(line, inVolumes))
	}

	// Clean up empty networks/labels sections
	result := strings.Join(out, "\n")
	result = cleanEmptySection(result, "networks:")
	result = cleanEmptySection(result, "labels:")
	return result
}

// injectSocketLabelDisable scans the compose document for services that
// bind-mount docker.sock or podman.sock and, for each one, ensures the
// service has `security_opt: [label=disable]` so SELinux doesn't silently
// deny the container's access to the socket on EL9/EL10 enforcing.
//
// Why label=disable instead of a per-mount :Z (like the regular bind mounts):
// the Podman/Docker socket file MUST keep its var_run_t SELinux label intact
// — relabeling breaks the socket. The only off-the-shelf way to let
// container_t reach var_run_t is to disable SELinux labeling for the whole
// container. This is a security-budget tradeoff, but the apps that mount
// docker.sock are container-management tools (portainer, dockge, dozzle,
// etc.) which are root-equivalent anyway.
//
// Idempotent: if a service already declares security_opt with label=disable
// the function is a no-op for that service. Other security_opt entries are
// preserved (label=disable is appended).
func injectSocketLabelDisable(content string) string {
	lines := strings.Split(content, "\n")

	// Phase 1: identify each service block. A service header is any line
	// *deeper* than `services:` whose trimmed form ends with ":" and
	// whose key looks like a YAML identifier — we intentionally DON'T
	// require services_indent+2 (Codex High: 4-space-indented composes
	// were silently skipped). Inline comments after the colon are
	// allowed (`app:  # my service`). The service body continues until
	// we see another line at the service's own indent with the same
	// header shape, or a line at <= services_indent that's not a comment.
	servicesIndent := -1
	type svc struct {
		start, end, indent        int
		mountsSock                bool
		secOptIndex               int // line number of existing `security_opt:` (-1 if none)
		secOptIndent              int // indent of that line
		secOptListItemIndent      int // indent of the first `- …` item under it (-1 if none)
		hasLabelDisable           bool
	}
	var svcs []svc
	var cur *svc
	inVolumes := false
	volIndent := -1
	inSecOpt := false

	isServiceHeader := func(trimmed string, indent int) bool {
		if servicesIndent < 0 || indent <= servicesIndent {
			return false
		}
		// Strip inline comment.
		code := trimmed
		if i := strings.Index(code, "#"); i >= 0 {
			code = strings.TrimSpace(code[:i])
		}
		if !strings.HasSuffix(code, ":") {
			return false
		}
		key := strings.TrimSuffix(code, ":")
		if key == "" {
			return false
		}
		// YAML keys with spaces are usually quoted or nested mappings,
		// not plain service names. Reject them to avoid false positives
		// on lines like `labels:\n  traefik.enable: true` — the latter
		// is nested, but its indent would be deeper than the service it
		// belongs to, so the shallowest matching indent wins (first
		// header after `services:` sets svc.indent).
		if strings.ContainsAny(key, " \t") {
			return false
		}
		// Only accept lines at the *shallowest* indent we've seen so far
		// for services — the first service header under services: sets
		// that level, and any deeper-indented mapping key is a child.
		if cur != nil && indent != cur.indent {
			return false
		}
		return true
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		if servicesIndent < 0 && trimmed == "services:" {
			servicesIndent = indent
			continue
		}
		if servicesIndent < 0 {
			continue
		}

		if isServiceHeader(trimmed, indent) {
			if cur != nil {
				cur.end = i - 1
				svcs = append(svcs, *cur)
			}
			cur = &svc{start: i, indent: indent, secOptIndex: -1, secOptListItemIndent: -1}
			inVolumes = false
			volIndent = -1
			inSecOpt = false
			continue
		}

		// Exit services: block when we hit a line at or shallower than
		// services_indent that isn't blank/comment.
		if cur != nil && trimmed != "" && !strings.HasPrefix(trimmed, "#") && indent <= servicesIndent {
			cur.end = i - 1
			svcs = append(svcs, *cur)
			cur = nil
			servicesIndent = -1
			continue
		}

		if cur == nil {
			continue
		}

		// Track volumes: for docker.sock detection.
		if trimmed == "volumes:" && indent == cur.indent+(len(line)-len(strings.TrimLeft(line, " \t")))-cur.indent {
			// The above is a no-op sanity guard; simpler condition below.
		}
		if trimmed == "volumes:" {
			inVolumes = true
			volIndent = indent
			inSecOpt = false
			continue
		}
		if inVolumes && trimmed != "" && indent <= volIndent {
			inVolumes = false
		}
		if inVolumes {
			if strings.Contains(trimmed, "docker.sock") || strings.Contains(trimmed, "podman.sock") {
				cur.mountsSock = true
			}
		}

		// Track existing security_opt block within this service.
		if trimmed == "security_opt:" {
			cur.secOptIndex = i
			cur.secOptIndent = indent
			inSecOpt = true
			inVolumes = false
			continue
		}
		if inSecOpt {
			if trimmed != "" && indent <= cur.secOptIndent {
				inSecOpt = false
			} else if strings.HasPrefix(trimmed, "- ") {
				if cur.secOptListItemIndent < 0 {
					cur.secOptListItemIndent = indent
				}
				item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				item = strings.Trim(item, "\"'")
				if item == "label=disable" {
					cur.hasLabelDisable = true
				}
			}
		}
	}
	if cur != nil {
		cur.end = len(lines) - 1
		svcs = append(svcs, *cur)
	}

	// Phase 2: decide the insertion per socket-mounting service.
	//   - already has `label=disable` → skip
	//   - has `security_opt:` without label=disable → append a list item
	//     at the same indent as the existing items (Codex High: previous
	//     behaviour injected a duplicate `security_opt:` block, which
	//     either dropped existing entries like `seccomp=unconfined` or
	//     produced invalid duplicate-key YAML).
	//   - no `security_opt:` → insert a new block right after the service
	//     header, using service-indent+2 for the key and service-indent+4
	//     for the list item.
	type insertion struct {
		atLine int
		text   string
	}
	var inserts []insertion
	for _, s := range svcs {
		if !s.mountsSock || s.hasLabelDisable {
			continue
		}
		if s.secOptIndex >= 0 {
			listIndent := s.secOptListItemIndent
			if listIndent < 0 {
				// security_opt: exists but has no items yet (e.g. an
				// empty or commented-only list). Use 2 extra spaces.
				listIndent = s.secOptIndent + 2
			}
			inserts = append(inserts, insertion{
				atLine: s.secOptIndex + 1,
				text:   strings.Repeat(" ", listIndent) + "- label=disable",
			})
		} else {
			inserts = append(inserts, insertion{
				atLine: s.start + 1,
				text: strings.Repeat(" ", s.indent+2) + "security_opt:\n" +
					strings.Repeat(" ", s.indent+4) + "- label=disable",
			})
		}
	}

	if len(inserts) == 0 {
		return content
	}

	result := lines
	for i := len(inserts) - 1; i >= 0; i-- {
		ins := inserts[i]
		result = append(result[:ins.atLine],
			append([]string{ins.text}, result[ins.atLine:]...)...)
	}
	return strings.Join(result, "\n")
}

// cleanEmptySection removes a YAML section that has no real content after it.
// A section is considered empty if all child lines are blank or comments.
func cleanEmptySection(content, sectionKey string) string {
	lines := strings.Split(content, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == sectionKey {
			indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
			// Scan all child lines — if they're all blank or comments, the section is empty.
			j := i + 1
			hasContent := false
			for j < len(lines) {
				childTrimmed := strings.TrimSpace(lines[j])
				if childTrimmed == "" || strings.HasPrefix(childTrimmed, "#") {
					j++
					continue
				}
				childIndent := len(lines[j]) - len(strings.TrimLeft(lines[j], " \t"))
				if childIndent > indent {
					// Real content found at deeper indent.
					hasContent = true
					break
				}
				// Same or lower indent = next sibling section, stop scanning.
				break
			}
			if !hasContent {
				// Skip section header and all its blank/comment children.
				i = j
				continue
			}
		}
		out = append(out, lines[i])
		i++
	}
	return strings.Join(out, "\n")
}

// GenerateRandomBase64Value generates a crypto-random base64-encoded string.
// length is the number of random bytes before encoding.
func GenerateRandomBase64Value(length int) string {
	if length <= 0 {
		length = 32
	}
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// GenerateVapidKeys generates a VAPID key pair for Web Push.
// Returns (publicKey, privateKey) as base64url-encoded strings.
func GenerateVapidKeys() (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate ECDSA key: %w", err)
	}

	// Private key: raw 32-byte scalar
	privBytes := key.D.Bytes()
	// Pad to 32 bytes if needed
	padded := make([]byte, 32)
	copy(padded[32-len(privBytes):], privBytes)

	// Public key: uncompressed 65-byte point (0x04 || x || y)
	pubBytes := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)

	privB64 := base64.RawURLEncoding.EncodeToString(padded)
	pubB64 := base64.RawURLEncoding.EncodeToString(pubBytes)

	return pubB64, privB64, nil
}

// getLocalIP returns the first non-loopback IPv4 address of the host.
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

// DetectSecurityFlags scans a compose file for privileged Docker features.
func DetectSecurityFlags(compose string) []string {
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(compose))
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "privileged: true" && !seen["privileged"] {
			seen["privileged"] = true
		}
		if strings.HasPrefix(trimmed, "cap_add:") && !seen["cap_add"] {
			seen["cap_add"] = true
		}
		if trimmed == "pid: host" && !seen["pid_host"] {
			seen["pid_host"] = true
		}
		if strings.Contains(trimmed, "docker.sock") && !seen["docker_socket"] {
			seen["docker_socket"] = true
		}
	}
	var result []string
	for k := range seen {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
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
