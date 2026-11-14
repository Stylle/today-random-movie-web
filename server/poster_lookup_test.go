package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type posterTransport func(*http.Request) (*http.Response, error)

func (f posterTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func posterResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func posterApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("USER", "posteradmin")
	t.Setenv("PASSWORD", "poster-test-password")
	a := newApp(t.TempDir(), t.TempDir())
	t.Cleanup(func() { a.db.Close() })
	a.exec("INSERT INTO movies(id,source_id,title,year,updated_at) VALUES(?,?,?,?,?)", "fixture", "source", "登堂入室", "2012", nowISO())
	return a
}

func TestPosterLookupQueriesTMDBWhenDoubanUnavailableAndCoalesces(t *testing.T) {
	a := posterApp(t)
	key := strings.Repeat("a", 32)
	encrypted, _ := seal(a.secret, key)
	a.updateSettings(map[string]string{"poster_tmdb_key": encrypted})
	var douban, tmdb atomic.Int32
	a.posterHTTP = &http.Client{Transport: posterTransport(func(r *http.Request) (*http.Response, error) {
		time.Sleep(30 * time.Millisecond)
		if r.URL.Host == "movie.douban.com" {
			douban.Add(1)
			if r.Header.Get("Authorization") != "" || r.URL.Query().Get("api_key") != "" {
				t.Error("TMDB key sent to Douban")
			}
			return posterResponse(418, "blocked"), nil
		}
		tmdb.Add(1)
		if r.URL.Query().Get("api_key") != key || r.URL.Query().Get("query") != "登堂入室" || r.URL.Query().Get("primary_release_year") != "2012" {
			t.Error("incorrect authenticated search")
		}
		return posterResponse(200, `{"results":[{"title":"登堂入室","release_date":"2020-01-01","poster_path":"/wrong.jpg"},{"title":"登堂入室","release_date":"2012-09-26","poster_path":"/correct.jpg"}]}`), nil
	})}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result := a.resolvePosters(context.Background(), "fixture")
			if jsonString(result["posterSources"]) != `["https://image.tmdb.org/t/p/w500/correct.jpg"]` {
				t.Error("TMDB fallback poster not resolved")
			}
		}()
	}
	workers.Wait()
	a.resolvePosters(context.Background(), "fixture")
	if douban.Load() != 1 || tmdb.Load() != 1 {
		t.Fatal("metadata requests were not cached/coalesced")
	}
	rec := httptest.NewRecorder()
	a.adminRoute(rec, httptest.NewRequest("GET", "/api/admin/settings", nil))
	if strings.Contains(rec.Body.String(), key) || !strings.Contains(rec.Body.String(), `"hasTmdbKey":true`) {
		t.Fatal("settings endpoint leaked key or omitted configuration status")
	}
}

func TestPosterLookupDoubanPreferredAndMissingKeyDoesNotCallTMDB(t *testing.T) {
	a := posterApp(t)
	a.posterHTTP = &http.Client{Transport: posterTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "movie.douban.com" {
			t.Error("TMDB called without key")
		}
		return posterResponse(200, `[{"title":"登堂入室","year":"2012","img":"https://img1.doubanio.com/view/photo/s_ratio_poster/public/p123.jpg"}]`), nil
	})}
	result := a.resolvePosters(context.Background(), "fixture")
	if !strings.Contains(jsonString(result["posterSources"]), "doubanio.com") {
		t.Fatal("Douban image not matched")
	}
	if got := providerID(M{"external_ids": M{"tmdb_id": "1234"}}, "tmdb"); got != "1234" {
		t.Fatal("provider IDs not extracted")
	}
	if matchingTitle("同名影片", "2012", M{"title": "同名影片", "year": "2020"}, "title") {
		t.Fatal("different-year namesake accepted")
	}
}

func TestPosterKeySavePreserveClearAndInvalidation(t *testing.T) {
	a := posterApp(t)
	key := strings.Repeat("b", 32)
	save := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		a.savePosterSettings(rec, httptest.NewRequest("PUT", "/api/admin/poster-settings", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("save failed: %d", rec.Code)
		}
	}
	save(`{"tmdbKey":"` + key + `","doubanEnabled":true}`)
	before := a.settings()
	if before["poster_tmdb_key"] == key {
		t.Fatal("key stored in plaintext")
	}
	save(`{"tmdbKey":"","doubanEnabled":true}`)
	after := a.settings()
	if after["poster_tmdb_key"] != before["poster_tmdb_key"] || after["poster_revision"] == before["poster_revision"] {
		t.Fatal("blank key did not preserve secret/invalidate cache")
	}
	save(`{"clearKey":true,"doubanEnabled":false}`)
	if a.settings()["poster_tmdb_key"] != "" || a.settings()["poster_douban_enabled"] != "0" {
		t.Fatal("clear/disable settings not saved")
	}
}

func TestTMDBReadTokenUsesHeaderAndKnownID(t *testing.T) {
	a := posterApp(t)
	token := "fixture.read.access.token"
	a.posterHTTP = &http.Client{Transport: posterTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("api_key") != "" || r.URL.Path != "/3/movie/77" {
			t.Error("read token/ID request invalid")
		}
		return posterResponse(200, `{"poster_path":"/known.jpg"}`), nil
	})}
	poster, status := a.lookupTMDB(context.Background(), Movie{TMDBID: "77"}, token)
	if status != "ok" || poster != "https://image.tmdb.org/t/p/w500/known.jpg" {
		t.Fatal("known-ID metadata lookup failed")
	}
}
