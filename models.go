package main

import "time"

type Review struct {
	ID          int64
	Type        string // table name: "albums", "songs", "articles"
	Slug        string
	Artist      string
	Title       string
	Subheader   string
	Rating      float64
	Body        string
	CoverPath   string
	ArticleType string // "News", "Opinion", "List" (only for articles)
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type User struct {
	ID           int64
	Username     string
	PasswordHash string
}

type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

// Content type configuration

type ContentType struct {
	Table     string // DB table name: "albums", "songs"
	Singular  string // display: "Album", "Song"
	Plural    string // display: "Albums", "Songs"
	URLPath   string // public URL segment: "albums", "songs"
	MaxRating float64 // max rating value
}

var contentTypeMap = map[string]*ContentType{
	"albums":   {"albums", "Album", "Albums", "albums", 10.0},
	"songs":    {"songs", "Song", "Songs", "songs", 10.0},
	"articles": {"articles", "Article", "Articles", "articles", 0},
}

// Ordered list for tabs/UI
var contentTypeList = []*ContentType{
	contentTypeMap["albums"],
	contentTypeMap["songs"],
	contentTypeMap["articles"],
}

// Map public URL path segments to content types
var urlPathToContentType = map[string]*ContentType{
	"albums":   contentTypeMap["albums"],
	"songs":    contentTypeMap["songs"],
	"articles": contentTypeMap["articles"],
}

// Valid article type values
var validArticleTypes = []string{"News", "Opinion", "List"}

// Settings keys and defaults
const (
	SettingSiteTitle  = "site_title"
	SettingNavBgColor = "nav_bg_color"
	SettingPageBgColor = "page_bg_color"
	SettingAccentColor = "accent_color"
)

var settingDefaults = map[string]string{
	SettingSiteTitle:   "Ditchfork",
	SettingNavBgColor:  "#111111",
	SettingPageBgColor: "#ffffff",
	SettingAccentColor: "#d62828",
}

var allowedSettingKeys = map[string]bool{
	SettingSiteTitle:   true,
	SettingNavBgColor:  true,
	SettingPageBgColor: true,
	SettingAccentColor: true,
}
