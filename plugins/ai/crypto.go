package ai

import "github.com/web-casa/webcasa/internal/crypto"

// Encrypt encrypts plaintext using AES-256-GCM via the shared crypto package.
var Encrypt = crypto.Encrypt

// Decrypt decrypts a base64-encoded AES-256-GCM ciphertext via the shared crypto package.
var Decrypt = crypto.Decrypt

// MaskAPIKey returns a masked version of the key for display.
var MaskAPIKey = crypto.MaskAPIKey
