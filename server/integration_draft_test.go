package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDraftIntegrationsDoNotPersist(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	before := a.settings()
	oldHits, newHits := 0, 0
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { oldHits++; w.WriteHeader(500) }))
	defer old.Close()
	current := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		newHits++
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/login") {
			fmt.Fprint(w, `{"code":0,"data":{"token":"fixture-session"}}`)
		} else {
			fmt.Fprint(w, `{"data":[{"id":"fixture","name":"movies"}]}`)
		}
	}))
	defer current.Close()
	a.updateSettings(map[string]string{"fn_server": old.URL, "webhook_url": old.URL})
	before = a.settings()
	w := movieBotReq(a, cookie, "POST", "/api/admin/fnos/test", M{"server": current.URL + "/v", "username": "fixture", "password": "fixture-password"})
	if w.Code != 200 {
		t.Fatal("draft FnOS failed", w.Code)
	}
	w = movieBotReq(a, cookie, "POST", "/api/admin/webhook/test", M{"url": current.URL, "method": "POST", "headers": "{}", "bodyType": "json", "bodyTemplate": ""})
	if w.Code != 200 {
		t.Fatal("draft webhook failed", w.Code)
	}
	if oldHits != 0 || newHits < 2 {
		t.Fatal("test used saved endpoint")
	}
	after := a.settings()
	for _, k := range []string{"fn_server", "fn_password", "webhook_url"} {
		if before[k] != after[k] {
			t.Fatal("test persisted draft")
		}
	}
}
