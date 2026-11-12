package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (a *App) defaultFilters() Filters {
	f := Filters{Libraries: []string{}, ExcludeWatched: true}
	if raw := a.getSetting("random_default_filters", ""); raw != "" {
		if json.Unmarshal([]byte(raw), &f) != nil {
			return Filters{Libraries: []string{}, ExcludeWatched: true}
		}
	}
	return f
}
func cleanDefaultFilters(value any) (Filters, error) {
	if value == nil {
		return Filters{}, errors.New("请提供默认筛选范围")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return Filters{}, errors.New("筛选范围格式无效")
	}
	var f Filters
	if json.Unmarshal(raw, &f) != nil || len(f.Libraries) > 100 || len(f.Genre) > 512 || len(f.Country) > 512 {
		return f, errors.New("筛选范围格式无效")
	}
	for _, id := range f.Libraries {
		if len(id) > 512 {
			return f, errors.New("媒体库 ID 过长")
		}
	}
	if f.Year != "" {
		f.YearFrom, f.YearTo, f.Year = f.Year, f.Year, ""
	}
	for _, year := range []string{f.YearFrom, f.YearTo} {
		if year != "" {
			n, e := strconv.Atoi(year)
			if e != nil || n < 1000 || n > 9999 {
				return f, errors.New("年份必须为四位数字或不限")
			}
		}
	}
	if f.YearFrom != "" && f.YearTo != "" && f.YearFrom > f.YearTo {
		f.YearFrom, f.YearTo = f.YearTo, f.YearFrom
	}
	if f.Libraries == nil {
		f.Libraries = []string{}
	}
	return f, nil
}

type movieBotConfig struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
	ChatID  string `json:"chatId"`
	Since   int64  `json:"since"`
}
type movieBotInput struct {
	Enabled  bool   `json:"enabled"`
	Token    string `json:"token"`
	ChatID   string `json:"chatId"`
	Revision string `json:"revision"`
}

var movieBotToken = regexp.MustCompile(`^[0-9]{5,20}:[A-Za-z0-9_-]{20,100}$`)

func (a *App) botConfig() (movieBotConfig, string, error) {
	var cfg movieBotConfig
	var encoded, revision string
	err := a.db.QueryRow(`SELECT value,revision FROM telegram_movie_config WHERE id=1`).Scan(&encoded, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, "", nil
	}
	if err != nil {
		return cfg, "", err
	}
	raw, err := openSealed(a.secret, encoded)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg, revision, err
}
func (a *App) telegramSettings(w http.ResponseWriter) {
	cfg, revision, err := a.botConfig()
	if err != nil {
		fail(500, "Telegram 配置无法解密，请检查完整 data 备份")
	}
	var lastError, lastReceived string
	a.db.QueryRow(`SELECT last_error,last_received FROM telegram_movie_config WHERE id=1`).Scan(&lastError, &lastReceived)
	_, custom := a.botFilters(cfg.ChatID)
	respond(w, M{"enabled": cfg.Enabled, "chatId": cfg.ChatID, "hasToken": cfg.Token != "", "revision": revision, "lastError": lastError, "lastReceived": lastReceived, "customRange": custom})
}
func (a *App) saveTelegram(w http.ResponseWriter, r *http.Request) {
	var in movieBotInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		fail(400, "请求格式不正确")
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		fail(400, "请求格式不正确")
	}
	previous, revision, err := a.botConfig()
	if err != nil {
		fail(500, "机器人配置无法读取")
	}
	if in.Revision != revision {
		fail(409, "设置已改变，请刷新后台后保存")
	}
	if in.Token == "" {
		in.Token = previous.Token
	}
	if in.Token != "" && !movieBotToken.MatchString(in.Token) {
		fail(400, "Bot Token 格式不正确")
	}
	if in.Enabled {
		id, e := strconv.ParseInt(in.ChatID, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != in.ChatID {
			fail(400, "请填写正整数私聊 Chat ID，不支持群聊")
		}
		if in.Token == "" {
			fail(400, "请填写 Bot Token")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if _, err = a.checkMovieBot(ctx, in.Token); err != nil {
			fail(400, err.Error())
		}
	}
	cfg := movieBotConfig{Enabled: in.Enabled, Token: in.Token, ChatID: in.ChatID, Since: previous.Since}
	if cfg.Enabled && (!previous.Enabled || previous.Token != cfg.Token || previous.ChatID != cfg.ChatID) {
		cfg.Since = time.Now().Unix()
	}
	encoded, err := seal(a.secret, jsonString(cfg))
	if err != nil {
		fail(500, "机器人配置加密失败")
	}
	a.tgConfigMu.Lock()
	defer a.tgConfigMu.Unlock()
	_, current, err := a.botConfig()
	if err != nil || current != revision {
		fail(409, "设置已改变，请刷新后保存")
	}
	a.wakeMovieBot()
	tx, err := a.db.Begin()
	must(err)
	defer tx.Rollback()
	var next int64
	if previous.Token == cfg.Token {
		tx.QueryRow(`SELECT next_update FROM telegram_movie_config WHERE id=1`).Scan(&next)
	}
	_, err = tx.Exec(`INSERT INTO telegram_movie_config(id,value,revision,next_update) VALUES(1,?,?,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value,revision=excluded.revision,next_update=excluded.next_update,last_error=''`, encoded, randomHex(16), next)
	must(err)
	_, err = tx.Exec(`DELETE FROM telegram_movie_buttons`)
	must(err)
	must(tx.Commit())
	a.wakeMovieBot()
	respond(w, M{"ok": true})
}
func (a *App) testTelegram(w http.ResponseWriter, r *http.Request) {
	v := readBody(r)
	token := str(v["token"])
	cfg, _, err := a.botConfig()
	if err != nil {
		fail(500, "机器人配置无法读取")
	}
	if token == "" {
		token = cfg.Token
	}
	if !movieBotToken.MatchString(token) {
		fail(400, "请填写有效的 Bot Token")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	username, err := a.checkMovieBot(ctx, token)
	if err != nil {
		fail(400, err.Error())
	}
	chats := []string{}
	if cfg.Enabled && token == cfg.Token {
		if cfg.ChatID != "" {
			chats = append(chats, cfg.ChatID)
		}
	} else {
		var updates []movieBotUpdate
		if err = a.botCall(ctx, token, "getUpdates", M{"timeout": 0, "limit": 100, "allowed_updates": []string{"message", "callback_query"}}, &updates); err != nil {
			fail(502, err.Error())
		}
		seen := map[int64]bool{}
		for _, u := range updates {
			m := u.Message
			if m != nil && m.Chat.Type == "private" && m.Chat.ID > 0 && !m.From.Bot && !seen[m.Chat.ID] {
				chats = append(chats, strconv.FormatInt(m.Chat.ID, 10))
				seen[m.Chat.ID] = true
			}
		}
	}
	respond(w, M{"username": username, "chats": chats})
}

type movieBotError struct{ Code, Retry int }

func (e movieBotError) Error() string {
	switch e.Code {
	case 401, 404:
		return "Telegram Token 无效或已撤销"
	case 409:
		return "机器人存在 Webhook 或被其他实例占用，请使用独立专用机器人"
	case 429:
		return "Telegram 限流，稍后自动重试"
	case 403:
		return "机器人无法发送，请先在 Telegram 私聊机器人并取消屏蔽"
	case 400:
		return "Telegram 拒绝请求，请检查 Chat ID 和机器人设置"
	}
	return "Telegram 网络或接口不可用，请检查网络连接"
}
func (a *App) botCall(ctx context.Context, token, method string, input, out any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return movieBotError{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tgBase+"/bot"+token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return movieBotError{}
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.tgHTTP.Do(req)
	if err != nil {
		return movieBotError{}
	}
	defer res.Body.Close()
	var body struct {
		OK         bool            `json:"ok"`
		Result     json.RawMessage `json:"result"`
		Code       int             `json:"error_code"`
		Parameters struct {
			Retry int `json:"retry_after"`
		} `json:"parameters"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&body) != nil {
		return movieBotError{Code: res.StatusCode}
	}
	if !body.OK || res.StatusCode != 200 {
		return movieBotError{Code: max(body.Code, res.StatusCode), Retry: min(max(body.Parameters.Retry, 1), 3600)}
	}
	if out != nil && json.Unmarshal(body.Result, out) != nil {
		return movieBotError{}
	}
	return nil
}
func (a *App) checkMovieBot(ctx context.Context, token string) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	if err := a.botCall(ctx, token, "getMe", M{}, &me); err != nil {
		return "", err
	}
	var hook struct {
		URL string `json:"url"`
	}
	if err := a.botCall(ctx, token, "getWebhookInfo", M{}, &hook); err != nil {
		return "", err
	}
	if hook.URL != "" {
		return "", movieBotError{Code: 409}
	}
	return me.Username, nil
}
func (a *App) wakeMovieBot() {
	a.tgMu.Lock()
	if a.tgCancel != nil {
		a.tgCancel()
	}
	for _, cancel := range a.tgSendCancels {
		cancel()
	}
	a.tgMu.Unlock()
	select {
	case a.tgWake <- struct{}{}:
	default:
	}
}
func movieBotWait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (a *App) botStatus(revision string, err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	a.db.Exec(`UPDATE telegram_movie_config SET last_error=? WHERE revision=?`, message, revision)
}
func (a *App) telegramLoop(ctx context.Context) {
	for ctx.Err() == nil {
		select {
		case <-a.tgWake:
		default:
		}
		a.tgConfigMu.Lock()
		cfg, revision, err := a.botConfig()
		a.tgConfigMu.Unlock()
		if err != nil || !cfg.Enabled {
			select {
			case <-ctx.Done():
				return
			case <-a.tgWake:
			}
			continue
		}
		child, cancel := context.WithCancel(ctx)
		a.tgMu.Lock()
		a.tgCancel = cancel
		a.tgMu.Unlock()
		done := make(chan struct{})
		go func() { defer close(done); a.pollMovieBot(child, cfg, revision) }()
		select {
		case <-ctx.Done():
		case <-a.tgWake:
		case <-child.Done():
		case <-done:
		}
		cancel()
		<-done
		a.tgMu.Lock()
		a.tgCancel = nil
		a.tgMu.Unlock()
		if !movieBotWait(ctx, 100*time.Millisecond) {
			return
		}
	}
}

type movieBotUser struct {
	ID  int64 `json:"id"`
	Bot bool  `json:"is_bot"`
}
type movieBotMessage struct {
	ID   int64  `json:"message_id"`
	Date int64  `json:"date"`
	Text string `json:"text"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From movieBotUser `json:"from"`
}
type movieBotCallback struct {
	ID      string           `json:"id"`
	Data    string           `json:"data"`
	From    movieBotUser     `json:"from"`
	Message *movieBotMessage `json:"message"`
}
type movieBotUpdate struct {
	ID       int64             `json:"update_id"`
	Message  *movieBotMessage  `json:"message"`
	Callback *movieBotCallback `json:"callback_query"`
}

func botCommand(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	cmd := strings.ToLower(strings.Split(strings.TrimPrefix(words[0], "/"), "@")[0])
	switch cmd {
	case "getmovie", "随机":
		return "pick"
	case "menu", "meau", "start", "help":
		return "root"
	}
	return ""
}
func authorizedMovieUpdate(cfg movieBotConfig, u movieBotUpdate) bool {
	if m := u.Message; m != nil {
		return m.Chat.Type == "private" && strconv.FormatInt(m.Chat.ID, 10) == cfg.ChatID && m.From.ID == m.Chat.ID && !m.From.Bot && m.Date >= cfg.Since && botCommand(m.Text) != ""
	}
	if c := u.Callback; c != nil && c.Message != nil {
		return c.Message.Chat.Type == "private" && strconv.FormatInt(c.Message.Chat.ID, 10) == cfg.ChatID && c.From.ID == c.Message.Chat.ID && !c.From.Bot
	}
	return false
}
func (a *App) pollMovieBot(ctx context.Context, cfg movieBotConfig, revision string) {
	chatID, _ := strconv.ParseInt(cfg.ChatID, 10, 64)
	commands := []M{{"command": "getmovie", "description": "按当前范围随机一部影片"}, {"command": "menu", "description": "设置分类、年份与观看范围"}, {"command": "meau", "description": "打开范围设置菜单"}, {"command": "help", "description": "使用帮助"}}
	_ = a.botCall(ctx, cfg.Token, "setMyCommands", M{"commands": commands, "scope": M{"type": "chat", "chat_id": chatID}}, nil)
	_ = a.botCall(ctx, cfg.Token, "setChatMenuButton", M{"chat_id": chatID, "menu_button": M{"type": "commands"}}, nil)
	for ctx.Err() == nil {
		var next int64
		if a.db.QueryRow(`SELECT next_update FROM telegram_movie_config WHERE revision=?`, revision).Scan(&next) != nil {
			return
		}
		var updates []movieBotUpdate
		err := a.botCall(ctx, cfg.Token, "getUpdates", M{"offset": next, "timeout": 25, "limit": 25, "allowed_updates": []string{"message", "callback_query"}}, &updates)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			for _, u := range updates {
				if u.ID < next {
					continue
				}
				if authorizedMovieUpdate(cfg, u) {
					if u.Callback != nil {
						_ = a.botCall(ctx, cfg.Token, "answerCallbackQuery", M{"callback_query_id": u.Callback.ID}, nil)
					}
					var payload M
					payload, err = a.prepareMovieReply(ctx, cfg, revision, u)
					if err == nil && payload != nil {
						err = a.botCall(ctx, cfg.Token, "sendMessage", payload, nil)
					}
					if err != nil {
						break
					}
				}
				if ctx.Err() != nil {
					return
				}
				if _, err = a.db.Exec(`UPDATE telegram_movie_config SET next_update=?,last_received=?,last_error='' WHERE revision=?`, u.ID+1, nowISO(), revision); err != nil {
					err = errors.New("TG 接收进度保存失败")
					break
				}
				next = u.ID + 1
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.botStatus(revision, err)
			delay := 10 * time.Second
			var apiErr movieBotError
			if errors.As(err, &apiErr) && apiErr.Code == 429 {
				delay = time.Duration(apiErr.Retry) * time.Second
			}
			if !movieBotWait(ctx, delay) {
				return
			}
		} else {
			a.botStatus(revision, nil)
		}
	}
}

func (a *App) botFilters(chatID string) (Filters, bool) {
	var raw string
	if a.db.QueryRow(`SELECT filters_json FROM telegram_movie_chat WHERE chat_id=?`, chatID).Scan(&raw) != nil {
		return a.defaultFilters(), false
	}
	var f Filters
	if json.Unmarshal([]byte(raw), &f) != nil {
		return a.defaultFilters(), false
	}
	return f, true
}
func (a *App) saveBotFilters(chatID string, f Filters) {
	a.exec(`INSERT INTO telegram_movie_chat(chat_id,filters_json) VALUES(?,?) ON CONFLICT(chat_id) DO UPDATE SET filters_json=excluded.filters_json`, chatID, jsonString(f))
}
func (a *App) botButton(cfg movieBotConfig, revision, label, action string, payload M) M {
	token := randomHex(8)
	a.exec(`INSERT INTO telegram_movie_buttons(token,chat_id,revision,action,payload,expires) VALUES(?,?,?,?,?,?)`, token, cfg.ChatID, revision, action, jsonString(payload), time.Now().Add(30*time.Minute).Unix())
	return M{"text": truncate(label, 60), "callback_data": "mv:" + token}
}
func (a *App) botSummary(f Filters, custom bool) string {
	names := []string{}
	for _, id := range f.Libraries {
		label := id
		for _, row := range a.rows(`SELECT DISTINCT library FROM movies WHERE library_id=?`, id) {
			label = str(row["library"])
			break
		}
		names = append(names, truncate(label, 40))
	}
	mode := "跟随后台默认"
	if custom {
		mode = "TG 自定义（不影响网站）"
	}
	return "范围：" + mode + "\n媒体库：" + fallback(strings.Join(names, " / "), "全部") + "\n类型：" + fallback(f.Genre, "全部") + "\n地区：" + fallback(f.Country, "全部") + "\n年份：" + fallback(f.YearFrom, "最早") + " — " + fallback(f.YearTo, "最新") + "\n排除已观看：" + map[bool]string{true: "是", false: "否"}[f.ExcludeWatched] + fmt.Sprintf("\n可随机：%d 部", len(a.candidates(f)))
}
func (a *App) botMenu(cfg movieBotConfig, revision, screen string, page int) M {
	f, custom := a.botFilters(cfg.ChatID)
	button := func(label, action string, data M) M { return a.botButton(cfg, revision, label, action, data) }
	keyboard := [][]M{}
	text := "🎬 今日想看\n\n" + a.botSummary(f, custom) + "\n\n发送 getmovie 随机，发送 meau / menu 设置范围。"
	switch screen {
	case "root", "":
		keyboard = append(keyboard, []M{button("🎲 随机一部", "pick", M{})}, []M{button("媒体库", "screen", M{"screen": "libraries"}), button("影片类型", "screen", M{"screen": "genre"})}, []M{button("地区", "screen", M{"screen": "country"}), button("年份范围", "screen", M{"screen": "years"})}, []M{button("排除已观看："+map[bool]string{true: "开", false: "关"}[f.ExcludeWatched], "set", M{"field": "watched", "value": !f.ExcludeWatched})}, []M{button("恢复后台默认", "reset", M{})})
	case "years":
		keyboard = append(keyboard, []M{button("起始："+fallback(f.YearFrom, "最早"), "screen", M{"screen": "yearFrom"}), button("结束："+fallback(f.YearTo, "最新"), "screen", M{"screen": "yearTo"})}, []M{button("不限年份", "set", M{"field": "years", "value": ""})}, []M{button("返回范围菜单", "screen", M{"screen": "root"})})
	default:
		options := a.movieOptionsData()
		choices := []M{}
		switch screen {
		case "libraries":
			for _, v := range options["libraries"].([]M) {
				id := str(v["id"])
				selected := false
				for _, value := range f.Libraries {
					selected = selected || value == id
				}
				label := str(v["name"])
				if selected {
					label = "✓ " + label
				}
				choices = append(choices, M{"label": label, "value": id, "selected": selected})
			}
		case "genre", "country":
			for _, v := range options[map[string]string{"genre": "genres", "country": "countries"}[screen]].([]string) {
				choices = append(choices, M{"label": v, "value": v})
			}
		case "yearFrom", "yearTo":
			for _, v := range options["years"].([]string) {
				if _, err := strconv.Atoi(v); err == nil {
					choices = append(choices, M{"label": v + " 年", "value": v})
				}
			}
		default:
			return a.botMenu(cfg, revision, "root", 0)
		}
		pages := max(1, (len(choices)+7)/8)
		page = min(max(page, 0), pages-1)
		text += "\n\n" + map[string]string{"libraries": "媒体库可多选；不选表示全部", "genre": "选择影片类型", "country": "选择地区", "yearFrom": "选择起始年份", "yearTo": "选择结束年份"}[screen] + fmt.Sprintf("（%d/%d）", page+1, pages)
		keyboard = append(keyboard, []M{button("全部 / 不限", "set", M{"field": screen, "value": ""})})
		for _, choice := range choices[page*8 : min(len(choices), (page+1)*8)] {
			keyboard = append(keyboard, []M{button(str(choice["label"]), "set", M{"field": screen, "value": choice["value"], "selected": !truth(choice["selected"]), "page": page})})
		}
		navigation := []M{}
		if page > 0 {
			navigation = append(navigation, button("上一页", "screen", M{"screen": screen, "page": page - 1}))
		}
		if page+1 < pages {
			navigation = append(navigation, button("下一页", "screen", M{"screen": screen, "page": page + 1}))
		}
		if len(navigation) > 0 {
			keyboard = append(keyboard, navigation)
		}
		keyboard = append(keyboard, []M{button("返回范围菜单", "screen", M{"screen": "root"})})
	}
	return M{"chat_id": cfg.ChatID, "text": truncate(text, 1600), "reply_markup": M{"inline_keyboard": keyboard}, "link_preview_options": M{"is_disabled": true}}
}

// Persist the chosen response before sending, so network retries do not pick a
// different movie or apply a menu action twice. Telegram delivery is at-least-once.
func (a *App) prepareMovieReply(ctx context.Context, cfg movieBotConfig, revision string, u movieBotUpdate) (payload M, err error) {
	defer func() {
		if recover() != nil {
			payload = nil
			err = errors.New("TG 选片处理失败，请检查站点数据库")
		}
	}()
	a.tgConfigMu.Lock()
	defer a.tgConfigMu.Unlock()
	current, currentRev, e := a.botConfig()
	if e != nil || currentRev != revision || !current.Enabled || ctx.Err() != nil {
		return nil, errors.New("机器人设置已改变")
	}
	if !authorizedMovieUpdate(cfg, u) {
		return nil, nil
	}
	bot := shaHex(cfg.Token)
	var saved, savedRevision string
	e = a.db.QueryRow(`SELECT payload,revision FROM telegram_movie_updates WHERE bot=? AND update_id=?`, bot, u.ID).Scan(&saved, &savedRevision)
	if e == nil {
		if savedRevision != revision {
			return nil, nil
		}
		if json.Unmarshal([]byte(saved), &payload) != nil {
			return nil, errors.New("TG 待发送响应损坏")
		}
		return payload, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, errors.New("TG 接收记录无法读取")
	}
	a.exec(`DELETE FROM telegram_movie_buttons WHERE expires<? OR rowid NOT IN(SELECT rowid FROM telegram_movie_buttons ORDER BY rowid DESC LIMIT 2000)`, time.Now().Unix())
	action := "root"
	data := M{}
	if u.Message != nil {
		action = botCommand(u.Message.Text)
	} else if u.Callback != nil {
		row := a.one(`SELECT action,payload FROM telegram_movie_buttons WHERE token=? AND chat_id=? AND revision=? AND expires>?`, strings.TrimPrefix(u.Callback.Data, "mv:"), cfg.ChatID, revision, time.Now().Unix())
		if strings.HasPrefix(u.Callback.Data, "mv:") && row != nil {
			action = str(row["action"])
			_ = json.Unmarshal([]byte(str(row["payload"])), &data)
		}
	}
	screen := "root"
	page := intValue(data["page"], 0)
	movieID := ""
	switch action {
	case "screen":
		screen = str(data["screen"])
	case "reset":
		a.exec(`DELETE FROM telegram_movie_chat WHERE chat_id=?`, cfg.ChatID)
	case "set":
		f, _ := a.botFilters(cfg.ChatID)
		field, value := str(data["field"]), str(data["value"])
		switch field {
		case "libraries":
			if value == "" {
				f.Libraries = []string{}
			} else {
				next := []string{}
				for _, id := range f.Libraries {
					if id != value {
						next = append(next, id)
					}
				}
				if truth(data["selected"]) {
					next = append(next, value)
				}
				f.Libraries = next
			}
			screen = "libraries"
		case "genre":
			f.Genre = value
		case "country":
			f.Country = value
		case "watched":
			f.ExcludeWatched = truth(data["value"])
		case "yearFrom":
			f.YearFrom = value
			f.Year = ""
			screen = "years"
		case "yearTo":
			f.YearTo = value
			f.Year = ""
			screen = "years"
		case "years":
			f.Year, f.YearFrom, f.YearTo = "", "", ""
			screen = "years"
		}
		if f.YearFrom != "" && f.YearTo != "" && f.YearFrom > f.YearTo {
			f.YearFrom, f.YearTo = f.YearTo, f.YearFrom
		}
		a.saveBotFilters(cfg.ChatID, f)
	case "pick":
		f, custom := a.botFilters(cfg.ChatID)
		movie, pickErr := a.chooseMovie(f)
		payload = a.botMenu(cfg, revision, "root", 0)
		if pickErr != nil {
			payload["text"] = "当前范围下没有影片，请调整范围或先同步媒体库。\n\n" + a.botSummary(f, custom)
		} else {
			movieID = movie.ID
			payload["text"] = movieBotText(movie)
			if server := a.getSetting("fn_server", ""); server != "" {
				if client, e := NewFnOSClient(server, "", "", false); e == nil {
					markup := payload["reply_markup"].(M)
					keyboard := markup["inline_keyboard"].([][]M)
					markup["inline_keyboard"] = append([][]M{{M{"text": "打开飞牛影片", "url": client.PlayURL(movie.SourceID)}}}, keyboard...)
				}
			}
		}
	}
	if payload == nil {
		payload = a.botMenu(cfg, revision, screen, page)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	tx, e := a.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, errors.New("TG 响应保存失败")
	}
	defer tx.Rollback()
	_, e = tx.Exec(`INSERT INTO telegram_movie_updates(bot,update_id,revision,payload,movie_id,created) VALUES(?,?,?,?,?,?)`, bot, u.ID, revision, jsonString(payload), movieID, time.Now().Unix())
	if e != nil {
		return nil, errors.New("TG 去重记录保存失败")
	}
	if movieID != "" {
		_, e = tx.Exec(`INSERT INTO random_history(movie_id,source,schedule_id,created_at) VALUES(?,'telegram','',?)`, movieID, nowISO())
		if e != nil {
			return nil, errors.New("TG 随机记录保存失败")
		}
	}
	_, e = tx.Exec(`DELETE FROM random_history WHERE id NOT IN(SELECT id FROM random_history ORDER BY id DESC LIMIT 1000)`)
	if e != nil {
		return nil, errors.New("TG 历史清理失败")
	}
	_, e = tx.Exec(`DELETE FROM telegram_movie_updates WHERE created<? OR rowid NOT IN(SELECT rowid FROM telegram_movie_updates ORDER BY rowid DESC LIMIT 5000)`, time.Now().Add(-30*24*time.Hour).Unix())
	if e != nil {
		return nil, errors.New("TG 接收记录清理失败")
	}
	if e = tx.Commit(); e != nil {
		return nil, errors.New("TG 响应保存失败")
	}
	return payload, nil
}
