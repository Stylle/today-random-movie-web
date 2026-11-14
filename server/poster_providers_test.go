package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProviderOrderAndQuotas(t *testing.T) {
	a := posterApp(t)
	p := map[string]int{"douban": 2, "tmdb": 1, "douying": 0, "cache": 3, "placeholder": 4}
	if !validPosterPriorities(p) {
		t.Fatal("valid disabled provider configuration rejected")
	}
	settings := map[string]string{"poster_priorities": jsonString(p)}
	got := orderedPosters([]string{"https://img1.doubanio.com/view/photo/public/a.jpg", "https://image.tmdb.org/t/p/w500/a.jpg", "https://doubaninfo.com/dbposter/a.jpg"}, settings)
	if len(got) != 2 || !strings.Contains(got[0], "tmdb") {
		t.Fatal("priority/disabled provider not respected")
	}
	if !a.reserveDouying() || a.reserveDouying() {
		t.Fatal("minute spacing not enforced")
	}
	stamps := make([]int64, 200)
	for i := range stamps {
		stamps[i] = time.Now().Add(-time.Hour).UnixMilli()
	}
	a.updateSettings(map[string]string{"poster_douying_usage": jsonString(stamps)})
	if a.reserveDouying() {
		t.Fatal("rolling daily quota not enforced")
	}
	if !matchingTitle("登堂入室", "2012", M{"title": "登堂入室 Dans la maison", "year": "2012"}, "title") {
		t.Fatal("localized/original title suffix not recognized")
	}
}

func TestDouyingHeaderAuthenticationAndMetadata(t *testing.T) {
	a := posterApp(t)
	key := "fixture-douying-key"
	encrypted, _ := seal(a.secret, key)
	a.posterHTTP = &http.Client{Transport: posterTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "doubaninfo.com" || r.Header.Get("X-API-KEY") != key || r.URL.Query().Get("key") != "" {
			t.Error("credential sent incorrectly")
		}
		return posterResponse(200, `{"success":true,"chinese_title":"登堂入室","year":"2012","poster":"https://doubaninfo.com/dbposter/fixture.jpg"}`), nil
	})}
	source, status := a.lookupDouying(context.Background(), Movie{Title: "登堂入室", Year: "2012"}, map[string]string{"poster_douying_key": encrypted})
	if status != "ok" || source != "https://doubaninfo.com/dbposter/fixture.jpg" {
		t.Fatal("Douying metadata not parsed")
	}
}

func TestTelegramPhotoAndTextFallback(t *testing.T) {
	for _, rejectPhoto := range []bool{false, true} {
		t.Run(map[bool]string{false: "photo", true: "fallback"}[rejectPhoto], func(t *testing.T) {
			a := posterApp(t)
			a.exec("UPDATE movies SET poster_sources=?,poster_checked_at=?,poster_lookup_status='ok' WHERE id='fixture'", jsonString([]string{"https://image.tmdb.org/t/p/w500/fixture.jpg"}), time.Now().Unix())
			var methods []string
			movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
				methods = append(methods, method)
				if rejectPhoto && method == "sendPhoto" {
					w.WriteHeader(400)
					w.Write([]byte(`{"ok":false,"error_code":400}`))
					return
				}
				w.Write([]byte(`{"ok":true,"result":{}}`))
			})
			err := a.sendMovieBotPayload(context.Background(), "fixture", M{"_poster_movie": "fixture", "chat_id": "42", "text": "movie fixture", "reply_markup": M{}})
			if err != nil || len(methods) == 0 || methods[0] != "sendPhoto" {
				t.Fatal("photo delivery not attempted")
			}
			if rejectPhoto && (len(methods) != 2 || methods[1] != "sendMessage") {
				t.Fatal("photo failure did not fall back to text")
			}
		})
	}
}

func TestStrictCacheMissContinuesToNextProvider(t *testing.T) {
	a := posterApp(t)
	rec := httptest.NewRecorder()
	func() {
		req := httptest.NewRequest("GET", "/api/posters/fixture?cache=only&strict=1", nil)
		defer a.recoverRequest(rec, req)
		a.posterRoute(rec, req)
	}()
	if rec.Code != 404 {
		t.Fatal("strict cache miss must allow browser fallback")
	}
}
