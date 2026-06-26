package httpapi

import (
	"net/http"

	"github.com/google/uuid"
)

// WithUserIDForTest injects a user id into the request context (test-only helper).
func WithUserIDForTest(r *http.Request, id uuid.UUID) *http.Request { return withUserID(r, id) }
