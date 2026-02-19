package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cachegrid "github.com/skshohagmiah/cachegrid"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	c, err := cachegrid.New(cachegrid.Config{
		NumShards:       16,
		DefaultTTL:      5 * time.Minute,
		SweeperInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Shutdown() })
	return New(c, ":0", log.New(io.Discard, "", 0))
}

func doRequest(s *Server, method, path string, body string) *httptest.ResponseRecorder {
	var b io.Reader
	if body != "" {
		b = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, b)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	mux.ServeHTTP(rec, req)
	return rec
}

func doRequestWithHeaders(s *Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var b io.Reader
	if body != "" {
		b = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, b)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCacheSetGetDelete(t *testing.T) {
	s := newTestServer(t)

	// Set
	rec := doRequestWithHeaders(s, "PUT", "/cache/foo", "hello world", map[string]string{
		"X-TTL": "5m",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT expected 204, got %d", rec.Code)
	}

	// Get
	rec = doRequest(s, "GET", "/cache/foo", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// HEAD (exists)
	rec = doRequest(s, "HEAD", "/cache/foo", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD expected 200, got %d", rec.Code)
	}

	// DELETE
	rec = doRequest(s, "DELETE", "/cache/foo", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE expected 204, got %d", rec.Code)
	}

	// GET after delete
	rec = doRequest(s, "GET", "/cache/foo", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE expected 404, got %d", rec.Code)
	}
}

func TestCacheMGet(t *testing.T) {
	s := newTestServer(t)

	// Set two keys
	doRequestWithHeaders(s, "PUT", "/cache/a", "val-a", nil)
	doRequestWithHeaders(s, "PUT", "/cache/b", "val-b", nil)

	body := `{"keys": ["a", "b", "missing"]}`
	rec := doRequest(s, "POST", "/cache/_mget", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("MGet expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]string
	json.NewDecoder(rec.Body).Decode(&result)

	if _, ok := result["a"]; !ok {
		t.Fatal("expected key 'a' in MGet response")
	}
	if _, ok := result["b"]; !ok {
		t.Fatal("expected key 'b' in MGet response")
	}
	if _, ok := result["missing"]; ok {
		t.Fatal("did not expect 'missing' in MGet response")
	}

	// Verify values are base64 encoded
	decoded, err := base64.StdEncoding.DecodeString(result["a"])
	if err != nil {
		t.Fatal("expected base64 encoded value")
	}
	// The value was stored as msgpack-serialized bytes
	_ = decoded
}

func TestCacheGetNotFound(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(s, "GET", "/cache/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestLockAcquireRelease(t *testing.T) {
	s := newTestServer(t)

	// Acquire
	body := `{"ttl": "30s"}`
	rec := doRequest(s, "POST", "/locks/mylock", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("Lock acquire expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["acquired"] != true {
		t.Fatal("expected acquired=true")
	}
}

func TestRateLimit(t *testing.T) {
	s := newTestServer(t)

	body := `{"limit": 5, "window": "1m"}`
	for i := 0; i < 5; i++ {
		rec := doRequest(s, "POST", "/ratelimit/api-key-1", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	// 6th request should be rate limited
	rec := doRequest(s, "POST", "/ratelimit/api-key-1", body)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(s, "GET", "/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", resp["status"])
	}
}

func TestMetrics(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(s, "GET", "/metrics", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "cachegrid_keys_total") {
		t.Fatal("expected Prometheus metric cachegrid_keys_total")
	}
	if !strings.Contains(body, "cachegrid_uptime_seconds") {
		t.Fatal("expected Prometheus metric cachegrid_uptime_seconds")
	}
}

func TestClusterNodesLocalMode(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(s, "GET", "/cluster/nodes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["mode"] != "local" {
		t.Fatalf("expected mode=local, got %v", resp["mode"])
	}
}

func TestCORSHeaders(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	handler := s.withMiddleware(mux)

	req := httptest.NewRequest("OPTIONS", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS expected 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("missing CORS origin header")
	}
}

func TestSetWithTags(t *testing.T) {
	s := newTestServer(t)

	rec := doRequestWithHeaders(s, "PUT", "/cache/tagged-key", "tagged-value", map[string]string{
		"X-Tags": "tag1, tag2",
		"X-TTL":  "5m",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	// Verify the key exists
	rec = doRequest(s, "HEAD", "/cache/tagged-key", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for tagged key, got %d", rec.Code)
	}
}

func TestParseTTL(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"3600", 3600 * time.Second},
		{"", 0},
		{"invalid", 0},
	}
	for _, tt := range tests {
		got := parseTTL(tt.input)
		if got != tt.expected {
			t.Errorf("parseTTL(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestRateLimitSlidingWindow(t *testing.T) {
	s := newTestServer(t)

	body := `{"limit": 3, "window": "1m", "algorithm": "sliding_window"}`
	for i := 0; i < 3; i++ {
		rec := doRequest(s, "POST", "/ratelimit/sliding-key", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	rec := doRequest(s, "POST", "/ratelimit/sliding-key", body)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

// Ensure doRequestWithHeaders works with nil body
func TestEmptyPut(t *testing.T) {
	s := newTestServer(t)
	rec := doRequestWithHeaders(s, "PUT", "/cache/empty-val", "", nil)
	// Empty body should still succeed
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func init() {
	_ = bytes.Compare // keep bytes import used
}
