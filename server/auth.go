package main

import (
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

type authContext struct {
	user       M
	csrf       string
	fromCookie bool
}

func validUsername(value string) bool {
	runes := []rune(value)
	if len(runes) < 3 || len(runes) > 32 {
		return false
	}
	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_.-", r) {
			return false
		}
	}
	return true
}

func (a *App) auth(r *http.Request) authContext {
	raw := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	fromCookie := false
	if raw == "" {
		if cookie, err := r.Cookie("trm_session"); err == nil {
			raw = cookie.Value
			fromCookie = true
		}
	}
	if raw == "" {
		return authContext{}
	}
	row := a.one("SELECT u.*,s.csrf FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token_hash=? AND s.expires_at>?", shaHex(raw), nowISO())
	if row == nil {
		return authContext{}
	}
	csrf := str(row["csrf"])
	delete(row, "csrf")
	delete(row, "pwd")
	return authContext{user: row, csrf: csrf, fromCookie: fromCookie}
}

func (a *App) require(r *http.Request) authContext {
	ctx := a.auth(r)
	if ctx.user == nil {
		fail(401, "需要登录")
	}
	if ctx.fromCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		if subtleString(r.Header.Get("X-CSRF-Token"), ctx.csrf) == false {
			fail(403, "CSRF 校验失败")
		}
	}
	return ctx
}

func subtleString(a, b string) bool {
	return a != "" && b != "" && len(a) == len(b) && subtleCompare([]byte(a), []byte(b))
}

func subtleCompare(a, b []byte) bool {
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || forwardedHTTPS(r)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time, remember bool) {
	cookie := &http.Cookie{Name: "trm_session", Value: token, Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode}
	if remember {
		cookie.Expires = expires
		cookie.MaxAge = int(time.Until(expires).Seconds())
	}
	http.SetCookie(w, cookie)
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "trm_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode})
}

func publicUser(user M) M {
	if user == nil {
		return nil
	}
	return M{"id": user["id"], "username": user["username"], "role": user["role"], "createdAt": user["created_at"]}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if forwarded := forwardedHost(r); forwarded != "" {
		host = forwarded
	}
	return strings.EqualFold(u.Host, host)
}

func (a *App) authRoute(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/auth/setup":
		if r.Method != http.MethodPost {
			fail(405, "method not allowed")
		}
		if !sameOrigin(r) {
			fail(403, "来源不允许")
		}
		if a.count("SELECT COUNT(*) FROM users") != 0 {
			fail(409, "站点已经初始化")
		}
		v := readBody(r)
		if want := env("SETUP_TOKEN", a.setupToken); want == "" || !subtleString(str(v["setupToken"]), want) {
			fail(403, "初始化令牌错误")
		}
		username := strings.TrimSpace(str(v["username"]))
		pass := str(v["password"])
		if !validUsername(username) {
			fail(400, "用户名需为 3-32 位字母、数字或 _.-")
		}
		if len([]rune(pass)) < 12 {
			fail(400, "密码至少 12 位")
		}
		a.exec("INSERT INTO users(id,username,pwd,role,created_at) VALUES(?,?,?,?,?)", randomHex(8), username, password(pass), "admin", nowISO())
		a.loginResponse(w, r, a.one("SELECT * FROM users WHERE username=?", username), truth(v["remember"]))
	case "/api/auth/login":
		if r.Method != http.MethodPost {
			fail(405, "method not allowed")
		}
		if !sameOrigin(r) {
			fail(403, "来源不允许")
		}
		key := "login:" + a.clientIP(r)
		if !a.rate(key, 8, 10*time.Minute) {
			fail(429, "尝试次数过多，请稍后再试")
		}
		v := readBody(r)
		user := a.one("SELECT * FROM users WHERE username=? COLLATE NOCASE", strings.TrimSpace(str(v["username"])))
		if user == nil || !verifyPassword(str(v["password"]), str(user["pwd"])) {
			fail(401, "用户名或密码错误")
		}
		a.clearRate(key)
		a.loginResponse(w, r, user, truth(v["remember"]))
	case "/api/auth/me":
		if r.Method != http.MethodGet {
			fail(405, "method not allowed")
		}
		ctx := a.require(r)
		respond(w, M{"user": publicUser(ctx.user), "csrf": ctx.csrf})
	case "/api/auth/logout":
		if r.Method != http.MethodPost {
			fail(405, "method not allowed")
		}
		ctx := a.require(r)
		a.exec("DELETE FROM sessions WHERE user_id=? AND csrf=?", ctx.user["id"], ctx.csrf)
		clearSessionCookie(w, r)
		respond(w, M{"ok": true})
	case "/api/auth/password":
		if r.Method != http.MethodPost {
			fail(405, "method not allowed")
		}
		ctx := a.require(r)
		v := readBody(r)
		stored := a.one("SELECT pwd FROM users WHERE id=?", ctx.user["id"])
		if stored == nil || !verifyPassword(str(v["current"]), str(stored["pwd"])) {
			fail(401, "当前密码错误")
		}
		if len([]rune(str(v["next"]))) < 12 {
			fail(400, "新密码至少 12 位")
		}
		a.exec("UPDATE users SET pwd=? WHERE id=?", password(str(v["next"])), ctx.user["id"])
		a.exec("DELETE FROM sessions WHERE user_id=? AND csrf<>?", ctx.user["id"], ctx.csrf)
		respond(w, M{"ok": true})
	default:
		fail(404, "not found")
	}
}

func (a *App) loginResponse(w http.ResponseWriter, r *http.Request, user M, remember bool) {
	ttl := time.Duration(num(env("SESSION_HOURS", "12"))) * time.Hour
	if remember {
		ttl = time.Duration(num(env("REMEMBER_DAYS", "30"))) * 24 * time.Hour
	}
	if ttl < time.Hour {
		ttl = 12 * time.Hour
	}
	token, csrf := randomHex(32), randomHex(24)
	expires := time.Now().UTC().Add(ttl)
	a.exec("DELETE FROM sessions WHERE expires_at<=?", nowISO())
	a.exec("INSERT INTO sessions(token_hash,user_id,csrf,expires_at,created_at) VALUES(?,?,?,?,?)", shaHex(token), user["id"], csrf, expires.Format(time.RFC3339), nowISO())
	setSessionCookie(w, r, token, expires, remember)
	respond(w, M{"ok": true, "user": publicUser(user), "csrf": csrf, "expiresAt": expires.Format(time.RFC3339)})
}
