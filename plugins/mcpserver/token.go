package mcpserver

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// TokenService manages API token lifecycle.
type TokenService struct {
	db *gorm.DB
}

// NewTokenService creates a new TokenService.
func NewTokenService(db *gorm.DB) *TokenService {
	return &TokenService{db: db}
}

// CreateToken generates a new API token and returns the plaintext (shown once).
// The token format is: wc_ + 32 random bytes hex-encoded (67 chars total).
func (s *TokenService) CreateToken(userID uint, name string, permissions string, expiresAt *time.Time) (*APIToken, string, error) {
	if name == "" {
		return nil, "", errors.New("token name is required")
	}

	// Generate 32 random bytes
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate random bytes: %w", err)
	}
	plaintext := "wc_" + hex.EncodeToString(raw) // 67 chars

	// Hash for storage
	hash := sha256.Sum256([]byte(plaintext))
	tokenHash := hex.EncodeToString(hash[:])

	// Prefix for fast lookup: "wc_" + first 8 hex chars of the random part
	prefix := plaintext[:11] // "wc_" + 8 chars

	if permissions == "" {
		permissions = "[]"
	}

	token := &APIToken{
		UserID:      userID,
		Name:        name,
		TokenHash:   tokenHash,
		Prefix:      prefix,
		Permissions: permissions,
		ExpiresAt:   expiresAt,
	}

	if err := s.db.Create(token).Error; err != nil {
		return nil, "", fmt.Errorf("create token: %w", err)
	}

	return token, plaintext, nil
}

// ValidateToken checks a plaintext token and returns the APIToken if valid.
func (s *TokenService) ValidateToken(plaintext string) (*APIToken, error) {
	if len(plaintext) < 11 {
		return nil, errors.New("invalid token format")
	}

	prefix := plaintext[:11]
	hash := sha256.Sum256([]byte(plaintext))
	tokenHash := hex.EncodeToString(hash[:])

	var candidates []APIToken
	if err := s.db.Where("prefix = ?", prefix).Find(&candidates).Error; err != nil {
		return nil, err
	}

	for i := range candidates {
		if candidates[i].TokenHash == tokenHash {
			// Check expiry
			if candidates[i].ExpiresAt != nil && candidates[i].ExpiresAt.Before(time.Now()) {
				return nil, errors.New("token expired")
			}
			// Update last_used_at
			now := time.Now()
			s.db.Model(&candidates[i]).Update("last_used_at", now)
			candidates[i].LastUsedAt = &now
			return &candidates[i], nil
		}
	}

	return nil, errors.New("invalid token")
}

// ListTokens returns all tokens for a user (without hashes).
func (s *TokenService) ListTokens(userID uint) ([]APIToken, error) {
	var tokens []APIToken
	err := s.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&tokens).Error
	return tokens, err
}

// DeleteToken revokes a token.
func (s *TokenService) DeleteToken(id uint, userID uint) error {
	result := s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&APIToken{})
	if result.RowsAffected == 0 {
		return errors.New("token not found")
	}
	return result.Error
}
