package cachegrid

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPRateLimitMiddleware(t *testing.T) {
	c := newTestCache(t)

	handler := HTTPRateLimit(c, 3, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Fatal("expected X-RateLimit-Limit header")
		}
	}

	// 4th request should be rate limited
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

func TestHTTPRateLimitByIP(t *testing.T) {
	c := newTestCache(t)

	handler := HTTPRateLimit(c, 1, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First IP
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:5000"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first IP first req: expected 200, got %d", rec.Code)
	}

	// Second IP — should be independent
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "10.0.0.2:5000"
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second IP first req: expected 200, got %d", rec2.Code)
	}
}

func TestHTTPCacheMiddleware(t *testing.T) {
	c := newTestCache(t)
	callCount := 0

	handler := HTTPCache(c, 5*time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	}))

	// First request — MISS
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/data", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("expected X-Cache: MISS, got %s", rec.Header().Get("X-Cache"))
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// Second request — HIT
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/data", nil)
	handler.ServeHTTP(rec2, req2)
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT, got %s", rec2.Header().Get("X-Cache"))
	}
	if callCount != 1 {
		t.Fatalf("handler should not be called on cache hit, got %d calls", callCount)
	}
}

func TestHTTPCacheSkipNonGet(t *testing.T) {
	c := newTestCache(t)

	handler := HTTPCache(c, 5*time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST should not be cached
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/data", nil)
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("X-Cache") != "" {
		t.Fatal("POST should not have X-Cache header")
	}
}

func TestClientIPExtraction(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remoteIP string
		expected string
	}{
		{
			name:     "X-Forwarded-For",
			headers:  map[string]string{"X-Forwarded-For": "203.0.113.1, 70.41.3.18"},
			remoteIP: "127.0.0.1:1234",
			expected: "203.0.113.1",
		},
		{
			name:     "X-Real-IP",
			headers:  map[string]string{"X-Real-IP": "203.0.113.2"},
			remoteIP: "127.0.0.1:1234",
			expected: "203.0.113.2",
		},
		{
			name:     "RemoteAddr",
			headers:  nil,
			remoteIP: "10.0.0.1:5000",
			expected: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteIP
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			got := clientIP(req)
			if got != tt.expected {
				t.Fatalf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}
