// MLPClient implements LocationClient against the GMLC's real Le adapter:
// OMA MLP (Mobile Location Protocol) v3.5 over XML/HTTP — internal/mlp on
// the GMLC side. It only ever speaks Standard Location Immediate Service
// (slir/slia): the console never needs Emergency (eme_lir) directly, since
// GMLC resolves LCS-Client-Type purely from which client credential
// authenticated, not from which MLP element was used — see L3 in
// docs/mlp-le-interface-plan.md. This is also why an "emergency" profile
// is just a separate set of MLP credentials, not a different protocol.
package gmlc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- wire types, mirroring GMLC's own internal/mlp/dtd.go. There is no
// shared module between the two repos, so this is an independent,
// deliberately reduced re-implementation of the same OMA MLP v3.5 subset
// GMLC's Phase A speaks — see that package's own dtd.go for the
// authoritative spec citations.

type mlpSvcInit struct {
	XMLName xml.Name `xml:"svc_init"`
	Ver     string   `xml:"ver,attr"`
	Hdr     mlpHdr   `xml:"hdr"`
	// Slir is a pointer so Ready's probe body can omit it entirely — an
	// svc_init with no recognized service element at all is what actually
	// exercises GMLC's GEM fallback path, rather than an empty-but-present
	// <slir> (which GMLC would instead decode as a malformed slir request
	// and answer with a top-level format-error slia, HTTP 200).
	Slir *mlpSlir `xml:"slir,omitempty"`
	Hlir *mlpHlir `xml:"hlir,omitempty"`
}
type mlpHdr struct {
	Ver    string    `xml:"ver,attr"`
	Client mlpClient `xml:"client"`
}
type mlpClient struct {
	ID  string `xml:"id"`
	Pwd string `xml:"pwd"`
}
type mlpMsid struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}
type mlpMsids struct {
	Msid []mlpMsid `xml:"msid"`
}
type mlpSlir struct {
	Ver     string      `xml:"ver,attr"`
	ResType string      `xml:"res_type,attr"`
	Msids   mlpMsids    `xml:"msids"`
	EQoP    *mlpEQoP    `xml:"eqop,omitempty"`
	LocType *mlpLocType `xml:"loc_type,omitempty"`
	Prio    *mlpPrio    `xml:"prio,omitempty"`
}
type mlpEQoP struct {
	RespReq *mlpRespReq `xml:"resp_req,omitempty"`
	HorAcc  *mlpHorAcc  `xml:"hor_acc,omitempty"`
}
type mlpRespReq struct {
	Type string `xml:"type,attr"`
}
type mlpHorAcc struct {
	Value string `xml:",chardata"`
}
type mlpLocType struct {
	Type string `xml:"type,attr"`
}
type mlpPrio struct {
	Type string `xml:"type,attr"`
}

type mlpSvcResult struct {
	XMLName xml.Name `xml:"svc_result"`
	Slia    *mlpSlia `xml:"slia"`
	Hlia    *mlpHlia `xml:"hlia"`
}

// mlpHlir is the Historic Location Immediate Request (§5.2.3.8.1),
// reduced to what GMLC's own hlir handler reads: a single msid,
// start_time (required), stop_time (optional — omitted means "up to
// now"). interval/no_of_reports/geo_info/qop are all valid per the DTD but
// not sent — the console has no UI concept of thinning/capping a history
// view yet, and GMLC only ever produces WGS84 anyway.
type mlpHlir struct {
	Ver       string  `xml:"ver,attr"`
	Msid      mlpMsid `xml:"msid"`
	StartTime string  `xml:"start_time"`
	StopTime  string  `xml:"stop_time,omitempty"`
}

// mlpHlia is the Historic Location Immediate Answer (§5.2.3.8.2):
// (pos+ | req_id | (result, add_info?)) — reusing mlpPos exactly as mlpSlia
// does; multiple pos entries mean multiple points in time for the one
// queried target, not multiple targets.
type mlpHlia struct {
	Pos     []mlpPos   `xml:"pos"`
	Result  *mlpResult `xml:"result,omitempty"`
	AddInfo string     `xml:"add_info,omitempty"`
}
type mlpSlia struct {
	Pos     []mlpPos   `xml:"pos"`
	Result  *mlpResult `xml:"result,omitempty"`
	AddInfo string     `xml:"add_info,omitempty"`
}
type mlpPos struct {
	Msid   mlpMsid    `xml:"msid"`
	Pd     *mlpPd     `xml:"pd,omitempty"`
	PosErr *mlpPosErr `xml:"poserr,omitempty"`
}
type mlpPd struct {
	Time  mlpTimeElem `xml:"time"`
	Shape mlpShape    `xml:"shape"`
}
type mlpTimeElem struct {
	UtcOff string `xml:"utc_off,attr"`
	Value  string `xml:",chardata"`
}
type mlpShape struct {
	CircularArea *mlpCircularArea `xml:"CircularArea,omitempty"`
}
type mlpCircularArea struct {
	Coord  mlpCoord `xml:"coord"`
	Radius string   `xml:"radius"`
}
type mlpCoord struct {
	X string `xml:"X"`
	Y string `xml:"Y"`
}
type mlpPosErr struct {
	Result mlpResult   `xml:"result"`
	Time   mlpTimeElem `xml:"time"`
}
type mlpResult struct {
	ResID string `xml:"resid,attr"`
	Value string `xml:",chardata"`
}

// mlpGem is a standalone root document (not wrapped in svc_result, per
// §5.6.1) sent as an HTTP 404 for any request GMLC's MLP listener doesn't
// recognize.
type mlpGem struct {
	XMLName xml.Name  `xml:"gem"`
	Result  mlpResult `xml:"result"`
	AddInfo string    `xml:"add_info,omitempty"`
}

const mlpVersion = "3.5.0"

var dmshPattern = regexp.MustCompile(`^(\d+) (\d+) (\d+(?:\.\d+)?)([NSEW])$`)

// decodeDMSH parses MLP's DMSH (degree-minute-second-hemisphere)
// coordinate format back to decimal degrees — only the decode direction is
// needed, since MLPClient only ever reads coordinates out of an slia, never
// sends them.
func decodeDMSH(s string) (float64, error) {
	m := dmshPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("mlp: invalid DMSH coordinate %q", s)
	}
	deg, _ := strconv.ParseFloat(m[1], 64)
	min, _ := strconv.ParseFloat(m[2], 64)
	sec, _ := strconv.ParseFloat(m[3], 64)
	v := deg + min/60 + sec/3600
	if m[4] == "S" || m[4] == "W" {
		v = -v
	}
	return v, nil
}

// parseMLPTime parses MLP's compact time format ("20020623134453") as
// UTC — GMLC's own slia responses always set utc_off="+0000" (see its
// buildPos), so treating the value as UTC directly is safe for this
// client, which only ever talks to this GMLC.
func parseMLPTime(s string) (time.Time, error) {
	return time.ParseInLocation("20060102150405", strings.TrimSpace(s), time.UTC)
}

// formatMLPTime renders a timestamp in MLP's compact time format
// (§5.3.133), e.g. "20020623134453" — the encode direction, needed for
// History's own start_time/stop_time (parseMLPTime is the decode
// direction, needed for Submit's pd.time).
func formatMLPTime(t time.Time) string {
	return t.UTC().Format("20060102150405")
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// MLPClient implements LocationClient by speaking OMA MLP directly to a
// GMLC's port-9210 (or wherever configured) listener.
//
// Two real protocol gaps, not glossed over: MLP's Standard Location
// Immediate service has no by-id GET and no cancellation message — there
// is nothing on the wire for Get/Cancel to call. Since slir/slia is
// synchronous request/response, Get just returns the last slia result
// already held in memory for that id (Submit already blocked until it
// arrived — there's nothing left to poll), and Cancel always answers with
// a StatusError saying so; internal/api's existing writeClientError
// already knows how to surface that without any new UI work — the
// console's "Cancel Request" button just won't do anything useful against
// a request that used the MLP path. Results only live as long as this
// process does; a console restart forgets every previously-submitted MLP
// request's id, which REST-submitted requests don't suffer from (the GMLC
// itself remains the source of truth there).
type MLPClient struct {
	baseURL     string
	clientID    string
	bearerToken string
	httpClient  *http.Client

	mu      sync.Mutex
	results map[string]*RequestStatus
}

// NewMLP returns a LocationClient speaking OMA MLP v3.5 to a GMLC's MLP
// listener.
func NewMLP(baseURL, clientID, bearerToken string, timeout time.Duration) *MLPClient {
	return &MLPClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		clientID:    clientID,
		bearerToken: bearerToken,
		httpClient:  &http.Client{Timeout: timeout},
		results:     map[string]*RequestStatus{},
	}
}

var _ LocationClient = (*MLPClient)(nil)

// residStatus maps GMLC's own MLP §5.4 resid values (see
// internal/mlp/resultcodes.go on the GMLC side) to a console-facing
// StatusError shape — used only for a whole-request-level slia <result>
// (an outright rejection, nothing dispatched at all). A per-target
// <poserr> is not an error at all; it's a normal "failed" RequestStatus,
// exactly like the REST API's own state=failed — see buildFailedStatus.
var residStatus = map[string]struct {
	httpStatus int
	code       string
}{
	"3":   {http.StatusUnauthorized, "unauthorized"},
	"4":   {http.StatusNotFound, "unknown_subscriber"},
	"6":   {http.StatusBadGateway, "position_method_failure"},
	"7":   {http.StatusGatewayTimeout, "timeout"},
	"105": {http.StatusBadRequest, "invalid_request"},
	"108": {http.StatusBadGateway, "service_not_supported"},
	"110": {http.StatusBadRequest, "invalid_request"},
	"113": {http.StatusBadRequest, "invalid_request"},
}

func statusErrorForResult(r mlpResult) *StatusError {
	m, ok := residStatus[r.ResID]
	if !ok {
		return &StatusError{HTTPStatus: http.StatusBadGateway, Code: "mlp_error", Detail: fmt.Sprintf("resid %s: %s", r.ResID, r.Value)}
	}
	return &StatusError{HTTPStatus: m.httpStatus, Code: m.code, Detail: r.Value}
}

func targetToMsid(t Target) (mlpMsid, error) {
	if t.IMSI != "" {
		return mlpMsid{Type: "IMSI", Value: t.IMSI}, nil
	}
	if t.MSISDN != "" {
		return mlpMsid{Type: "MSISDN", Value: t.MSISDN}, nil
	}
	return mlpMsid{}, fmt.Errorf("target requires imsi or msisdn")
}

// buildSlir renders req as an slir wire request. Only the fields MLP's
// slir DTD (and GMLC's own reduced Phase A implementation of it) actually
// carries have a mapping — qos.class, qos.vertical_accuracy_meters, and
// qos.vertical_requested have no MLP eqop equivalent (TS 29.172's
// assured/best_effort QoS-class concept and vertical accuracy don't exist
// in MLP's DTD) and are silently not sent, a genuine protocol gap rather
// than an oversight — GMLC's own MLP handler wouldn't read them even if
// present.
func buildSlir(req SubmitRequest) (mlpSlir, error) {
	m, err := targetToMsid(req.Target)
	if err != nil {
		return mlpSlir{}, err
	}
	s := mlpSlir{Ver: mlpVersion, ResType: "SYNC", Msids: mlpMsids{Msid: []mlpMsid{m}}}
	switch req.LocationType {
	case "", "current":
	case "current_or_last_known":
		s.LocType = &mlpLocType{Type: "CURRENT_OR_LAST"}
	default:
		return mlpSlir{}, fmt.Errorf("unsupported location_type %q over MLP", req.LocationType)
	}
	if req.Priority != nil && *req.Priority == 0 {
		s.Prio = &mlpPrio{Type: "HIGH"}
	}
	if req.QoS != nil {
		var e mlpEQoP
		var any bool
		if req.QoS.HorizontalAccuracyMeters != nil {
			e.HorAcc = &mlpHorAcc{Value: strconv.FormatFloat(*req.QoS.HorizontalAccuracyMeters, 'g', -1, 64)}
			any = true
		}
		switch req.QoS.ResponseTime {
		case "":
		case "low_delay":
			e.RespReq = &mlpRespReq{Type: "LOW_DELAY"}
			any = true
		case "delay_tolerant":
			e.RespReq = &mlpRespReq{Type: "DELAY_TOL"}
			any = true
		}
		if any {
			s.EQoP = &e
		}
	}
	return s, nil
}

func (c *MLPClient) post(ctx context.Context, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/xml")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("mlp request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read mlp response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

// buildFailedStatus renders a per-target poserr as a normal "failed"
// RequestStatus — not a Submit error: GMLC dispatched the request, it
// just didn't resolve to a position.
func buildFailedStatus(id string, req SubmitRequest, posErr *mlpPosErr, now string) *RequestStatus {
	return &RequestStatus{
		ID: id, ServiceType: "immediate", State: "failed",
		FailureCode:  fmt.Sprintf("%s: %s", posErr.Result.ResID, posErr.Result.Value),
		LocationType: req.LocationType, Priority: req.Priority, QoS: req.QoS,
		CreatedAt: now, UpdatedAt: now,
	}
}

func (c *MLPClient) Submit(ctx context.Context, req SubmitRequest, idempotencyKey string) (*RequestStatus, error) {
	// idempotencyKey is a REST/JSON-adapter concept (an HTTP header GMLC's
	// httpapi de-duplicates on); MLP's slir DTD has no equivalent field,
	// and since Submit blocks until the slia arrives synchronously, there
	// is no retry/replay window for it to protect against here anyway.
	slir, err := buildSlir(req)
	if err != nil {
		return nil, &StatusError{HTTPStatus: http.StatusBadRequest, Code: "invalid_request", Detail: err.Error()}
	}
	envelope := mlpSvcInit{Ver: mlpVersion, Hdr: mlpHdr{Ver: mlpVersion, Client: mlpClient{ID: c.clientID, Pwd: c.bearerToken}}, Slir: &slir}
	body, err := xml.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode slir: %w", err)
	}

	status, respBody, err := c.post(ctx, append([]byte(xml.Header), body...))
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		var g mlpGem
		if xml.Unmarshal(respBody, &g) == nil && g.Result.ResID != "" {
			return nil, statusErrorForResult(g.Result)
		}
		return nil, &StatusError{HTTPStatus: http.StatusBadGateway, Code: "mlp_error", Detail: "GMLC returned an unrecognized error response"}
	}

	var v mlpSvcResult
	if err := xml.Unmarshal(respBody, &v); err != nil {
		return nil, fmt.Errorf("decode svc_result: %w", err)
	}
	if v.Slia == nil {
		return nil, &StatusError{HTTPStatus: http.StatusBadGateway, Code: "mlp_error", Detail: "GMLC response had no slia"}
	}
	if v.Slia.Result != nil {
		return nil, statusErrorForResult(*v.Slia.Result)
	}
	if len(v.Slia.Pos) != 1 {
		return nil, &StatusError{HTTPStatus: http.StatusBadGateway, Code: "mlp_error", Detail: fmt.Sprintf("expected exactly 1 pos, got %d", len(v.Slia.Pos))}
	}
	pos := v.Slia.Pos[0]
	id := newRequestID()

	if pos.PosErr != nil {
		s := buildFailedStatus(id, req, pos.PosErr, time.Now().UTC().Format(time.RFC3339))
		c.mu.Lock()
		c.results[id] = s
		c.mu.Unlock()
		return s, nil
	}
	if pos.Pd == nil || pos.Pd.Shape.CircularArea == nil {
		return nil, &StatusError{HTTPStatus: http.StatusBadGateway, Code: "mlp_error", Detail: "slia pos had neither pd nor poserr"}
	}
	ca := pos.Pd.Shape.CircularArea
	lat, err := decodeDMSH(ca.Coord.X)
	if err != nil {
		return nil, fmt.Errorf("decode latitude: %w", err)
	}
	lon, err := decodeDMSH(ca.Coord.Y)
	if err != nil {
		return nil, fmt.Errorf("decode longitude: %w", err)
	}
	radius, err := strconv.ParseFloat(ca.Radius, 64)
	if err != nil {
		return nil, fmt.Errorf("decode radius: %w", err)
	}
	createdAt := time.Now().UTC()
	if t, err := parseMLPTime(pos.Pd.Time.Value); err == nil {
		createdAt = t
	}
	createdAtStr := createdAt.Format(time.RFC3339)
	s := &RequestStatus{
		ID: id, ServiceType: "immediate", State: "completed",
		LocationType: req.LocationType, Priority: req.Priority, QoS: req.QoS,
		CreatedAt: createdAtStr, UpdatedAt: createdAtStr,
		Result: &Result{
			CreatedAt: createdAtStr, Shape: "ellipsoid_point_uncertainty_circle",
			Latitude: &lat, Longitude: &lon, UncertaintyMeters: &radius,
		},
	}
	c.mu.Lock()
	c.results[id] = s
	c.mu.Unlock()
	return s, nil
}

// Get returns the in-memory result recorded by Submit — see MLPClient's
// own doc comment for why there's nothing to poll or fetch remotely.
func (c *MLPClient) Get(ctx context.Context, id string) (*RequestStatus, error) {
	c.mu.Lock()
	s, ok := c.results[id]
	c.mu.Unlock()
	if !ok {
		return nil, &StatusError{HTTPStatus: http.StatusNotFound, Code: "not_found", Detail: "unknown request id (MLP requests are only remembered for this console process's lifetime)"}
	}
	return s, nil
}

// Cancel always fails — MLP's Standard Location Immediate service has no
// cancellation message on the wire (it's synchronous request/response;
// by the time Submit returns, the request has already reached a terminal
// state).
func (c *MLPClient) Cancel(ctx context.Context, id string) (*RequestStatus, error) {
	return nil, &StatusError{HTTPStatus: http.StatusBadRequest, Code: "not_supported", Detail: "cancellation is not supported over MLP (no wire message for it)"}
}

// Ready does a best-effort transport-level reachability check: POST a
// request GMLC's MLP listener won't recognize as any defined service and
// confirm we get a well-formed HTTP response back at all. MLP's HTTP
// binding has no dedicated health endpoint — this can only prove the
// listener itself is up, not that GMLC's backend (Diameter peers,
// storage) is healthy. That's a real, documented gap, not an oversight.
func (c *MLPClient) Ready(ctx context.Context) error {
	envelope := mlpSvcInit{Ver: mlpVersion, Hdr: mlpHdr{Ver: mlpVersion, Client: mlpClient{ID: c.clientID, Pwd: c.bearerToken}}}
	body, err := xml.Marshal(envelope)
	if err != nil {
		return err
	}
	_, _, err = c.post(ctx, append([]byte(xml.Header), body...))
	return err
}

// History queries target's recorded fixes via GMLC's Historic Location
// Immediate service (hlir/hlia). A stop zero value omits stop_time (GMLC
// treats that as "up to now" — see internal/mlp's own hlir handling on
// the GMLC side).
func (c *MLPClient) History(ctx context.Context, target Target, start, stop time.Time) ([]HistoryPoint, error) {
	m, err := targetToMsid(target)
	if err != nil {
		return nil, &StatusError{HTTPStatus: http.StatusBadRequest, Code: "invalid_request", Detail: err.Error()}
	}
	h := &mlpHlir{Ver: mlpVersion, Msid: m, StartTime: formatMLPTime(start)}
	if !stop.IsZero() {
		h.StopTime = formatMLPTime(stop)
	}
	envelope := mlpSvcInit{Ver: mlpVersion, Hdr: mlpHdr{Ver: mlpVersion, Client: mlpClient{ID: c.clientID, Pwd: c.bearerToken}}, Hlir: h}
	body, err := xml.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode hlir: %w", err)
	}

	status, respBody, err := c.post(ctx, append([]byte(xml.Header), body...))
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		var g mlpGem
		if xml.Unmarshal(respBody, &g) == nil && g.Result.ResID != "" {
			return nil, statusErrorForResult(g.Result)
		}
		return nil, &StatusError{HTTPStatus: http.StatusBadGateway, Code: "mlp_error", Detail: "GMLC returned an unrecognized error response"}
	}

	var v mlpSvcResult
	if err := xml.Unmarshal(respBody, &v); err != nil {
		return nil, fmt.Errorf("decode svc_result: %w", err)
	}
	if v.Hlia == nil {
		return nil, &StatusError{HTTPStatus: http.StatusBadGateway, Code: "mlp_error", Detail: "GMLC response had no hlia"}
	}
	if v.Hlia.Result != nil {
		// resid 6 (POSITION METHOD FAILURE) is what GMLC's own hlir
		// handler sends specifically for "nothing recorded in this
		// window" (see internal/mlp's handleHLIR on the GMLC side) — a
		// completely normal outcome for a target with no history yet, not
		// an error condition for this console to surface as one.
		if v.Hlia.Result.ResID == "6" {
			return nil, nil
		}
		return nil, statusErrorForResult(*v.Hlia.Result)
	}

	points := make([]HistoryPoint, 0, len(v.Hlia.Pos))
	for _, p := range v.Hlia.Pos {
		if p.Pd == nil || p.Pd.Shape.CircularArea == nil {
			continue // a per-point poserr within pos+ — nothing to show for it
		}
		ca := p.Pd.Shape.CircularArea
		lat, err := decodeDMSH(ca.Coord.X)
		if err != nil {
			continue
		}
		lon, err := decodeDMSH(ca.Coord.Y)
		if err != nil {
			continue
		}
		recordedAt := p.Pd.Time.Value
		if t, err := parseMLPTime(p.Pd.Time.Value); err == nil {
			recordedAt = t.Format(time.RFC3339)
		}
		point := HistoryPoint{RecordedAt: recordedAt, Latitude: &lat, Longitude: &lon}
		if radius, err := strconv.ParseFloat(ca.Radius, 64); err == nil {
			point.UncertaintyMeters = &radius
		}
		points = append(points, point)
	}
	return points, nil
}
