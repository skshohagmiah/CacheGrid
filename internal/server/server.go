package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	cachegrid "github.com/skshohagmiah/cachegrid"
)

// Server is the standalone HTTP server for CacheGrid.
type Server struct {
	cache      *cachegrid.Cache
	httpServer *http.Server
	logger     *log.Logger
	startTime  time.Time
}

// New creates a new HTTP server wrapping the given cache.
func New(cache *cachegrid.Cache, addr string, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}

	s := &Server{
		cache:     cache,
		logger:    logger,
		startTime: time.Now(),
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.withMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the HTTP server (blocking).
func (s *Server) Start() error {
	s.logger.Printf("CacheGrid HTTP server listening on %s", s.httpServer.Addr)
	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes an error JSON response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// readJSON reads a JSON request body into v.
func readJSON(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return fmt.Errorf("empty request body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
