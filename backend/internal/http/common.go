package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AGlElectronics/den/backend/internal/authbase"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

// decodeJSON reads one JSON object into dst with the shared decoder: at most
// 1 MiB (413 request_too_large), unknown fields refused (400 bad_request).
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	err := authbase.DecodeJSON(w, r, dst, authbase.DecodeOptions{DisallowUnknownFields: true})
	var de *authbase.DecodeError
	if errors.As(err, &de) {
		return httperror.New(httperror.Code(de.Code), de.Message).WithCause(err)
	}
	return err
}

// orgRef is defaultOrgID as the audit_events.organization_id value.
func orgRef() *uuid.UUID {
	id := defaultOrgID
	return &id
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httperror.BadRequest("invalid " + name + " id")
	}
	return id, nil
}
