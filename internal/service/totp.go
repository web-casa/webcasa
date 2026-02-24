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
	mrand "math/rand"
	"time"

	"github.com/caddypanel/caddypanel/internal/config"
	"github.com/caddypanel/caddypanel/internal/model"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

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

// deriveAESKey derives a 32-byte AES key from the JWT secret using SHA-256
func deriveAESKey(jwtSecret string) []byte {
	hash := sha256.Sum256([]byte(jwtSecret))
	return hash[:]
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

// GenerateSecret generates a TOTP secret for a user, encrypts and stores it.
// Returns the otpauth URI for QR code generation.
func (s *TOTPService) GenerateSecret(userID uint) (string, error) {
	var user model.User
	if err := s.db.First(&user, userID).Error; err != nil {
		return "", fmt.Errorf("error.user_not_found")
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "CaddyPanel",
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

// generateRecoveryCodes generates 8 random alphanumeric recovery codes of 8 chars each
func generateRecoveryCodes() []string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	src := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	codes := make([]string, 8)
	for i := 0; i < 8; i++ {
		code := make([]byte, 8)
		for j := range code {
			code[j] = charset[src.Intn(len(charset))]
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
	aesKey := deriveAESKey(s.cfg.JWTSecret)
	secretBytes, err := decryptAESGCM(user.TOTPSecret, aesKey)
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

	// Enable 2FA
	enabled := true
	user.TOTPEnabled = &enabled
	user.RecoveryCodes = string(codesJSON)
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
	aesKey := deriveAESKey(s.cfg.JWTSecret)
	secretBytes, err := decryptAESGCM(user.TOTPSecret, aesKey)
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
	aesKey := deriveAESKey(s.cfg.JWTSecret)
	secretBytes, err := decryptAESGCM(user.TOTPSecret, aesKey)
	if err != nil {
		return false, fmt.Errorf("failed to decrypt TOTP secret: %w", err)
	}

	if totp.Validate(code, string(secretBytes)) {
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
