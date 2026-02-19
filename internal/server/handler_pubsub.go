package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleSubscribeSSE handles GET /subscribe/{pattern...} via Server-Sent Events.
func (s *Server) handleSubscribeSSE(w http.ResponseWriter, r *http.Request) {
	pattern := r.PathValue("pattern")
	if pattern == "" {
		pattern = "*"
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := s.cache.Subscribe(pattern)
	defer sub.Close()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-sub.Events():
			if !ok {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": string(evt.Type),
				"key":  evt.Key,
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
