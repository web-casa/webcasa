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
