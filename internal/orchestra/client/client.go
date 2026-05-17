package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const defaultBaseURL = "https://app.getorchestra.io/api/engine/public"

// Client is a minimal Orchestra API HTTP client.
//
// Authentication uses the OpenAPI BearerAuth scheme with an API key:
//   Authorization: Bearer <ORCHESTRA_API_KEY>
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func New(apiKey, baseURL string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) base() string {
	if strings.TrimSpace(c.baseURL) == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(c.baseURL, "/")
}

func (c *Client) authHeader() (string, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return "", errors.New("ORCHESTRA_API_KEY is not set")
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

	// Ensure exactly one slash boundary.
	p = "/" + strings.TrimLeft(p, "/")
	u.Path = path.Join(strings.TrimRight(u.Path, "/"), p)
	return u.String(), nil
}

func (c *Client) doJSON(ctx context.Context, method, p string, query url.Values, body any, out any) error {
	auth, err := c.authHeader()
	if err != nil {
		return err
	}

	u, err := joinURL(c.base(), p)
	if err != nil {
		return err
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = nil
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if query != nil {
		req.URL.RawQuery = query.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Treat any non-2xx as an error.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("orchestra api %s %s failed: status=%d body=%s", method, p, resp.StatusCode, string(b))
	}

	if out == nil {
		// Caller doesn't care about response body.
		return nil
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		// Some endpoints can return empty bodies for 204; allow EOF.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("decode response body: %w", err)
	}

	return nil
}

func (c *Client) DoJSON(ctx context.Context, method, p string, query url.Values, body any, out any) error {
	return c.doJSON(ctx, method, p, query, body, out)
}

// DoJSONRaw is useful when you don't want strict decoding (for partially-known responses).
func (c *Client) DoJSONRaw(ctx context.Context, method, p string, query url.Values, body any) (json.RawMessage, error) {
	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}

	u, err := joinURL(c.base(), p)
	if err != nil {
		return nil, err
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if query != nil {
		req.URL.RawQuery = query.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("orchestra api %s %s failed: status=%d body=%s", method, p, resp.StatusCode, string(b))
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

