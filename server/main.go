package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

type syncState struct {
	Running    bool   `json:"running"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	Stage      string `json:"stage"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	Error      string `json:"error"`
}

type App struct {
	db         *sql.DB
	dataDir    string
	webDir     string
	secret     []byte
	setupToken string
	writeMu    sync.Mutex
	rateMu     sync.Mutex
	rates      map[string]*rateBucket
	fnMu       sync.Mutex
	fnClient   *FnOSClient
	posterMu   sync.Mutex
	syncMu     sync.Mutex
	syncState  syncState
	stop       chan struct{}
}

func newApp(dataDir, webDir string) *App {
	must(os.MkdirAll(filepath.Join(dataDir, "posters"), 0750))
	a := &App{dataDir: dataDir, webDir: webDir, secret: loadSecret(dataDir), rates: map[string]*rateBucket{}, stop: make(chan struct{})}
	a.db = openDB(filepath.Join(dataDir, "app.db"))
	if a.count("SELECT COUNT(*) FROM users") == 0 && env("SETUP_TOKEN", "") == "" {
		a.setupToken = randomHex(12)
		log.Printf("首次初始化令牌（仅本次进程有效）：%s", a.setupToken)
	}
	return a
}

func (a *App) rate(key string, limit int, span time.Duration) bool {
	a.rateMu.Lock()
	if len(a.rates) > 5000 {
		cut := time.Now().Add(-30 * time.Minute)
		for existing, bucket := range a.rates {
			bucket.mu.Lock()
			stale := bucket.last.Before(cut)
			bucket.mu.Unlock()
			if stale {
				delete(a.rates, existing)
			}
		}
	}
	bucket := a.rates[key]
	if bucket == nil {
		bucket = &rateBucket{limit: limit, span: span}
		a.rates[key] = bucket
	}
	a.rateMu.Unlock()
	return bucket.allow(time.Now())
}
func (a *App) clearRate(key string) {
	a.rateMu.Lock()
	delete(a.rates, key)
	a.rateMu.Unlock()
}

func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	must(json.NewEncoder(w).Encode(value))
}

func readBody(r *http.Request) M {
	value := M{}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		fail(400, "JSON 请求体无效")
	}
	return value
}

func (a *App) recoverRequest(w http.ResponseWriter, r *http.Request) {
	if recovered := recover(); recovered != nil {
		status, message := 500, "服务器内部错误"
		if api, ok := recovered.(apiError); ok {
			status, message = api.status, api.message
		} else {
			log.Printf("%s %s: %v\n%s", r.Method, r.URL.Path, recovered, debug.Stack())
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(M{"detail": message})
	}
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer a.recoverRequest(w, r)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		a.api(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "." || strings.HasPrefix(clean, "..") {
		clean = "index.html"
	}
	path := filepath.Join(a.webDir, clean)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		if filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		path = filepath.Join(a.webDir, "index.html")
	}
	if strings.Contains(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFile(w, r, path)
}

func (a *App) api(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/api/health" && r.Method == http.MethodGet:
		a.count("SELECT 1")
		respond(w, M{"ok": true, "version": version})
	case p == "/api/public/config" && r.Method == http.MethodGet:
		s := a.settings()
		respond(w, M{"siteTitle": s["site_title"], "publicAccess": truth(s["public_access"]), "publicPlay": truth(s["public_play"]), "setupRequired": a.count("SELECT COUNT(*) FROM users") == 0, "movieCount": a.count("SELECT COUNT(*) FROM movies"), "lastSyncAt": s["last_sync_at"]})
	case strings.HasPrefix(p, "/api/auth/"):
		a.authRoute(w, r)
	case p == "/api/movies/options" && r.Method == http.MethodGet:
		a.requirePublic(r)
		a.movieOptions(w)
	case p == "/api/movies" && r.Method == http.MethodGet:
		a.requirePublic(r)
		a.listMovies(w, r)
	case p == "/api/random" && r.Method == http.MethodPost:
		a.requirePublic(r)
		if !a.rate("random:"+a.clientIP(r), 30, time.Minute) {
			fail(429, "随机请求过于频繁")
		}
		a.randomMovieRoute(w, r)
	case strings.HasPrefix(p, "/api/posters/") && r.Method == http.MethodGet:
		a.requirePublic(r)
		if !a.rate("poster:"+a.clientIP(r), 240, time.Minute) {
			fail(429, "封面请求过于频繁")
		}
		a.posterRoute(w, r)
	case p == "/api/play" && r.Method == http.MethodGet:
		a.playRoute(w, r)
	case strings.HasPrefix(p, "/api/admin/"):
		a.require(r)
		a.adminRoute(w, r)
	default:
		fail(404, "not found")
	}
}

func (a *App) requirePublic(r *http.Request) {
	if !truth(a.getSetting("public_access", "1")) {
		a.require(r)
	}
}

const version = "0.2.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + env("PORT", "8080") + "/api/health")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		resp.Body.Close()
		return
	}
	a := newApp(env("DATA_DIR", "../data"), env("WEB_DIR", "../frontend/dist"))
	defer a.db.Close()
	go a.schedulerLoop()
	server := &http.Server{
		Addr:              env("LISTEN_ADDR", ":"+env("PORT", "8080")),
		Handler:           securityHeaders(a),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	stop, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()
	go func() {
		<-stop.Done()
		close(a.stop)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("today-random-movie %s · data=%s · listen=%s", version, a.dataDir, server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(fmt.Errorf("listen: %w", err))
	}
}
