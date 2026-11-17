package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func automaticRule(t *testing.T, a *App, cookie *http.Cookie, channel string) M {
	t.Helper()
	id := movieBotJSON(t, movieBotReq(a, cookie, "POST", "/api/admin/schedules", M{"name": "Retry fixture", "enabled": true, "kind": "daily", "time": "00:00", "timezone": "UTC", "notifyChannel": channel, "filters": Filters{Libraries: []string{"a"}}}))["id"]
	return a.one("SELECT * FROM schedules WHERE id=?", id)
}

func TestAutomaticRetryKeepsMovieAndSkipsSuccessfulChannel(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	var tg, web atomic.Int32
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		tg.Add(1)
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	})
	enableMovieBot(t, a, cookie)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if web.Add(1) == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer upstream.Close()
	a.updateSettings(map[string]string{"webhook_url": upstream.URL})
	rule := automaticRule(t, a, cookie, "both")
	now := time.Now()
	a.runAutomaticSchedule(rule, "test-period", now)
	run := a.one("SELECT * FROM schedule_deliveries WHERE schedule_id=?", rule["id"])
	if run["state"] != "pending" || tg.Load() != 1 || web.Load() != 1 {
		t.Fatal("partial failure was not retained for retry")
	}
	status := movieBotJSON(t, movieBotReq(a, cookie, "GET", "/api/admin/schedules", nil))["items"].([]any)[0].(map[string]any)
	if status["deliveryState"] != "pending" || str(status["retryAt"]) == "" || str(status["deliveryError"]) == "" {
		t.Fatal("retry timing or failure was hidden from the administrator")
	}
	a.runAutomaticSchedule(rule, "test-period", now.Add(20*time.Second))
	if web.Load() != 1 {
		t.Fatal("retried before backoff")
	}
	// Reload all scheduling state from SQLite, as after a process restart.
	rule = a.one("SELECT * FROM schedules WHERE id=?", rule["id"])
	a.runAutomaticSchedule(rule, "test-period", now.Add(2*time.Minute))
	a.runAutomaticSchedule(rule, "test-period", now.Add(3*time.Minute))
	if web.Load() != 2 || tg.Load() != 1 || a.count("SELECT COUNT(*) FROM random_history") != 1 {
		t.Fatal("retry reselected a movie or resent a successful channel")
	}
	run = a.one("SELECT * FROM schedule_deliveries WHERE schedule_id=?", rule["id"])
	if run["state"] != "sent" || num(run["attempts"]) != 2 {
		t.Fatal("recovered run not marked successful")
	}
	api := movieBotJSON(t, movieBotReq(a, cookie, "GET", "/api/admin/schedules", nil))
	if api["items"].([]any)[0].(map[string]any)["deliveryState"] != "sent" {
		t.Fatal("delivery outcome hidden from UI")
	}
}

func TestAutomaticRetryBoundsFailuresAndPreservesLegacyClaim(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	rule := automaticRule(t, a, cookie, "webhook")
	now := time.Now()
	for _, minutes := range []int{0, 2, 8, 24, 60} {
		a.runAutomaticSchedule(rule, "empty-fixture", now.Add(time.Duration(minutes)*time.Minute))
	}
	run := a.one("SELECT * FROM schedule_deliveries WHERE schedule_id=?", rule["id"])
	if run["state"] != "failed" || num(run["attempts"]) != 4 || str(run["last_error"]) == "" {
		t.Fatal("empty catalog failure was silently consumed or retried without limit")
	}
	rule["last_run_key"] = "legacy-period"
	a.runAutomaticSchedule(rule, "legacy-period", now)
	if a.count("SELECT COUNT(*) FROM schedule_deliveries WHERE period_key='legacy-period'") != 0 {
		t.Fatal("upgrade replayed an old claimed period")
	}
}

func TestPendingRetryCrossesCalendarBoundaryAndHonorsDisabledRule(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1); w.WriteHeader(204) }))
	defer upstream.Close()
	rule := automaticRule(t, a, cookie, "webhook")
	// Yesterday's weekly period is not due today. A failed attempt from that
	// period still needs its queued retry, including after process restart.
	a.exec("UPDATE schedules SET kind='weekly',weekday=?,enabled=0 WHERE id=?", (int(time.Now().UTC().Weekday())+6)%7, rule["id"])
	a.exec("INSERT INTO schedule_deliveries(schedule_id,period_key,movie_id,attempts,next_retry_at,updated_at) VALUES(?,?,?,1,?,?)", rule["id"], "previous-period", "unwatched", time.Now().Add(-time.Minute).Unix(), nowISO())
	a.updateSettings(map[string]string{"webhook_url": upstream.URL})
	a.checkSchedules()
	if sends.Load() != 0 {
		t.Fatal("disabled rule retried")
	}
	a.exec("UPDATE schedules SET enabled=1 WHERE id=?", rule["id"])
	a.checkSchedules()
	if sends.Load() != 1 {
		t.Fatal("pending retry was abandoned at a calendar boundary")
	}
}
