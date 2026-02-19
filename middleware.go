package cachegrid

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPRateLimit returns an HTTP middleware that rate limits by client IP.
func HTTPRateLimit(c *Cache, limit int64, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			key := "ratelimit:" + ip

			allowed, state := c.RateLimit(key, RateLimitOptions{Limit: limit, Window: window})

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", state.Limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", state.Remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", state.ResetsAt.Unix()))

			if !allowed {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HTTPCache returns an HTTP middleware that caches responses.
func HTTPCache(c *Cache, ttl time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			cacheKey := "httpcache:" + hashRequest(r)

			var cached cachedResponse
			if c.Get(cacheKey, &cached) {
				for k, v := range cached.Headers {
					w.Header().Set(k, v)
				}
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(cached.StatusCode)
				w.Write(cached.Body)
				return
			}

			rec := &responseRecorder{
				ResponseWriter: w,
				body:           &bytes.Buffer{},
				statusCode:     http.StatusOK,
			}
			next.ServeHTTP(rec, r)

			if rec.statusCode >= 200 && rec.statusCode < 400 {
				resp := cachedResponse{
					StatusCode: rec.statusCode,
					Body:       rec.body.Bytes(),
					Headers:    make(map[string]string),
				}
				for k := range w.Header() {
					resp.Headers[k] = w.Header().Get(k)
				}
				c.Set(cacheKey, resp, ttl)
			}
			w.Header().Set("X-Cache", "MISS")
		})
	}
}

type cachedResponse struct {
	StatusCode int               `msgpack:"status_code"`
	Body       []byte            `msgpack:"body"`
	Headers    map[string]string `msgpack:"headers"`
}

type responseRecorder struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	rec.body.Write(b)
	return rec.ResponseWriter.Write(b)
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.SplitN(forwarded, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	return host
}

func hashRequest(r *http.Request) string {
	h := sha256.New()
	io.WriteString(h, r.Method)
	io.WriteString(h, r.URL.String())
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}