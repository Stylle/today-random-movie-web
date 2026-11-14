package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func movieBotTestApp(t *testing.T) (*App, *http.Cookie) {
	t.Helper()
	t.Setenv("USER", "admin")
	t.Setenv("PASSWORD", "test-only-password")
	a := newApp(t.TempDir(), t.TempDir())
	t.Cleanup(func() { a.db.Close() })
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"test-only-password"}`))
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("test login failed: %d", w.Code)
	}
	return a, w.Result().Cookies()[0]
}
func movieBotReq(a *App, cookie *http.Cookie, method, url string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, url, strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}
func movieBotJSON(t *testing.T, w *httptest.ResponseRecorder) M {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var out M
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func movieBotMock(t *testing.T, a *App, handler func(http.ResponseWriter, *http.Request, string)) {
	a.posterHTTP = &http.Client{Transport: posterTransport(func(*http.Request) (*http.Response, error) {
		return posterResponse(503, "fixture metadata unavailable"), nil
	})}
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "getMe":
			fmt.Fprint(w, `{"ok":true,"result":{"username":"test_movie_bot"}}`)
		case "getWebhookInfo":
			fmt.Fprint(w, `{"ok":true,"result":{"url":""}}`)
		default:
			handler(w, r, method)
		}
	}))
	t.Cleanup(s.Close)
	a.tgBase = s.URL
}
func enableMovieBot(t *testing.T, a *App, cookie *http.Cookie) (movieBotConfig, string) {
	t.Helper()
	_, rev, e := a.botConfig()
	if e != nil {
		t.Fatal(e)
	}
	in := movieBotInput{Enabled: true, ChatID: "42", Token: "123456:" + strings.Repeat("x", 30), Revision: rev}
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/telegram", in))
	cfg, rev, e := a.botConfig()
	if e != nil {
		t.Fatal(e)
	}
	return cfg, rev
}
func seedMovieBot(a *App) {
	for _, m := range []Movie{{ID: "unwatched", Title: "未看剧情", Year: "2024", Genres: []string{"剧情"}, Countries: []string{"中国"}, LibraryID: "a", Library: "电影 A"}, {ID: "watched", Title: "已看影片", Watched: true, Year: "2010", Genres: []string{"悬疑"}, Countries: []string{"日本"}, LibraryID: "b", Library: "电影 B"}} {
		a.exec(`INSERT INTO movies(id,source_id,title,year,genres,countries,library_id,library,watched,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, m.ID, "source-"+m.ID, m.Title, m.Year, jsonString(m.Genres), jsonString(m.Countries), m.LibraryID, m.Library, boolInt(m.Watched), nowISO())
	}
}
func movieCommand(cfg movieBotConfig, id int64, text string) movieBotUpdate {
	m := &movieBotMessage{ID: id, Date: cfg.Since, Text: text, From: movieBotUser{ID: 42}}
	m.Chat.ID = 42
	m.Chat.Type = "private"
	return movieBotUpdate{ID: id, Message: m}
}
func movieCallback(cfg movieBotConfig, id int64, data string) movieBotUpdate {
	u := movieCommand(cfg, id, "")
	return movieBotUpdate{ID: id, Callback: &movieBotCallback{ID: fmt.Sprint(id), Data: data, From: movieBotUser{ID: 42}, Message: u.Message}}
}
func movieButton(t *testing.T, payload M, prefix string) string {
	t.Helper()
	raw, _ := json.Marshal(payload)
	var data struct {
		Markup struct {
			Buttons [][]struct {
				Text string `json:"text"`
				Data string `json:"callback_data"`
			} `json:"inline_keyboard"`
		} `json:"reply_markup"`
	}
	json.Unmarshal(raw, &data)
	for _, row := range data.Markup.Buttons {
		for _, b := range row {
			if strings.HasPrefix(b.Text, prefix) {
				if len(b.Data) > 64 {
					t.Fatal("callback data exceeds Telegram limit")
				}
				return b.Data
			}
		}
	}
	t.Fatalf("button not found: %s", prefix)
	return ""
}

func TestMovieBotDefaultsAndCredentialConfiguration(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) { fmt.Fprint(w, `{"ok":true,"result":[]}`) })
	if movieBotReq(a, nil, "GET", "/api/admin/telegram", nil).Code != 401 {
		t.Fatal("public access to token settings")
	}
	defaults := movieBotJSON(t, movieBotReq(a, cookie, "GET", "/api/random/defaults", nil))["filters"].(map[string]any)
	if defaults["excludeWatched"] != true || len(defaults["libraries"].([]any)) != 0 {
		t.Fatal("default should be all libraries excluding watched")
	}
	cfg, rev := enableMovieBot(t, a, cookie)
	var encoded string
	a.db.QueryRow(`SELECT value FROM telegram_movie_config`).Scan(&encoded)
	if strings.Contains(encoded, cfg.Token) {
		t.Fatal("token stored in plaintext")
	}
	w := movieBotReq(a, cookie, "GET", "/api/admin/telegram", nil)
	body := movieBotJSON(t, w)
	if strings.Contains(w.Body.String(), cfg.Token) || body["hasToken"] != true {
		t.Fatal("token leaked or saved indicator missing")
	}
	bad := movieBotInput{Enabled: true, ChatID: "-42", Revision: rev}
	if movieBotReq(a, cookie, "PUT", "/api/admin/telegram", bad).Code != 400 {
		t.Fatal("accepted group chat")
	}
	if movieBotReq(a, cookie, "PUT", "/api/admin/telegram", movieBotInput{Revision: "old"}).Code != 409 {
		t.Fatal("accepted stale config")
	}
	seedMovieBot(a)
	f := Filters{Libraries: []string{}, ExcludeWatched: false, Genre: "悬疑", YearFrom: "2025", YearTo: "2000"}
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/random-defaults", M{"filters": f}))
	if got := a.defaultFilters(); got.ExcludeWatched || got.YearFrom != "2000" || got.YearTo != "2025" {
		t.Fatal("defaults were not normalized")
	}
	if _, e := cleanDefaultFilters(M{"yearFrom": "not-year"}); e == nil {
		t.Fatal("invalid year accepted")
	}
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/telegram", movieBotInput{Revision: rev}))
	disabled, _, _ := a.botConfig()
	if disabled.Enabled || disabled.Token != cfg.Token {
		t.Fatal("disable/preserve token failed")
	}
}

func TestMovieBotCommandsReusePickAndFollowDefaults(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) { fmt.Fprint(w, `{"ok":true,"result":{}}`) })
	cfg, rev := enableMovieBot(t, a, cookie)
	for _, command := range []string{"menu", "MENU", "/menu@test_movie_bot"} {
		if botCommand(command) != "root" {
			t.Fatalf("menu alias failed %s", command)
		}
	}
	for _, command := range []string{"meau", "/meau", "help", "/help"} {
		if botCommand(command) != "" {
			t.Fatalf("removed command still accepted: %s", command)
		}
	}
	if botCommand("/start") != "start" {
		t.Fatal("start should only show onboarding text")
	}
	u := movieCommand(cfg, 1, "getmovie")
	first, e := a.prepareMovieReply(context.Background(), cfg, rev, u)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(str(first["text"]), "未看剧情") {
		t.Fatal("watched movie included by default")
	}
	second, e := a.prepareMovieReply(context.Background(), cfg, rev, u)
	if e != nil || jsonString(first) != jsonString(second) {
		t.Fatal("retry changed chosen response")
	}
	if a.count(`SELECT COUNT(*) FROM random_history WHERE source='telegram'`) != 1 {
		t.Fatal("duplicate random history")
	}
	a.updateSettings(map[string]string{"random_default_filters": jsonString(Filters{Genre: "悬疑", ExcludeWatched: false})})
	newPick, e := a.prepareMovieReply(context.Background(), cfg, rev, movieCommand(cfg, 2, "/getmovie"))
	if e != nil || !strings.Contains(str(newPick["text"]), "已看影片") {
		t.Fatal("backend defaults not followed", e)
	}
}

func TestMovieBotMenuScopeAndReset(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) { fmt.Fprint(w, `{"ok":true,"result":{}}`) })
	cfg, rev := enableMovieBot(t, a, cookie)
	ctx := context.Background()
	id := int64(1)
	root, e := a.prepareMovieReply(ctx, cfg, rev, movieCommand(cfg, id, "menu"))
	if e != nil {
		t.Fatal(e)
	}
	click := func(payload M, label string) M {
		id++
		result, e := a.prepareMovieReply(ctx, cfg, rev, movieCallback(cfg, id, movieButton(t, payload, label)))
		if e != nil {
			t.Fatal(e)
		}
		return result
	}
	genres := click(root, "影片类型")
	root = click(genres, "悬疑")
	f, custom := a.botFilters(cfg.ChatID)
	if !custom || f.Genre != "悬疑" || a.defaultFilters().Genre != "" {
		t.Fatal("menu changed backend defaults or failed to persist")
	}
	root = click(root, "排除已观看")
	f, _ = a.botFilters(cfg.ChatID)
	if f.ExcludeWatched || !a.defaultFilters().ExcludeWatched {
		t.Fatal("watched preference leaked to website")
	}
	libraries := click(root, "媒体库")
	libraries = click(libraries, "电影 A")
	libraries = click(libraries, "电影 B")
	f, _ = a.botFilters(cfg.ChatID)
	if len(f.Libraries) != 2 {
		t.Fatal("multi-select failed")
	}
	libraries = click(libraries, "全部 / 不限")
	root = click(libraries, "返回范围菜单")
	years := click(root, "年份范围")
	from := click(years, "起始")
	years = click(from, "2024 年")
	to := click(years, "结束")
	years = click(to, "2010 年")
	f, _ = a.botFilters(cfg.ChatID)
	if f.YearFrom != "2010" || f.YearTo != "2024" {
		t.Fatal("year interval normalization failed")
	}
	root = click(years, "返回范围菜单")
	root = click(root, "恢复后台默认")
	f, custom = a.botFilters(cfg.ChatID)
	if custom || !f.ExcludeWatched || f.Genre != "" {
		t.Fatal("reset does not follow defaults")
	}
	_ = root
}

func TestMovieBotRejectsOtherChatsAndOldCallbacks(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) { fmt.Fprint(w, `{"ok":true,"result":{}}`) })
	cfg, rev := enableMovieBot(t, a, cookie)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		u := movieCommand(cfg, int64(i+1), "getmovie")
		switch i {
		case 0:
			u.Message.Chat.ID = 99
		case 1:
			u.Message.Chat.Type = "group"
		case 2:
			u.Message.From.Bot = true
		case 3:
			u.Message.Date = cfg.Since - 1
		}
		p, e := a.prepareMovieReply(ctx, cfg, rev, u)
		if e != nil || p != nil {
			t.Fatal("unauthorized update accepted")
		}
	}
	root, e := a.prepareMovieReply(ctx, cfg, rev, movieCommand(cfg, 9, "menu"))
	if e != nil {
		t.Fatal(e)
	}
	oldButton := movieButton(t, root, "排除已观看")
	newCfg, newRev := enableMovieBot(t, a, cookie)
	if p, e := a.prepareMovieReply(ctx, newCfg, newRev, movieCommand(newCfg, 9, "menu")); e != nil || p != nil {
		t.Fatal("old pending response delivered after configuration change")
	}
	if _, e = a.prepareMovieReply(ctx, newCfg, newRev, movieCallback(newCfg, 10, oldButton)); e != nil {
		t.Fatal(e)
	}
	f, custom := a.botFilters(cfg.ChatID)
	if custom || !f.ExcludeWatched {
		t.Fatal("stale menu changed filters")
	}
}

func TestMovieBotPollingAndCancellation(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	var since atomic.Int64
	var sent atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		switch method {
		case "getUpdates":
			var in struct {
				Offset int64 `json:"offset"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.Offset > 0 {
				cancel()
				fmt.Fprint(w, `{"ok":true,"result":[]}`)
				return
			}
			fmt.Fprintf(w, `{"ok":true,"result":[{"update_id":1,"message":{"date":%d,"text":"getmovie","from":{"id":42},"chat":{"id":42,"type":"private"}}}]}`, since.Load())
		case "sendMessage":
			sent.Add(1)
			fmt.Fprint(w, `{"ok":true,"result":{}}`)
		case "setMyCommands":
			var input struct {
				Commands []struct {
					Command string `json:"command"`
				} `json:"commands"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Commands) != 2 || !((input.Commands[0].Command == "getmovie" && input.Commands[1].Command == "menu") || (input.Commands[0].Command == "start" && input.Commands[1].Command == "bind")) {
				t.Error("unexpected public or bound command menu")
			}
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		default:
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		}
	})
	cfg, rev := enableMovieBot(t, a, cookie)
	since.Store(cfg.Since)
	done := make(chan struct{})
	go func() { defer close(done); a.pollMovieBot(ctx, cfg, rev) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("polling stuck")
	}
	if sent.Load() != 1 {
		t.Fatal("command response not sent")
	}
	var next int64
	a.db.QueryRow(`SELECT next_update FROM telegram_movie_config`).Scan(&next)
	if next != 2 {
		t.Fatal("offset not persisted")
	}
}

func TestMovieBotDisableCancelsLongPoll(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	started := make(chan struct{}, 1)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		if method == "getUpdates" {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			select {
			case started <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	_, rev := enableMovieBot(t, a, cookie)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); a.telegramLoop(ctx) }()
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("worker not started")
	}
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/telegram", movieBotInput{Revision: rev}))
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestMovieBotLongOptionPaginationAndExpiredButtons(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	long := "类型" + strings.Repeat("很长的分类名称", 15)
	genres := []string{long}
	for i := 0; i < 24; i++ {
		genres = append(genres, fmt.Sprintf("type%02d", i))
	}
	a.exec(`UPDATE movies SET genres=? WHERE id='unwatched'`, jsonString(genres))
	cfg, rev := enableMovieBot(t, a, cookie)
	ctx := context.Background()
	id := int64(1)
	root, e := a.prepareMovieReply(ctx, cfg, rev, movieCommand(cfg, id, "menu"))
	if e != nil {
		t.Fatal(e)
	}
	click := func(payload M, label string) M {
		id++
		p, e := a.prepareMovieReply(ctx, cfg, rev, movieCallback(cfg, id, movieButton(t, payload, label)))
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	page := click(root, "影片类型")
	for i := 0; i < 3; i++ {
		page = click(page, "下一页")
	}
	root = click(page, "类型")
	f, _ := a.botFilters(cfg.ChatID)
	if f.Genre != long {
		t.Fatal("long option was truncated or lost")
	}
	oldPick := movieButton(t, root, "🎲 随机")
	a.exec(`UPDATE telegram_movie_buttons SET expires=0`)
	id++
	if _, e = a.prepareMovieReply(ctx, cfg, rev, movieCallback(cfg, id, oldPick)); e != nil {
		t.Fatal(e)
	}
	if a.count(`SELECT COUNT(*) FROM random_history`) != 0 {
		t.Fatal("expired button executed")
	}
}

func TestMovieBotRejectsWebhookAndRedactsErrors(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	var redirectHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectHits.Add(1) }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			fmt.Fprint(w, `{"ok":true,"result":{"username":"test"}}`)
		case strings.HasSuffix(r.URL.Path, "/getWebhookInfo"):
			fmt.Fprint(w, `{"ok":true,"result":{"url":"https://example.com/existing-webhook"}}`)
		default:
			http.Redirect(w, r, target.URL, 302)
		}
	}))
	defer s.Close()
	a.tgBase = s.URL
	token := "123456:" + strings.Repeat("x", 30)
	w := movieBotReq(a, cookie, "PUT", "/api/admin/telegram", movieBotInput{Enabled: true, Token: token, ChatID: "42"})
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Webhook") {
		t.Fatal("accepted existing webhook")
	}
	e := a.botCall(context.Background(), token, "sendMessage", M{}, nil)
	if e == nil || strings.Contains(e.Error(), token) || strings.Contains(e.Error(), s.URL) || redirectHits.Load() != 0 {
		t.Fatal("unsafe redirect or token error")
	}
}

func assertMovieButtons(t *testing.T, payload M, wantURL bool) {
	t.Helper()
	var parsed struct {
		Markup struct {
			Buttons [][]struct {
				Text     string `json:"text"`
				URL      string `json:"url"`
				Callback string `json:"callback_data"`
			} `json:"inline_keyboard"`
		} `json:"reply_markup"`
	}
	if err := json.Unmarshal([]byte(jsonString(payload)), &parsed); err != nil {
		t.Fatal(err)
	}
	buttons := parsed.Markup.Buttons
	if len(buttons) != 1 || len(buttons[0]) != 2 {
		t.Fatal("movie result should only have two buttons")
	}
	if buttons[0][0].Text != "再随机一部" || buttons[0][0].Callback == "" || buttons[0][1].Text != "打开飞牛影视" {
		t.Fatal("unexpected movie actions")
	}
	if wantURL && buttons[0][1].URL == "" {
		t.Fatal("missing FnOS play link")
	}
	if !wantURL && buttons[0][1].Callback == "" {
		t.Fatal("unavailable play action must explain missing configuration")
	}
}

func TestMovieResultOnlyTwoButtonsAndSettingsOnlyInMenu(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) { fmt.Fprint(w, `{"ok":true,"result":{}}`) })
	cfg, rev := enableMovieBot(t, a, cookie)
	ctx := context.Background()
	result, err := a.prepareMovieReply(ctx, cfg, rev, movieCommand(cfg, 1, "getmovie"))
	if err != nil {
		t.Fatal(err)
	}
	assertMovieButtons(t, result, false)
	missing, err := a.prepareMovieReply(ctx, cfg, rev, movieCallback(cfg, 2, movieButton(t, result, "打开飞牛影视")))
	if err != nil || missing["reply_markup"] != nil || !strings.Contains(str(missing["text"]), "配置飞牛") {
		t.Fatal("missing FnOS configuration should not open settings keyboard")
	}
	fnos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("creating link must not contact FnOS") }))
	defer fnos.Close()
	a.updateSettings(map[string]string{"fn_server": fnos.URL})
	rerolled, err := a.prepareMovieReply(ctx, cfg, rev, movieCallback(cfg, 3, movieButton(t, result, "再随机一部")))
	if err != nil {
		t.Fatal(err)
	}
	assertMovieButtons(t, rerolled, true)
	menu, err := a.prepareMovieReply(ctx, cfg, rev, movieCommand(cfg, 4, "menu"))
	if err != nil {
		t.Fatal(err)
	}
	_ = movieButton(t, menu, "媒体库")
	_ = movieButton(t, menu, "年份范围")
	start, err := a.prepareMovieReply(ctx, cfg, rev, movieCommand(cfg, 5, "/start"))
	if err != nil || start["reply_markup"] != nil {
		t.Fatal("start should be a short introduction, not a duplicate settings menu")
	}
	a.saveBotFilters(cfg.ChatID, Filters{Genre: "不存在的类型"})
	empty, err := a.prepareMovieReply(ctx, cfg, rev, movieCommand(cfg, 6, "getmovie"))
	if err != nil || empty["reply_markup"] != nil || !strings.Contains(str(empty["text"]), "menu") {
		t.Fatal("empty selection should point to menu without expanding it")
	}
}

func TestNotificationRerollPreservesRuleScopeWithoutResendingWebhook(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	seedMovieBot(a)
	messages := make(chan M, 2)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		var payload M
		_ = json.NewDecoder(r.Body).Decode(&payload)
		messages <- payload
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	})
	cfg, rev := enableMovieBot(t, a, cookie)
	a.updateSettings(map[string]string{"fn_server": "http://127.0.0.1:5666", "random_default_filters": jsonString(Filters{Genre: "悬疑"})})
	a.saveBotFilters(cfg.ChatID, Filters{Libraries: []string{"b"}, Genre: "悬疑"})
	if _, err := a.executeSchedule(notificationRule("telegram"), true); err != nil {
		t.Fatal(err)
	}
	result := <-messages
	assertMovieButtons(t, result, true)
	for id := int64(1); id <= 2; id++ {
		var err error
		result, err = a.prepareMovieReply(context.Background(), cfg, rev, movieCallback(cfg, id, movieButton(t, result, "再随机一部")))
		if err != nil || !strings.Contains(str(result["text"]), "未看剧情") {
			t.Fatal("notification reroll lost the rule's original scope")
		}
		assertMovieButtons(t, result, true)
	}
	if a.count("SELECT COUNT(*) FROM webhook_logs") != 1 {
		t.Fatal("reroll should not resend rule notifications")
	}
	filters, custom := a.botFilters(cfg.ChatID)
	if !custom || filters.Genre != "悬疑" {
		t.Fatal("rule reroll changed menu configuration")
	}
}
