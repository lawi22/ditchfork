package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

var (
	slugNonAlnum  = regexp.MustCompile(`[^a-z0-9-]+`)
	slugMultiDash = regexp.MustCompile(`-{2,}`)
)

func generateSlug(artist, title string) string {
	s := strings.ToLower(artist + "-" + title)
	s = slugNonAlnum.ReplaceAllString(s, "-")
	s = slugMultiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	return s
}

func uniqueSlug(db *sql.DB, table, artist, title string, excludeID int64) (string, error) {
	base := generateSlug(artist, title)
	slug := base
	for i := 2; ; i++ {
		var exists bool
		var err error
		if excludeID > 0 {
			exists, err = dbSlugExistsExcluding(db, table, slug, excludeID)
		} else {
			exists, err = dbSlugExists(db, table, slug)
		}
		if err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}
