package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestExternalPosterSources(t *testing.T) {
	douban := "https://img1.doubanio.com/view/photo/s_ratio_poster/public/p123.jpg"
	tmdb := "https://image.tmdb.org/t/p/w500/abc.jpg"
	got := externalPosters(M{"poster_path": "/abc.jpg", "cover": douban, "nested": M{"poster": tmdb}, "posters": []any{
		"https://nas.example/sys/img/file", "https://image.tmdb.org.evil.test/t/p/a.jpg", "https://image.tmdb.org/t/p/a.jpg?token=private", "http://img1.doubanio.com/view/photo/a.jpg", douban,
	}})
	if !reflect.DeepEqual(got, []string{douban, tmdb}) {
		t.Fatalf("unexpected ordered sources: %#v", got)
	}
	if got := externalPosters(M{"tmdb_id": 123, "douban_id": 456}); len(got) != 0 {
		t.Fatal("IDs must not be guessed into image paths")
	}
}

func TestPosterCacheNeverFetchesUpstream(t *testing.T) {
	t.Setenv("USER", "testadmin")
	t.Setenv("PASSWORD", "test-password-strong")
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.Write([]byte("upstream")) }))
	defer upstream.Close()
	a := newApp(t.TempDir(), t.TempDir())
	defer a.db.Close()
	a.updateSettings(map[string]string{"fn_server": upstream.URL})
	a.exec(`INSERT INTO movies(id,source_id,title,cover_path,updated_at) VALUES(?,?,?,?,?)`, "movie", "source", "Test", "cover.jpg", nowISO())
	req := httptest.NewRequest("GET", "/api/posters/movie?cache=only", nil)
	rec := httptest.NewRecorder()
	a.posterRoute(rec, req)
	if !strings.Contains(rec.Header().Get("Content-Type"), "image/svg+xml") || hits.Load() != 0 {
		t.Fatal("cache miss must show placeholder without upstream fetch")
	}
	data := []byte("\x89PNG\r\n\x1a\ncached-image")
	cache := filepath.Join(a.dataDir, "posters", "movie")
	if err := os.WriteFile(cache, data, 0600); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	a.posterRoute(rec, req)
	if !reflect.DeepEqual(rec.Body.Bytes(), data) || hits.Load() != 0 {
		t.Fatal("must serve existing cache only")
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("cache must be preserved")
	}
	rec = httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatal("cache endpoint must require login")
	}
}

func TestPosterSourcesPersistAndMigrate(t *testing.T) {
	t.Setenv("USER", "testadmin")
	t.Setenv("PASSWORD", "test-password-strong")
	dir := t.TempDir()
	a := newApp(dir, t.TempDir())
	poster := "https://image.tmdb.org/t/p/w500/abc.jpg"
	a.exec(`INSERT INTO movies(id,source_id,title,updated_at,poster_sources) VALUES(?,?,?,?,?)`, "m", "s", "Test", nowISO(), jsonString([]string{poster}))
	movie := decodeMovie(a.one("SELECT * FROM movies WHERE id=?", "m"))
	if !reflect.DeepEqual(movie.PosterSources, []string{poster}) {
		t.Fatal("poster metadata round trip failed")
	}
	a.db.Close()
	a = newApp(dir, t.TempDir())
	defer a.db.Close()
	if a.one("SELECT id FROM movies WHERE id=?", "m") == nil {
		t.Fatal("upgrade must preserve movies")
	}
}
