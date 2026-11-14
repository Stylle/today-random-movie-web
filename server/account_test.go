package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordChangeRevokesAllSessions(t *testing.T) {
	t.Setenv("USER", "admin")
	t.Setenv("PASSWORD", "Original-Password-123")
	a := newApp(t.TempDir(), t.TempDir())
	defer a.db.Close()
	call := func(path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	login := call("/api/auth/login", `{"username":"admin","password":"Original-Password-123"}`, nil)
	if login.Code != 200 {
		t.Fatal("login failed")
	}
	cookie := login.Result().Cookies()[0]
	failed := call("/api/auth/password", `{"currentPassword":"wrong","newPassword":"Replacement-Password-123"}`, cookie)
	if failed.Code != 403 {
		t.Fatal("wrong password accepted")
	}
	changed := call("/api/auth/password", `{"currentPassword":"Original-Password-123","newPassword":"Replacement-Password-123"}`, cookie)
	if changed.Code != 200 {
		t.Fatal("change failed", changed.Code)
	}
	r := httptest.NewRequest("GET", "/api/auth/me", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("old session survived")
	}
	if call("/api/auth/login", `{"username":"admin","password":"Original-Password-123"}`, nil).Code != 401 {
		t.Fatal("old password survived")
	}
	if call("/api/auth/login", `{"username":"admin","password":"Replacement-Password-123"}`, nil).Code != 200 {
		t.Fatal("new password rejected")
	}
}
