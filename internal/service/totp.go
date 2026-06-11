package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/hkdf"
	"gorm.io/gorm"
)

// totpInfo is the HKDF domain-separation label for TOTP-secret encryption. It is
// distinct from the credentials label so TOTP secrets and credentials never share
// an effective key, even though both derive from the JWT secret.
const totpInfo = "webcasa-totp-v1"

// RecoveryCodeEntry represents a single recovery code stored in the database
type RecoveryCodeEntry struct {
	Hash string `json:"hash"`
	Used bool   `json:"used"`
}

// TOTPService handles TOTP 2FA operations
type TOTPService struct {
	db  *gorm.DB
	cfg *config.Config
}

// NewTOTPService creates a new TOTPService
func NewTOTPService(db *gorm.DB, cfg *config.Config) *TOTPService {
	return &TOTPService{db: db, cfg: cfg}
}

// deriveAESKey derives a 32-byte AES key from the JWT secret using HKDF-SHA256
// with the TOTP domain-separation label.
func deriveAESKey(jwtSecret string) []byte {
	key := make([]byte, 32)
	r := hkdf.New(sha256.New, []byte(jwtSecret), nil, []byte(totpInfo))
	if _, err := io.ReadFull(r, key); err != nil {
		// HKDF over SHA-256 cannot fail for a 32-byte output; fall back to SHA-256.
		return legacyDeriveAESKey(jwtSecret)
	}
	return key
}

// legacyDeriveAESKey reproduces the old bare-SHA256 key derivation. It exists only
// so TOTP secrets encrypted before the HKDF migration can still be decrypted.
func legacyDeriveAESKey(jwtSecret string) []byte {
	hash := sha256.Sum256([]byte(jwtSecret))
	return hash[:]
}

// decryptTOTPSecret decrypts a stored TOTP secret, trying the HKDF key first and
// falling back to the legacy SHA-256 key for pre-migration data. Legacy values are
// re-encrypted with the HKDF key the next time the secret is regenerated.
func (s *TOTPService) decryptTOTPSecret(encrypted string) ([]byte, error) {
	secretBytes, err := decryptAESGCM(encrypted, deriveAESKey(s.cfg.JWTSecret))
	if err != nil {
		secretBytes, err = decryptAESGCM(encrypted, legacyDeriveAESKey(s.cfg.JWTSecret))
	}
	return secretBytes, err
}

// encryptAESGCM encrypts plaintext using AES-GCM with the derived key
func encryptAESGCM(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aesGCM.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptAESGCM decrypts AES-GCM encrypted data
func decryptAESGCM(encoded string, key []byte) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return aesGCM.Open(nil, nonce, ciphertext, nil)
}

// totpPeriod is the TOTP step length in seconds, matching totp.Validate defaults.
const totpPeriod = 30

// matchedTimestep validates code against secret over the same ±1 window that
// totp.Validate uses, and returns the timestep (Unix time / period) the code
// matched. ok is false if the code is not valid for any step in the window.
func matchedTimestep(code, secret string, now time.Time) (step int64, ok bool) {
	current := now.Unix() / totpPeriod
	// Check current step first, then ±1, so the latest matching step wins.
	for _, s := range []int64{current, current + 1, current - 1} {
		expected, err := totp.GenerateCode(secret, time.Unix(s*totpPeriod, 0))
		if err != nil {
			continue
		}
		if expected == code {
			return s, true
		}
	}
	return 0, false
}

// GenerateSecret generates a TOTP secret for a user, encrypts and stores it.
// Returns the otpauth URI for QR code generation.
func (s *TOTPService) GenerateSecret(userID uint) (string, error) {
	var user model.User
	if err := s.db.First(&user, userID).Error; err != nil {
		return "", fmt.Errorf("error.user_not_found")
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "WebCasa",
		AccountName: user.Username,
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate TOTP key: %w", err)
	}

	// Encrypt the secret with AES-GCM
	aesKey := deriveAESKey(s.cfg.JWTSecret)
	encrypted, err := encryptAESGCM([]byte(key.Secret()), aesKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt TOTP secret: %w", err)
	}

	// Store encrypted secret but don't enable 2FA yet
	user.TOTPSecret = encrypted
	if err := s.db.Save(&user).Error; err != nil {
		return "", fmt.Errorf("failed to save TOTP secret: %w", err)
	}

	return key.URL(), nil
}

// generateRecoveryCodes generates 8 cryptographically random alphanumeric recovery codes of 8 chars each
func generateRecoveryCodes() []string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	charsetLen := big.NewInt(int64(len(charset)))
	codes := make([]string, 8)
	for i := 0; i < 8; i++ {
		code := make([]byte, 8)
		for j := range code {
			n, err := rand.Int(rand.Reader, charsetLen)
			if err != nil {
				// Fallback: this should never happen
				code[j] = charset[0]
				continue
			}
			code[j] = charset[n.Int64()]
		}
		codes[i] = string(code)
	}
	return codes
}

// VerifyAndEnable verifies a TOTP code and enables 2FA for the user.
// Returns the 8 plaintext recovery codes on success.
func (s *TOTPService) VerifyAndEnable(userID uint, code string) ([]string, error) {
	var user model.User
	if err := s.db.First(&user, userID).Error; err != nil {
		return nil, fmt.Errorf("error.user_not_found")
	}

	if user.TOTPSecret == "" {
		return nil, fmt.Errorf("error.2fa_not_setup")
	}

	// Decrypt the TOTP secret
	secretBytes, err := s.decryptTOTPSecret(user.TOTPSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt TOTP secret: %w", err)
	}

	// Validate the TOTP code
	valid := totp.Validate(code, string(secretBytes))
	if !valid {
		return nil, fmt.Errorf("error.invalid_totp")
	}

	// Generate 8 recovery codes
	plainCodes := generateRecoveryCodes()
	var entries []RecoveryCodeEntry
	for _, pc := range plainCodes {
		hash, err := bcrypt.GenerateFromPassword([]byte(pc), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash recovery code: %w", err)
		}
		entries = append(entries, RecoveryCodeEntry{Hash: string(hash), Used: false})
	}

	codesJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal recovery codes: %w", err)
	}

	// Enable 2FA. Record the timestep used to enable so the same code cannot be
	// replayed at login (TOTP replay protection).
	enabled := true
	user.TOTPEnabled = &enabled
	user.RecoveryCodes = string(codesJSON)
	if step, ok := matchedTimestep(code, string(secretBytes), time.Now()); ok {
		user.LastTOTPTimestep = step
	}
	if err := s.db.Save(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to enable 2FA: %w", err)
	}

	return plainCodes, nil
}

// Disable verifies a TOTP code and disables 2FA for the user.
func (s *TOTPService) Disable(userID uint, code string) error {
	var user model.User
	if err := s.db.First(&user, userID).Error; err != nil {
		return fmt.Errorf("error.user_not_found")
	}

	if user.TOTPEnabled == nil || !*user.TOTPEnabled {
		return fmt.Errorf("error.2fa_not_enabled")
	}

	// Decrypt and validate TOTP code
	secretBytes, err := s.decryptTOTPSecret(user.TOTPSecret)
	if err != nil {
		return fmt.Errorf("failed to decrypt TOTP secret: %w", err)
	}

	valid := totp.Validate(code, string(secretBytes))
	if !valid {
		return fmt.Errorf("error.invalid_totp")
	}

	// Disable 2FA
	disabled := false
	user.TOTPEnabled = &disabled
	user.TOTPSecret = ""
	user.RecoveryCodes = ""
	if err := s.db.Model(&user).Updates(map[string]interface{}{
		"totp_enabled":   false,
		"totp_secret":    "",
		"recovery_codes": "",
	}).Error; err != nil {
		return fmt.Errorf("failed to disable 2FA: %w", err)
	}

	return nil
}

// ValidateLogin validates a TOTP code or recovery code for login.
// Returns true if the code is valid.
func (s *TOTPService) ValidateLogin(userID uint, code string) (bool, error) {
	var user model.User
	if err := s.db.First(&user, userID).Error; err != nil {
		return false, fmt.Errorf("error.user_not_found")
	}

	if user.TOTPEnabled == nil || !*user.TOTPEnabled {
		return false, fmt.Errorf("error.2fa_not_enabled")
	}

	// Try TOTP code first
	secretBytes, err := s.decryptTOTPSecret(user.TOTPSecret)
	if err != nil {
		return false, fmt.Errorf("failed to decrypt TOTP secret: %w", err)
	}

	// Replay protection: find the timestep the code matches and reject any code
	// from a timestep already consumed (<= the last accepted one). On success we
	// persist the new timestep so the same code cannot be reused within its window.
	if step, ok := matchedTimestep(code, string(secretBytes), time.Now()); ok {
		if step <= user.LastTOTPTimestep {
			return false, nil
		}
		if err := s.db.Model(&model.User{}).Where("id = ?", user.ID).
			Update("last_totp_timestep", step).Error; err != nil {
			return false, fmt.Errorf("failed to record TOTP timestep: %w", err)
		}
		return true, nil
	}

	// Try recovery codes
	if user.RecoveryCodes == "" {
		return false, nil
	}

	var entries []RecoveryCodeEntry
	if err := json.Unmarshal([]byte(user.RecoveryCodes), &entries); err != nil {
		return false, fmt.Errorf("failed to parse recovery codes: %w", err)
	}

	for i, entry := range entries {
		if entry.Used {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(entry.Hash), []byte(code)) == nil {
			// Mark as used
			entries[i].Used = true
			updatedJSON, err := json.Marshal(entries)
			if err != nil {
				return false, fmt.Errorf("failed to update recovery codes: %w", err)
			}
			user.RecoveryCodes = string(updatedJSON)
			if err := s.db.Save(&user).Error; err != nil {
				return false, fmt.Errorf("failed to save recovery code usage: %w", err)
			}
			return true, nil
		}
	}

	return false, nil
}
