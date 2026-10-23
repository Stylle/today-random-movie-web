package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	mu    sync.Mutex
	hits  []time.Time
	limit int
	span  time.Duration
	last  time.Time
}

func (b *rateBucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	cut := now.Add(-b.span)
	kept := b.hits[:0]
	for _, hit := range b.hits {
		if hit.After(cut) {
			kept = append(kept, hit)
		}
	}
	b.hits = kept
	b.last = now
	if len(b.hits) >= b.limit {
		return false
	}
	b.hits = append(b.hits, now)
	return true
}

func matchIP(ip, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if strings.Contains(pattern, "/") {
		_, network, err := net.ParseCIDR(pattern)
		return err == nil && network.Contains(net.ParseIP(ip))
	}
	want := net.ParseIP(pattern)
	got := net.ParseIP(ip)
	return want != nil && got != nil && want.Equal(got)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *App) clientIP(r *http.Request) string {
	ip := remoteIP(r)
	if !trustedProxy(r) {
		return ip
	}
	if direct := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(direct) != nil {
		return direct
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(parts) > 0 {
		candidate := strings.TrimSpace(parts[0])
		if net.ParseIP(candidate) != nil {
			return candidate
		}
	}
	return ip
}

func osEnv(key string) string { return env(key, "") }

// trustedProxy uses an explicit allowlist when configured. Without one, only a
// private/loopback peer is treated as a reverse proxy, which covers NAS tunnel
// and local proxy services without trusting forwarded headers from the public
// internet.
func trustedProxy(r *http.Request) bool {
	ip := remoteIP(r)
	patterns := strings.TrimSpace(osEnv("TRUSTED_PROXIES"))
	if patterns == "" {
		return privateAddress(net.ParseIP(ip))
	}
	for _, pattern := range strings.Split(patterns, ",") {
		if matchIP(ip, pattern) {
			return true
		}
	}
	return false
}

func forwardedHost(r *http.Request) string {
	if !trustedProxy(r) {
		return ""
	}
	value := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if value == "" {
		value = strings.TrimSpace(strings.Split(r.Header.Get("X-Original-Host"), ",")[0])
	}
	return value
}

func forwardedHTTPS(r *http.Request) bool {
	return trustedProxy(r) && strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func blockedAddress(ip net.IP) bool {
	return ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast()
}

func privateAddress(ip net.IP) bool {
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}

func validateTarget(rawURL string, mode string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("仅支持不含账号信息的 http/https URL")
	}
	ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip", u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("目标域名无法解析")
	}
	for _, ip := range ips {
		if blockedAddress(ip) {
			return nil, fmt.Errorf("目标地址不在允许的网络范围")
		}
	}
	return u, nil
}

func restrictedClient(mode string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          24,
		MaxIdleConnsPerHost:   12,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("目标域名无法解析")
			}
			for _, ip := range ips {
				if blockedAddress(ip) {
					continue
				}
				return (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
			return nil, fmt.Errorf("目标地址不在允许的网络范围")
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("重定向次数过多")
			}
			_, err := validateTarget(req.URL.String(), mode)
			return err
		},
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.TLS != nil || forwardedHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
