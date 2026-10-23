package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
)

type authContext struct{ user M }

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

func adminEnv() (string, string, error) {
	username := strings.TrimSpace(os.Getenv("USER"))
	pass := os.Getenv("PASSWORD")
	if !validUsername(username) {
		return "", "", fmt.Errorf("USER 必须是 3-32 位字母、数字或 _.-")
	}
	if len([]rune(pass)) < 8 {
		return "", "", fmt.Errorf("PASSWORD 至少需要 8 位")
	}
	return username, pass, nil
}

// ensureInitialAdmin consumes USER/PASSWORD only when the database has no user.
func (a *App) ensureInitialAdmin() error {
	if a.count("SELECT COUNT(*) FROM users") > 0 {
		return nil
	}
	username, pass, err := adminEnv()
	if err != nil {
		return fmt.Errorf("首次启动配置无效: %w", err)
	}
	a.exec("INSERT INTO users(id,username,pwd,role,created_at) VALUES(?,?,?,?,?)", randomHex(8), username, password(pass), "admin", nowISO())
	return nil
}

// syncAdminFromEnv is called only by the explicit admin-sync command.
func (a *App) syncAdminFromEnv() error {
	username, pass, err := adminEnv()
	if err != nil {
		return err
	}
	row := a.one("SELECT id FROM users ORDER BY created_at LIMIT 1")
	if row == nil {
		a.exec("INSERT INTO users(id,username,pwd,role,created_at) VALUES(?,?,?,?,?)", randomHex(8), username, password(pass), "admin", nowISO())
	} else {
		a.exec("UPDATE users SET username=?,pwd=?,role='admin' WHERE id=?", username, password(pass), row["id"])
	}
	a.exec("DELETE FROM sessions")
	return nil
}

func (a *App) auth(r *http.Request) authContext {
	raw := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if raw == "" {
		if cookie, err := r.Cookie("trm_session"); err == nil {
			raw = cookie.Value
		}
	}
	if raw == "" {
		return authContext{}
	}
	row := a.one("SELECT u.* FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token_hash=? AND s.expires_at>?", shaHex(raw), nowISO())
	if row == nil {
		return authContext{}
	}
	delete(row, "pwd")
	return authContext{user: row}
}

func (a *App) require(r *http.Request) authContext {
	ctx := a.auth(r)
	if ctx.user == nil {
		fail(401, "需要登录")
	}
	return ctx
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time, remember bool) {
	cookie := &http.Cookie{Name: "trm_session", Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode}
	if remember {
		cookie.Expires = expires
		cookie.MaxAge = int(time.Until(expires).Seconds())
	}
	http.SetCookie(w, cookie)
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "trm_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
}

func publicUser(user M) M {
	if user == nil {
		return nil
	}
	return M{"id": user["id"], "username": user["username"], "role": user["role"], "createdAt": user["created_at"]}
}

func (a *App) authRoute(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/auth/login":
		if r.Method != http.MethodPost {
			fail(405, "method not allowed")
		}
		v := readBody(r)
		user := a.one("SELECT * FROM users WHERE username=? COLLATE NOCASE", strings.TrimSpace(str(v["username"])))
		if user == nil || !verifyPassword(str(v["password"]), str(user["pwd"])) {
			fail(401, "用户名或密码错误")
		}
		a.loginResponse(w, r, user, truth(v["remember"]))
	case "/api/auth/me":
		if r.Method != http.MethodGet {
			fail(405, "method not allowed")
		}
		ctx := a.require(r)
		respond(w, M{"user": publicUser(ctx.user)})
	case "/api/auth/logout":
		if r.Method != http.MethodPost {
			fail(405, "method not allowed")
		}
		ctx := a.require(r)
		a.exec("DELETE FROM sessions WHERE user_id=?", ctx.user["id"])
		clearSessionCookie(w, r)
		respond(w, M{"ok": true})
	default:
		fail(404, "not found")
	}
}

func (a *App) loginResponse(w http.ResponseWriter, r *http.Request, user M, remember bool) {
	ttl := 12 * time.Hour
	if remember {
		ttl = 30 * 24 * time.Hour
	}
	token := randomHex(32)
	expires := time.Now().UTC().Add(ttl)
	a.exec("DELETE FROM sessions WHERE expires_at<=?", nowISO())
	a.exec("INSERT INTO sessions(token_hash,user_id,csrf,expires_at,created_at) VALUES(?,?,?,?,?)", shaHex(token), user["id"], "-", expires.Format(time.RFC3339), nowISO())
	setSessionCookie(w, r, token, expires, remember)
	respond(w, M{"ok": true, "user": publicUser(user), "expiresAt": expires.Format(time.RFC3339)})
}
