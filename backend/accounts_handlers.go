package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Session cookie. HttpOnly + SameSite=Lax blocks script access and most CSRF;
// Secure is set automatically when the request arrives over HTTPS.
const sessionCookieName = "tyschem_session"

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		first := strings.TrimSpace(strings.Split(proto, ",")[0])
		return strings.EqualFold(first, "https")
	}
	return false
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// currentUser resolves the session cookie to a user, if present and valid.
func currentUser(r *http.Request, store *Store) (User, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return User{}, false
	}
	u, err := store.UserForSession(c.Value)
	if err != nil {
		return User{}, false
	}
	return u, true
}

// requireUser writes a 401 and returns false when unauthenticated.
func requireUser(w http.ResponseWriter, r *http.Request, store *Store) (User, bool) {
	u, ok := currentUser(r, store)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return User{}, false
	}
	return u, true
}

// ── Simple per-IP rate limiting for auth endpoints ─────────────

type rateEntry struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu    sync.Mutex
	hits  map[string]*rateEntry
	limit int
	win   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: map[string]*rateEntry{}, limit: limit, win: window}
}

func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e := rl.hits[key]
	if e == nil || now.After(e.reset) {
		rl.hits[key] = &rateEntry{count: 1, reset: now.Add(rl.win)}
		return true
	}
	e.count++
	return e.count <= rl.limit
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// registerAuthRoutes mounts /api/auth/*. Authentication and registration
// share a single endpoint (and a per-IP limiter to slow credential stuffing):
// an unknown username is registered, a known one is logged in.
func registerAuthRoutes(mux *http.ServeMux, store *Store) {
	limiter := newRateLimiter(15, 15*time.Minute)

	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !limiter.allow(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
			return
		}
		handleAuth(w, r, store)
	})

	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleLogout(w, r, store)
	})

	mux.HandleFunc("/api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		u, ok := currentUser(r, store)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": u})
	})
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (username, password string, ok bool) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return "", "", false
	}
	return strings.TrimSpace(body.Username), body.Password, true
}

// POST /api/auth — logs in when the username exists, otherwise registers it.
// Responds 200 for an existing account and 201 when a new one was created.
func handleAuth(w http.ResponseWriter, r *http.Request, store *Store) {
	username, password, ok := decodeCredentials(w, r)
	if !ok {
		return
	}
	u, created, err := store.LoginOrRegister(username, password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidLogin):
			writeErr(w, http.StatusUnauthorized, "invalid username or password")
		default:
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if err := startSession(w, r, store, u); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"user": u, "created": created})
}

// POST /api/auth/logout
func handleLogout(w http.ResponseWriter, r *http.Request, store *Store) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		_ = store.DeleteSession(c.Value)
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func startSession(w http.ResponseWriter, r *http.Request, store *Store, u User) error {
	token, expires, err := store.CreateSession(u.ID)
	if err != nil {
		return err
	}
	setSessionCookie(w, r, token, expires)
	return nil
}
