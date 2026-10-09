package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	client "github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

func newClient(t *testing.T, handler http.Handler, opts ...client.Option) *client.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Keep retries fast so the tests stay quick.
	opts = append([]client.Option{client.WithRetry(3, time.Millisecond)}, opts...)
	return client.New("test-key", srv.URL, opts...)
}

func TestSendsAuthAndUserAgent(t *testing.T) {
	var gotAuth, gotUA, gotAccount string

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccount = r.Header.Get("X-Orchestra-Account-Id")
		w.Write([]byte(`{}`))
	}), client.WithUserAgent("terraform-provider-orchestra/1.2.3"), client.WithAccountID("acct-1"))

	if err := c.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, nil); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotUA != "terraform-provider-orchestra/1.2.3" {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotAccount != "acct-1" {
		t.Errorf("X-Orchestra-Account-Id = %q, want acct-1", gotAccount)
	}
}

func TestOmitsAccountHeaderWhenUnset(t *testing.T) {
	var present bool

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["X-Orchestra-Account-Id"]
		w.Write([]byte(`{}`))
	}))

	if err := c.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, nil); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if present {
		t.Error("X-Orchestra-Account-Id was sent even though no account was configured")
	}
}

func TestMissingAPIKeyFailsBeforeRequest(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)

	c := client.New("", srv.URL)
	err := c.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error when no API key is configured")
	}
	if called {
		t.Error("a request was sent without an API key")
	}
}

func TestNotFoundIsDistinguishable(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"nope"}`, http.StatusNotFound)
	}))

	err := c.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, nil)
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false; err = %v", err)
	}
}

func TestOtherErrorsAreNotNotFound(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"bad"}`, http.StatusUnprocessableEntity)
	}))

	err := c.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, client.ErrNotFound) {
		t.Error("422 was reported as not-found")
	}

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", apiErr.StatusCode)
	}
}

func TestRetriesRateLimitAndServerErrors(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var attempts int32

			c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.AddInt32(&attempts, 1) < 3 {
					w.Header().Set("Retry-After", "0")
					http.Error(w, "slow down", status)
					return
				}
				w.Write([]byte(`{"ok":true}`))
			}))

			var out map[string]any
			if err := c.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, &out); err != nil {
				t.Fatalf("DoJSON: %v", err)
			}
			if got := atomic.LoadInt32(&attempts); got != 3 {
				t.Errorf("attempts = %d, want 3", got)
			}
			if out["ok"] != true {
				t.Errorf("body = %#v", out)
			}
		})
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	var attempts int32

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))

	if err := c.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, nil); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 — 4xx should not be retried", got)
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	var attempts int32

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))

	if err := c.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, nil); err == nil {
		t.Fatal("expected an error")
	}
	// WithRetry(3, …): the initial attempt plus three retries.
	if got := atomic.LoadInt32(&attempts); got != 4 {
		t.Errorf("attempts = %d, want 4", got)
	}
}

func TestEmptyBodyIsNotAnError(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	var out map[string]any
	if err := c.DoJSON(context.Background(), http.MethodDelete, "/x", nil, nil, &out); err != nil {
		t.Fatalf("DoJSON on 204: %v", err)
	}
}

func TestPreservesTrailingSlash(t *testing.T) {
	var gotPath string

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[]`))
	}))

	// GET /integrations/ and /sensor_checks/ are declared with a trailing
	// slash and 404 without it.
	if err := c.DoJSON(context.Background(), http.MethodGet, "/integrations/", nil, nil, nil); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if gotPath != "/integrations/" {
		t.Errorf("path = %q, want /integrations/", gotPath)
	}
}

type item struct {
	ID string `json:"id"`
}

func TestListAllFollowsPaginatedEnvelope(t *testing.T) {
	const total = 250
	var requestedPages []string

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		requestedPages = append(requestedPages, r.URL.Query().Get("page"))

		var results []item
		for i := (page - 1) * size; i < page*size && i < total; i++ {
			results = append(results, item{ID: fmt.Sprintf("item-%d", i)})
		}

		json.NewEncoder(w).Encode(map[string]any{
			"page": page, "pageSize": size, "total": total, "results": results,
		})
	}))

	var got []item
	if err := c.ListAll(context.Background(), "/things", nil, &got); err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	if len(got) != total {
		t.Fatalf("got %d items, want %d — pagination stopped early", len(got), total)
	}
	if got[0].ID != "item-0" || got[total-1].ID != fmt.Sprintf("item-%d", total-1) {
		t.Errorf("unexpected first/last item: %q / %q", got[0].ID, got[total-1].ID)
	}
	if len(requestedPages) != 3 {
		t.Errorf("requested pages %v, want 3 pages at the maximum page size", requestedPages)
	}
}

func TestListAllHandlesBareArray(t *testing.T) {
	// GET /pipelines returns a bare array rather than an envelope.
	var requests int32

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		json.NewEncoder(w).Encode([]item{{ID: "a"}, {ID: "b"}})
	}))

	var got []item
	if err := c.ListAll(context.Background(), "/pipelines", nil, &got); err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d items, want 2", len(got))
	}
	if n := atomic.LoadInt32(&requests); n != 1 {
		t.Errorf("made %d requests, want 1 — a bare array means there is nothing to page through", n)
	}
}

func TestListAllKeepsCallerFilters(t *testing.T) {
	var gotIntegration string

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIntegration = r.URL.Query().Get("integration")
		json.NewEncoder(w).Encode(map[string]any{
			"page": 1, "pageSize": 100, "total": 0, "results": []item{},
		})
	}))

	query := url.Values{}
	query.Set("integration", "SNOWFLAKE")

	var got []item
	if err := c.ListAll(context.Background(), "/assets", query, &got); err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if gotIntegration != "SNOWFLAKE" {
		t.Errorf("integration filter = %q, want SNOWFLAKE", gotIntegration)
	}
	// The caller's url.Values must not be mutated with pagination parameters.
	if query.Get("page") != "" || query.Get("page_size") != "" {
		t.Errorf("ListAll mutated the caller's query: %v", query)
	}
}

func TestListAllRespectsRequestedPageSize(t *testing.T) {
	var gotPageSize string

	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPageSize = r.URL.Query().Get("page_size")
		json.NewEncoder(w).Encode(map[string]any{
			"page": 1, "pageSize": 25, "total": 0, "results": []item{},
		})
	}))

	query := url.Values{}
	query.Set("page_size", "25")

	var got []item
	if err := c.ListAll(context.Background(), "/assets", query, &got); err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if gotPageSize != "25" {
		t.Errorf("page_size = %q, want 25", gotPageSize)
	}
}

// TestErrorBodyRedactsValidationInput covers the leak path: FastAPI echoes the
// rejected request payload in a 422, and error bodies are printed verbatim in
// Terraform diagnostics, which are never redacted.
func TestErrorBodyRedactsValidationInput(t *testing.T) {
	const secret = "super-secret-credential"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":[{"loc":["body","values","TOKEN"],` +
			`"msg":"invalid","type":"value_error","input":"` + secret + `"}]}`))
	}))
	defer srv.Close()

	c := client.New("key", srv.URL)
	err := c.DoJSON(context.Background(), http.MethodPatch, "/environments/x", nil, map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks the submitted value:\n%s", err)
	}
	// The diagnostic must still be useful.
	for _, want := range []string{"invalid", "TOKEN", "redacted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lost %q, leaving nothing to debug with:\n%s", want, err)
		}
	}
}

// TestErrorBodyPassesThroughNonValidationErrors makes sure the redaction does
// not swallow ordinary error details.
func TestErrorBodyPassesThroughNonValidationErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"Environment limit reached for this account"}`))
	}))
	defer srv.Close()

	c := client.New("key", srv.URL)
	err := c.DoJSON(context.Background(), http.MethodPost, "/environments", nil, map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), "Environment limit reached") {
		t.Errorf("ordinary error detail was lost: %v", err)
	}
}

// TestListAllStopsOnOverReportedTotal covers a server that claims far more
// items than it serves. Without a bound the loop re-requests pages forever.
func TestListAllStopsOnOverReportedTotal(t *testing.T) {
	var requests atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		// Always a full page, with a total that will never be reached — so
		// neither the short-page nor the total check can end the loop.
		items := make([]string, 100)
		for i := range items {
			items[i] = `{"id":"x"}`
		}
		_, _ = w.Write([]byte(`{"page":1,"pageSize":100,"total":1000000,"results":[` +
			strings.Join(items, ",") + `]}`))
	}))
	defer srv.Close()

	c := client.New("key", srv.URL)
	var out []struct {
		ID string `json:"id"`
	}
	err := c.ListAll(context.Background(), "/assets", nil, &out)
	if err == nil {
		t.Fatal("expected an error rather than an unbounded read")
	}
	if !strings.Contains(err.Error(), "refusing to keep paging") {
		t.Errorf("unexpected error: %v", err)
	}
	if n := requests.Load(); n > 600 {
		t.Errorf("made %d requests; the page bound did not hold", n)
	}
}

// TestListAllStopsOnShortPage is the normal termination path: a page smaller
// than page_size is the last one, whatever `total` says.
func TestListAllStopsOnShortPage(t *testing.T) {
	var requests atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":1,"pageSize":100,"total":9999,"results":[{"id":"only"}]}`))
	}))
	defer srv.Close()

	c := client.New("key", srv.URL)
	var out []struct {
		ID string `json:"id"`
	}
	if err := c.ListAll(context.Background(), "/assets", nil, &out); err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("got %d items, want 1", len(out))
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("made %d requests; a short page should end the read", n)
	}
}

// TestAgentsClient pins where Agents API calls go: beside the Engine API on
// the same host, or straight to a base URL that is not the Engine API's, as a
// test stub's is.
func TestAgentsClient(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	for _, base := range []string{srv.URL + "/api/engine/public", srv.URL} {
		c := client.New("key", base)
		if err := c.Agents().DoJSON(context.Background(), http.MethodGet, "/agents", nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		// The original client is untouched.
		if err := c.DoJSON(context.Background(), http.MethodGet, "/pipelines", nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"/api/ai/v1/agents", "/api/engine/public/pipelines", "/agents", "/pipelines"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("requested %v, want %v", got, want)
	}
}
