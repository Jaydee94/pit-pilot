package apperr

import (
	"errors"
	"net/http"
)

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newError(status int, code, msg string) *Error {
	return &Error{Code: code, Message: msg, HTTPStatus: status}
}

func NotFound(code, msg string) *Error     { return newError(http.StatusNotFound, code, msg) }
func Forbidden(code, msg string) *Error     { return newError(http.StatusForbidden, code, msg) }
func BadRequest(code, msg string) *Error    { return newError(http.StatusBadRequest, code, msg) }
func Conflict(code, msg string) *Error      { return newError(http.StatusConflict, code, msg) }
func Unauthorized(code, msg string) *Error  { return newError(http.StatusUnauthorized, code, msg) }

// As unwraps err to a *Error if present anywhere in the chain.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
