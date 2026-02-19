package server

import (
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleCacheGet handles GET /cache/{key}
func (s *Server) handleCacheGet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	var raw []byte
	if s.cache.Get(key, &raw) {
		w.Header().Set("Content-Type", "application/octet-stream")
		ttl := s.cache.TTL(key)
		if ttl > 0 {
			w.Header().Set("X-TTL", ttl.String())
		}
		w.WriteHeader(http.StatusOK)
		w.Write(raw)
		return
	}

	writeError(w, http.StatusNotFound, "key not found")
}

// handleCacheSet handles PUT /cache/{key}
func (s *Server) handleCacheSet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10MB limit
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	ttl := parseTTL(r.Header.Get("X-TTL"))
	tags := parseTags(r.Header.Get("X-Tags"))

	if len(tags) > 0 {
		if err := s.cache.SetWithTags(key, body, ttl, tags); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := s.cache.Set(key, body, ttl); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleCacheDelete handles DELETE /cache/{key}
func (s *Server) handleCacheDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	s.cache.Delete(key)
	w.WriteHeader(http.StatusNoContent)
}

// handleCacheExists handles HEAD /cache/{key}
func (s *Server) handleCacheExists(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if s.cache.Exists(key) {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// handleCacheMGet handles POST /cache/_mget
func (s *Server) handleCacheMGet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	results := s.cache.MGet(req.Keys...)
	resp := make(map[string]string, len(results))
	for k, v := range results {
		resp[k] = base64.StdEncoding.EncodeToString(v)
	}

	writeJSON(w, http.StatusOK, resp)
}

// parseTTL parses a duration string from a header, returning 0 if invalid.
func parseTTL(s string) time.Duration {
	if s == "" {
		return 0
	}
	// Try Go duration format first (e.g., "5m", "1h30m")
	d, err := time.ParseDuration(s)
	if err == nil {
		return d
	}
	// Try plain seconds
	sec, err := strconv.Atoi(s)
	if err == nil {
		return time.Duration(sec) * time.Second
	}
	return 0
}

// parseTags splits a comma-separated tag header.
func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}
