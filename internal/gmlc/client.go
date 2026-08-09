// JSONClient implements LocationClient against the GMLC's interim Le
// REST/JSON adapter (internal/httpapi on the GMLC side — see api.md at the
// repo root). This is expected to be swapped out for a client speaking the
// real Le standard (OMA MLP over XML/HTTP) later; see LocationClient in
// types.go.
package gmlc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type JSONClient struct {
	baseURL     string
	clientID    string
	bearerToken string
	httpClient  *http.Client
}

// New returns a LocationClient speaking the GMLC's current JSON REST Le
// adapter.
func New(baseURL, clientID, bearerToken string, timeout time.Duration) *JSONClient {
	return &JSONClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		clientID:    clientID,
		bearerToken: bearerToken,
		httpClient:  &http.Client{Timeout: timeout},
	}
}

var _ LocationClient = (*JSONClient)(nil)

type errorEnvelope struct {
	Error struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	} `json:"error"`
}

// do sends the request and, for a non-2xx response, decodes the GMLC's
// {"error":{"code":...,"detail":...}} envelope into a *StatusError.
func (c *JSONClient) do(ctx context.Context, method, path string, body []byte, headers map[string]string, out any) error {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("X-LCS-Client-ID", c.clientID)
	req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gmlc request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read gmlc response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var env errorEnvelope
		code, detail := "gmlc_error", string(respBody)
		if json.Unmarshal(respBody, &env) == nil && env.Error.Code != "" {
			code, detail = env.Error.Code, env.Error.Detail
		}
		return &StatusError{HTTPStatus: resp.StatusCode, Code: code, Detail: detail}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode gmlc response: %w", err)
		}
	}
	return nil
}

func (c *JSONClient) Submit(ctx context.Context, req SubmitRequest, idempotencyKey string) (*RequestStatus, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode submit request: %w", err)
	}

	var headers map[string]string
	if idempotencyKey != "" {
		headers = map[string]string{"Idempotency-Key": idempotencyKey}
	}

	var status RequestStatus
	if err := c.do(ctx, http.MethodPost, "/v1/location-requests", body, headers, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *JSONClient) Get(ctx context.Context, id string) (*RequestStatus, error) {
	var status RequestStatus
	if err := c.do(ctx, http.MethodGet, "/v1/location-requests/"+url.PathEscape(id), nil, nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *JSONClient) Cancel(ctx context.Context, id string) (*RequestStatus, error) {
	var status RequestStatus
	if err := c.do(ctx, http.MethodDelete, "/v1/location-requests/"+url.PathEscape(id), nil, nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *JSONClient) Ready(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/readyz", nil, nil, nil)
}
