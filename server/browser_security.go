package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

func trustedProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	for _, value := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		value = strings.TrimSpace(value)
		if parsed := net.ParseIP(value); parsed != nil && parsed.Equal(ip) {
			return true
		}
		if _, block, err := net.ParseCIDR(value); err == nil && block.Contains(ip) {
			return true
		}
	}
	return false
}
func requestHTTPS(r *http.Request) bool {
	return r.TLS != nil || os.Getenv("PUBLIC_HTTPS") == "1" || trustedProxy(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
func browserWriteAllowed(r *http.Request) bool {
	if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || !strings.EqualFold(parsed.Host, r.Host) {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	// HTTPS origins also support ordinary TLS-terminating NAS reverse proxies.
	// Never accept a downgrade when HTTPS is known/configured.
	return parsed.Scheme == "https" || !requestHTTPS(r)
}

type loginWindow struct {
	count int
	until time.Time
}
type loginGate struct {
	sync.Mutex
	windows map[string]loginWindow
}

func (g *loginGate) allow(r *http.Request, username string) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if trustedProxy(r) {
		// Only a validated single peer header is accepted; never arbitrary forwarded chains.
		if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
			host = ip.String()
		}
	}
	digest := sha256.Sum256([]byte(host + "\x00" + strings.ToLower(strings.TrimSpace(username))))
	key := hex.EncodeToString(digest[:])
	g.Lock()
	defer g.Unlock()
	if g.windows == nil {
		g.windows = make(map[string]loginWindow)
	}
	now := time.Now()
	if len(g.windows) >= 4096 {
		for k, v := range g.windows {
			if now.After(v.until) {
				delete(g.windows, k)
			}
		}
	}
	// Bound retention without turning a full cache into a global login lockout.
	for len(g.windows) >= 4094 {
		var oldest string
		var until time.Time
		for k, v := range g.windows {
			if oldest == "" || v.until.Before(until) {
				oldest, until = k, v.until
			}
		}
		delete(g.windows, oldest)
	}
	peerDigest := sha256.Sum256([]byte(host))
	peerKey := "peer:" + hex.EncodeToString(peerDigest[:])
	admit := func(k string, limit int) bool {
		v := g.windows[k]
		if now.After(v.until) {
			v = loginWindow{until: now.Add(5 * time.Minute)}
		}
		v.count++
		g.windows[k] = v
		return v.count <= limit
	}
	if !admit(peerKey, 120) {
		return false
	}
	return admit(key, 12)
}
