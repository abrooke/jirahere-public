package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"testing"
)

var base64URLNoPadding = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestGenerateVerifier(t *testing.T) {
	v, err := generateVerifier()
	if err != nil {
		t.Fatalf("generateVerifier: %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Errorf("verifier length = %d, want 43-128 per RFC 7636", len(v))
	}
	if !base64URLNoPadding.MatchString(v) {
		t.Errorf("verifier %q contains characters outside base64url", v)
	}

	v2, err := generateVerifier()
	if err != nil {
		t.Fatalf("generateVerifier: %v", err)
	}
	if v == v2 {
		t.Error("two calls to generateVerifier produced the same value")
	}
}

func TestChallengeFromVerifier(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := base64.RawURLEncoding.EncodeToString(sha256sum(verifier))
	got := challengeFromVerifier(verifier)
	if got != want {
		t.Errorf("challengeFromVerifier(%q) = %q, want %q", verifier, got, want)
	}
	if !base64URLNoPadding.MatchString(got) {
		t.Errorf("challenge %q contains characters outside base64url", got)
	}
}

func sha256sum(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func TestGenerateState(t *testing.T) {
	s1, err := generateState()
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	s2, err := generateState()
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	if s1 == s2 {
		t.Error("two calls to generateState produced the same value")
	}
	if !base64URLNoPadding.MatchString(s1) {
		t.Errorf("state %q contains characters outside base64url", s1)
	}
	if len(s1) < 20 {
		t.Errorf("state %q is too short to be unguessable", s1)
	}
}
