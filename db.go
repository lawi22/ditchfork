package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_time_format=sqlite")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	return db, nil
}

const tableSchema = `(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT NOT NULL UNIQUE,
	artist TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL,
	subheader TEXT NOT NULL DEFAULT '',
	rating REAL NOT NULL DEFAULT 0,
	body TEXT NOT NULL DEFAULT '',
	cover_path TEXT NOT NULL DEFAULT '',
	article_type TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`

func migrate(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS albums ` + tableSchema,
		`CREATE TABLE IF NOT EXISTS songs ` + tableSchema,
		`CREATE TABLE IF NOT EXISTS articles ` + tableSchema,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration: %w\nSQL: %s", err, m)
		}
	}

	// Migrate data from old reviews table if it exists
	var hasOld int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='reviews'`).Scan(&hasOld)
	if hasOld > 0 {
		db.Exec(`INSERT OR IGNORE INTO albums (id, slug, artist, title, subheader, rating, body, cover_path, created_at, updated_at)
			SELECT id, slug, artist, title, subheader, rating, body, cover_path, created_at, updated_at
			FROM reviews WHERE type = 'album'`)
		db.Exec(`DROP TABLE reviews`)
		log.Println("migrated reviews table to albums")
	}

	// Add article_type column to existing tables (idempotent — ignore if already exists)
	for _, t := range []string{"albums", "songs"} {
		db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN article_type TEXT NOT NULL DEFAULT ''`, t))
	}

	// Seed default settings if table is empty
	var settingCount int
	db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&settingCount)
	if settingCount == 0 {
		for k, v := range settingDefaults {
			db.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`, k, v)
		}
	}

	log.Println("database migrations complete")
	return nil
}

// validTable prevents SQL injection in table-name-parameterized queries.
func validTable(table string) bool {
	_, ok := contentTypeMap[table]
	return ok
}

// Reviews — all queries are parameterized by table name

func dbGetByTable(db *sql.DB, table string) ([]Review, error) {
	if !validTable(table) {
		return nil, fmt.Errorf("invalid table: %s", table)
	}
	rows, err := db.Query(fmt.Sprintf(
		`SELECT id, '%s' as type, slug, artist, title, subheader, rating, body, cover_path, article_type, created_at, updated_at
		 FROM %s ORDER BY created_at DESC`, table, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReviews(rows)
}

func dbGetFeed(db *sql.DB) ([]Review, error) {
	rows, err := db.Query(`
		SELECT id, 'albums' as type, slug, artist, title, subheader, rating, body, cover_path, article_type, created_at, updated_at FROM albums
		UNION ALL
		SELECT id, 'songs' as type, slug, artist, title, subheader, rating, body, cover_path, article_type, created_at, updated_at FROM songs
		UNION ALL
		SELECT id, 'articles' as type, slug, artist, title, subheader, rating, body, cover_path, article_type, created_at, updated_at FROM articles
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReviews(rows)
}

func dbGetBySlug(db *sql.DB, table, slug string) (*Review, error) {
	if !validTable(table) {
		return nil, fmt.Errorf("invalid table: %s", table)
	}
	r := &Review{}
	err := db.QueryRow(fmt.Sprintf(
		`SELECT id, '%s' as type, slug, artist, title, subheader, rating, body, cover_path, article_type, created_at, updated_at
		 FROM %s WHERE slug = ?`, table, table), slug).Scan(
		&r.ID, &r.Type, &r.Slug, &r.Artist, &r.Title, &r.Subheader,
		&r.Rating, &r.Body, &r.CoverPath, &r.ArticleType, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func dbGetByID(db *sql.DB, table string, id int64) (*Review, error) {
	if !validTable(table) {
		return nil, fmt.Errorf("invalid table: %s", table)
	}
	r := &Review{}
	err := db.QueryRow(fmt.Sprintf(
		`SELECT id, '%s' as type, slug, artist, title, subheader, rating, body, cover_path, article_type, created_at, updated_at
		 FROM %s WHERE id = ?`, table, table), id).Scan(
		&r.ID, &r.Type, &r.Slug, &r.Artist, &r.Title, &r.Subheader,
		&r.Rating, &r.Body, &r.CoverPath, &r.ArticleType, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func dbCreateReview(db *sql.DB, table string, r *Review) (int64, error) {
	if !validTable(table) {
		return 0, fmt.Errorf("invalid table: %s", table)
	}
	res, err := db.Exec(fmt.Sprintf(
		`INSERT INTO %s (slug, artist, title, subheader, rating, body, cover_path, article_type)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, table),
		r.Slug, r.Artist, r.Title, r.Subheader, r.Rating, r.Body, r.CoverPath, r.ArticleType)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func dbUpdateReview(db *sql.DB, table string, r *Review) error {
	if !validTable(table) {
		return fmt.Errorf("invalid table: %s", table)
	}
	_, err := db.Exec(fmt.Sprintf(
		`UPDATE %s SET slug = ?, artist = ?, title = ?, subheader = ?, rating = ?, body = ?,
		 cover_path = ?, article_type = ?, updated_at = datetime('now') WHERE id = ?`, table),
		r.Slug, r.Artist, r.Title, r.Subheader, r.Rating, r.Body, r.CoverPath, r.ArticleType, r.ID)
	return err
}

func dbDeleteReview(db *sql.DB, table string, id int64) error {
	if !validTable(table) {
		return fmt.Errorf("invalid table: %s", table)
	}
	_, err := db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, table), id)
	return err
}

func dbSlugExists(db *sql.DB, table, slug string) (bool, error) {
	if !validTable(table) {
		return false, fmt.Errorf("invalid table: %s", table)
	}
	var count int
	err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE slug = ?`, table), slug).Scan(&count)
	return count > 0, err
}

func dbSlugExistsExcluding(db *sql.DB, table, slug string, excludeID int64) (bool, error) {
	if !validTable(table) {
		return false, fmt.Errorf("invalid table: %s", table)
	}
	var count int
	err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE slug = ? AND id != ?`, table),
		slug, excludeID).Scan(&count)
	return count > 0, err
}

// Users

func dbGetUserByUsername(db *sql.DB, username string) (*User, error) {
	u := &User{}
	err := db.QueryRow(`SELECT id, username, password_hash FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func dbCreateUser(db *sql.DB, username, passwordHash string) error {
	_, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, passwordHash)
	return err
}

// Sessions

func dbCreateSession(db *sql.DB, s *Session) error {
	_, err := db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		s.Token, s.UserID, s.ExpiresAt)
	return err
}

func dbGetSession(db *sql.DB, token string) (*Session, error) {
	s := &Session{}
	err := db.QueryRow(`SELECT token, user_id, expires_at FROM sessions WHERE token = ?`, token).
		Scan(&s.Token, &s.UserID, &s.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func dbDeleteSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func dbCleanExpiredSessions(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE expires_at < datetime('now')`)
	return err
}

// Settings

func dbGetAllSettings(db *sql.DB) (map[string]string, error) {
	settings := make(map[string]string)
	for k, v := range settingDefaults {
		settings[k] = v
	}
	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return settings, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

func dbUpdateSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?`, key, value, value)
	return err
}

func dbHasUsers(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0, err
}

// Helpers

func scanReviews(rows *sql.Rows) ([]Review, error) {
	var reviews []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.Type, &r.Slug, &r.Artist, &r.Title, &r.Subheader,
			&r.Rating, &r.Body, &r.CoverPath, &r.ArticleType, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}
