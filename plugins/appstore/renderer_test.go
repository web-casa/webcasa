package appstore

import (
	"strings"
	"testing"
)

// TestSanitizeCompose_RelabelHostBindMounts pins the v0.12 SELinux fix
// (Round-1 VPS finding #3): host-path bind mounts must get a `:Z`
// suffix so the container's container_t domain can read/write them
// on EL9/EL10 enforcing hosts. Named volumes, sockets, and already-
// suffixed mounts must be left alone.
func TestSanitizeCompose_RelabelHostBindMounts(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		mustHave  []string // substrings that must appear in output
		mustMiss  []string // substrings that must NOT appear in output
	}{
		{
			name: "host path with env var → :Z appended",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - ${APP_DATA_DIR}/data:/data
      - ${APP_DATA_DIR}/cache:/cache:ro`,
			mustHave: []string{
				"- ${APP_DATA_DIR}/data:/data:Z",
				"- ${APP_DATA_DIR}/cache:/cache:ro", // ro preserved, no :Z added
			},
			mustMiss: []string{
				"/data:/data:Z:Z", // no double-suffix
				"/cache:/cache:ro:Z",
			},
		},
		{
			name: "absolute host path → :Z appended",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - /opt/appdata:/data`,
			mustHave: []string{"- /opt/appdata:/data:Z"},
		},
		{
			name: "named volume → no relabel",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - mydata:/data
volumes:
  mydata:`,
			mustHave: []string{"- mydata:/data"},
			mustMiss: []string{"mydata:/data:Z"},
		},
		{
			name: "docker.sock → not relabeled (would break socket)",
			in: `services:
  portainer:
    image: portainer/portainer-ce:2.39
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ${APP_DATA_DIR}/data:/data`,
			mustHave: []string{
				"- /var/run/docker.sock:/var/run/docker.sock",
				"- ${APP_DATA_DIR}/data:/data:Z",
			},
			mustMiss: []string{"docker.sock:Z"},
		},
		{
			name: "podman.sock → also not relabeled",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - /run/podman/podman.sock:/var/run/docker.sock`,
			mustMiss: []string{"podman.sock:Z"},
		},
		{
			name: "already :Z → no double-suffix",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - ${APP_DATA_DIR}/data:/data:Z`,
			mustHave: []string{"- ${APP_DATA_DIR}/data:/data:Z"},
			mustMiss: []string{":Z:Z"},
		},
		{
			name: "already :z (shared) → no upgrade to :Z",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - ${APP_DATA_DIR}/shared:/shared:z`,
			mustHave: []string{"- ${APP_DATA_DIR}/shared:/shared:z"},
			mustMiss: []string{":z:Z"},
		},
		{
			name: "service mounting docker.sock gets security_opt label=disable",
			in: `services:
  portainer:
    image: portainer/portainer-ce:2.39
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ${APP_DATA_DIR}/data:/data`,
			mustHave: []string{
				"security_opt:",
				"- label=disable",
				"- ${APP_DATA_DIR}/data:/data:Z",
			},
		},
		{
			name: "service NOT mounting docker.sock gets no security_opt",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - ${APP_DATA_DIR}/data:/data`,
			mustMiss: []string{"security_opt:", "label=disable"},
		},
		{
			name: "podman.sock variant also triggers label=disable",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - /run/podman/podman.sock:/var/run/docker.sock`,
			mustHave: []string{"security_opt:", "- label=disable"},
		},
		{
			name: "existing security_opt label=disable is not duplicated",
			in: `services:
  portainer:
    image: portainer/portainer-ce:2.39
    security_opt:
      - label=disable
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock`,
			mustHave: []string{"- label=disable"},
			// only one occurrence: counted via assertion below
		},
		{
			// Critical: Codex Round-4 finding. 7 apps in the catalogue
			// (archivebox, linkstack, mixpost-pro, nitter, photoprism,
			// searxng, zipline) quote their volume entries. A naive
			// append produced `- "...":Z` which is invalid YAML. :Z
			// must land INSIDE the closing quote.
			name: "double-quoted bind mount → :Z inside the quotes",
			in: `services:
  app:
    image: example/app:1
    volumes:
      - "${APP_DATA_DIR}/data:/data"
      - '/opt/app/logs:/logs'`,
			mustHave: []string{
				`- "${APP_DATA_DIR}/data:/data:Z"`,
				`- '/opt/app/logs:/logs:Z'`,
			},
			mustMiss: []string{
				`"${APP_DATA_DIR}/data:/data":Z`,
				`'/opt/app/logs:/logs':Z`,
			},
		},
		{
			// Phase 5 Round 3 finding: gladys mounts /dev:/dev. Podman
			// refuses `SELinux relabeling of /dev`, container stuck in
			// Created with no logs. Skip pseudofs/device paths entirely.
			name: "pseudofs paths (/dev, /sys, /proc, /run/udev) NOT relabeled",
			in: `services:
  gladys:
    image: example/iot:1
    volumes:
      - /dev:/dev
      - /sys:/sys
      - /proc/sys/net:/proc/sys/net
      - /run/udev:/run/udev:ro
      - ${APP_DATA_DIR}/data:/data`,
			mustHave: []string{
				"- /dev:/dev",
				"- /sys:/sys",
				"- /proc/sys/net:/proc/sys/net",
				"- /run/udev:/run/udev:ro",
				"- ${APP_DATA_DIR}/data:/data:Z", // regular paths still relabeled
			},
			mustMiss: []string{
				"/dev:/dev:Z",
				"/sys:/sys:Z",
				"/proc/sys/net:/proc/sys/net:Z",
				"/run/udev:/run/udev:ro:Z",
			},
		},
		{
			// Regression: an earlier version naively appended :Z to any
			// host:container shape, which corrupted port mappings.
			name: "ports block (same shape as bind) is NOT relabeled",
			in: `services:
  app:
    image: example/app:1
    ports:
      - ${APP_PORT}:9443
      - "8080:80"
    volumes:
      - ${APP_DATA_DIR}/data:/data`,
			mustHave: []string{
				"- ${APP_PORT}:9443",
				"- ${APP_DATA_DIR}/data:/data:Z",
			},
			mustMiss: []string{
				"${APP_PORT}:9443:Z",
				"\"8080:80\":Z",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeCompose(tc.in)
			for _, want := range tc.mustHave {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in output:\n%s", want, got)
				}
			}
			for _, bad := range tc.mustMiss {
				if strings.Contains(got, bad) {
					t.Errorf("unexpected %q in output:\n%s", bad, got)
				}
			}
		})
	}
}

// TestSanitizeCompose_ServiceIndent4 — compose files that use 4-space
// indentation for service definitions (legal YAML, found in the wild)
// must still have socket-mounting services detected. Codex Round-4
// High: previous implementation hard-coded `services_indent + 2` and
// silently skipped 4-space-indented services.
func TestSanitizeCompose_ServiceIndent4(t *testing.T) {
	in := `services:
    portainer:
        image: portainer/portainer-ce:2.39
        volumes:
            - /var/run/docker.sock:/var/run/docker.sock
            - ${APP_DATA_DIR}/data:/data`
	got := SanitizeCompose(in)
	if !strings.Contains(got, "security_opt:") {
		t.Errorf("4-space indent service did NOT receive security_opt:\n%s", got)
	}
	if !strings.Contains(got, "- label=disable") {
		t.Errorf("4-space indent service did NOT receive label=disable:\n%s", got)
	}
	// Host bind also needs :Z under the 4-space service.
	if !strings.Contains(got, "${APP_DATA_DIR}/data:/data:Z") {
		t.Errorf("4-space indent bind mount not relabeled:\n%s", got)
	}
}

// TestSanitizeCompose_SecurityOptMerge — Codex Round-4 High: a service
// that already has security_opt with OTHER options (not label=disable)
// must get label=disable APPENDED to the existing list, not a duplicate
// security_opt: block that would produce invalid YAML or drop existing
// entries.
func TestSanitizeCompose_SecurityOptMerge(t *testing.T) {
	in := `services:
  portainer:
    image: portainer/portainer-ce:2.39
    security_opt:
      - seccomp=unconfined
      - apparmor=unconfined
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock`
	got := SanitizeCompose(in)
	if n := strings.Count(got, "security_opt:"); n != 1 {
		t.Errorf("expected exactly 1 security_opt: block (merge), got %d:\n%s", n, got)
	}
	for _, must := range []string{
		"- seccomp=unconfined",
		"- apparmor=unconfined",
		"- label=disable",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("missing %q after merge:\n%s", must, got)
		}
	}
}

// TestSanitizeCompose_LabelDisableIdempotent — a service that already
// declares `security_opt: [label=disable]` must not get a duplicate
// security_opt block injected.
func TestSanitizeCompose_LabelDisableIdempotent(t *testing.T) {
	in := `services:
  portainer:
    image: portainer/portainer-ce:2.39
    security_opt:
      - label=disable
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock`
	got := SanitizeCompose(in)
	if n := strings.Count(got, "security_opt:"); n != 1 {
		t.Errorf("expected exactly 1 security_opt: block, got %d in:\n%s", n, got)
	}
	if n := strings.Count(got, "label=disable"); n != 1 {
		t.Errorf("expected exactly 1 label=disable, got %d in:\n%s", n, got)
	}
}
