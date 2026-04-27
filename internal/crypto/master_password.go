// Helper functions for master password handling
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	
	"golang.org/x/crypto/argon2"
)

// DeriveKeyFromMasterPassword derives an encryption key from the master password
func DeriveKeyFromMasterPassword(masterPassword, salt string) ([]byte, error) {
	if masterPassword == "" {
		return nil, nil // No encryption
	}
	
	// Use Argon2id for key derivation
	key := argon2.IDKey(
		[]byte(masterPassword),
		[]byte(salt),
		3,           // iterations
		64*1024,     // 64 MB memory
		4,           // parallelism
		32,          // key length
	)
	
	return key, nil
}

// EncryptWithMasterPassword encrypts data using master password
func EncryptWithMasterPassword(data []byte, masterPassword string) (string, error) {
	if masterPassword == "" {
		// No encryption - return base64 encoded
		return base64.StdEncoding.EncodeToString(data), nil
	}
	
	// Generate salt
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	
	// Derive key
	key, err := DeriveKeyFromMasterPassword(masterPassword, string(salt))
	if err != nil {
		return "", err
	}
	
	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	
	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	
	// Encrypt
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	
	// Encode: salt + ciphertext
	result := append(salt, ciphertext...)
	return base64.StdEncoding.EncodeToString(result), nil
}

// DecryptWithMasterPassword decrypts data using master password
func DecryptWithMasterPassword(encodedData string, masterPassword string) ([]byte, error) {
	if masterPassword == "" {
		// No encryption - decode base64
		return base64.StdEncoding.DecodeString(encodedData)
	}
	
	// Decode
	data, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		return nil, err
	}
	
	// Extract salt
	if len(data) < 16 {
		return nil, fmt.Errorf("invalid encrypted data")
	}
	salt := data[:16]
	ciphertext := data[16:]
	
	// Derive key
	key, err := DeriveKeyFromMasterPassword(masterPassword, string(salt))
	if err != nil {
		return nil, err
	}
	
	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	
	// Extract nonce
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	
	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	
	return plaintext, nil
}

// CreateMasterPasswordChecksum creates a checksum to verify master password
func CreateMasterPasswordChecksum(masterPassword string) (string, error) {
	if masterPassword == "" {
		return "", nil
	}
	
	// Create test string
	testData := []byte("ds3backup-master-password-test")
	
	// Encrypt it
	encrypted, err := EncryptWithMasterPassword(testData, masterPassword)
	if err != nil {
		return "", err
	}
	
	return encrypted, nil
}

// VerifyMasterPasswordChecksum verifies the master password
func VerifyMasterPasswordChecksum(encryptedChecksum, masterPassword string) (bool, error) {
	if masterPassword == "" && encryptedChecksum == "" {
		return true, nil // No password set
	}
	
	// Try to decrypt
	decrypted, err := DecryptWithMasterPassword(encryptedChecksum, masterPassword)
	if err != nil {
		return false, nil // Wrong password
	}
	
	// Verify content
	expected := "ds3backup-master-password-test"
	return string(decrypted) == expected, nil
}

// HashPassword creates a SHA256 hash of password for comparison
func HashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return base64.StdEncoding.EncodeToString(hash[:])
}
