package security

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash := HashPassword("correct-horse")
	if hash == "correct-horse" {
		t.Fatal("password hash must not store plaintext")
	}
	if !IsHashedPassword(hash) {
		t.Fatalf("expected hashed password prefix, got %q", hash)
	}
	if !VerifyPassword(hash, "correct-horse") {
		t.Fatal("expected password to verify")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("wrong password verified")
	}
}

func TestVerifyPasswordPlaintextFallback(t *testing.T) {
	if !VerifyPassword("legacy", "legacy") {
		t.Fatal("legacy plaintext password should verify")
	}
	if VerifyPassword("legacy", "other") {
		t.Fatal("wrong legacy plaintext password verified")
	}
}

func TestNewToken(t *testing.T) {
	token := NewToken(8)
	if len(token) != 16 {
		t.Fatalf("expected 16 hex chars, got %d", len(token))
	}
}
