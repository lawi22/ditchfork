package main

import (
	"database/sql"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type setupHandler struct {
	db  *sql.DB
	app *application
}

func newSetupHandler(app *application) *setupHandler {
	return &setupHandler{db: app.db, app: app}
}

func (h *setupHandler) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	hasUsers, _ := dbHasUsers(h.db)
	if hasUsers {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.app.render(w, "setup.html", nil)
}

func (h *setupHandler) handleSetup(w http.ResponseWriter, r *http.Request) {
	hasUsers, _ := dbHasUsers(h.db)
	if hasUsers {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	siteTitle := strings.TrimSpace(r.FormValue("site_title"))

	if username == "" {
		h.app.render(w, "setup.html", map[string]any{"Error": "Username is required."})
		return
	}
	if len(password) < 8 {
		h.app.render(w, "setup.html", map[string]any{"Error": "Password must be at least 8 characters."})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := dbCreateUser(h.db, username, string(hash)); err != nil {
		h.app.render(w, "setup.html", map[string]any{"Error": "Could not create user. Username may already exist."})
		return
	}

	if siteTitle != "" {
		dbUpdateSetting(h.db, SettingSiteTitle, siteTitle)
	}

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// setupGuard redirects all non-static, non-setup routes to /setup if no users exist.
func setupGuard(db *sql.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/setup") {
			next.ServeHTTP(w, r)
			return
		}
		hasUsers, _ := dbHasUsers(db)
		if !hasUsers {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
