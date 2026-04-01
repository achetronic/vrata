// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package encrypt

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSealAndOpen(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("sensitive secret value")
	ct, err := c.Seal(plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if bytes.Equal(ct, plaintext) {
		t.Error("ciphertext should differ from plaintext")
	}

	got, err := c.Open(ct)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("expected %q, got %q", plaintext, got)
	}
}

func TestWrongKeyFails(t *testing.T) {
	c1, _ := NewCipher(testKey(t))
	c2, _ := NewCipher(testKey(t))

	ct, _ := c1.Seal([]byte("secret"))
	_, err := c2.Open(ct)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestTamperedDataFails(t *testing.T) {
	c, _ := NewCipher(testKey(t))
	ct, _ := c.Seal([]byte("secret"))

	ct[len(ct)-1] ^= 0xff

	_, err := c.Open(ct)
	if err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestShortCiphertextFails(t *testing.T) {
	c, _ := NewCipher(testKey(t))
	_, err := c.Open([]byte("short"))
	if err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}

func TestInvalidKeyLength(t *testing.T) {
	_, err := NewCipher([]byte("too-short"))
	if err == nil {
		t.Fatal("expected error for invalid key length")
	}
}

func TestEmptyPlaintext(t *testing.T) {
	c, _ := NewCipher(testKey(t))
	ct, err := c.Seal([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Open(ct)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d bytes", len(got))
	}
}

func TestDifferentNoncePerSeal(t *testing.T) {
	c, _ := NewCipher(testKey(t))
	ct1, _ := c.Seal([]byte("same"))
	ct2, _ := c.Seal([]byte("same"))

	if bytes.Equal(ct1, ct2) {
		t.Error("two seals of same plaintext should produce different ciphertext (different nonce)")
	}
}
