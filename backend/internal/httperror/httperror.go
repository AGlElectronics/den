// Package httperror defines typed service errors and an HTTP renderer that
// turns them into a consistent JSON shape: {"error":{"code","message","details"}}.
package httperror

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"
)

// Code is a stable string returned to API clients so they can switch on it.
type Code string

const (
	CodeBadRequest   Code = "bad_request"
	CodeValidation   Code = "validation_failed"
	CodeUnauthorized Code = "unauthorized"
	CodeForbidden    Code = "forbidden"
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeInternal     Code = "internal"
	CodeUnavailable  Code = "unavailable"
	CodeRateLimited  Code = "rate_limited"
)

// Error is the canonical service error type.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.cause != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.cause.Error()
	}
	return string(e.Code) + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.cause }

// New constructs a typed error.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithDetails attaches arbitrary details (e.g. per-field validation issues).
func (e *Error) WithDetails(d any) *Error {
	e.Details = d
	return e
}

// WithCause keeps the original wrapped error for logging while exposing only
// Message to the API client.
func (e *Error) WithCause(cause error) *Error {
	e.cause = cause
	return e
}

// Helpers for common cases.

func NotFound(resource string) *Error { return New(CodeNotFound, resource+" not found") }
func BadRequest(msg string) *Error    { return New(CodeBadRequest, msg) }
func Conflict(msg string) *Error      { return New(CodeConflict, msg) }
func Unauthorized(msg string) *Error  { return New(CodeUnauthorized, msg) }
func Forbidden(msg string) *Error     { return New(CodeForbidden, msg) }
func Validation(msg string, d any) *Error {
	return New(CodeValidation, msg).WithDetails(d)
}

// Unavailable is a dependency (e.g. the operator) failing; msg is shown.
func Unavailable(msg string) *Error { return New(CodeUnavailable, msg) }

// RateLimited is a client calling a route too often; Retry-After says when
// to try again.
func RateLimited() *Error {
	return New(CodeRateLimited, "too many requests, try again later")
}

func Internal(cause error) *Error {
	return New(CodeInternal, "internal server error").WithCause(cause)
}

type response struct {
	Error *Error `json:"error"`
}

// Render writes err as JSON. Anything that isn't *Error is treated as
// internal and logged with the request path.
func Render(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = Internal(err)
	}

	status := statusFor(e.Code)
	if status >= 500 {
		log.Ctx(r.Context()).Error().Err(err).
			Str("path", r.URL.Path).
			Str("method", r.Method).
			Msg("http handler error")
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Error: e})
}

func statusFor(code Code) int {
	switch code {
	case CodeBadRequest:
		return http.StatusBadRequest
	case CodeValidation:
		return http.StatusUnprocessableEntity
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeUnavailable:
		return http.StatusBadGateway
	case CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
