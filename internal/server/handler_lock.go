package server

import (
	"net/http"
	"time"

	cachegrid "github.com/skshohagmiah/cachegrid"
)

// handleLockAcquire handles POST /locks/{key}
func (s *Server) handleLockAcquire(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	var req struct {
		TTL        string `json:"ttl"`
		RetryCount int    `json:"retry_count"`
		RetryDelay string `json:"retry_delay"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ttl, _ := time.ParseDuration(req.TTL)
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	retryDelay, _ := time.ParseDuration(req.RetryDelay)

	handle, err := s.cache.Lock(key, cachegrid.LockOptions{
		TTL:        ttl,
		RetryCount: req.RetryCount,
		RetryDelay: retryDelay,
	})
	if err != nil {
		if err == cachegrid.ErrLockNotAcquired {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"acquired": false,
				"error":    err.Error(),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"acquired":      true,
		"token":         handle.Token(),
		"fencing_token": handle.Token(),
	})
}

// handleLockRelease handles DELETE /locks/{key}
func (s *Server) handleLockRelease(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	token := r.Header.Get("X-Lock-Token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "X-Lock-Token header is required")
		return
	}

	lockKey := "__lock:" + key
	released := s.cache.LockEngine().Release(lockKey, token)
	if !released {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"released": false,
			"error":    "lock not held or expired",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"released": true,
	})
}

// handleLockExtend handles PUT /locks/{key}
func (s *Server) handleLockExtend(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	token := r.Header.Get("X-Lock-Token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "X-Lock-Token header is required")
		return
	}

	var req struct {
		TTL string `json:"ttl"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ttl, _ := time.ParseDuration(req.TTL)
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	lockKey := "__lock:" + key
	extended := s.cache.LockEngine().Extend(lockKey, token, ttl)
	if !extended {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"extended": false,
			"error":    "lock not held or expired",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"extended": true,
	})
}
