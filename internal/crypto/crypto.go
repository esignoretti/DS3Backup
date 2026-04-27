package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/crypto/blake2b"
)

// EncryptedFile represents an encrypted file
type EncryptedFile struct {
	Nonce          []byte `json:"nonce"`
	Ciphertext     []byte `json:"ciphertext"`
	Tag            []byte `json:"tag"`
	OriginalHash   []byte `json:"originalHash"`
	OriginalSize   int64  `json:"originalSize"`
	CompressedSize int64  `json:"compressedSize"`
}

// CryptoEngine handles encryption and compression
type CryptoEngine struct {
	masterKey []byte
	salt      []byte
}

// NewCryptoEngine creates a new crypto engine from password
func NewCryptoEngine(password string, salt []byte) (*CryptoEngine, error) {
	if len(salt) == 0 {
		// Generate random salt
		salt = make([]byte, 16)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, err
		}
	}

	// Derive master key using Argon2id
	masterKey := argon2.IDKey(
		[]byte(password),
		salt,
		3,           // iterations
		64*1024,     // 64 MB memory
		4,           // parallelism
		32,          // key length
	)

	return &CryptoEngine{
		masterKey: masterKey,
		salt:      salt,
	}, nil
}

// GetSalt returns the salt (for storage in config)
func (e *CryptoEngine) GetSalt() []byte {
	return e.salt
}

// deriveFileKey derives a per-file key from master key and file hash
func (e *CryptoEngine) deriveFileKey(fileHash []byte) []byte {
	h := hmac.New(sha256.New, e.masterKey)
	h.Write(fileHash)
	return h.Sum(nil)
}

// CompressAndEncrypt compresses and encrypts file content
func (e *CryptoEngine) CompressAndEncrypt(content []byte) (*EncryptedFile, error) {
	// 1. Calculate hash of original content (for dedup)
	hash := blake2b.Sum256(content)

	// 2. Compress with zstd
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	compressed := enc.EncodeAll(content, nil)
	enc.Close()

	// 3. Derive per-file key
	fileKey := e.deriveFileKey(hash[:])

	// 4. Encrypt compressed data
	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Seal with nil dst to get clean ciphertext+tag (nonce NOT included)
	encryptedData := gcm.Seal(nil, nonce, compressed, nil)
	
	// encryptedData now contains: ciphertext + tag
	// Extract components
	ciphertext := make([]byte, len(encryptedData)-gcm.Overhead())
	copy(ciphertext, encryptedData[:len(encryptedData)-gcm.Overhead()])
	
	tag := make([]byte, gcm.Overhead())
	copy(tag, encryptedData[len(encryptedData)-gcm.Overhead():])

	return &EncryptedFile{
		Nonce:          nonce,
		Ciphertext:     ciphertext,
		Tag:            tag,
		OriginalHash:   hash[:],
		OriginalSize:   int64(len(content)),
		CompressedSize: int64(len(compressed)),
	}, nil
}

// DecryptAndDecompress decrypts and decompresses file content
func (e *CryptoEngine) DecryptAndDecompress(encFile *EncryptedFile) ([]byte, error) {
	// 1. Derive per-file key from stored hash
	fileKey := e.deriveFileKey(encFile.OriginalHash)

	// 2. Reconstruct ciphertext with nonce and tag
	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Reconstruct: nonce + ciphertext + tag
	ciphertext := make([]byte, 0, len(encFile.Nonce)+len(encFile.Ciphertext)+len(encFile.Tag))
	ciphertext = append(ciphertext, encFile.Nonce...)
	ciphertext = append(ciphertext, encFile.Ciphertext...)
	ciphertext = append(ciphertext, encFile.Tag...)

	// 3. Decrypt
	compressed, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
	if err != nil {
		return nil, errors.New("decryption failed: " + err.Error())
	}

	// 4. Decompress
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	original, err := dec.DecodeAll(compressed, nil)
	if err != nil {
		return nil, errors.New("decompression failed: " + err.Error())
	}

	// 5. Verify hash
	hash := blake2b.Sum256(original)
	if string(hash[:]) != string(encFile.OriginalHash) {
		return nil, errors.New("hash verification failed")
	}

	return original, nil
}

// Serialize converts EncryptedFile to bytes for storage
func (e *EncryptedFile) Serialize() ([]byte, error) {
	data := make([]byte, 0)
	
	data = append(data, byte(len(e.Nonce)))
	data = append(data, e.Nonce...)
	
	data = append(data, byte(len(e.OriginalHash)))
	data = append(data, e.OriginalHash...)
	
	data = append(data, byte(e.OriginalSize>>56), byte(e.OriginalSize>>48), byte(e.OriginalSize>>40), byte(e.OriginalSize>>32))
	data = append(data, byte(e.OriginalSize>>24), byte(e.OriginalSize>>16), byte(e.OriginalSize>>8), byte(e.OriginalSize))
	
	data = append(data, byte(e.CompressedSize>>56), byte(e.CompressedSize>>48), byte(e.CompressedSize>>40), byte(e.CompressedSize>>32))
	data = append(data, byte(e.CompressedSize>>24), byte(e.CompressedSize>>16), byte(e.CompressedSize>>8), byte(e.CompressedSize))
	
	data = append(data, byte(len(e.Tag)))
	data = append(data, e.Tag...)
	
	data = append(data, e.Ciphertext...)
	
	return data, nil
}

// DeserializeEncryptedFile reconstructs EncryptedFile from bytes
func DeserializeEncryptedFile(data []byte) (*EncryptedFile, error) {
	if len(data) < 20 {
		return nil, errors.New("data too short")
	}
	
	idx := 0
	
	nonceLen := int(data[idx])
	idx++
	nonce := data[idx : idx+nonceLen]
	idx += nonceLen
	
	hashLen := int(data[idx])
	idx++
	hash := data[idx : idx+hashLen]
	idx += hashLen
	
	origSize := int64(uint64(data[idx])<<56 | uint64(data[idx+1])<<48 | uint64(data[idx+2])<<40 | uint64(data[idx+3])<<32 |
		uint64(data[idx+4])<<24 | uint64(data[idx+5])<<16 | uint64(data[idx+6])<<8 | uint64(data[idx+7]))
	idx += 8
	
	compSize := int64(uint64(data[idx])<<56 | uint64(data[idx+1])<<48 | uint64(data[idx+2])<<40 | uint64(data[idx+3])<<32 |
		uint64(data[idx+4])<<24 | uint64(data[idx+5])<<16 | uint64(data[idx+6])<<8 | uint64(data[idx+7]))
	idx += 8
	
	tagLen := int(data[idx])
	idx++
	tag := data[idx : idx+tagLen]
	idx += tagLen
	
	ciphertext := data[idx:]
	
	return &EncryptedFile{
		Nonce:          nonce,
		Ciphertext:     ciphertext,
		Tag:            tag,
		OriginalHash:   hash,
		OriginalSize:   origSize,
		CompressedSize: compSize,
	}, nil
}
