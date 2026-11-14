package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

var numericProviderID = regexp.MustCompile(`^[0-9]{1,12}$`)

func providerID(metadata M, provider string) string {
	var find func(M, string) string
	find = func(m M, parent string) string {
		for _, key := range []string{provider + "_id", provider + "Id", provider + "ID", provider} {
			if v := str(m[key]); numericProviderID.MatchString(v) {
				return v
			}
		}
		if strings.EqualFold(parent, provider) {
			if v := str(m["id"]); numericProviderID.MatchString(v) {
				return v
			}
		}
		for k, v := range m {
			if child, ok := v.(map[string]any); ok {
				if result := find(M(child), k); result != "" {
					return result
				}
			}
		}
		return ""
	}
	return find(metadata, "")
}

func normalizedTitle(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
func matchingTitle(title, year string, row M, keys ...string) bool {
	if year != "" {
		candidate := str(first(row, "year", "release_date"))
		if len(candidate) >= 4 && candidate[:4] != year {
			return false
		}
		if len(candidate) < 4 {
			return false
		}
	}
	for _, key := range keys {
		if v := normalizedTitle(str(row[key])); v != "" && (v == normalizedTitle(title) || normalizedTitle(shortChineseTitle(str(row[key]))) == normalizedTitle(shortChineseTitle(title))) {
			return true
		}
	}
	return false
}

// These requests obtain metadata only. Image bytes are always downloaded by the browser.
func (a *App) posterMetadataRequest(ctx context.Context, raw, credential string) ([]byte, string) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return nil, "unavailable"
	}
	req.Header.Set("Accept", "application/json,text/html;q=0.8")
	req.Header.Set("User-Agent", "TodayRandomMovie/0.8 (metadata lookup)")
	if credential != "" {
		if len(credential) == 32 && regexp.MustCompile(`^[a-fA-F0-9]{32}$`).MatchString(credential) {
			q := req.URL.Query()
			q.Set("api_key", credential)
			req.URL.RawQuery = q.Encode()
		} else {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
	}
	response, e := a.posterHTTP.Do(req)
	if e != nil {
		return nil, "unavailable"
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return nil, "unauthorized"
	}
	if response.StatusCode != 200 {
		return nil, "unavailable"
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if e != nil || len(body) > 2<<20 {
		return nil, "unavailable"
	}
	return body, "ok"
}

func (a *App) lookupTMDB(ctx context.Context, movie Movie, key string) (string, string) {
	if key == "" {
		return "", "missing_key"
	}
	endpoint := "https://api.themoviedb.org/3/search/movie?language=zh-CN&include_adult=false&query=" + url.QueryEscape(movie.Title)
	if numericProviderID.MatchString(movie.TMDBID) {
		endpoint = "https://api.themoviedb.org/3/movie/" + movie.TMDBID + "?language=zh-CN"
	} else if movie.Year != "" {
		endpoint += "&primary_release_year=" + url.QueryEscape(movie.Year)
	}
	body, status := a.posterMetadataRequest(ctx, endpoint, key)
	if status != "ok" {
		return "", status
	}
	var result M
	if json.Unmarshal(body, &result) != nil {
		return "", "unavailable"
	}
	candidates := []M{result}
	if !numericProviderID.MatchString(movie.TMDBID) {
		candidates = nil
		rows, _ := result["results"].([]any)
		for _, row := range rows {
			if m, ok := row.(map[string]any); ok && matchingTitle(movie.Title, movie.Year, M(m), "title", "original_title") {
				candidates = append(candidates, M(m))
			}
		}
	}
	for _, row := range candidates {
		if sources := externalPosters(M{"poster_path": row["poster_path"]}); len(sources) > 0 {
			a.fillMovieSummary(movie, str(row["overview"]))
			return sources[0], "ok"
		}
	}
	return "", "not_found"
}

var metaTag = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
var metaAttr = regexp.MustCompile(`(?i)(property|name|content)\s*=\s*["']([^"']*)["']`)

func (a *App) lookupDouban(ctx context.Context, movie Movie) (string, string) {
	if !a.reservePublicDouban() {
		return "", "rate_limited"
	}
	endpoint := "https://movie.douban.com/j/subject_suggest?q=" + url.QueryEscape(movie.Title)
	if numericProviderID.MatchString(movie.DoubanID) {
		endpoint = "https://movie.douban.com/subject/" + movie.DoubanID + "/"
	}
	body, status := a.posterMetadataRequest(ctx, endpoint, "")
	if status != "ok" {
		return "", status
	}
	if numericProviderID.MatchString(movie.DoubanID) {
		for _, tag := range metaTag.FindAllString(string(body), -1) {
			attrs := map[string]string{}
			for _, attr := range metaAttr.FindAllStringSubmatch(tag, -1) {
				attrs[strings.ToLower(attr[1])] = html.UnescapeString(attr[2])
			}
			if attrs["property"] == "og:image" {
				if sources := externalPosters(M{"cover": attrs["content"]}); len(sources) > 0 {
					return sources[0], "ok"
				}
			}
		}
	} else {
		var rows []M
		if json.Unmarshal(body, &rows) != nil {
			return "", "unavailable"
		}
		for _, row := range rows {
			if matchingTitle(movie.Title, movie.Year, row, "title", "sub_title") {
				if sources := externalPosters(M{"image": row["img"]}); len(sources) > 0 {
					return sources[0], "ok"
				}
			}
		}
	}
	return "", "not_found"
}

func (a *App) resolvePosters(ctx context.Context, id string) M {
	row := a.one("SELECT * FROM movies WHERE id=?", id)
	if row == nil {
		fail(404, "影片不存在")
	}
	settings := a.settings()
	revision := settings["poster_revision"]
	checked := int64(num(row["poster_checked_at"]))
	ttl := int64(600)
	if str(row["poster_lookup_status"]) == "ok" {
		ttl = 86400
	}
	if str(row["poster_revision"]) == revision && checked > time.Now().Unix()-ttl {
		return a.posterLookupJSON(row)
	}
	a.posterMu.Lock()
	if pending := a.posterPending[id]; pending != nil {
		a.posterMu.Unlock()
		select {
		case <-pending:
		case <-ctx.Done():
		}
		return a.posterLookupJSON(a.one("SELECT * FROM movies WHERE id=?", id))
	}
	done := make(chan struct{})
	a.posterPending[id] = done
	a.posterMu.Unlock()
	defer func() { a.posterMu.Lock(); delete(a.posterPending, id); close(done); a.posterMu.Unlock() }()
	select {
	case a.posterSlots <- struct{}{}:
		defer func() { <-a.posterSlots }()
	case <-ctx.Done():
		return a.posterLookupJSON(row)
	}
	movie := decodeMovie(row)
	key, _ := openSealed(a.secret, settings["poster_tmdb_key"])
	sources := externalPosters(M{"posters": movie.PosterSources, "cover": movie.CoverPath})
	statuses := []string{}
	higherSourceFound := false
	for _, provider := range posterOrder(settings) {
		for _, saved := range sources {
			if provider == "douban" && strings.Contains(saved, ".doubanio.com/") || provider == "tmdb" && strings.HasPrefix(saved, "https://image.tmdb.org/") {
				higherSourceFound = true
			}
		}
		var image, status string
		attempt, stop := context.WithTimeout(ctx, 3*time.Second)
		switch provider {
		case "douban":
			if settings["poster_douban_enabled"] == "0" {
				stop()
				continue
			}
			image, status = a.lookupDouban(attempt, movie)
		case "tmdb":
			image, status = a.lookupTMDB(attempt, movie, key)
		case "douying":
			if higherSourceFound {
				stop()
				continue
			}
			image, status = a.lookupDouying(attempt, movie, settings)
		default:
			stop()
			continue
		}
		stop()
		statuses = append(statuses, provider+":"+status)
		if image != "" {
			higherSourceFound = true
			sources = append(sources, image)
		}
	}
	if ctx.Err() != nil {
		return a.posterLookupJSON(row)
	}
	sources = orderedPosters(sources, settings)
	status := strings.Join(statuses, ",")
	if len(sources) > 0 && len(statuses) > 0 && strings.HasSuffix(statuses[0], ":ok") {
		status = "ok"
	}
	checkedAt := time.Now().Unix()
	if strings.Contains(status, "rate_limited") {
		checkedAt -= 570
	}
	a.exec("UPDATE movies SET poster_sources=?,poster_checked_at=?,poster_revision=?,poster_lookup_status=? WHERE id=? AND title=? AND year=? AND tmdb_id=? AND douban_id=?", jsonString(sources), checkedAt, revision, status, id, movie.Title, movie.Year, movie.TMDBID, movie.DoubanID)

	return a.posterLookupJSON(a.one("SELECT * FROM movies WHERE id=?", id))
}

// This quota-limited fallback is requested only when earlier images failed.
func (a *App) resolveDouyingFallback(ctx context.Context, id string) M {
	row := a.one("SELECT * FROM movies WHERE id=?", id)
	if row == nil {
		fail(404, "影片不存在")
	}
	settings := a.settings()
	if posterPriorities(settings)["douying"] == 0 {
		return a.posterLookupJSON(row)
	}
	movie := decodeMovie(row)
	for _, source := range movie.PosterSources {
		if strings.HasPrefix(source, "https://doubaninfo.com/dbposter/") {
			return a.posterLookupJSON(row)
		}
	}
	source, status := a.lookupDouying(ctx, movie, settings)
	if source != "" {
		movie.PosterSources = externalPosters(M{"posters": append(movie.PosterSources, source)})
		a.exec("UPDATE movies SET poster_sources=? WHERE id=?", jsonString(movie.PosterSources), id)
	}
	result := a.posterLookupJSON(a.one("SELECT * FROM movies WHERE id=?", id))
	result["status"] = status
	return result
}

func (a *App) posterLookupJSON(row M) M {
	if row == nil {
		return M{"posterSources": []string{}}
	}
	movie := decodeMovie(row)
	return M{"posterSources": orderedPosters(externalPosters(M{"posters": movie.PosterSources, "cover": movie.CoverPath}), a.settings()), "fallbackOrder": posterOrder(a.settings()), "overview": movie.Overview, "status": str(row["poster_lookup_status"])}
}

func (a *App) savePosterSettings(w http.ResponseWriter, r *http.Request) {
	v := readBody(r)
	updates := map[string]string{"poster_douban_enabled": boolString(truth(v["doubanEnabled"])), "poster_revision": randomHex(8)}
	key := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(str(v["tmdbKey"])), "Bearer "))
	if key != "" {
		if len(key) < 20 || len(key) > 4096 || strings.ContainsAny(key, "\r\n\t ") {
			fail(400, "请填写有效的 TMDB API Key 或 API Read Access Token")
		}
		encoded, e := seal(a.secret, key)
		if e != nil {
			fail(500, "无法加密保存密钥")
		}
		updates["poster_tmdb_key"] = encoded
	} else if truth(v["clearKey"]) {
		updates["poster_tmdb_key"] = ""
	}
	if value, ok := v["priorities"]; ok {
		var priorities map[string]int
		b, _ := json.Marshal(value)
		if json.Unmarshal(b, &priorities) != nil || !validPosterPriorities(priorities) {
			fail(400, "优先级必须为 0–5，启用项不得重复，且必须启用占位图")
		}
		updates["poster_priorities"] = jsonString(priorities)
		updates["poster_douban_enabled"] = boolString(priorities["douban"] > 0)
	}
	if value := strings.TrimSpace(str(v["douyingKey"])); value != "" {
		if len(value) > 4096 || strings.ContainsAny(value, "\r\n\t ") {
			fail(400, "豆影密钥格式无效")
		}
		encoded, e := seal(a.secret, value)
		if e != nil {
			fail(500, "密钥保存失败")
		}
		updates["poster_douying_key"] = encoded
	} else if truth(v["clearDouyingKey"]) {
		updates["poster_douying_key"] = ""
	}
	a.updateSettings(updates)
	respond(w, M{"ok": true, "hasTmdbKey": a.settings()["poster_tmdb_key"] != "", "hasDouyingKey": a.settings()["poster_douying_key"] != ""})
}

func (a *App) testPosterSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	settings := a.settings()
	key, _ := openSealed(a.secret, settings["poster_tmdb_key"])
	movie := Movie{Title: "登堂入室", Year: "2012"}
	var douban, tmdb, douying string
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		if settings["poster_douban_enabled"] == "0" || posterPriorities(settings)["douban"] == 0 {
			douban = "disabled"
			return
		}
		_, douban = a.lookupDouban(ctx, movie)
	}()
	go func() { defer workers.Done(); _, tmdb = a.lookupTMDB(ctx, movie, key) }()
	go func() {
		defer workers.Done()
		if posterPriorities(settings)["douying"] == 0 {
			douying = "disabled"
			return
		}
		_, douying = a.lookupDouying(ctx, movie, settings)
	}()
	workers.Wait()
	respond(w, M{"douban": douban, "tmdb": tmdb, "douying": douying, "message": fmt.Sprintf("豆瓣：%s；TMDB：%s", douban, tmdb)})
}

func (a *App) fillMovieSummary(movie Movie, summary string) {
	summary = strings.TrimSpace(summary)
	if movie.ID == "" || summary == "" {
		return
	}
	a.exec("UPDATE movies SET overview=? WHERE id=? AND title=? AND year=? AND overview=''", truncate(summary, 20000), movie.ID, movie.Title, movie.Year)
}
