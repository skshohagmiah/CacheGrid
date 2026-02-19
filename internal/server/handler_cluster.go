package server

import (
	"fmt"
	"net/http"
	"runtime"
	"time"
)

// handleClusterNodes handles GET /cluster/nodes
func (s *Server) handleClusterNodes(w http.ResponseWriter, r *http.Request) {
	cs := s.cache.ClusterState()
	if cs == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"mode":  "local",
			"nodes": []interface{}{},
		})
		return
	}

	nodes := cs.AllNodes()
	result := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		result = append(result, map[string]interface{}{
			"name":      n.Name,
			"addr":      n.Addr,
			"rpc_addr":  n.RPCAddr,
			"http_addr": n.HTTPAddr,
			"status":    n.Status.String(),
			"joined_at": n.JoinedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"self":       cs.SelfName(),
		"node_count": cs.NodeCount(),
		"nodes":      result,
	})
}

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"status": "ok",
		"uptime": time.Since(s.startTime).String(),
		"keys":   s.cache.Len(),
	}

	if cs := s.cache.ClusterState(); cs != nil {
		resp["cluster_nodes"] = cs.NodeCount()
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleMetrics handles GET /metrics (Prometheus text exposition format).
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	fmt.Fprintf(w, "# HELP cachegrid_keys_total Total number of keys in cache\n")
	fmt.Fprintf(w, "# TYPE cachegrid_keys_total gauge\n")
	fmt.Fprintf(w, "cachegrid_keys_total %d\n\n", s.cache.Len())

	fmt.Fprintf(w, "# HELP cachegrid_uptime_seconds Server uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE cachegrid_uptime_seconds gauge\n")
	fmt.Fprintf(w, "cachegrid_uptime_seconds %.0f\n\n", time.Since(s.startTime).Seconds())

	fmt.Fprintf(w, "# HELP cachegrid_go_goroutines Number of goroutines\n")
	fmt.Fprintf(w, "# TYPE cachegrid_go_goroutines gauge\n")
	fmt.Fprintf(w, "cachegrid_go_goroutines %d\n\n", runtime.NumGoroutine())

	fmt.Fprintf(w, "# HELP cachegrid_go_memstats_alloc_bytes Number of bytes allocated and still in use\n")
	fmt.Fprintf(w, "# TYPE cachegrid_go_memstats_alloc_bytes gauge\n")
	fmt.Fprintf(w, "cachegrid_go_memstats_alloc_bytes %d\n\n", mem.Alloc)

	fmt.Fprintf(w, "# HELP cachegrid_go_memstats_sys_bytes Number of bytes obtained from system\n")
	fmt.Fprintf(w, "# TYPE cachegrid_go_memstats_sys_bytes gauge\n")
	fmt.Fprintf(w, "cachegrid_go_memstats_sys_bytes %d\n\n", mem.Sys)

	if cs := s.cache.ClusterState(); cs != nil {
		fmt.Fprintf(w, "# HELP cachegrid_cluster_nodes Number of cluster nodes\n")
		fmt.Fprintf(w, "# TYPE cachegrid_cluster_nodes gauge\n")
		fmt.Fprintf(w, "cachegrid_cluster_nodes %d\n", cs.NodeCount())
	}
}
