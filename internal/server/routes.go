package server

import "net/http"

// registerRoutes registers all HTTP routes using Go 1.22+ method routing.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Cache operations
	mux.HandleFunc("GET /cache/{key}", s.handleCacheGet)
	mux.HandleFunc("PUT /cache/{key}", s.handleCacheSet)
	mux.HandleFunc("DELETE /cache/{key}", s.handleCacheDelete)
	mux.HandleFunc("HEAD /cache/{key}", s.handleCacheExists)
	mux.HandleFunc("POST /cache/_mget", s.handleCacheMGet)

	// Lock operations
	mux.HandleFunc("POST /locks/{key}", s.handleLockAcquire)
	mux.HandleFunc("DELETE /locks/{key}", s.handleLockRelease)
	mux.HandleFunc("PUT /locks/{key}", s.handleLockExtend)

	// Rate limiting
	mux.HandleFunc("POST /ratelimit/{key}", s.handleRateLimit)

	// Pub/Sub
	mux.HandleFunc("GET /subscribe/{pattern...}", s.handleSubscribeSSE)

	// Cluster / health / metrics
	mux.HandleFunc("GET /cluster/nodes", s.handleClusterNodes)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
}
