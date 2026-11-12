package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func notificationRule(channel string) M {
	return M{"id": "test-rule", "name": "晚间推荐", "notify_channel": channel, "filters_json": jsonString(Filters{Libraries: []string{"a"}, ExcludeWatched: true})}
}

func TestRuleNotificationChannelsCRUD(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	input := M{"name": "每日规则", "enabled": true, "kind": "daily", "time": "20:00", "timezone": "Asia/Shanghai", "filters": M{"excludeWatched": true}}
	id := movieBotJSON(t, movieBotReq(a, cookie, "POST", "/api/admin/schedules", input))["id"].(string)
	if str(a.one("SELECT notify_channel FROM schedules WHERE id=?", id)["notify_channel"]) != "webhook" {
		t.Fatal("legacy default changed")
	}
	input["notifyChannel"] = "telegram"
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/schedules/"+id, input))
	delete(input, "notifyChannel")
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/schedules/"+id, input))
	items := movieBotJSON(t, movieBotReq(a, cookie, "GET", "/api/admin/schedules", nil))["items"].([]any)
	if items[0].(map[string]any)["notifyChannel"] != "telegram" {
		t.Fatal("partial update lost notification channel")
	}
	input["notifyChannel"] = "unknown"
	if movieBotReq(a, cookie, "PUT", "/api/admin/schedules/"+id, input).Code != 400 {
		t.Fatal("invalid notification channel accepted")
	}
	input["notifyChannel"] = "both"
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/schedules/"+id, input))
	if scanSchedule(a.one("SELECT * FROM schedules WHERE id=?", id))["notifyChannel"] != "both" {
		t.Fatal("dual channel not persisted")
	}
}

func TestScheduleChannelRoutingChoosesOneMovie(t *testing.T) {
	for _, channel := range []string{"webhook", "telegram", "both"} {
		t.Run(channel, func(t *testing.T) {
			a, cookie := movieBotTestApp(t)
			seedMovieBot(a)
			var tgCount, webCount atomic.Int32
			tgMessages := make(chan M, 2)
			webMessages := make(chan M, 2)
			movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
				if method != "sendMessage" {
					t.Errorf("unexpected method %s", method)
					return
				}
				var payload M
				_ = json.NewDecoder(r.Body).Decode(&payload)
				tgMessages <- payload
				tgCount.Add(1)
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
			})
			web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload M
				_ = json.NewDecoder(r.Body).Decode(&payload)
				webMessages <- payload
				webCount.Add(1)
				w.WriteHeader(204)
			}))
			defer web.Close()
			if channel != "telegram" {
				a.updateSettings(map[string]string{"webhook_url": web.URL})
			}
			if channel != "webhook" {
				enableMovieBot(t, a, cookie)
			}
			// Rule filters must not accidentally use the separate /menu scope.
			a.saveBotFilters("42", Filters{Libraries: []string{"b"}, ExcludeWatched: false})
			movie, err := a.executeSchedule(notificationRule(channel), false)
			if err != nil || movie.ID != "unwatched" {
				t.Fatalf("rule selection failed: %v", err)
			}
			if a.count("SELECT COUNT(*) FROM random_history") != 1 {
				t.Fatal("multiple movies selected for one rule")
			}
			if channel == "telegram" && webCount.Load() != 0 || channel == "webhook" && tgCount.Load() != 0 {
				t.Fatal("unselected channel called")
			}
			if channel != "webhook" {
				if tgCount.Load() != 1 {
					t.Fatal("telegram not sent once")
				}
				p := <-tgMessages
				if str(p["chat_id"]) != "42" || !strings.Contains(str(p["text"]), movie.Title) || !strings.Contains(str(p["text"]), "晚间推荐") {
					t.Fatal("wrong telegram target or movie")
				}
			}
			if channel != "telegram" {
				if webCount.Load() != 1 {
					t.Fatal("webhook not sent once")
				}
				if str((<-webMessages)["movie"]) != movie.Title {
					t.Fatal("channels did not receive same movie")
				}
			}
			want := int64(1)
			if channel == "both" {
				want = 2
			}
			if a.count("SELECT COUNT(*) FROM webhook_logs WHERE ok=1") != want {
				t.Fatal("delivery records missing")
			}
		})
	}
}

func TestDualChannelPartialFailureDoesNotSkipOtherChannel(t *testing.T) {
	for _, failed := range []string{"webhook", "telegram"} {
		t.Run(failed, func(t *testing.T) {
			a, cookie := movieBotTestApp(t)
			seedMovieBot(a)
			var sends atomic.Int32
			movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
				sends.Add(1)
				if failed == "telegram" {
					w.WriteHeader(429)
					fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"sensitive body must not be logged","parameters":{"retry_after":1}}`)
				} else {
					fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
				}
			})
			enableMovieBot(t, a, cookie)
			web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failed == "webhook" {
					w.WriteHeader(500)
				} else {
					w.WriteHeader(204)
				}
			}))
			defer web.Close()
			a.updateSettings(map[string]string{"webhook_url": web.URL})
			movie, err := a.executeSchedule(notificationRule("both"), true)
			if err == nil || movie.ID == "" || sends.Load() != 1 {
				t.Fatal("partial failure was hidden or other channel skipped")
			}
			if a.count("SELECT COUNT(*) FROM webhook_logs WHERE ok=1") != 1 || a.count("SELECT COUNT(*) FROM webhook_logs WHERE ok=0") != 1 {
				t.Fatal("partial results not recorded independently")
			}
			row := a.one("SELECT * FROM webhook_logs WHERE ok=0")
			if row["channel"] != failed || strings.Contains(str(row["error"]), "sensitive") {
				t.Fatal("bad channel status or sensitive error leaked")
			}
			if a.count("SELECT COUNT(*) FROM random_history") != 1 {
				t.Fatal("failure re-picked movie")
			}
		})
	}
}

func TestTelegramRuleDisabledAndTestNotification(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	if _, err := a.executeSchedule(notificationRule("telegram"), false); err == nil {
		t.Fatal("disabled bot reported success")
	}
	if a.count("SELECT COUNT(*) FROM webhook_logs WHERE channel='telegram' AND ok=0") != 1 {
		t.Fatal("missing configuration failure not visible")
	}
	if movieBotReq(a, nil, "POST", "/api/admin/telegram/send-test", nil).Code != 401 {
		t.Fatal("public test endpoint")
	}
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	})
	enableMovieBot(t, a, cookie)
	movieBotJSON(t, movieBotReq(a, cookie, "POST", "/api/admin/telegram/send-test", nil))
	if a.count("SELECT COUNT(*) FROM webhook_logs WHERE channel='telegram' AND event='test' AND ok=1") != 1 {
		t.Fatal("test delivery not logged")
	}
	history := movieBotJSON(t, movieBotReq(a, cookie, "GET", "/api/admin/history", nil))
	if len(history["notifications"].([]any)) != 2 || len(history["webhooks"].([]any)) != 2 {
		t.Fatal("history API missing new or legacy results")
	}
}

func TestAutomaticTelegramRuleClaimsPeriodOnce(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	var sends atomic.Int32
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		sends.Add(1)
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	})
	enableMovieBot(t, a, cookie)
	a.exec(`INSERT INTO schedules(id,name,enabled,kind,time_of_day,timezone,filters_json,notify_channel,created_at,updated_at) VALUES('auto','自动规则',1,'daily','00:00','UTC',?,'telegram',?,?)`, jsonString(Filters{ExcludeWatched: true}), nowISO(), nowISO())
	a.checkSchedules()
	a.checkSchedules()
	if sends.Load() != 1 || a.count("SELECT COUNT(*) FROM random_history WHERE source='scheduled'") != 1 {
		t.Fatal("automatic period delivered twice")
	}
}

func TestTelegramNotificationCanceledByConfigurationChange(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	started := make(chan struct{})
	finished := make(chan bool, 1)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		_, _ = io.ReadAll(r.Body)
		close(started)
		<-r.Context().Done()
	})
	_, revision := enableMovieBot(t, a, cookie)
	go func() { ok, _, _ := a.sendTelegramNotification("test", Movie{Title: "test"}, "", ""); finished <- ok }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("send did not start")
	}
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/telegram", movieBotInput{Enabled: false, ChatID: "42", Revision: revision}))
	select {
	case ok := <-finished:
		if ok {
			t.Fatal("canceled send reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("settings did not cancel old-target send")
	}
}

func TestNotificationChannelMigration(t *testing.T) {
	t.Setenv("USER", "admin")
	t.Setenv("PASSWORD", "test-only-password")
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.ReplaceAll(strings.ReplaceAll(schema, "\r\n", "\n"), "    notify_channel TEXT NOT NULL DEFAULT 'webhook',\n", "")
	legacy = strings.ReplaceAll(legacy, "    channel      TEXT NOT NULL DEFAULT 'webhook',\n", "")
	if _, err = db.Exec(legacy); err != nil {
		t.Fatal(err)
	}
	var unexpected string
	if db.QueryRow("SELECT notify_channel FROM schedules").Scan(&unexpected) == nil {
		t.Fatal("migration fixture already has new column")
	}
	_, err = db.Exec(`INSERT INTO schedules(id,name,kind,time_of_day,timezone,created_at,updated_at) VALUES('legacy','旧规则','daily','20:00','UTC','old','old');INSERT INTO webhook_logs(event,ok,created_at) VALUES('old',1,'old')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	a := newApp(dir, t.TempDir())
	if a.one("SELECT notify_channel FROM schedules WHERE id='legacy'")["notify_channel"] != "webhook" || a.one("SELECT channel FROM webhook_logs")["channel"] != "webhook" {
		t.Fatal("legacy channels were not migrated")
	}
	a.db.Close()
	a = newApp(dir, t.TempDir())
	defer a.db.Close()
	if a.count("SELECT COUNT(*) FROM schedules") != 1 {
		t.Fatal("idempotent migration lost existing rule")
	}
}
