// Example: sessions
//
// A session store using CacheGrid's disk mode. Sessions persist across
// server restarts because they're stored on disk via PebbleDB.
//
// Run: go run ./examples/sessions
//
// Try it:
//   curl -X POST localhost:8080/login -d '{"username":"alice"}'
//   curl localhost:8080/me -H "Authorization: Bearer <token>"
//   curl -X POST localhost:8080/logout -H "Authorization: Bearer <token>"
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/skshohagmiah/cachegrid"
)

// Session represents an active user session.
type Session struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

var cache *cachegrid.Cache

const sessionTTL = 24 * time.Hour

func main() {
	// Use disk mode so sessions survive server restarts
	dataDir := "./session-data"
	os.MkdirAll(dataDir, 0755)

	var err error
	cache, err = cachegrid.NewDisk(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Shutdown()

	// Rate limit login attempts
	loginCache, _ := cachegrid.NewMemory()
	defer loginCache.Shutdown()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", handleLogin)
	mux.HandleFunc("GET /me", requireAuth(handleMe))
	mux.HandleFunc("POST /logout", requireAuth(handleLogout))
	mux.HandleFunc("GET /sessions/count", handleSessionCount)

	// Rate limit: 10 requests/minute on login endpoint
	handler := cachegrid.HTTPRateLimit(loginCache, 10, time.Minute)(mux)

	addr := ":8080"
	log.Printf("Session store running at http://localhost%s", addr)
	log.Printf("  POST /login           - Login (body: {\"username\":\"alice\"})")
	log.Printf("  GET  /me              - Get current session (Authorization: Bearer <token>)")
	log.Printf("  POST /logout          - Logout")
	log.Printf("  GET  /sessions/count  - Count active sessions")
	log.Printf("")
	log.Printf("Sessions are stored on disk at %s/ and persist across restarts.", dataDir)
	log.Fatal(http.ListenAndServe(addr, handler))
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		http.Error(w, `{"error":"username required"}`, http.StatusBadRequest)
		return
	}

	// Generate session token
	token := generateToken()

	session := Session{
		UserID:    "user-" + body.Username,
		Username:  body.Username,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
	}

	// Store session on disk — survives server restarts
	if err := cache.Set("session:"+token, session, sessionTTL); err != nil {
		http.Error(w, `{"error":"failed to create session"}`, http.StatusInternalServerError)
		return
	}

	// Track active sessions count
	cache.Incr("stats:active_sessions", 1)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      token,
		"expires_in": sessionTTL.String(),
		"message":    "Login successful. Use Authorization: Bearer " + token,
	})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(contextKeySession).(*Session)

	// Update last seen
	session.LastSeen = time.Now()
	cache.Set("session:"+extractToken(r), *session, sessionTTL)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	cache.Delete("session:" + token)
	cache.Decr("stats:active_sessions", 1)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Logged out successfully",
	})
}

func handleSessionCount(w http.ResponseWriter, r *http.Request) {
	count, _ := cache.Incr("stats:active_sessions", 0)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_sessions": count,
		"storage":         "disk (PebbleDB)",
	})
}

// ── Auth middleware ───────────────────────────────────────

type contextKey string

const contextKeySession contextKey = "session"

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
			return
		}

		var session Session
		if !cache.Get("session:"+token, &session) {
			http.Error(w, `{"error":"invalid or expired session"}`, http.StatusUnauthorized)
			return
		}

		ctx := r.Context()
		ctx = contextWithSession(ctx, &session)
		next(w, r.WithContext(ctx))
	}
}

func contextWithSession(ctx interface{ Value(any) any }, s *Session) interface {
	Deadline() (time.Time, bool)
	Done() <-chan struct{}
	Err() error
	Value(any) any
} {
	// Simple context wrapper — in production use context.WithValue
	return &sessionContext{parent: ctx.(interface {
		Deadline() (time.Time, bool)
		Done() <-chan struct{}
		Err() error
		Value(any) any
	}), session: s}
}

type sessionContext struct {
	parent interface {
		Deadline() (time.Time, bool)
		Done() <-chan struct{}
		Err() error
		Value(any) any
	}
	session *Session
}

func (c *sessionContext) Deadline() (time.Time, bool) { return c.parent.Deadline() }
func (c *sessionContext) Done() <-chan struct{}        { return c.parent.Done() }
func (c *sessionContext) Err() error                  { return c.parent.Err() }
func (c *sessionContext) Value(key any) any {
	if key == contextKeySession {
		return c.session
	}
	return c.parent.Value(key)
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
