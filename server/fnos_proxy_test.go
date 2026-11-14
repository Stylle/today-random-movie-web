package main

import (
	"net/http"
	"net/url"
	"testing"
)

func TestFnOSPrivateAddressBypassesPublicProxyOnly(t *testing.T) {
	used := false
	fallback := func(*http.Request) (*url.URL, error) { used = true; return url.Parse("http://proxy.example:3128") }
	for _, target := range []string{"http://192.168.10.10:5666/v", "http://10.0.0.2/v", "http://[fd00::1]/v"} {
		r, _ := http.NewRequest("GET", target, nil)
		used = false
		p, e := integrationProxy(r, "fnos", fallback)
		if e != nil || p != nil || used {
			t.Fatal("private NAS sent to public proxy")
		}
	}
	r, _ := http.NewRequest("GET", "https://api.telegram.org", nil)
	used = false
	p, _ := integrationProxy(r, "telegram", fallback)
	if p == nil || !used {
		t.Fatal("Telegram proxy behavior changed")
	}
	r, _ = http.NewRequest("GET", "https://nas.example/v", nil)
	used = false
	p, _ = integrationProxy(r, "fnos", fallback)
	if p == nil || !used {
		t.Fatal("public FnOS proxy behavior changed")
	}
}
