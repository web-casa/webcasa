package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveJWTSecret_GeneratesNew verifies that when no .jwt_secret file
// exists and no env var is set, resolveJWTSecret generates a new random hex
// secret and persists it to disk.
func TestResolveJWTSecret_GeneratesNew(t *testing.T) {
	dir := t.TempDir()

	// Ensure no env var interferes
	t.Setenv("WEBCASA_JWT_SECRET", "")

	secret := resolveJWTSecret(dir)

	if secret == "" {
		t.Fatal("expected a non-empty secret, got empty string")
	}

	// The secret file should now exist
	secretFile := filepath.Join(dir, ".jwt_secret")
	if _, err := os.Stat(secretFile); os.IsNotExist(err) {
		t.Fatalf("expected .jwt_secret file to exist at %s, but it does not", secretFile)
	}
}

// TestResolveJWTSecret_LoadsExisting verifies that when a .jwt_secret file
// already exists with a known value, resolveJWTSecret returns that value.
func TestResolveJWTSecret_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WEBCASA_JWT_SECRET", "")

	knownSecret := "my-pre-existing-secret-value-1234567890abcdef"
	secretFile := filepath.Join(dir, ".jwt_secret")
	if err := os.WriteFile(secretFile, []byte(knownSecret+"\n"), 0600); err != nil {
		t.Fatalf("failed to write test secret file: %v", err)
	}

	secret := resolveJWTSecret(dir)

	if secret != knownSecret {
		t.Errorf("expected secret %q, got %q", knownSecret, secret)
	}
}

// TestResolveJWTSecret_EnvOverride verifies that when WEBCASA_JWT_SECRET is
// set to a custom (non-default) value, resolveJWTSecret returns that value,
// ignoring any file on disk.
func TestResolveJWTSecret_EnvOverride(t *testing.T) {
	dir := t.TempDir()

	customSecret := "env-override-secret-abc123"
	t.Setenv("WEBCASA_JWT_SECRET", customSecret)

	// Also write a different secret to the file, to confirm env takes priority
	secretFile := filepath.Join(dir, ".jwt_secret")
	if err := os.WriteFile(secretFile, []byte("file-secret-should-be-ignored\n"), 0600); err != nil {
		t.Fatalf("failed to write test secret file: %v", err)
	}

	secret := resolveJWTSecret(dir)

	if secret != customSecret {
		t.Errorf("expected env secret %q, got %q", customSecret, secret)
	}
}

// TestResolveJWTSecret_IgnoresOldDefault verifies that when WEBCASA_JWT_SECRET
// is set to a known insecure default, the function ignores it and generates a
// new random secret instead.
func TestResolveJWTSecret_IgnoresOldDefault(t *testing.T) {
	insecureDefaults := []string{
		"webcasa-change-me-in-production",
		"change-me-in-production",
	}
	for _, oldDefault := range insecureDefaults {
		t.Run(oldDefault, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("WEBCASA_JWT_SECRET", oldDefault)

			secret := resolveJWTSecret(dir)

			if secret == oldDefault {
				t.Error("expected resolveJWTSecret to ignore the insecure default, but it returned it")
			}
			if secret == "" {
				t.Error("expected a non-empty generated secret, got empty string")
			}
		})
	}
}

// TestResolveJWTSecret_Persistence verifies that calling resolveJWTSecret
// twice with the same directory returns the same secret both times, confirming
// the value is persisted and reloaded.
func TestResolveJWTSecret_Persistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WEBCASA_JWT_SECRET", "")

	first := resolveJWTSecret(dir)
	second := resolveJWTSecret(dir)

	if first != second {
		t.Errorf("expected same secret on both calls, got %q and %q", first, second)
	}
}

// TestResolveJWTSecret_SecretLength verifies that the auto-generated secret
// is exactly 64 hex characters (encoding 32 random bytes).
func TestResolveJWTSecret_SecretLength(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WEBCASA_JWT_SECRET", "")

	secret := resolveJWTSecret(dir)

	if len(secret) != 64 {
		t.Errorf("expected secret length 64, got %d (secret: %q)", len(secret), secret)
	}

	// Verify it is valid hex
	if _, err := hex.DecodeString(secret); err != nil {
		t.Errorf("expected valid hex string, got decode error: %v (secret: %q)", err, secret)
	}
}

// TestResolveJWTSecret_FilePermissions verifies that the persisted .jwt_secret
// file is created with 0600 permissions (owner read/write only).
func TestResolveJWTSecret_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WEBCASA_JWT_SECRET", "")

	resolveJWTSecret(dir)

	secretFile := filepath.Join(dir, ".jwt_secret")
	info, err := os.Stat(secretFile)
	if err != nil {
		t.Fatalf("failed to stat .jwt_secret file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected file permissions 0600, got %04o", perm)
	}
}
