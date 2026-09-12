// Package crypto 提供基于 AES-256-GCM 的敏感数据(如 kubeconfig)加解密。
//
// 密文格式为 "v1:<base64(nonce)>:<base64(ciphertext)>",版本前缀支撑未来算法轮转。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const (
	versionV1  = "v1"
	keyLen     = 32
	nonceLen   = 12
	formatPart = 3
)

// Cipher 持有主密钥派生的 AES-GCM 实例,并发安全。
type Cipher struct {
	gcm cipher.AEAD
}

// NewCipher 用 32 字节 base64 编码的主密钥构造 Cipher。
func NewCipher(masterKeyB64 string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(key) != keyLen {
		return nil, fmt.Errorf("master key must be %d bytes, got %d", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Cipher{gcm: gcm}, nil
}

// Encrypt 加密 plaintext,输出 "v1:<nonce>:<ciphertext>" 格式(均为 base64)。
func (c *Cipher) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ct := c.gcm.Seal(nil, nonce, plaintext, nil)
	return versionV1 + ":" +
		base64.StdEncoding.EncodeToString(nonce) + ":" +
		base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt 解密 Encrypt 的输出。
func (c *Cipher) Decrypt(sealed string) ([]byte, error) {
	parts := strings.Split(sealed, ":")
	if len(parts) != formatPart || parts[0] != versionV1 {
		return nil, fmt.Errorf("invalid ciphertext format")
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	plain, err := c.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return plain, nil
}

// GenerateMasterKeyBase64 生成随机 32 字节并返回 base64 编码,用于零配置启动时的开发密钥。
func GenerateMasterKeyBase64() (string, error) {
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("generate master key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
