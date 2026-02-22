package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "ditchfork_session"

// ---------- rate limiter ----------

type loginAttempt struct {
	failures int
	lastFail time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

func newLoginLimiter() *loginLimiter {
	ll := &loginLimiter{attempts: make(map[string]*loginAttempt)}
	// clean up stale entries every 30 minutes
	go func() {
		for range time.Tick(30 * time.Minute) {
			ll.mu.Lock()
			for ip, a := range ll.attempts {
				if time.Since(a.lastFail) > 30*time.Minute {
					delete(ll.attempts, ip)
				}
			}
			ll.mu.Unlock()
		}
	}()
	return ll
}

// cooldown returns how long the IP must wait, or 0 if they can try now.
func (ll *loginLimiter) cooldown(ip string) time.Duration {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	a, ok := ll.attempts[ip]
	if !ok || a.failures < 3 {
		return 0
	}
	// exponential: 1s, 2s, 4s, 8s … capped at 15 min
	shift := a.failures - 3
	if shift > 9 {
		shift = 9
	}
	wait := time.Second * (1 << shift)
	elapsed := time.Since(a.lastFail)
	if elapsed >= wait {
		return 0
	}
	return wait - elapsed
}

func (ll *loginLimiter) recordFailure(ip string) {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	a, ok := ll.attempts[ip]
	if !ok {
		a = &loginAttempt{}
		ll.attempts[ip] = a
	}
	a.failures++
	a.lastFail = time.Now()
}

func (ll *loginLimiter) reset(ip string) {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	delete(ll.attempts, ip)
}

type authHandler struct {
	db      *sql.DB
	app     *application
	limiter *loginLimiter
}

func newAuthHandler(app *application) *authHandler {
	return &authHandler{db: app.db, app: app, limiter: newLoginLimiter()}
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.SplitN(fwd, ",", 2)[0]
	}
	// strip port
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}

func (h *authHandler) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	h.app.render(w, "admin/login.html", nil)
}

func (h *authHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	if wait := h.limiter.cooldown(ip); wait > 0 {
		secs := int(wait.Seconds()) + 1
		msg := fmt.Sprintf("Too many attempts. Try again in %ds.", secs)
		log.Printf("login rate-limited: ip=%s wait=%s", ip, wait)
		h.app.render(w, "admin/login.html", map[string]any{"Error": msg})
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := dbGetUserByUsername(h.db, username)
	if err != nil {
		h.limiter.recordFailure(ip)
		log.Printf("login failed: ip=%s user=%q (not found)", ip, username)
		h.app.render(w, "admin/login.html", map[string]any{"Error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		h.limiter.recordFailure(ip)
		log.Printf("login failed: ip=%s user=%q (bad password)", ip, username)
		h.app.render(w, "admin/login.html", map[string]any{"Error": "Invalid credentials"})
		return
	}

	h.limiter.reset(ip)
	log.Printf("login success: ip=%s user=%q", ip, username)

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(tokenBytes)

	session := &Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := dbCreateSession(h.db, session); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (h *authHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		dbDeleteSession(h.db, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *authHandler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		session, err := dbGetSession(h.db, cookie.Value)
		if err != nil || time.Now().After(session.ExpiresAt) {
			if err == nil {
				dbDeleteSession(h.db, cookie.Value)
			}
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}

func startSessionCleanup(db *sql.DB) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := dbCleanExpiredSessions(db); err != nil {
				log.Printf("session cleanup error: %v", err)
			}
		}
	}()
}
