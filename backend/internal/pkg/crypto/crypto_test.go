package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testKey() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher(testKey())
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	plaintext := []byte("apiVersion: v1\nkind: Config\n")
	sealed, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	parts := strings.Split(sealed, ":")
	if len(parts) != 3 || parts[0] != "v1" {
		t.Fatalf("ciphertext format = %q, want v1:<nonce>:<ciphertext>", sealed)
	}
	got, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("round trip mismatch: got %q", string(got))
	}
}

func TestEncryptNonceIsRandom(t *testing.T) {
	c, _ := NewCipher(testKey())
	a, _ := c.Encrypt([]byte("same"))
	b, _ := c.Encrypt([]byte("same"))
	if a == b {
		t.Fatal("nonce must be random per encryption")
	}
}

func TestNewCipherRejectsBadKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "not base64", key: "!!!"},
		{name: "short key", key: base64.StdEncoding.EncodeToString(make([]byte, 16))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewCipher(tt.key); err == nil {
				t.Fatal("expected error for invalid master key")
			}
		})
	}
}

func TestDecryptRejectsMalformedInput(t *testing.T) {
	c, _ := NewCipher(testKey())
	tests := []struct {
		name   string
		sealed string
	}{
		{name: "no version prefix", sealed: "AAAA:BBBB"},
		{name: "wrong version", sealed: "v2:AAAA:BBBB"},
		{name: "garbage ciphertext", sealed: "v1:AAAA:not-base64!!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.Decrypt(tt.sealed); err == nil {
				t.Fatal("expected error for malformed ciphertext")
			}
		})
	}
}

func TestGenerateMasterKeyBase64(t *testing.T) {
	k, err := GenerateMasterKeyBase64()
	if err != nil {
		t.Fatalf("GenerateMasterKeyBase64: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(k)
	if err != nil || len(raw) != 32 {
		t.Fatalf("key must decode to 32 bytes, got len=%d err=%v", len(raw), err)
	}
}
