package main

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Stylle/today-random-movie-web/server/internal/portablebackup"
)

var backupSpec = portablebackup.Spec{App: "today-random-movie", Database: "app.db", Key: "secret.key", Directories: []string{"posters"}, Tables: []string{"users", "settings", "movies", "schedules", "sessions"}}

func validateMovieBackup(stage string) error {
	key, e := os.ReadFile(filepath.Join(stage, "secret.key"))
	if e != nil {
		return e
	}
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(stage, "app.db"))+"?mode=ro")
	if e != nil {
		return e
	}
	defer db.Close()
	var admins int
	if e = db.QueryRow("SELECT COUNT(*) FROM users WHERE role='admin'").Scan(&admins); e != nil || admins < 1 {
		return errors.New("backup has no administrator")
	}
	rows, e := db.Query("SELECT value FROM settings WHERE key IN ('fn_username','fn_password','poster_tmdb_key','poster_douying_key') UNION ALL SELECT value FROM telegram_movie_config")
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if e = rows.Scan(&v); e != nil {
			return e
		}
		if v != "" {
			if _, e = openSealed(key, v); e != nil {
				return errors.New("backup key does not match credentials")
			}
		}
	}
	return rows.Err()
}

func (a *App) backupRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		f, e := os.CreateTemp(a.dataDir, ".download-*.zip")
		must(e)
		name := f.Name()
		f.Close()
		os.Remove(name)
		defer os.Remove(name)
		if e = portablebackup.Export(a.dataDir, a.db, backupSpec, name); e != nil {
			fail(500, "创建完整备份失败，请检查磁盘和缓存文件")
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="today-random-movie-`+time.Now().Format("20060102-150405")+`.zip"`)
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, name)
		return
	}
	if r.Header.Get("X-Confirm-Restore") != "replace" {
		fail(400, "请确认恢复将替换当前数据")
	}
	if !a.restoring.CompareAndSwap(false, true) {
		fail(409, "已有恢复正在进行")
	}
	accepted := false
	defer func() {
		if !accepted {
			a.restoring.Store(false)
		}
	}()
	a.syncMu.Lock()
	running := a.syncState.Running
	a.syncMu.Unlock()
	if running {
		fail(409, "请等待媒体库同步完成后恢复")
	}
	r.Body = http.MaxBytesReader(w, r.Body, portablebackup.Limit+32<<20)
	f, e := os.CreateTemp(a.dataDir, ".import-*.zip")
	must(e)
	name := f.Name()
	defer os.Remove(name)
	_, e = io.Copy(f, r.Body)
	ce := f.Close()
	if e != nil || ce != nil {
		fail(400, "备份上传不完整或超过大小限制")
	}
	if e = portablebackup.Stage(a.dataDir, name, backupSpec, validateMovieBackup); e != nil {
		fail(400, "备份无效、损坏或不属于今日想看，当前数据未修改")
	}
	accepted = true
	respond(w, M{"ok": true, "relogin": true, "message": "备份已校验，服务正在重新加载。请用备份中的账号登录。"})
	close(a.restart)
}
