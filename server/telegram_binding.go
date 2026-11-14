package main

import (
	"context"
	"errors"
	probe "github.com/Stylle/today-random-movie-web/server/internal/integrationtest"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *App) movieBindingRoute(w http.ResponseWriter, r *http.Request) {
	owner := a.require(r)
	cfg, rev, e := a.botConfig()
	if e != nil {
		fail(500, "配置读取失败")
	}
	if !cfg.Enabled || cfg.Token == "" {
		fail(400, "请先保存 Token 并启用机器人")
	}
	if strings.HasSuffix(r.URL.Path, "/unbind") {
		a.tgConfigMu.Lock()
		defer a.tgConfigMu.Unlock()
		current, currentRev, e := a.botConfig()
		if e != nil || currentRev != rev {
			fail(409, "配置已改变，请刷新")
		}
		current.ChatID = ""
		if e = a.storeMovieBinding(current, rev); e != nil {
			fail(409, "配置已改变，请刷新")
		}
		a.botBinding.Clear()
		a.wakeMovieBot()
		respond(w, M{"ok": true})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	name, e := a.checkMovieBot(ctx, cfg.Token)
	if e != nil {
		fail(502, e.Error())
	}
	a.tgConfigMu.Lock()
	defer a.tgConfigMu.Unlock()
	_, current, e := a.botConfig()
	if e != nil || current != rev {
		fail(409, "配置已改变，请刷新")
	}
	command, e := a.botBinding.Create(str(owner.user["id"]), rev)
	if e != nil {
		fail(500, "生成绑定码失败")
	}
	respond(w, M{"command": command, "expiresIn": 300, "bot": name})
}
func (a *App) storeMovieBinding(cfg movieBotConfig, rev string) error {
	encoded, e := seal(a.secret, jsonString(cfg))
	if e != nil {
		return e
	}
	tx, e := a.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.Exec("UPDATE telegram_movie_config SET value=?,revision=?,last_error='' WHERE id=1 AND revision=?", encoded, randomHex(16), rev)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("配置已改变")
	}
	if _, e = tx.Exec("DELETE FROM telegram_movie_buttons"); e != nil {
		return e
	}
	return tx.Commit()
}
func (a *App) acceptMovieBinding(ctx context.Context, cfg movieBotConfig, rev string, u movieBotUpdate) bool {
	m := u.Message
	if m == nil || m.Chat.Type != "private" || m.From.Bot || m.Chat.ID <= 0 || m.From.ID != m.Chat.ID || m.Date < time.Now().Unix()-120 || m.Date > time.Now().Unix()+30 {
		return false
	}
	parts := strings.Fields(m.Text)
	if len(parts) != 2 || strings.ToLower(strings.Split(parts[0], "@")[0]) != "/bind" {
		return false
	}
	a.tgConfigMu.Lock()
	e := a.botBinding.Consume(parts[1], rev, func(owner string) error {
		if a.one("SELECT id FROM users WHERE id=? AND role='admin'", owner) == nil {
			return errors.New("账户已失效")
		}
		current, currentRev, e := a.botConfig()
		if e != nil || currentRev != rev || !current.Enabled || current.Token != cfg.Token {
			return errors.New("配置已变化")
		}
		current.ChatID = strconv.FormatInt(m.Chat.ID, 10)
		current.Since = time.Now().Unix()
		return a.storeMovieBinding(current, rev)
	})
	a.tgConfigMu.Unlock()
	if e != nil {
		return false
	}
	_ = a.botCall(ctx, cfg.Token, "sendMessage", M{"chat_id": m.Chat.ID, "text": "绑定成功。使用菜单中的 /getmovie 随机选片，/menu 设置选片范围。"}, nil)
	a.registerMovieMenu(ctx, cfg, m.Chat.ID)
	a.wakeMovieBot()
	return true
}
func (a *App) registerMovieMenu(ctx context.Context, cfg movieBotConfig, chat int64) {
	commands := []probe.Command{{Command: "start", Description: "使用说明"}, {Command: "bind", Description: "绑定网站账户（后接绑定码）"}}
	if chat > 0 {
		commands = []probe.Command{{Command: "getmovie", Description: "随机一部影片"}, {Command: "menu", Description: "设置选片范围"}}
	}
	_ = (probe.Telegram{HTTP: a.tgHTTP, BaseURL: a.tgBase}).Menu(ctx, cfg.Token, chat, commands)
}
