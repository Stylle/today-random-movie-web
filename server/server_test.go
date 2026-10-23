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

func TestEnvironmentAdminLoginAndPrivateRandom(t *testing.T) {
	t.Setenv("USER", "admin")
	t.Setenv("PASSWORD", "a-strong-password")
	a := newApp(t.TempDir(), t.TempDir())
	defer a.db.Close()

	loginBody := `{"username":"admin","password":"a-strong-password","remember":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
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
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("random without login: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/random", strings.NewReader(`{"filters":{"excludeWatched":true}}`))
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "测试电影") {
		t.Fatalf("random: %d %s", rec.Code, rec.Body.String())
	}

	t.Setenv("USER", "newadmin")
	t.Setenv("PASSWORD", "new-password")
	if err := a.syncAdminFromEnv(); err != nil {
		t.Fatal(err)
	}
	row := a.one("SELECT username,pwd FROM users LIMIT 1")
	if row["username"] != "newadmin" || !verifyPassword("new-password", str(row["pwd"])) {
		t.Fatalf("admin-sync did not update credentials: %#v", row)
	}
}

func TestExistingAdminIgnoresEnvironmentUntilSync(t *testing.T) {
	dataDir := t.TempDir()
	webDir := t.TempDir()
	t.Setenv("USER", "firstadmin")
	t.Setenv("PASSWORD", "first-password")
	a := newApp(dataDir, webDir)
	a.db.Close()

	t.Setenv("USER", "changedadmin")
	t.Setenv("PASSWORD", "changed-password")
	a = newApp(dataDir, webDir)
	defer a.db.Close()
	row := a.one("SELECT username,pwd FROM users LIMIT 1")
	if row["username"] != "firstadmin" || !verifyPassword("first-password", str(row["pwd"])) {
		t.Fatalf("restart unexpectedly changed credentials: %#v", row)
	}
	if err := a.syncAdminFromEnv(); err != nil {
		t.Fatal(err)
	}
	row = a.one("SELECT username,pwd FROM users LIMIT 1")
	if row["username"] != "changedadmin" || !verifyPassword("changed-password", str(row["pwd"])) {
		t.Fatalf("admin-sync did not apply credentials: %#v", row)
	}
}
