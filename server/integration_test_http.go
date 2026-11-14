package main

import (
	"context"
	"encoding/json"
	probe "github.com/Stylle/today-random-movie-web/server/internal/integrationtest"
	"net/http"
	"strings"
	"time"
)

func draftString(v map[string]json.RawMessage, k, f string) string {
	s, e := probe.String(v, k, f)
	if e != nil {
		fail(400, e.Error())
	}
	return s
}
func (a *App) testFnOSDraft(w http.ResponseWriter, r *http.Request) {
	v, e := probe.Read(r.Body)
	if e != nil {
		fail(400, e.Error())
	}
	s := a.settings()
	server := strings.TrimSpace(draftString(v, "server", s["fn_server"]))
	username, e := openSealed(a.secret, s["fn_username"])
	if e != nil {
		fail(500, "已保存的飞牛用户名无法解密")
	}
	username = draftString(v, "username", username)
	password, e := openSealed(a.secret, s["fn_password"])
	if e != nil {
		fail(500, "已保存的飞牛密码无法解密")
	}
	password = probe.Secret(draftString(v, "password", ""), password)
	insecure := truth(s["fn_insecure_tls"])
	if raw, ok := v["insecureTls"]; ok {
		if json.Unmarshal(raw, &insecure) != nil {
			fail(400, "TLS 选项无效")
		}
	}
	if server == "" {
		fail(400, "请填写飞牛地址")
	}
	client, e := NewFnOSClient(server, username, password, insecure)
	if e != nil {
		fail(400, e.Error())
	}
	defer client.client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	if e = client.Login(ctx); e != nil {
		fail(502, e.Error())
	}
	libs, e := client.Libraries(ctx)
	if e != nil {
		fail(502, e.Error())
	}
	respond(w, M{"ok": true, "libraries": len(libs), "persisted": false})
}
func (a *App) testWebhookDraft(w http.ResponseWriter, r *http.Request) {
	v, e := probe.Read(r.Body)
	if e != nil {
		fail(400, e.Error())
	}
	s := a.settings()
	for input, key := range map[string]string{"url": "webhook_url", "method": "webhook_method", "bodyType": "webhook_body_type", "headers": "webhook_headers", "bodyTemplate": "webhook_body_tpl"} {
		s[key] = draftString(v, input, s[key])
	}
	method := strings.ToUpper(fallback(s["webhook_method"], "POST"))
	if method != "GET" && method != "POST" && method != "PUT" {
		fail(400, "Webhook 方法仅支持 GET/POST/PUT")
	}
	s["webhook_method"] = method
	if s["webhook_body_type"] != "text" && s["webhook_body_type"] != "json" {
		fail(400, "正文类型仅支持 json/text")
	}
	ok, status, message := a.sendWebhookSettings("test", Movie{Title: "Webhook 草稿测试", Year: "2026"}, s)
	a.logNotification("webhook", "test", "", "", ok, status, message)
	if !ok {
		fail(502, message)
	}
	respond(w, M{"ok": true, "status": status, "persisted": false})
}
