package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type M = map[string]any

type apiError struct {
	status  int
	message string
}

func fail(status int, message string) { panic(apiError{status: status, message: message}) }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func num(v any) int64 {
	n, _ := strconv.ParseInt(str(v), 10, 64)
	return n
}
func truth(v any) bool {
	s := strings.ToLower(strings.TrimSpace(str(v)))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }
func randomHex(bytes int) string {
	b := make([]byte, bytes)
	_, err := rand.Read(b)
	must(err)
	return hex.EncodeToString(b)
}
func shaHex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func password(value string) string {
	saltHex := randomHex(16)
	salt, _ := hex.DecodeString(saltHex)
	key, err := pbkdf2.Key(sha256.New, value, salt, 310000, 32)
	must(err)
	return fmt.Sprintf("pbkdf2_sha256$310000$%s$%x", saltHex, key)
}
func verifyPassword(value, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 100000 || iterations > 2_000_000 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, value, salt, iterations, 32)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(key)), []byte(parts[3])) == 1
}

func openDB(path string) *sql.DB {
	db, err := sql.Open("sqlite", path)
	must(err)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=15000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-4096",
	} {
		_, err = db.Exec(pragma)
		must(err)
	}
	_, err = db.Exec(schema)
	must(err)
	ensureColumn(db, "movies", "tmdb_id", "TEXT NOT NULL DEFAULT ''")
	ensureColumn(db, "movies", "tmdb_poster_path", "TEXT NOT NULL DEFAULT ''")
	ensureColumn(db, "movies", "metadata_source", "TEXT NOT NULL DEFAULT 'fnos'")
	return db
}

func ensureColumn(db *sql.DB, table, column, definition string) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	must(err)
	found := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		must(rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey))
		found = found || name == column
	}
	must(rows.Close())
	if !found {
		_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition)
		must(err)
	}
}

func scanRows(rows *sql.Rows) []M {
	defer rows.Close()
	columns, err := rows.Columns()
	must(err)
	result := []M{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		must(rows.Scan(pointers...))
		item := M{}
		for i, column := range columns {
			value := values[i]
			if b, ok := value.([]byte); ok {
				value = string(b)
			}
			item[column] = value
		}
		result = append(result, item)
	}
	must(rows.Err())
	return result
}

func (a *App) rows(query string, args ...any) []M {
	rows, err := a.db.Query(query, args...)
	must(err)
	return scanRows(rows)
}
func (a *App) one(query string, args ...any) M {
	items := a.rows(query, args...)
	if len(items) == 0 {
		return nil
	}
	return items[0]
}
func (a *App) exec(query string, args ...any) sql.Result {
	result, err := a.db.Exec(query, args...)
	must(err)
	return result
}
func (a *App) count(query string, args ...any) int64 {
	var result int64
	must(a.db.QueryRow(query, args...).Scan(&result))
	return result
}
func (a *App) getSetting(key, fallback string) string {
	row := a.one("SELECT value FROM settings WHERE key=?", key)
	if row == nil {
		return fallback
	}
	return str(row["value"])
}
func (a *App) settings() map[string]string {
	defaults := map[string]string{
		"site_title":        "今日想看",
		"public_access":     "1",
		"public_play":       "0",
		"webhook_url":       "",
		"webhook_method":    "POST",
		"webhook_body_type": "json",
		"webhook_headers":   "",
		"webhook_body_tpl":  "",
		"last_sync_at":      "",
		"last_sync_error":   "",
		"public_base_url":   "",
		"metadata_source":   "auto",
		"tmdb_token":        "",
		"tmdb_language":     "zh-CN",
	}
	for _, row := range a.rows("SELECT key,value FROM settings") {
		defaults[str(row["key"])] = str(row["value"])
	}
	return defaults
}
func (a *App) updateSettings(values map[string]string) {
	tx, err := a.db.Begin()
	must(err)
	defer tx.Rollback()
	for key, value := range values {
		_, err = tx.Exec("INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at", key, value, nowISO())
		must(err)
	}
	must(tx.Commit())
}
func jsonString(value any) string {
	b, err := json.Marshal(value)
	must(err)
	return string(b)
}
