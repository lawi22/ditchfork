package main

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type adminHandler struct {
	db        *sql.DB
	app       *application
	uploadDir string
}

func newAdminHandler(app *application) *adminHandler {
	return &adminHandler{db: app.db, app: app, uploadDir: app.uploadDir}
}

func (h *adminHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	reviews, err := dbGetFeed(h.db)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	h.app.render(w, "admin/dashboard.html", map[string]any{
		"Reviews":      reviews,
		"ContentTypes": contentTypeList,
	})
}

func (h *adminHandler) handleNewForm(w http.ResponseWriter, r *http.Request) {
	h.app.render(w, "admin/form.html", map[string]any{
		"IsNew":        true,
		"IsArticle":    false,
		"ContentTypes": contentTypeList,
		"ArticleTypes": validArticleTypes,
	})
}

func (h *adminHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Request too large", http.StatusBadRequest)
		return
	}

	table := r.FormValue("type")
	ct, ok := contentTypeMap[table]
	if !ok {
		http.Error(w, "Invalid review type", http.StatusBadRequest)
		return
	}

	artist := strings.TrimSpace(r.FormValue("artist"))
	title := strings.TrimSpace(r.FormValue("title"))
	subheader := strings.TrimSpace(r.FormValue("subheader"))
	rating, _ := strconv.ParseFloat(r.FormValue("rating"), 64)
	body := r.FormValue("body")
	articleType := r.FormValue("article_type")
	isArticle := table == "articles"

	if isArticle {
		artist = ""
		rating = 0
	} else {
		articleType = ""
	}

	formData := map[string]string{
		"type": table, "artist": artist, "title": title,
		"subheader": subheader, "rating": r.FormValue("rating"), "body": body,
		"article_type": articleType,
	}

	renderErr := func(msg string) {
		h.app.render(w, "admin/form.html", map[string]any{
			"IsNew": true, "IsArticle": isArticle, "Error": msg,
			"Form": formData, "ContentTypes": contentTypeList, "ArticleTypes": validArticleTypes,
		})
	}

	if !isArticle && artist == "" {
		renderErr("Artist and title are required")
		return
	}
	if title == "" {
		renderErr("Title is required")
		return
	}

	if !isArticle && (rating < 0 || rating > ct.MaxRating) {
		renderErr(fmt.Sprintf("Rating must be between 0 and %.1f", ct.MaxRating))
		return
	}

	slug, err := uniqueSlug(h.db, table, artist, title, 0)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	coverPath, err := h.handleUpload(r)
	if err != nil {
		renderErr(err.Error())
		return
	}

	review := &Review{
		Slug:        slug,
		Artist:      artist,
		Title:       title,
		Subheader:   subheader,
		Rating:      rating,
		Body:        body,
		CoverPath:   coverPath,
		ArticleType: articleType,
	}

	if _, err := dbCreateReview(h.db, table, review); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (h *adminHandler) resolveType(r *http.Request) (*ContentType, bool) {
	table := r.PathValue("type")
	ct, ok := contentTypeMap[table]
	return ct, ok
}

func (h *adminHandler) handleEditForm(w http.ResponseWriter, r *http.Request) {
	ct, ok := h.resolveType(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	review, err := dbGetByID(h.db, ct.Table, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	h.app.render(w, "admin/form.html", map[string]any{
		"IsNew":        false,
		"IsArticle":    ct.Table == "articles",
		"Review":       review,
		"ContentType":  ct,
		"ContentTypes": contentTypeList,
		"ArticleTypes": validArticleTypes,
	})
}

func (h *adminHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ct, ok := h.resolveType(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	existing, err := dbGetByID(h.db, ct.Table, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Request too large", http.StatusBadRequest)
		return
	}

	artist := strings.TrimSpace(r.FormValue("artist"))
	title := strings.TrimSpace(r.FormValue("title"))
	subheader := strings.TrimSpace(r.FormValue("subheader"))
	rating, _ := strconv.ParseFloat(r.FormValue("rating"), 64)
	body := r.FormValue("body")
	articleType := r.FormValue("article_type")
	isArticle := ct.Table == "articles"

	if isArticle {
		artist = ""
		rating = 0
	} else {
		articleType = ""
	}

	renderErr := func(msg string) {
		h.app.render(w, "admin/form.html", map[string]any{
			"IsNew": false, "IsArticle": isArticle, "Error": msg,
			"Review": existing, "ContentType": ct, "ContentTypes": contentTypeList,
			"ArticleTypes": validArticleTypes,
		})
	}

	if !isArticle && artist == "" {
		renderErr("Artist and title are required")
		return
	}
	if title == "" {
		renderErr("Title is required")
		return
	}

	if !isArticle && (rating < 0 || rating > ct.MaxRating) {
		renderErr(fmt.Sprintf("Rating must be between 0 and %.1f", ct.MaxRating))
		return
	}

	slug, err := uniqueSlug(h.db, ct.Table, artist, title, id)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	coverPath, err := h.handleUpload(r)
	if err != nil {
		renderErr(err.Error())
		return
	}
	if coverPath == "" {
		coverPath = existing.CoverPath
	}

	existing.Slug = slug
	existing.Artist = artist
	existing.Title = title
	existing.Subheader = subheader
	existing.Rating = rating
	existing.Body = body
	existing.CoverPath = coverPath
	existing.ArticleType = articleType

	if err := dbUpdateReview(h.db, ct.Table, existing); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (h *adminHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	ct, ok := h.resolveType(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := dbDeleteReview(h.db, ct.Table, id); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (h *adminHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := dbGetAllSettings(h.db)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	h.app.render(w, "admin/settings.html", map[string]any{
		"Settings": settings,
	})
}

func (h *adminHandler) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	for key := range allowedSettingKeys {
		val := r.FormValue(key)
		if val != "" {
			if err := dbUpdateSetting(h.db, key, val); err != nil {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}
	}

	settings, err := dbGetAllSettings(h.db)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	h.app.render(w, "admin/settings.html", map[string]any{
		"Settings": settings,
		"Success":  "Settings saved successfully.",
	})
}

var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

func (h *adminHandler) handleUpload(r *http.Request) (string, error) {
	file, header, err := r.FormFile("cover")
	if err != nil {
		return "", nil // no file uploaded, not an error
	}
	defer file.Close()

	if header.Size > 5<<20 {
		return "", fmt.Errorf("image too large (max 5MB)")
	}

	contentType := header.Header.Get("Content-Type")
	if !allowedImageTypes[contentType] {
		return "", fmt.Errorf("unsupported image type: %s (allowed: jpeg, png, webp)", contentType)
	}

	now := time.Now()
	dir := filepath.Join(h.uploadDir, now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create upload dir: %w", err)
	}

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".jpg"
	}
	filename := fmt.Sprintf("%d%s", now.UnixNano(), ext)
	fullPath := filepath.Join(dir, filename)

	dst, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", fmt.Errorf("save file: %w", err)
	}

	relPath := filepath.Join(now.Format("2006"), now.Format("01"), filename)
	return relPath, nil
}
