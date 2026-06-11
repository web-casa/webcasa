package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// credentialsInfo is the HKDF domain-separation label for credential encryption.
// A distinct label is used for TOTP secrets (see internal/service/totp.go) so the
// two never share an effective key.
const credentialsInfo = "webcasa-credentials-v1"

// deriveKey derives a 32-byte AES key from the secret using HKDF-SHA256 with the
// given domain-separation info label. This replaces a bare SHA-256 of the secret.
func deriveKey(secret, info string) []byte {
	key := make([]byte, 32)
	r := hkdf.New(sha256.New, []byte(secret), nil, []byte(info))
	if _, err := io.ReadFull(r, key); err != nil {
		// HKDF over SHA-256 cannot fail for a 32-byte output; fall back to SHA-256.
		h := sha256.Sum256([]byte(secret))
		return h[:]
	}
	return key
}

// legacyDeriveKey reproduces the old bare-SHA256 key derivation. It exists only so
// that data encrypted before the HKDF migration can still be decrypted.
func legacyDeriveKey(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// Encrypt encrypts plaintext using AES-256-GCM.
func Encrypt(plaintext, secret string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key := deriveKey(secret, credentialsInfo)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded AES-256-GCM ciphertext.
//
// Backward compatibility: new ciphertexts use the HKDF-derived key, but databases
// created before the HKDF migration hold data encrypted with the legacy bare-SHA256
// key. We therefore try the HKDF key first and, on a GCM authentication failure,
// fall back to the legacy key. Such legacy values are transparently re-encrypted
// with the HKDF key the next time they are saved.
func Decrypt(encoded, secret string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	plaintext, err := decryptWithKey(data, deriveKey(secret, credentialsInfo))
	if err != nil {
		// Auth failure: try the legacy SHA-256 key for pre-migration data.
		plaintext, err = decryptWithKey(data, legacyDeriveKey(secret))
		if err != nil {
			return "", fmt.Errorf("decrypt: %w", err)
		}
	}
	return string(plaintext), nil
}

// decryptWithKey opens AES-256-GCM ciphertext with the supplied key.
func decryptWithKey(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// MaskAPIKey returns a masked version of the key for display (first 4 + last 4 chars).
func MaskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// IsEncrypted checks if a string looks like base64-encoded encrypted data.
// This is a heuristic used for migration detection.
func IsEncrypted(s string) bool {
	if s == "" {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(s) > 40
}
