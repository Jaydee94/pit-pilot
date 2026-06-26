// backend/internal/httpapi/render.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func WriteError(w http.ResponseWriter, err error) {
	if e, ok := apperr.As(err); ok {
		WriteJSON(w, e.HTTPStatus, map[string]any{
			"error": map[string]string{"code": e.Code, "message": e.Message},
		})
		return
	}
	WriteJSON(w, http.StatusInternalServerError, map[string]any{
		"error": map[string]string{"code": "internal", "message": "internal error"},
	})
}
