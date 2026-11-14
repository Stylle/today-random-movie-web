package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMovieBindingFlow(t *testing.T) {
	a, cookie := movieBotTestApp(t)
	movieBotMock(t, a, func(w http.ResponseWriter, r *http.Request, method string) {
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	_, rev, _ := a.botConfig()
	movieBotJSON(t, movieBotReq(a, cookie, "PUT", "/api/admin/telegram", movieBotInput{Enabled: true, BindFlow: true, Token: "123456:" + strings.Repeat("x", 30), Revision: rev}))
	cfg, rev, _ := a.botConfig()
	if cfg.ChatID != "" {
		t.Fatal("save implicitly bound")
	}
	pair := movieBotJSON(t, movieBotReq(a, cookie, "POST", "/api/admin/telegram/pair", nil))
	var u movieBotUpdate
	u.Message = &movieBotMessage{}
	u.Message.Text = str(pair["command"])
	u.Message.Date = time.Now().Unix()
	u.Message.Chat.ID = 42
	u.Message.Chat.Type = "group"
	u.Message.From.ID = 42
	if a.acceptMovieBinding(context.Background(), cfg, rev, u) {
		t.Fatal("group accepted")
	}
	u.Message.Chat.Type = "private"
	if !a.acceptMovieBinding(context.Background(), cfg, rev, u) {
		t.Fatal("private bind failed")
	}
	bound, next, _ := a.botConfig()
	if bound.ChatID != "42" || next == rev {
		t.Fatal("binding not persisted")
	}
	if a.acceptMovieBinding(context.Background(), cfg, rev, u) {
		t.Fatal("proof replayed")
	}
	movieBotJSON(t, movieBotReq(a, cookie, "POST", "/api/admin/telegram/unbind", nil))
	bound, _, _ = a.botConfig()
	if bound.ChatID != "" || bound.Token != cfg.Token {
		t.Fatal("unbind lost token")
	}
}
