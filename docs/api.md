# GMLC REST/JSON API reference (Le interface)

This is the non-standard interim Le adapter (`internal/httpapi`) — see `docs/architecture.md`
for where it sits relative to Diameter/SLg and the planned OMA MLP adapter. This document
describes exactly what the handlers in `internal/httpapi/api.go` accept and return today;
it is not a spec, it is a reference for building a client (console, script, integration)
against the real, current behavior.

## Authentication

Every endpoint except `/healthz` and `/readyz` requires two headers:

```
X-LCS-Client-ID: <client id>
Authorization: Bearer <token>
```

Both are required together — a request with only one is rejected the same as a request with
neither (`401 unauthenticated`). The client ID/token pair is matched against an
operator-configured client record (`clients[]` in `gmlc.yaml`); there is no self-service
client registration. A valid pair that isn't authorized for the requested target's prefix or
service type gets `403 forbidden`, not `401` — the two are deliberately distinguishable.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness. No auth required. |
| `GET` | `/readyz` | Readiness (Diameter peers up, storage reachable). No auth required. |
| `POST` | `/v1/location-requests` | Submit a location request (single or batch). |
| `GET` | `/v1/location-requests/{id}` | Poll a request's current state/result. |
| `DELETE` | `/v1/location-requests/{id}` | Cancel a request. |

### `GET /healthz`

```json
{"status": "ok"}
```

### `GET /readyz`

`200 {"status": "ready"}`, or `503 not_ready` if a Diameter peer isn't up yet or storage isn't
reachable.

### `POST /v1/location-requests`

Headers:

```
X-LCS-Client-ID: <client id>
Authorization: Bearer <token>
Content-Type: application/json
Idempotency-Key: <opaque string>          (recommended — see below)
```

Body (single target):

```json
{
  "target": {"imsi": "311435000070572"},
  "service_type": "immediate",
  "location_type": "current",
  "priority": 0,
  "qos": {
    "class": "assured",
    "horizontal_accuracy_meters": 50,
    "vertical_accuracy_meters": 20,
    "vertical_requested": false,
    "response_time": "low_delay"
  },
  "callback_url": "https://example.com/gmlc-callback",
  "callback_secret": "a-shared-secret"
}
```

Only `target` and `service_type` are required. Every other field is optional and independent —
send just `{"target": {...}, "service_type": "immediate"}` for the minimal case.

#### `target`

Exactly one of `imsi` or `msisdn` (or both) — at least one identity is required. Each, if
present, must be 1-15 decimal digits (`ErrInvalidTarget` otherwise — `400 invalid_target`).

```json
{"imsi": "311435000070572"}
{"msisdn": "15551234567"}
```

#### `targets` (batch — mutually exclusive with `target`)

```json
{
  "targets": [{"imsi": "311435000070572"}, {"imsi": "311435000070571"}],
  "service_type": "immediate"
}
```

Sending both `target` and `targets`, or neither, is `400 invalid_request`. A batch creates one
independent `location_requests` row per target — there is no batch-grouping id. Response shape
differs (see below); each created request is polled/cancelled individually afterward via the
normal single-request endpoints. Validation is all-or-nothing up front: if any target in the
array is malformed, the whole call is rejected and nothing is created.

#### `service_type`

Only `"immediate"` is currently supported. Anything else is
`400 unsupported_service_type`.

#### `location_type` (optional, default `"current"`)

- `"current"`
- `"current_or_last_known"`

Anything else is `400 invalid_request`.

#### `priority` (optional)

Unsigned integer, TS 29.172 LCS-Priority. Omitted sends no priority AVP upstream.

#### `qos` (optional)

Every child field is independently optional — send only the ones you need.

| Field | Type | Notes |
|---|---|---|
| `class` | `"assured"` \| `"best_effort"` | |
| `horizontal_accuracy_meters` | number | Converted to the TS 23.032 log-scale uncertainty code on encode, floored (never rounded up) so the request never asks for looser accuracy than specified. |
| `vertical_accuracy_meters` | number | |
| `vertical_requested` | boolean | |
| `response_time` | `"low_delay"` \| `"delay_tolerant"` | |

An unrecognized `class` or `response_time` string is `400 invalid_request`.

#### `callback_url` / `callback_secret` (optional — async completion)

Register a webhook for this request's own completion (API-ASYNC). Once the request reaches a
terminal state, the same JSON shape `GET` returns is `POST`ed to `callback_url`, signed:

```
X-GMLC-Signature: sha256=<hex HMAC-SHA256 of the raw body, keyed by callback_secret>
```

Rules:
- Setting one without the other is `400 invalid_request` (`ErrCallbackRequiresSecret`).
- `callback_url` must parse as an absolute `http://` or `https://` URL, or `400 invalid_request`
  (`ErrInvalidCallbackURL`).
- If the GMLC's own `delivery` config section is disabled, either field is
  `400 delivery_not_configured` — there is no encryptor available to protect the secret at rest.
- For a batch (`targets`) submission, both apply to *every* request the call creates, sharing
  one callback destination.
- Delivery retries with exponential backoff up to `delivery.max_attempts`, then gives up
  permanently. There is no separate delivery-status endpoint today — the request's own
  terminal state (via `GET`) is authoritative regardless of whether the callback ever
  succeeded.

#### `Idempotency-Key`

Read from the `Idempotency-Key` header first; if absent, falls back to a body field
`"idempotency_key"`. Strongly recommended for anything that might be retried (network hiccups,
client-side timeouts) — replaying the same key returns the original request
(`200`, not `201/202`) rather than creating a duplicate. For a batch submission, each target
gets a derived key (`"<key>#<index>"`), so retrying an identical batch call is idempotent
per-target.

#### Response — single target

`202 Accepted` if newly created, `200 OK` if this was an idempotent replay of an existing
request. Body is the request status shape (see **Status object** below), always without a
`result` yet (a request is never immediately `completed`).

#### Response — batch (`targets`)

`202` if at least one request in the batch was newly created, `200` if the whole batch was a
replay:

```json
{"requests": [ <status object>, <status object>, ... ]}
```

Array order matches the `targets` array order.

### `GET /v1/location-requests/{id}`

Returns the current **status object** (below) for `id`, scoped to the authenticated client —
`404 not_found` if the id doesn't exist or belongs to a different client (deliberately
indistinguishable, to avoid leaking existence to another client's credentials).

### `DELETE /v1/location-requests/{id}`

Cancels the request if it's in a cancellable state, returns the resulting status object.
`409 invalid_state` if the request has already reached a terminal state.

## The status object

Returned by submit (single), each element of a batch response, `GET`, `DELETE`, and the
async-completion callback body — all four render the exact same shape
(`httpapi.RequestJSON`).

```json
{
  "id": "d819ab8c-025a-4a67-8342-509f3c38b4bf",
  "service_type": "immediate",
  "state": "completed",
  "failure_code": "",
  "location_type": "current",
  "priority": 0,
  "qos": {"class": "assured"},
  "created_at": "2026-08-08T21:20:27.450401952Z",
  "updated_at": "2026-08-08T21:21:28.643519646Z",
  "result": {
    "created_at": "2026-08-08T21:21:28.643426352Z",
    "shape": "ellipsoid_point_uncertainty_circle",
    "latitude": 32.62242078781128,
    "longitude": -86.29533290863037,
    "uncertainty_meters": 11.43588810000001
  }
}
```

| Field | Always present? | Notes |
|---|---|---|
| `id` | yes | |
| `service_type` | yes | |
| `state` | yes | see **States** below |
| `failure_code` | yes | empty string unless `state` is `failed` |
| `location_type` | yes | echoes the effective value, including the `"current"` default |
| `priority` | only if set on submit | |
| `qos` | only if set on submit | only the child fields actually set are echoed |
| `created_at` / `updated_at` | yes | RFC 3339 |
| `result` | only when `state == "completed"` | see below |

### `result` (present only once `state` is `"completed"`)

A completed request always has a result row, but **not every completed request has a position**
— an ECGI-only completion or a Polygon-shaped GAD (no single center point) legitimately has
neither `latitude`/`longitude`. Every field below is independently optional; check for its
presence rather than assuming the whole object is uniform:

| Field | When present |
|---|---|
| `created_at` | always |
| `shape` | when the network returned a decodable GAD shape |
| `latitude` / `longitude` | when the shape has a single center point (not Polygon) |
| `ecgi` | ECGI-only completions |
| `uncertainty_meters` | circle shape |
| `semi_major_meters` / `semi_minor_meters` / `orientation_degrees` / `confidence_percent` | ellipse shape |
| `age_of_location_estimate_minutes` | if the network sent Age-Of-Location-Estimate |
| `accuracy_fulfilment` | if the network sent Accuracy-Fulfilment-Indicator |

`shape` is one of the GAD shapes `internal/gad` currently decodes:
`ellipsoid_point`, `ellipsoid_point_uncertainty_circle`, `ellipsoid_point_uncertainty_ellipse`,
`polygon`. Altitude variants, arc, and high-accuracy shapes are not decoded by the GMLC itself
today and will not appear here even if the network sends them (the raw bytes are retained
server-side for diagnostics only, not exposed over this API).

### States

| State | Meaning |
|---|---|
| `queued` | Accepted, waiting for the single worker to dispatch it. |
| `resolving` | Looking up the serving MME (SLh). |
| `locating` | SLg PLR sent, waiting on the network's PLA. |
| `completed` | Terminal — see `result`. |
| `failed` | Terminal — see `failure_code`. |
| `cancelled` | Terminal — caller cancelled via `DELETE`. |
| `expired` | Terminal — retention swept it before completion. |
| `indeterminate` | Terminal — reached only in narrow edge cases; treat like `failed` with no further detail. |

`failure_code` values observed in practice include `network_failure` (generic — the network
answered but positioning did not succeed, or an unretriable Diameter error),
`temporarily_unavailable` (retry budget exhausted against a transient Diameter/timeout
condition), and `no_immediate_result` (the network's answer had no location to report, e.g. a
deferred/no-immediate-result PLA). This is not an exhaustive enum — treat `failure_code` as a
diagnostic string, not a fixed set to switch on.

## Errors

Every error response has the same envelope:

```json
{"error": {"code": "invalid_target", "detail": "IMSI must contain only decimal digits: invalid target identity"}}
```

| HTTP | `code` | Cause |
|---|---|---|
| 400 | `invalid_request` | Malformed JSON, unknown field, bad `location_type`/`qos`/callback input, empty batch |
| 400 | `invalid_target` | `target`/`targets` identity fails validation |
| 400 | `unsupported_service_type` | `service_type` isn't `"immediate"` |
| 400 | `idempotency_required` | Endpoint requires an idempotency key and none was supplied |
| 400 | `delivery_not_configured` | `callback_url`/`callback_secret` set but delivery isn't enabled server-side |
| 401 | `unauthenticated` | Missing or invalid credentials |
| 403 | `forbidden` | Valid credentials, not authorized for this target/service |
| 404 | `not_found` | Unknown id, or an id belonging to a different client |
| 409 | `invalid_state` | e.g. cancelling an already-terminal request |
| 500 | `internal_error` | Unexpected server-side failure — detail text is deliberately generic and never discloses internals |

Request bodies are decoded with `DisallowUnknownFields` — an unrecognized field in the JSON body
is a `400 invalid_request`, not silently ignored.

## Examples

Minimal single-target request:

```bash
curl -s -X POST http://<gmlc-host>:8086/v1/location-requests \
  -H "X-LCS-Client-ID: example-lcs-client" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"target":{"imsi":"311435000070572"},"service_type":"immediate"}'
```

Poll until terminal:

```bash
curl -s http://<gmlc-host>:8086/v1/location-requests/<id> \
  -H "X-LCS-Client-ID: example-lcs-client" \
  -H "Authorization: Bearer <token>"
```

Batch with QoS and an async callback:

```bash
curl -s -X POST http://<gmlc-host>:8086/v1/location-requests \
  -H "X-LCS-Client-ID: example-lcs-client" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "targets": [{"imsi":"311435000070572"}, {"imsi":"311435000070571"}],
    "service_type": "immediate",
    "qos": {"class": "assured", "horizontal_accuracy_meters": 50},
    "callback_url": "https://example.com/gmlc-callback",
    "callback_secret": "a-shared-secret"
  }'
```

## What a console needs to know that this API doesn't expose

- **No push/streaming.** There is no WebSocket/SSE — a console has to poll `GET` until a
  terminal state, or rely on the async callback (`callback_url`) if it can host a public
  endpoint to receive it.
- **No list/search endpoint.** There is no `GET /v1/location-requests` — only lookup by known
  `id`. A console that wants a history view needs to keep its own record of ids it has
  submitted (or gain a new endpoint — not present today).
- **No delivery-status visibility.** If a callback is registered, there's no way to ask "did the
  webhook actually fire yet / how many attempts" — only the request's own state.
- **`location_type`/`priority`/`qos` are per-request, not defaultable server-side per client** —
  every submit call must set what it wants.
