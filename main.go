package main

import (
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
)

//go:embed all:templates
var templateFS embed.FS

//go:embed all:static
var staticFS embed.FS

type application struct {
	db        *sql.DB
	templates map[string]*template.Template
	uploadDir string
}

func (app *application) render(w http.ResponseWriter, name string, data map[string]any) {
	tmpl, ok := app.templates[name]
	if !ok {
		log.Printf("template %s not found", name)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if data == nil {
		data = make(map[string]any)
	}
	data["Year"] = time.Now().Year()
	if _, ok := data["Settings"]; !ok {
		settings, _ := dbGetAllSettings(app.db)
		data["Settings"] = settings
	}

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("template error (%s): %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func main() {
	initAdmin := flag.String("init-admin", "", "Create admin user with given username:password and exit")
	flag.Parse()

	port := envOr("DITCHFORK_PORT", "8080")
	dbPath := envOr("DITCHFORK_DB_PATH", "./ditchfork.db")
	uploadDir := envOr("DITCHFORK_UPLOAD_DIR", "./uploads")

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatalf("create upload dir: %v", err)
	}

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if *initAdmin != "" {
		handleInitAdmin(db, *initAdmin)
		return
	}

	templates := parseTemplates()

	app := &application{
		db:        db,
		templates: templates,
		uploadDir: uploadDir,
	}

	pub := newPublicHandler(app)
	auth := newAuthHandler(app)
	adm := newAdminHandler(app)
	setup := newSetupHandler(app)

	mux := http.NewServeMux()

	// Static files (embedded)
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Uploaded images (filesystem)
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))

	// Setup routes
	mux.HandleFunc("GET /setup", setup.handleSetupForm)
	mux.HandleFunc("POST /setup", setup.handleSetup)

	// Public routes
	mux.HandleFunc("GET /{$}", pub.handleHome)
	mux.HandleFunc("GET /music/{category}/{slug}", pub.handleReview)

	// Auth routes
	mux.HandleFunc("GET /admin/login", auth.handleLoginForm)
	mux.HandleFunc("POST /admin/login", auth.handleLogin)
	mux.HandleFunc("POST /admin/logout", auth.requireAuth(auth.handleLogout))

	// Admin routes (all require auth)
	mux.HandleFunc("GET /admin/{$}", auth.requireAuth(adm.handleDashboard))
	mux.HandleFunc("GET /admin/reviews/new", auth.requireAuth(adm.handleNewForm))
	mux.HandleFunc("POST /admin/reviews", auth.requireAuth(adm.handleCreate))
	mux.HandleFunc("GET /admin/{type}/{id}/edit", auth.requireAuth(adm.handleEditForm))
	mux.HandleFunc("POST /admin/{type}/{id}", auth.requireAuth(adm.handleUpdate))
	mux.HandleFunc("POST /admin/{type}/{id}/delete", auth.requireAuth(adm.handleDelete))

	// Settings
	mux.HandleFunc("GET /admin/settings", auth.requireAuth(adm.handleSettings))
	mux.HandleFunc("POST /admin/settings", auth.requireAuth(adm.handleSettingsSave))

	startSessionCleanup(db)

	handler := setupGuard(db, mux)

	addr := ":" + port
	log.Printf("ditchfork starting on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func parseTemplates() map[string]*template.Template {
	funcMap := template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"typeLabel": func(table string) string {
			if ct, ok := contentTypeMap[table]; ok {
				return ct.Singular
			}
			return table
		},
		"typePath": func(table string) string {
			if ct, ok := contentTypeMap[table]; ok {
				return ct.URLPath
			}
			return table
		},
		"maxRating": func(table string) float64 {
			if ct, ok := contentTypeMap[table]; ok {
				return ct.MaxRating
			}
			return 10.0
		},
		"fmtRating": func(r float64) string {
			return fmt.Sprintf("%.1f", r)
		},
		"ratingClass": func(rating float64, table string) string {
			max := 10.0
			if ct, ok := contentTypeMap[table]; ok {
				max = ct.MaxRating
			}
			if max == 0 {
				return ""
			}
			pct := (rating / max) * 100
			if pct >= 80 {
				return "rating-high"
			}
			return ""
		},
		"isArticle": func(table string) bool {
			return table == "articles"
		},
	}

	pages := []string{
		"templates/home.html",
		"templates/review.html",
		"templates/admin/login.html",
		"templates/admin/dashboard.html",
		"templates/admin/form.html",
		"templates/admin/settings.html",
		"templates/setup.html",
	}

	templates := make(map[string]*template.Template)
	for _, page := range pages {
		tmpl := template.Must(
			template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/base.html", page),
		)
		name := page[len("templates/"):]
		templates[name] = tmpl
	}
	return templates
}

func handleInitAdmin(db *sql.DB, creds string) {
	parts := splitOnce(creds, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		log.Fatal("--init-admin expects username:password")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(parts[1]), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}

	if err := dbCreateUser(db, parts[0], string(hash)); err != nil {
		log.Fatalf("create user: %v", err)
	}

	fmt.Printf("admin user '%s' created successfully\n", parts[0])
}

func splitOnce(s, sep string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep[0] {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
