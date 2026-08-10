package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/vectorcore/lcs/internal/gmlc"
)

type locationHandler struct {
	client gmlc.LocationClient
}

// newUUID generates a fallback Idempotency-Key when the browser didn't
// supply one (crypto.randomUUID() is expected to be the normal source).
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "detail": detail},
	})
}

// writeClientError maps a LocationClient error to the console's own error
// envelope, preserving the upstream HTTP status/code when the error came
// back from the location service itself rather than a transport failure.
func writeClientError(w http.ResponseWriter, err error) {
	var se *gmlc.StatusError
	if errors.As(err, &se) {
		writeError(w, se.HTTPStatus, se.Code, se.Detail)
		return
	}
	writeError(w, http.StatusBadGateway, "gmlc_unreachable", err.Error())
}

// submit accepts a single-target location request (see gmlc.SubmitRequest)
// and relays it to the configured LocationClient, injecting a fallback
// Idempotency-Key if the browser didn't send one.
func (h *locationHandler) submit(w http.ResponseWriter, r *http.Request) {
	var req gmlc.SubmitRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("malformed request body: %v", err))
		return
	}
	if req.Target.IMSI == "" && req.Target.MSISDN == "" {
		writeError(w, http.StatusBadRequest, "invalid_target", "target requires imsi or msisdn")
		return
	}
	if req.ServiceType == "" {
		req.ServiceType = "immediate"
	}

	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = newUUID()
	}

	status, err := h.client.Submit(r.Context(), req, idem)
	if err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *locationHandler) get(w http.ResponseWriter, r *http.Request) {
	status, err := h.client.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *locationHandler) cancel(w http.ResponseWriter, r *http.Request) {
	status, err := h.client.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// gmlcStatus surfaces the location service's readiness for a status
// indicator in the UI. It always responds 200 from the console's own
// perspective — "reachable: false" means the far end couldn't be dialed
// at all, distinct from "reachable: true, ready: false".
func (h *locationHandler) gmlcStatus(w http.ResponseWriter, r *http.Request) {
	err := h.client.Ready(r.Context())
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": true, "ready": true})
		return
	}

	var se *gmlc.StatusError
	if errors.As(err, &se) {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": true, "ready": false, "detail": se.Detail})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "detail": err.Error()})
}

// history queries a target's recorded fixes (GMLC's Historic Location
// Immediate service, MLP-only today — see gmlc.LocationClient.History).
// Query params: target_kind (imsi|msisdn, required), target_value
// (required), start (RFC3339, required), stop (RFC3339, optional — a
// missing stop means "up to now").
func (h *locationHandler) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var target gmlc.Target
	switch q.Get("target_kind") {
	case "imsi":
		target.IMSI = q.Get("target_value")
	case "msisdn":
		target.MSISDN = q.Get("target_value")
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "target_kind must be imsi or msisdn")
		return
	}
	if target.IMSI == "" && target.MSISDN == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "target_value is required")
		return
	}
	start, err := time.Parse(time.RFC3339, q.Get("start"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "start is required and must be RFC3339")
		return
	}
	var stop time.Time
	if s := q.Get("stop"); s != "" {
		if stop, err = time.Parse(time.RFC3339, s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "stop must be RFC3339")
			return
		}
	}

	points, err := h.client.History(r.Context(), target, start, stop)
	if err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": points})
}

// emergencyNotConfigured answers every /api/v1/emergency/* route with a
// real 404 when no "emergency" gmlc_clients profile exists — see
// NewServer's own doc comment for why this can't just be left to the SPA
// fallback.
func emergencyNotConfigured(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "emergency_not_configured", "no emergency gmlc_clients profile is configured")
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
