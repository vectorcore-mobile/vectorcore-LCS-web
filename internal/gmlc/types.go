package gmlc

import (
	"context"
	"fmt"
	"time"
)

// These types are the console's own stable contract, decoupled from the
// wire shapes MLPClient actually speaks (OMA MLP over XML, TS
// 29.171/29.172) — see LocationClient below. The console's own /api/v1
// handlers and the frontend only ever deal in these.

type Target struct {
	IMSI   string `json:"imsi,omitempty"`
	MSISDN string `json:"msisdn,omitempty"`
}

type QoS struct {
	Class                    string   `json:"class,omitempty"`
	HorizontalAccuracyMeters *float64 `json:"horizontal_accuracy_meters,omitempty"`
	VerticalAccuracyMeters   *float64 `json:"vertical_accuracy_meters,omitempty"`
	VerticalRequested        *bool    `json:"vertical_requested,omitempty"`
	ResponseTime             string   `json:"response_time,omitempty"`
}

// SubmitRequest is a single-target location request. The GMLC API also
// supports a batch `targets` form, but this console only ever submits one
// target at a time, so batching isn't modeled here.
type SubmitRequest struct {
	Target       Target  `json:"target"`
	ServiceType  string  `json:"service_type"`
	LocationType string  `json:"location_type,omitempty"`
	Priority     *uint64 `json:"priority,omitempty"`
	QoS          *QoS    `json:"qos,omitempty"`
}

// Result is only present once a RequestStatus's State is "completed".
// Every field is independently optional.
type Result struct {
	CreatedAt                    string   `json:"created_at,omitempty"`
	Shape                        string   `json:"shape,omitempty"`
	Latitude                     *float64 `json:"latitude,omitempty"`
	Longitude                    *float64 `json:"longitude,omitempty"`
	ECGI                         string   `json:"ecgi,omitempty"`
	UncertaintyMeters            *float64 `json:"uncertainty_meters,omitempty"`
	SemiMajorMeters              *float64 `json:"semi_major_meters,omitempty"`
	SemiMinorMeters              *float64 `json:"semi_minor_meters,omitempty"`
	OrientationDegrees           *float64 `json:"orientation_degrees,omitempty"`
	ConfidencePercent            *float64 `json:"confidence_percent,omitempty"`
	AgeOfLocationEstimateMinutes *float64 `json:"age_of_location_estimate_minutes,omitempty"`
	AccuracyFulfilment           string   `json:"accuracy_fulfilment,omitempty"`
}

// RequestStatus is the shape returned by submit, GET, DELETE, and the
// async callback — the console's own "status object".
type RequestStatus struct {
	ID           string  `json:"id"`
	ServiceType  string  `json:"service_type"`
	State        string  `json:"state"`
	FailureCode  string  `json:"failure_code"`
	LocationType string  `json:"location_type"`
	Priority     *uint64 `json:"priority,omitempty"`
	QoS          *QoS    `json:"qos,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	Result       *Result `json:"result,omitempty"`
}

// HistoryPoint is one recorded fix returned by LocationClient.History,
// oldest first — the subset of Result a Historic Location Immediate query
// (MLP hlir/hlia, GMLC Phase C) actually carries: point + circular
// uncertainty only, no shape/ellipse fields, matching what GMLC's own
// location_history table stores.
type HistoryPoint struct {
	RecordedAt        string   `json:"recorded_at"`
	Latitude          *float64 `json:"latitude,omitempty"`
	Longitude         *float64 `json:"longitude,omitempty"`
	UncertaintyMeters *float64 `json:"uncertainty_meters,omitempty"`
}

// StatusError is returned by a LocationClient when the GMLC (or whatever
// sits behind LocationClient) answers with a non-2xx response. HTTPStatus
// is the status this console's own API should reflect back to the browser.
type StatusError struct {
	HTTPStatus int
	Code       string
	Detail     string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

// LocationClient is what the console's HTTP handlers depend on. It's
// implemented by MLPClient, which speaks the real Le standard (OMA MLP
// over XML/HTTP, TS 29.171/29.172) — the console's own API and frontend
// never need to know anything about the wire protocol behind it.
type LocationClient interface {
	Submit(ctx context.Context, req SubmitRequest, idempotencyKey string) (*RequestStatus, error)
	Get(ctx context.Context, id string) (*RequestStatus, error)
	Cancel(ctx context.Context, id string) (*RequestStatus, error)
	// Ready reports whether the underlying location service is currently
	// usable (e.g. the GMLC's Diameter peers/storage), returning a
	// StatusError describing why not, or nil if it's ready.
	Ready(ctx context.Context) error
	// History queries target's recorded fixes within [start, stop]
	// (stop's zero value means "up to now"), oldest first. Backed by
	// GMLC's Historic Location Immediate service (see GMLC's Phase C). An
	// empty, non-error result means the query succeeded but nothing has
	// been recorded for target yet — not the same as a StatusError.
	History(ctx context.Context, target Target, start, stop time.Time) ([]HistoryPoint, error)
}
