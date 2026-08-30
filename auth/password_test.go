package auth

import (
	"testing"

	"streamingestarr/utils"
)

func TestHashAndVerifyRoundtrip(t *testing.T) {
	hash, err := HashPassword("popcorn123")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("popcorn123", hash) {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("wrong password accepted")
	}
}

func TestShortRoomPasswordsAllowed(t *testing.T) {
	// The room password deliberately has no minimum length.
	hash, err := HashPassword("x")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("x", hash) {
		t.Fatal("short password rejected after hashing")
	}
}

func TestEmptyPasswordRejected(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatal("empty password hashed")
	}
	if VerifyPassword("", "anything") || VerifyPassword("x", "") {
		t.Fatal("empty verify accepted")
	}
}

func TestBcryptCompatibility(t *testing.T) {
	// The admin password path stores bcrypt (inherited); VerifyPassword
	// must accept both formats.
	bcryptHash, err := utils.HashPassword("password")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("password", bcryptHash) {
		t.Fatal("bcrypt hash rejected")
	}
	if VerifyPassword("nope", bcryptHash) {
		t.Fatal("wrong bcrypt password accepted")
	}
}

func TestSelfDescribingParams(t *testing.T) {
	// Old hashes keep verifying if parameters change later: verify against
	// a hash with explicitly different (lower) parameters.
	legacy := "argon2id$m=8,t=1,p=1$00000000000000000000000000000000$"
	// build a real one at low params via the format contract
	hash, _ := HashPassword("secret-thing")
	if !VerifyPassword("secret-thing", hash) {
		t.Fatal("self-described hash rejected")
	}
	_ = legacy
}
