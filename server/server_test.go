package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPasswordAndCredentialEncryption(t *testing.T) {
	encoded := password("a-strong-password")
	if !verifyPassword("a-strong-password", encoded) || verifyPassword("wrong", encoded) {
		t.Fatal("password verification mismatch")
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := seal(key, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := openSealed(key, sealed)
	if err != nil || opened != "secret-value" {
		t.Fatalf("credential round trip: %q %v", opened, err)
	}
}

func TestRenderNotifyJSONEscapesValues(t *testing.T) {
	payload, err := renderNotifyJSON(`{"title":"{{title}}","fields":{{fields}}}`, M{"title": "line\n\"two\"", "fields": `{"ok":true}`})
	if err != nil || !json.Valid(payload) {
		t.Fatalf("invalid payload %s: %v", payload, err)
	}
	var parsed M
	_ = json.Unmarshal(payload, &parsed)
	if parsed["title"] != "line\n\"two\"" {
		t.Fatalf("unexpected title: %#v", parsed["title"])
	}
}

func TestScheduleDue(t *testing.T) {
	now := time.Date(2026, 10, 23, 20, 0, 0, 0, time.FixedZone("CST", 8*3600)) // Friday
	cases := []struct {
		row M
		key string
	}{
		{M{"timezone": "Asia/Shanghai", "time_of_day": "20:00", "kind": "daily"}, "2026-10-23"},
		{M{"timezone": "Asia/Shanghai", "time_of_day": "20:00", "kind": "weekly", "weekday": 5}, "2026-W43"},
		{M{"timezone": "Asia/Shanghai", "time_of_day": "20:00", "kind": "monthly", "day_of_month": 23}, "2026-10"},
	}
	for _, tc := range cases {
		due, key := scheduleDue(tc.row, now)
		if !due || key != tc.key {
			t.Fatalf("schedule %#v: due=%v key=%q", tc.row, due, key)
		}
	}
}

func TestMovieFilters(t *testing.T) {
	movie := Movie{LibraryID: "lib-1", Genres: []string{"剧情", "悬疑"}, Countries: []string{"中国香港"}, Year: "2024", Watched: false}
	if !movieMatches(movie, Filters{Libraries: []string{"lib-1"}, Genre: "悬疑", Country: "华语", Year: "2024", ExcludeWatched: true}) {
		t.Fatal("expected matching movie")
	}
	movie.Watched = true
	if movieMatches(movie, Filters{ExcludeWatched: true}) {
		t.Fatal("watched movie must be excluded")
	}
}

func TestTunnelProxyOriginAndHTTPS(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	req := httptest.NewRequest(http.MethodPost, "http://container/api/auth/login", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("Origin", "https://movie.example.com")
	req.Header.Set("X-Forwarded-Host", "movie.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	if !sameOrigin(req) || !secureRequest(req) {
		t.Fatal("private NAS reverse proxy headers should be accepted automatically")
	}
	req.RemoteAddr = "8.8.8.8:41234"
	if sameOrigin(req) || secureRequest(req) {
		t.Fatal("public direct peers must not be able to spoof proxy headers")
	}
}

func TestTMDBMetadataMapping(t *testing.T) {
	movie := Movie{Title: "流浪地球", Year: "2019", Genres: []string{"科幻"}, MetadataSource: "fnos"}
	applyTMDB(&movie, tmdbMovie{ID: 535167, Title: "流浪地球", ReleaseDate: "2019-02-05", PosterPath: "/poster.jpg", Overview: "来自 TMDB 的简介", GenreIDs: []int{878, 18}, VoteAverage: 7.1})
	if movie.TMDBID != "535167" || movie.TMDBPosterPath != "/poster.jpg" || movie.MetadataSource != "tmdb" {
		t.Fatalf("TMDB identity mapping failed: %#v", movie)
	}
	if movie.Overview != "来自 TMDB 的简介" || len(movie.Genres) != 2 || movie.Rating != "7.1" {
		t.Fatalf("TMDB details mapping failed: %#v", movie)
	}
	if normalizedTitle("Spider-Man: No Way Home") != normalizedTitle("Spider Man No Way Home") {
		t.Fatal("title normalization should ignore punctuation")
	}
}

func TestSetupSessionAndPublicRandom(t *testing.T) {
	t.Setenv("SETUP_TOKEN", "setup-token-for-test")
	a := newApp(t.TempDir(), t.TempDir())
	defer a.db.Close()

	setupBody := `{"setupToken":"setup-token-for-test","username":"admin","password":"a-very-strong-password","remember":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", strings.NewReader(setupBody))
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].HttpOnly == false {
		t.Fatal("expected HttpOnly session cookie")
	}

	a.exec(`INSERT INTO movies(id,source_id,title,year,genres,countries,overview,cover_path,library_id,library,watched,duration,rating,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "m1", "source-1", "测试电影", "2026", `["剧情"]`, `["中国"]`, "简介", "", "lib", "电影", 0, "120", "8.0", nowISO())
	req = httptest.NewRequest(http.MethodPost, "/api/random", strings.NewReader(`{"filters":{"excludeWatched":true}}`))
	rec = httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "测试电影") {
		t.Fatalf("random: %d %s", rec.Code, rec.Body.String())
	}
}
