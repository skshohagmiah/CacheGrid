package server

import (
	"fmt"
	"net/http"
	"time"

	cachegrid "github.com/skshohagmiah/cachegrid"
)

// handleRateLimit handles POST /ratelimit/{key}
func (s *Server) handleRateLimit(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	var req struct {
		Limit     int64  `json:"limit"`
		Window    string `json:"window"`
		Algorithm string `json:"algorithm"` // "token_bucket" (default) or "sliding_window"
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Limit <= 0 {
		writeError(w, http.StatusBadRequest, "limit must be > 0")
		return
	}

	window, _ := time.ParseDuration(req.Window)
	if window <= 0 {
		window = time.Minute
	}

	var allowed bool
	var state cachegrid.RateLimitState

	switch req.Algorithm {
	case "sliding_window":
		allowed, state = s.cache.RateLimitSliding(key, cachegrid.SlidingWindowOptions{
			Limit:  req.Limit,
			Window: window,
		})
	default:
		allowed, state = s.cache.RateLimit(key, cachegrid.RateLimitOptions{
			Limit:  req.Limit,
			Window: window,
		})
	}

	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", state.Limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", state.Remaining))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", state.ResetsAt.Unix()))

	status := http.StatusOK
	if !allowed {
		status = http.StatusTooManyRequests
	}

	writeJSON(w, status, map[string]interface{}{
		"allowed":   allowed,
		"remaining": state.Remaining,
		"limit":     state.Limit,
		"resets_at": state.ResetsAt.Unix(),
	})
}
