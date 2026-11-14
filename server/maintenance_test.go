package main

import (
	"bytes"
	"crypto/tls"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyDamageNeverOverwritesExistingState(t *testing.T) {
	for _, kind := range []string{"corrupt", "missing"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "secret.key")
			if kind == "corrupt" {
				if e := os.WriteFile(file, []byte("damaged"), 0600); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.WriteFile(filepath.Join(dir, "app.db"), []byte("existing"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Error("unsafe initialization accepted")
					}
				}()
				loadSecret(dir)
			}()
			raw, e := os.ReadFile(file)
			if kind == "corrupt" && (e != nil || !bytes.Equal(raw, []byte("damaged"))) {
				t.Fatal("damaged key changed")
			}
			if kind == "missing" && !os.IsNotExist(e) {
				t.Fatal("replacement key created")
			}
		})
	}
	dir := t.TempDir()
	key := loadSecret(dir)
	encrypted, e := seal(key, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	value, e := openSealed(loadSecret(dir), encrypted)
	if e != nil || value != "fixture" {
		t.Fatal("valid key changed")
	}
}
func TestBrowserWriteOriginBoundary(t *testing.T) {
	for _, origin := range []string{"http://evil.test", "null", "http://example.com.evil.test", "http://user@example.com", "http://example.com/path", "http://example.com?x=1"} {
		r := httptest.NewRequest("POST", "http://example.com/api/auth/login", nil)
		r.Header.Set("Origin", origin)
		if browserWriteAllowed(r) {
			t.Errorf("accepted origin %q", origin)
		}
	}
	for _, origin := range []string{"", "http://example.com", "https://example.com"} {
		r := httptest.NewRequest("POST", "http://example.com/api/auth/login", nil)
		r.Header.Set("Origin", origin)
		if !browserWriteAllowed(r) {
			t.Errorf("legitimate origin rejected %q", origin)
		}
	}
	r := httptest.NewRequest("POST", "https://example.com/api/auth/login", nil)
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Origin", "http://example.com")
	if browserWriteAllowed(r) {
		t.Fatal("HTTPS downgraded")
	}
	r.Header.Del("Origin")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if browserWriteAllowed(r) {
		t.Fatal("cross-site browser accepted without origin")
	}
}
func TestLoginThrottleAndForwardedHTTPS(t *testing.T) {
	var g loginGate
	r := httptest.NewRequest("POST", "http://example.com/api/auth/login", nil)
	r.RemoteAddr = "192.0.2.1:123"
	r.Header.Set("X-Forwarded-Proto", "https")
	if requestHTTPS(r) {
		t.Fatal("untrusted proxy controlled cookie")
	}
	for i := 0; i < 12; i++ {
		if !g.allow(r, "admin") {
			t.Fatal("limit too early")
		}
	}
	if g.allow(r, "admin") {
		t.Fatal("unlimited attempts")
	}
	r.RemoteAddr = "192.0.2.2:123"
	if !g.allow(r, "admin") {
		t.Fatal("unrelated peer locked out")
	}
	t.Setenv("TRUSTED_PROXIES", "192.0.2.0/24")
	if !requestHTTPS(r) {
		t.Fatal("trusted proxy ignored")
	}
	a, c := movieBotTestApp(t)
	request := httptest.NewRequest("POST", "http://example.com/api/admin/random-defaults", strings.NewReader(`{}`))
	request.AddCookie(c)
	request.Header.Set("Origin", "http://evil.test")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("handler bypassed origin guard")
	}
}
