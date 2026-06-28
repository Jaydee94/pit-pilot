package password_test

import (
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/password"
)

func TestHashVerifyRoundtrip(t *testing.T) {
	h, err := password.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := password.Verify("correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatalf("verify should succeed: ok=%v err=%v", ok, err)
	}
	bad, err := password.Verify("wrong password", h)
	if err != nil || bad {
		t.Fatalf("verify should fail for wrong password: ok=%v err=%v", bad, err)
	}
}

func TestHashIsSalted(t *testing.T) {
	a, _ := password.Hash("samepw12")
	b, _ := password.Hash("samepw12")
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if ok, err := password.Verify("x", "not-a-phc-string"); ok || err == nil {
		t.Fatalf("expected rejection, got ok=%v err=%v", ok, err)
	}
}
