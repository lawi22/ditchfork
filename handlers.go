package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
)

type publicHandler struct {
	db  *sql.DB
	app *application
}

func newPublicHandler(app *application) *publicHandler {
	return &publicHandler{db: app.db, app: app}
}

func (h *publicHandler) handleHome(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")

	var reviews []Review
	var err error

	if tab != "" && tab != "all" {
		if !validTable(tab) {
			http.NotFound(w, r)
			return
		}
		reviews, err = dbGetByTable(h.db, tab)
	} else {
		tab = "all"
		reviews, err = dbGetFeed(h.db)
	}

	if err != nil {
		log.Printf("home: get reviews: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	sectionTitle := "Feed"
	if ct, ok := contentTypeMap[tab]; ok {
		sectionTitle = ct.Plural
	}

	h.app.render(w, "home.html", map[string]any{
		"Reviews":      reviews,
		"ActiveTab":    tab,
		"SectionTitle": sectionTitle,
		"ContentTypes": contentTypeList,
	})
}

func (h *publicHandler) handleReview(w http.ResponseWriter, r *http.Request) {
	category := r.PathValue("category")
	slug := r.PathValue("slug")

	ct, ok := urlPathToContentType[category]
	if !ok {
		http.NotFound(w, r)
		return
	}

	review, err := dbGetBySlug(h.db, ct.Table, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	h.app.render(w, "review.html", map[string]any{
		"Review":         review,
		"ReviewBodyHTML": template.HTML(review.Body),
		"MaxRating":      ct.MaxRating,
	})
}
