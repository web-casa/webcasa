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
