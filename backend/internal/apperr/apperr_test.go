package apperr

import (
	"fmt"
	"net/http"
	"testing"
)

func TestConstructorsSetStatus(t *testing.T) {
	cases := []struct {
		err  *Error
		want int
	}{
		{NotFound("group_not_found", "x"), http.StatusNotFound},
		{Forbidden("not_member", "x"), http.StatusForbidden},
		{BadRequest("invalid", "x"), http.StatusBadRequest},
		{Conflict("dup", "x"), http.StatusConflict},
		{Unauthorized("no_session", "x"), http.StatusUnauthorized},
	}
	for _, c := range cases {
		if c.err.HTTPStatus != c.want {
			t.Fatalf("%s: status %d, want %d", c.err.Code, c.err.HTTPStatus, c.want)
		}
	}
}

func TestAsUnwrapsWrapped(t *testing.T) {
	base := NotFound("group_not_found", "missing")
	wrapped := fmt.Errorf("service: %w", base)
	got, ok := As(wrapped)
	if !ok || got.Code != "group_not_found" {
		t.Fatalf("As failed to unwrap: %v %v", got, ok)
	}
}
