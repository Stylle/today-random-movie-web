package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (a *App) adminRoute(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/api/admin/settings" && r.Method == http.MethodGet:
		s := a.settings()
		username, _ := openSealed(a.secret, s["fn_username"])
		respond(w, M{
			"fnServer": s["fn_server"], "fnUsername": username, "fnHasPassword": s["fn_password"] != "", "fnInsecureTls": truth(s["fn_insecure_tls"]),
			"lastSyncAt": s["last_sync_at"], "lastSyncError": s["last_sync_error"],
		})
	case p == "/api/admin/fnos" && r.Method == http.MethodPut:
		v := readBody(r)
		server := strings.TrimSpace(str(v["server"]))
		if _, err := validateTarget(server, "fnos"); err != nil {
			fail(400, err.Error())
		}
		username, err := seal(a.secret, str(v["username"]))
		if err != nil {
			fail(500, err.Error())
		}
		updates := map[string]string{"fn_server": server, "fn_username": username, "fn_insecure_tls": boolString(truth(v["insecureTls"]))}
		if pass := str(v["password"]); pass != "" {
			updates["fn_password"], err = seal(a.secret, pass)
			if err != nil {
				fail(500, err.Error())
			}
		} else if truth(v["clearPassword"]) {
			updates["fn_password"] = ""
		}
		a.updateSettings(updates)
		a.resetClient()
		respond(w, M{"ok": true})
	case p == "/api/admin/fnos/test" && r.Method == http.MethodPost:
		client, err := a.configuredClient()
		if err != nil {
			fail(400, err.Error())
		}
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		libraries, err := client.Libraries(ctx)
		if err != nil {
			fail(502, err.Error())
		}
		respond(w, M{"ok": true, "libraries": len(libraries)})
	case p == "/api/admin/sync" && r.Method == http.MethodPost:
		if !a.startSync() {
			fail(409, "同步任务正在运行")
		}
		w.WriteHeader(http.StatusAccepted)
		respond(w, a.syncSnapshot())
	case p == "/api/admin/sync" && r.Method == http.MethodGet:
		respond(w, a.syncSnapshot())
	case p == "/api/admin/sync-schedule" && r.Method == http.MethodGet:
		respond(w, a.syncSchedule())
	case p == "/api/admin/sync-schedule" && r.Method == http.MethodPut:
		fields := cleanSyncSchedule(readBody(r))
		a.updateSettings(map[string]string{
			"sync_schedule_enabled": boolString(truth(fields["enabled"])),
			"sync_schedule_kind":    str(fields["kind"]), "sync_schedule_time": str(fields["time"]),
			"sync_schedule_timezone": str(fields["timezone"]), "sync_schedule_weekday": str(fields["weekday"]),
		})
		respond(w, a.syncSchedule())
	case p == "/api/admin/posters/purge" && r.Method == http.MethodPost:
		entries, _ := os.ReadDir(filepath.Join(a.dataDir, "posters"))
		removed := 0
		for _, entry := range entries {
			if !entry.IsDir() {
				if os.Remove(filepath.Join(a.dataDir, "posters", entry.Name())) == nil {
					removed++
				}
			}
		}
		respond(w, M{"ok": true, "removed": removed})
	case p == "/api/admin/webhook" && r.Method == http.MethodGet:
		s := a.settings()
		respond(w, M{"url": s["webhook_url"], "method": s["webhook_method"], "bodyType": s["webhook_body_type"], "headers": s["webhook_headers"], "bodyTemplate": s["webhook_body_tpl"]})
	case p == "/api/admin/webhook" && r.Method == http.MethodPut:
		v := readBody(r)
		urlValue := strings.TrimSpace(str(v["url"]))
		if urlValue != "" {
			if _, err := validateTarget(urlValue, "webhook"); err != nil {
				fail(400, err.Error())
			}
		}
		method := strings.ToUpper(str(v["method"]))
		if method != "POST" && method != "PUT" && method != "GET" {
			fail(400, "Webhook 方法仅支持 GET/POST/PUT")
		}
		bodyType := strings.ToLower(str(v["bodyType"]))
		if bodyType != "json" && bodyType != "text" {
			fail(400, "正文类型仅支持 json/text")
		}
		headers := strings.TrimSpace(str(v["headers"]))
		if headers != "" {
			parsed := map[string]string{}
			if json.Unmarshal([]byte(headers), &parsed) != nil {
				fail(400, "自定义请求头必须是 JSON 对象")
			}
		}
		template := str(v["bodyTemplate"])
		if bodyType == "json" && template != "" {
			if _, err := renderNotifyJSON(template, sampleWebhookContext()); err != nil {
				fail(400, err.Error())
			}
		}
		a.updateSettings(map[string]string{"webhook_url": urlValue, "webhook_method": method, "webhook_body_type": bodyType, "webhook_headers": headers, "webhook_body_tpl": template})
		respond(w, M{"ok": true})
	case p == "/api/admin/webhook/test" && r.Method == http.MethodPost:
		ok, status, err := a.sendWebhook("test", Movie{Title: "Webhook 测试影片", Year: "2026", Genres: []string{"剧情"}, Rating: "8.6"}, "")
		if !ok {
			fail(502, err)
		}
		respond(w, M{"ok": true, "status": status})
	case p == "/api/admin/schedules" && r.Method == http.MethodGet:
		items := []M{}
		for _, row := range a.rows("SELECT * FROM schedules ORDER BY created_at") {
			items = append(items, scanSchedule(row))
		}
		respond(w, M{"items": items})
	case p == "/api/admin/schedules" && r.Method == http.MethodPost:
		v := readBody(r)
		fields := cleanSchedule(v)
		id := randomHex(8)
		now := nowISO()
		a.exec("INSERT INTO schedules(id,name,enabled,kind,time_of_day,timezone,weekday,day_of_month,filters_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", id, fields["name"], fields["enabled"], fields["kind"], fields["time"], fields["timezone"], fields["weekday"], fields["dayOfMonth"], jsonString(fields["filters"]), now, now)
		respond(w, M{"ok": true, "id": id})
	case strings.HasPrefix(p, "/api/admin/schedules/"):
		a.scheduleItemRoute(w, r)
	case p == "/api/admin/history" && r.Method == http.MethodGet:
		items := a.rows("SELECT h.id,h.source,h.schedule_id,h.created_at,m.title,m.year FROM random_history h JOIN movies m ON m.id=h.movie_id ORDER BY h.id DESC LIMIT 100")
		logs := a.rows("SELECT * FROM webhook_logs ORDER BY id DESC LIMIT 100")
		respond(w, M{"random": items, "webhooks": logs})
	default:
		fail(404, "not found")
	}
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

var timePattern = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)

func (a *App) syncSchedule() M {
	s := a.settings()
	return M{
		"enabled": truth(s["sync_schedule_enabled"]), "kind": s["sync_schedule_kind"],
		"time": s["sync_schedule_time"], "timezone": s["sync_schedule_timezone"],
		"weekday": intValue(s["sync_schedule_weekday"], 1), "lastRunAt": s["sync_schedule_last_at"],
	}
}

func cleanSyncSchedule(v M) M {
	kind := strings.ToLower(str(v["kind"]))
	if kind != "daily" && kind != "weekly" {
		fail(400, "同步周期仅支持 daily/weekly")
	}
	timeOfDay := str(v["time"])
	if !timePattern.MatchString(timeOfDay) {
		fail(400, "时间格式必须是 HH:MM")
	}
	tz := fallback(strings.TrimSpace(str(v["timezone"])), env("TZ", "Asia/Shanghai"))
	if _, err := time.LoadLocation(tz); err != nil {
		fail(400, "无效的 IANA 时区")
	}
	weekday := int(num(v["weekday"]))
	if weekday < 0 || weekday > 6 {
		weekday = 1
	}
	return M{"enabled": truth(v["enabled"]), "kind": kind, "time": timeOfDay, "timezone": tz, "weekday": weekday}
}

func cleanSchedule(v M) M {
	name := strings.TrimSpace(str(v["name"]))
	if len([]rune(name)) < 1 || len([]rune(name)) > 60 {
		fail(400, "规则名称需为 1-60 个字符")
	}
	kind := strings.ToLower(str(v["kind"]))
	if kind != "daily" && kind != "weekly" && kind != "monthly" {
		fail(400, "规则类型仅支持 daily/weekly/monthly")
	}
	timeOfDay := str(v["time"])
	if !timePattern.MatchString(timeOfDay) {
		fail(400, "时间格式必须是 HH:MM")
	}
	tz := fallback(strings.TrimSpace(str(v["timezone"])), env("TZ", "Asia/Shanghai"))
	if _, err := time.LoadLocation(tz); err != nil {
		fail(400, "无效的 IANA 时区")
	}
	weekday := num(v["weekday"])
	day := num(v["dayOfMonth"])
	if weekday < 0 || weekday > 6 {
		weekday = 1
	}
	if day < 1 || day > 28 {
		day = 1
	}
	filters := v["filters"]
	if filters == nil {
		filters = M{}
	}
	return M{"name": name, "enabled": boolInt(truth(v["enabled"])), "kind": kind, "time": timeOfDay, "timezone": tz, "weekday": weekday, "dayOfMonth": day, "filters": filters}
}

func (a *App) scheduleItemRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/schedules/")
	run := strings.HasSuffix(rest, "/run")
	id := strings.TrimSuffix(rest, "/run")
	row := a.one("SELECT * FROM schedules WHERE id=?", id)
	if row == nil {
		fail(404, "规则不存在")
	}
	if run && r.Method == http.MethodPost {
		movie, err := a.executeSchedule(row, false)
		if err != nil {
			fail(502, err.Error())
		}
		respond(w, M{"ok": true, "movie": a.movieJSON(movie)})
		return
	}
	switch r.Method {
	case http.MethodPut:
		fields := cleanSchedule(readBody(r))
		a.exec("UPDATE schedules SET name=?,enabled=?,kind=?,time_of_day=?,timezone=?,weekday=?,day_of_month=?,filters_json=?,updated_at=? WHERE id=?", fields["name"], fields["enabled"], fields["kind"], fields["time"], fields["timezone"], fields["weekday"], fields["dayOfMonth"], jsonString(fields["filters"]), nowISO(), id)
		respond(w, M{"ok": true})
	case http.MethodDelete:
		a.exec("DELETE FROM schedules WHERE id=?", id)
		respond(w, M{"ok": true})
	default:
		fail(405, "method not allowed")
	}
}

func sampleWebhookContext() M {
	return M{"title": "🎬 今日想看", "text": "测试内容", "event": "test", "time": "2026-01-01 09:00:00", "site": "今日想看", "movie": "测试影片", "year": "2026", "genres": "剧情", "rating": "8.6", "poster_url": "https://example.com/api/posters/demo", "fields": `{}`}
}

func intValue(value any, fallbackValue int) int {
	n, err := strconv.Atoi(str(value))
	if err != nil {
		return fallbackValue
	}
	return n
}

var _ = fmt.Sprint
