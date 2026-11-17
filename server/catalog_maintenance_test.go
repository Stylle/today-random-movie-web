package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFullSyncRemovesDeletedMoviesButPreservesIndexOnEmptyOrFailedResponse(t *testing.T) {
	a, _ := movieBotTestApp(t)
	seedMovieBot(a)
	mode := "populated"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if mode == "failed" {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/item/list" {
			if mode == "empty" {
				_, _ = w.Write([]byte(`{"items":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"guid":"current","title":"Current movie","libraryId":"a","libraryName":"Movies"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	a.fnClient = &FnOSClient{apiBases: []string{upstream.URL}, client: upstream.Client()}
	a.runSync()
	if a.syncSnapshot().Error != "" || a.count("SELECT COUNT(*) FROM movies") != 1 || a.count("SELECT COUNT(*) FROM movies WHERE source_id='current'") != 1 {
		t.Fatalf("full sync did not replace deleted movies: error=%s count=%d", a.syncSnapshot().Error, a.count("SELECT COUNT(*) FROM movies"))
	}
	for _, next := range []string{"empty", "failed"} {
		mode = next
		a.runSync()
		if a.syncSnapshot().Error == "" || a.count("SELECT COUNT(*) FROM movies") != 1 {
			t.Fatal("empty/failed sync changed the previous index")
		}
	}
}

func TestPurgeCoverCacheDoesNotClearMovieIndex(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	dir := filepath.Join(a.dataDir, "posters")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.jpg"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	movieBotJSON(t, movieBotReq(a, cookie, "POST", "/api/admin/posters/purge", nil))
	if _, err := os.Stat(filepath.Join(dir, "fixture.jpg")); !os.IsNotExist(err) {
		t.Fatal("cache was not removed")
	}
	if a.count("SELECT COUNT(*) FROM movies") != 2 {
		t.Fatal("cover cleanup changed the catalog")
	}
}
