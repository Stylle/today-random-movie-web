package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Each period keeps the selected movie and successful channels across restarts.
// Failed channels get at most four attempts, with 1m/5m/15m backoff.
func (a *App) runAutomaticSchedule(rule M, key string, now time.Time) {
	id := str(rule["id"])
	run := a.one("SELECT * FROM schedule_deliveries WHERE schedule_id=? AND period_key=?", id, key)
	if run == nil {
		// Do not replay periods already claimed by older versions on upgrade.
		if key == str(rule["last_run_key"]) {
			return
		}
		a.exec("INSERT OR IGNORE INTO schedule_deliveries(schedule_id,period_key,updated_at) VALUES(?,?,?)", id, key, nowISO())
	}
	claim := a.exec("UPDATE schedule_deliveries SET attempts=attempts+1,next_retry_at=?,updated_at=? WHERE schedule_id=? AND period_key=? AND state='pending' AND attempts<4 AND next_retry_at<=?", now.Add(time.Minute).Unix(), nowISO(), id, key, now.Unix())
	if n, err := claim.RowsAffected(); err != nil || n != 1 {
		return
	}
	a.exec("UPDATE schedules SET last_run_key=?,last_run_at=?,updated_at=? WHERE id=?", key, nowISO(), nowISO(), id)
	run = a.one("SELECT * FROM schedule_deliveries WHERE schedule_id=? AND period_key=?", id, key)
	channels, err := scheduleChannels(rule)
	var movie Movie
	if err == nil {
		if str(run["movie_id"]) != "" {
			if row := a.one("SELECT * FROM movies WHERE id=?", run["movie_id"]); row != nil {
				movie = decodeMovie(row)
			} else {
				err = fmt.Errorf("本周期已选影片已移除；请调整规则或等待下一周期")
			}
		} else {
			var filters Filters
			err = json.Unmarshal([]byte(str(rule["filters_json"])), &filters)
			if err == nil {
				movie, err = a.pickMovie(filters, "scheduled", id)
			}
			if err == nil {
				a.exec("UPDATE schedule_deliveries SET movie_id=? WHERE schedule_id=? AND period_key=?", movie.ID, id, key)
			}
		}
	}
	completed := []string{}
	_ = json.Unmarshal([]byte(str(run["completed_channels"])), &completed)
	if err == nil {
		failures := []string{}
		for _, channel := range channels {
			alreadySent := false
			for _, done := range completed {
				alreadySent = alreadySent || channel == done
			}
			if alreadySent {
				continue
			}
			var ok bool
			var message string
			if channel == "telegram" {
				ok, _, message = a.sendTelegramNotification("movie_pick", movie, id, str(rule["name"]), str(rule["filters_json"]))
			} else {
				ok, _, message = a.sendWebhook("movie_pick", movie, id)
			}
			if ok {
				completed = append(completed, channel)
				a.exec("UPDATE schedule_deliveries SET completed_channels=? WHERE schedule_id=? AND period_key=?", jsonString(completed), id, key)
			} else {
				failures = append(failures, channel+": "+message)
			}
		}
		if len(failures) > 0 {
			err = fmt.Errorf("%s", strings.Join(failures, "; "))
		}
	} else {
		for _, channel := range channels {
			a.logNotification(channel, "movie_pick", id, movie.ID, false, 0, "选片失败："+err.Error())
		}
	}
	state, message, next := "sent", "", int64(0)
	if err != nil {
		state, message = "failed", err.Error()
		if attempt := int(num(run["attempts"])); attempt < 4 {
			state = "pending"
			delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}
			next = now.Add(delays[attempt-1]).Unix()
		}
	}
	a.exec("UPDATE schedule_deliveries SET state=?,last_error=?,next_retry_at=?,updated_at=? WHERE schedule_id=? AND period_key=?", state, message, next, nowISO(), id, key)
	// Bound auxiliary history while retaining the latest period for every rule.
	a.exec("DELETE FROM schedule_deliveries WHERE updated_at<? AND state<>'pending' AND (schedule_id,period_key) NOT IN (SELECT id,last_run_key FROM schedules)", time.Now().AddDate(0, -3, 0).UTC().Format(time.RFC3339))
}
