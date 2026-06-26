package auth

import (
	"context"
	"testing"
)

func TestOIDCVerifierRejectsUnknownProvider(t *testing.T) {
	v := &OIDCVerifier{} // verifiers map nil → unknown provider path
	if _, err := v.Verify(context.Background(), "facebook", "x"); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
