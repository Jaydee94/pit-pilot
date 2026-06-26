package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func fixedNow() time.Time { return time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC) }

func TestSessionRoundTrip(t *testing.T) {
	m := NewSessionManager([]byte("k"), time.Hour, fixedNow)
	id := uuid.New()
	tok, err := m.Issue(id)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	got, err := m.Validate(tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got != id {
		t.Fatalf("round-trip mismatch: %s != %s", got, id)
	}
}

func TestSessionRejectsExpired(t *testing.T) {
	m := NewSessionManager([]byte("k"), time.Hour, fixedNow)
	tok, _ := m.Issue(uuid.New())
	expired := NewSessionManager([]byte("k"), time.Hour,
		func() time.Time { return fixedNow().Add(2 * time.Hour) })
	if _, err := expired.Validate(tok); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestSessionRejectsWrongKey(t *testing.T) {
	tok, _ := NewSessionManager([]byte("k1"), time.Hour, fixedNow).Issue(uuid.New())
	if _, err := NewSessionManager([]byte("k2"), time.Hour, fixedNow).Validate(tok); err == nil {
		t.Fatal("expected wrong-key token to be rejected")
	}
}
