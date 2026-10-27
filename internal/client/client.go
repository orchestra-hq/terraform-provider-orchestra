package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://app.getorchestra.io/api/engine/public"

// The Agents API is a sibling of the Engine API on the same host, with its own
// spec (internal/apicontract/agents.yaml) and base path.
const (
	enginePath = "/api/engine/public"
	agentsPath = "/api/ai/v1"
)

// accountHeader lets a caller act on an account other than the one the
// credential resolves to. Every Orchestra endpoint accepts it.
const accountHeader = "X-Orchestra-Account-Id"

const (
	defaultMaxRetries = 4
	defaultRetryBase  = 500 * time.Millisecond
	maxRetryDelay     = 30 * time.Second
)

// ErrNotFound is returned when the API responds 404. Callers use it to tell a
// missing resource (remove from state) apart from a genuine failure.
var ErrNotFound = errors.New("orchestra api: resource not found")

// APIError carries the status code and body of a non-2xx response.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("orchestra api %s %s failed: status=%d body=%s", e.Method, e.Path, e.StatusCode, e.Body)
}

// redactErrorBody strips the echoed request payload out of a validation error.
//
// FastAPI's 422 handler returns {"detail":[{"loc":…,"msg":…,"input":…}]}, where
// `input` is the rejected value from the request. Error bodies are printed
// verbatim in Terraform diagnostics, which are not redacted and end up in
// console output and CI logs — so for an endpoint like PATCH /environments a
// validation failure would print the submitted credential values.
//
// Anything that is not that shape is returned unchanged.
func redactErrorBody(b []byte) string {
	var envelope struct {
		Detail []map[string]json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil || len(envelope.Detail) == 0 {
		return string(b)
	}

	redacted := false
	for _, item := range envelope.Detail {
		if _, ok := item["input"]; ok {
			item["input"] = json.RawMessage(`"[redacted]"`)
			redacted = true
		}
	}
	if !redacted {
		return string(b)
	}

	out, err := json.Marshal(map[string]any{"detail": envelope.Detail})
	if err != nil {
		// Never fall back to the raw body here: that is the value being hidden.
		return `{"detail":"[redacted validation error]"}`
	}
	return string(out)
}

func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

// Client is a minimal Orchestra API HTTP client.
//
// Authentication uses the OpenAPI BearerAuth scheme with an API key:
//
//	Authorization: Bearer <ORCHESTRA_API_KEY>
type Client struct {
	apiKey     string
	baseURL    string
	accountID  string
	userAgent  string
	httpClient *http.Client

	maxRetries int
	retryBase  time.Duration
}

// Option customises a Client at construction time.
type Option func(*Client)

// WithAccountID sets the X-Orchestra-Account-Id header sent with every request.
func WithAccountID(accountID string) Option {
	return func(c *Client) { c.accountID = strings.TrimSpace(accountID) }
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if strings.TrimSpace(ua) != "" {
			c.userAgent = ua
		}
	}
}

// WithRetry tunes the retry policy. A maxRetries of 0 disables retrying.
func WithRetry(maxRetries int, base time.Duration) Option {
	return func(c *Client) {
		if maxRetries >= 0 {
			c.maxRetries = maxRetries
		}
		if base > 0 {
			c.retryBase = base
		}
	}
}

func New(apiKey, baseURL string, opts ...Option) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}

	c := &Client{
		apiKey:    apiKey,
		baseURL:   strings.TrimRight(baseURL, "/"),
		userAgent: "terraform-provider-orchestra",
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		maxRetries: defaultMaxRetries,
		retryBase:  defaultRetryBase,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Agents returns a client for the Agents API. Credentials and settings are
// shared; only the base URL differs. A base URL that does not end in the
// Engine API's path, such as a test stub's, is assumed to serve both APIs.
func (c *Client) Agents() *Client {
	cp := *c
	if base := c.base(); strings.HasSuffix(base, enginePath) {
		cp.baseURL = strings.TrimSuffix(base, enginePath) + agentsPath
	}
	return &cp
}

// base returns the configured base URL, falling back to the production API.
func (c *Client) base() string {
	if strings.TrimSpace(c.baseURL) == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(c.baseURL, "/")
}

func (c *Client) authHeader() (string, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return "", errors.New("no Orchestra API key configured: set the provider `api_key` attribute or the ORCHESTRA_API_KEY environment variable")
	}
	return "Bearer " + c.apiKey, nil
}

func joinURL(baseURL, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return baseURL, nil
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	// Ensure exactly one slash boundary. path.Join drops a trailing slash, but
	// some Orchestra paths (/integrations/, /sensor_checks/) require one.
	trailing := strings.HasSuffix(p, "/")
	p = "/" + strings.TrimLeft(p, "/")
	u.Path = path.Join(strings.TrimRight(u.Path, "/"), p)
	if trailing && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u.String(), nil
}

// shouldRetry reports whether a status code is worth retrying.
func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// retryDelay honours Retry-After when present, otherwise backs off exponentially.
func (c *Client) retryDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && secs >= 0 {
				d := time.Duration(secs) * time.Second
				if d > maxRetryDelay {
					return maxRetryDelay
				}
				return d
			}
			if t, err := http.ParseTime(ra); err == nil {
				if d := time.Until(t); d > 0 {
					if d > maxRetryDelay {
						return maxRetryDelay
					}
					return d
				}
			}
		}
	}

	d := time.Duration(float64(c.retryBase) * math.Pow(2, float64(attempt)))
	if d > maxRetryDelay {
		return maxRetryDelay
	}
	return d
}

// do issues the request, retrying transient failures, and returns the response body.
func (c *Client) do(ctx context.Context, method, p string, query url.Values, body any) ([]byte, error) {
	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}

	u, err := joinURL(c.base(), p)
	if err != nil {
		return nil, err
	}

	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		var reqBody io.Reader
		if payload != nil {
			reqBody = bytes.NewReader(payload)
		}

		req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", auth)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.accountID != "" {
			req.Header.Set(accountHeader, c.accountID)
		}
		if query != nil {
			req.URL.RawQuery = query.Encode()
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Transport errors are worth one more go, but honour cancellation.
			if ctx.Err() != nil || attempt >= c.maxRetries {
				return nil, err
			}
			if werr := wait(ctx, c.retryDelay(attempt, nil)); werr != nil {
				return nil, werr
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()

			apiErr := &APIError{Method: method, Path: p, StatusCode: resp.StatusCode, Body: redactErrorBody(b)}
			if !shouldRetry(resp.StatusCode) || attempt >= c.maxRetries {
				return nil, apiErr
			}

			if werr := wait(ctx, c.retryDelay(attempt, resp)); werr != nil {
				return nil, werr
			}
			continue
		}

		out, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// DoJSON issues a request and decodes a JSON response into out. Pass a nil out
// when the response body is not needed.
func (c *Client) DoJSON(ctx context.Context, method, p string, query url.Values, body any, out any) error {
	raw, err := c.do(ctx, method, p, query, body)
	if err != nil {
		return err
	}

	if out == nil {
		return nil
	}

	// 204 and other empty bodies are not an error.
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

// maxPageSize is the largest page_size the Orchestra API accepts.
const maxPageSize = 100

// maxPages bounds a paginated read. At the API's maximum page size this allows
// 50,000 items, far beyond any real workspace — it exists so that a server
// reporting a `total` it never delivers cannot spin forever.
const maxPages = 500

// paginatedEnvelope matches PaginatedResponse_* in the OpenAPI spec.
type paginatedEnvelope struct {
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int             `json:"total"`
	Results  json.RawMessage `json:"results"`
}

// ListAll walks every page of a paginated endpoint and appends the decoded
// items to out, which must be a pointer to a slice.
//
// Orchestra has two list shapes: most endpoints always return a
// {page, pageSize, total, results} envelope, while GET /pipelines returns a
// bare array unless page/page_size are supplied. Both are handled here.
func (c *Client) ListAll(ctx context.Context, p string, query url.Values, out any) error {
	if query == nil {
		query = url.Values{}
	} else {
		// Don't mutate the caller's values.
		cloned := url.Values{}
		for k, v := range query {
			cloned[k] = append([]string(nil), v...)
		}
		query = cloned
	}

	pageSize := maxPageSize
	if existing := query.Get("page_size"); existing != "" {
		if n, err := strconv.Atoi(existing); err == nil && n > 0 && n <= maxPageSize {
			pageSize = n
		}
	}
	query.Set("page_size", strconv.Itoa(pageSize))

	// Accumulate the raw JSON items so we only decode into the caller's
	// concrete type once.
	var items []json.RawMessage

	for page := 1; page <= maxPages; page++ {
		query.Set("page", strconv.Itoa(page))

		raw, err := c.do(ctx, http.MethodGet, p, query, nil)
		if err != nil {
			return err
		}

		pageItems, total, envelope, err := decodePage(raw)
		if err != nil {
			return fmt.Errorf("decode %s page %d: %w", p, page, err)
		}

		items = append(items, pageItems...)

		// A bare array means the endpoint ignored pagination; it gave us
		// everything. GET /pipelines does exactly this when no page parameters
		// are sent, and returns an envelope when they are.
		if !envelope {
			break
		}
		// A short page is the normal last page. Checking this before `total`
		// matters: a server that over-reports `total` would otherwise keep the
		// loop going, re-requesting pages forever.
		if len(pageItems) < pageSize || len(items) >= total {
			break
		}
		if page == maxPages {
			return fmt.Errorf(
				"%s returned more than %d pages of %d (%d items so far, total reported as %d); "+
					"refusing to keep paging",
				p, maxPages, pageSize, len(items), total)
		}
	}

	combined, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(combined, out); err != nil {
		return fmt.Errorf("decode %s results: %w", p, err)
	}
	return nil
}

// decodePage splits a list response into its items, reporting whether the
// response was a paginated envelope and, if so, the reported total.
func decodePage(raw []byte) (items []json.RawMessage, total int, envelope bool, err error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, 0, false, nil
	}

	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, 0, false, err
		}
		return items, len(items), false, nil
	}

	var env paginatedEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, 0, false, err
	}
	if len(env.Results) > 0 {
		if err := json.Unmarshal(env.Results, &items); err != nil {
			return nil, 0, false, err
		}
	}
	return items, env.Total, true, nil
}
