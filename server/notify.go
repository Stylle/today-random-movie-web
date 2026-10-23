package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var templateVar = regexp.MustCompile(`\{\{[a-zA-Z0-9_]+\}\}`)

func renderNotifyJSON(template string, context M) ([]byte, error) {
	var out strings.Builder
	quoted, escaped := false, false
	for i := 0; i < len(template); {
		if strings.HasPrefix(template[i:], "{{") {
			if match := templateVar.FindStringIndex(template[i:]); match != nil && match[0] == 0 {
				key := template[i+2 : i+match[1]-2]
				value, found := context[key]
				if quoted {
					encoded, _ := json.Marshal(str(value))
					out.Write(encoded[1 : len(encoded)-1])
				} else if !found {
					out.WriteString("null")
				} else if key == "fields" && json.Valid([]byte(str(value))) {
					out.WriteString(str(value))
				} else {
					encoded, err := json.Marshal(value)
					if err != nil {
						return nil, err
					}
					out.Write(encoded)
				}
				i += match[1]
				continue
			}
		}
		char := template[i]
		out.WriteByte(char)
		if escaped {
			escaped = false
		} else if quoted && char == '\\' {
			escaped = true
		} else if char == '"' {
			quoted = !quoted
		}
		i++
	}
	result := []byte(out.String())
	if !json.Valid(result) {
		return nil, fmt.Errorf("请求体模板不是合法 JSON")
	}
	return result, nil
}

func (a *App) webhookContext(event string, movie Movie) M {
	s := a.settings()
	base := strings.TrimSuffix(env("PUBLIC_BASE_URL", ""), "/")
	poster := ""
	if base != "" && movie.ID != "" {
		poster = base + "/api/posters/" + movie.ID
	}
	title := "🎬 今日想看"
	if event == "test" {
		title = "🔔 Webhook 测试"
	}
	text := title + "\n\n· 站点：" + s["site_title"] + "\n· 时间：" + time.Now().Format("2006-01-02 15:04:05")
	if movie.Title != "" {
		text += "\n· 影片：" + movie.Title
		if movie.Year != "" {
			text += "（" + movie.Year + "）"
		}
		if len(movie.Genres) > 0 {
			text += "\n· 类型：" + strings.Join(movie.Genres, " / ")
		}
		if movie.Rating != "" {
			text += "\n· 评分：" + movie.Rating
		}
		if movie.Overview != "" {
			text += "\n· 简介：" + truncate(movie.Overview, 240)
		}
		if poster != "" {
			text += "\n· 封面：" + poster
		}
	}
	fields := M{"movie_id": movie.ID, "movie": movie.Title, "year": movie.Year, "genres": strings.Join(movie.Genres, ", "), "rating": movie.Rating, "poster_url": poster}
	fieldsJSON, _ := json.Marshal(fields)
	return M{"title": title, "text": text, "event": event, "time": time.Now().Format("2006-01-02 15:04:05"), "site": s["site_title"], "movie": movie.Title, "year": movie.Year, "genres": strings.Join(movie.Genres, ", "), "rating": movie.Rating, "overview": movie.Overview, "poster_url": poster, "fields": string(fieldsJSON)}
}

func (a *App) sendWebhook(event string, movie Movie, scheduleID string) (bool, int, string) {
	s := a.settings()
	webhookURL := strings.TrimSpace(s["webhook_url"])
	if webhookURL == "" {
		return false, 0, "尚未配置 Webhook URL"
	}
	if _, err := validateTarget(webhookURL, "webhook"); err != nil {
		return false, 0, err.Error()
	}
	context := a.webhookContext(event, movie)
	payload, _ := json.Marshal(context)
	contentType := "application/json"
	if s["webhook_body_type"] == "text" {
		payload = []byte(str(context["text"]))
		contentType = "text/plain; charset=utf-8"
	}
	if template := s["webhook_body_tpl"]; template != "" {
		if s["webhook_body_type"] == "json" {
			var err error
			payload, err = renderNotifyJSON(template, context)
			if err != nil {
				return false, 0, err.Error()
			}
		} else {
			for key, value := range context {
				template = strings.ReplaceAll(template, "{{"+key+"}}", str(value))
			}
			payload = []byte(templateVar.ReplaceAllString(template, ""))
		}
	}
	method := fallback(s["webhook_method"], "POST")
	var body io.Reader
	if method != http.MethodGet {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, webhookURL, body)
	if err != nil {
		return false, 0, err.Error()
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "today-random-movie/"+version)
	if raw := s["webhook_headers"]; raw != "" {
		headers := map[string]string{}
		if err = json.Unmarshal([]byte(raw), &headers); err != nil {
			return false, 0, "自定义请求头不是合法 JSON"
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}
	client := restrictedClient("webhook", 15*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		a.logWebhook(event, scheduleID, movie.ID, false, 0, err.Error())
		return false, 0, err.Error()
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := fmt.Sprintf("HTTP %d · %s", resp.StatusCode, truncate(string(response), 320))
		a.logWebhook(event, scheduleID, movie.ID, false, resp.StatusCode, message)
		return false, resp.StatusCode, message
	}
	a.logWebhook(event, scheduleID, movie.ID, true, resp.StatusCode, "")
	return true, resp.StatusCode, ""
}

func (a *App) logWebhook(event, scheduleID, movieID string, ok bool, status int, message string) {
	a.exec("INSERT INTO webhook_logs(event,schedule_id,movie_id,ok,status_code,error,created_at) VALUES(?,?,?,?,?,?,?)", event, scheduleID, movieID, boolInt(ok), status, truncate(message, 1000), nowISO())
	a.exec("DELETE FROM webhook_logs WHERE id NOT IN (SELECT id FROM webhook_logs ORDER BY id DESC LIMIT 1000)")
}

func (a *App) schedulerLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	a.checkSchedules()
	for {
		select {
		case <-a.stop:
			return
		case <-ticker.C:
			a.exec("DELETE FROM sessions WHERE expires_at<=?", nowISO())
			a.checkSchedules()
		}
	}
}

func (a *App) checkSchedules() {
	for _, row := range a.rows("SELECT * FROM schedules WHERE enabled=1 ORDER BY created_at") {
		due, key := scheduleDue(row, time.Now())
		if !due || key == str(row["last_run_key"]) {
			continue
		}
		// Claim the period before sending. The rule promises at most one automatic
		// attempt per period; delivery outcome remains visible in webhook_logs.
		a.exec("UPDATE schedules SET last_run_key=?,last_run_at=?,updated_at=? WHERE id=? AND last_run_key<>?", key, nowISO(), nowISO(), row["id"], key)
		if _, err := a.executeSchedule(row, true); err != nil {
			log.Printf("schedule %s: %v", row["id"], err)
		}
	}
}

func scheduleDue(row M, now time.Time) (bool, string) {
	location, err := time.LoadLocation(fallback(str(row["timezone"]), env("TZ", "Asia/Shanghai")))
	if err != nil {
		return false, ""
	}
	local := now.In(location)
	parts := strings.Split(str(row["time_of_day"]), ":")
	if len(parts) != 2 {
		return false, ""
	}
	hour, minute := intValue(parts[0], -1), intValue(parts[1], -1)
	if local.Hour()*60+local.Minute() < hour*60+minute {
		return false, ""
	}
	switch str(row["kind"]) {
	case "daily":
		return true, local.Format("2006-01-02")
	case "weekly":
		if int(local.Weekday()) != int(num(row["weekday"])) {
			return false, ""
		}
		year, week := local.ISOWeek()
		return true, fmt.Sprintf("%04d-W%02d", year, week)
	case "monthly":
		if local.Day() != int(num(row["day_of_month"])) {
			return false, ""
		}
		return true, local.Format("2006-01")
	}
	return false, ""
}

func (a *App) executeSchedule(row M, automatic bool) (Movie, error) {
	filters := Filters{}
	if err := json.Unmarshal([]byte(str(row["filters_json"])), &filters); err != nil {
		return Movie{}, fmt.Errorf("规则筛选条件损坏: %w", err)
	}
	movie, err := a.pickMovie(filters, map[bool]string{true: "scheduled", false: "manual-schedule"}[automatic], str(row["id"]))
	if err != nil {
		return Movie{}, err
	}
	ok, _, message := a.sendWebhook("movie_pick", movie, str(row["id"]))
	if !ok {
		return movie, fmt.Errorf("Webhook 推送失败: %s", message)
	}
	return movie, nil
}
