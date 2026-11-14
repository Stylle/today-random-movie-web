package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func posterPriorities(settings map[string]string) map[string]int {
	p := map[string]int{"douban": 1, "tmdb": 2, "douying": 3, "cache": 4, "placeholder": 5}
	if settings["poster_douban_enabled"] == "0" {
		p = map[string]int{"douban": 0, "tmdb": 1, "douying": 2, "cache": 3, "placeholder": 4}
	}
	if settings["poster_priorities"] != "" {
		var saved map[string]int
		if json.Unmarshal([]byte(settings["poster_priorities"]), &saved) == nil && validPosterPriorities(saved) {
			return saved
		}
	}
	return p
}
func validPosterPriorities(p map[string]int) bool {
	if len(p) != 5 || p["placeholder"] <= 0 {
		return false
	}
	seen := map[int]bool{}
	for _, key := range []string{"douban", "tmdb", "douying", "cache", "placeholder"} {
		v, ok := p[key]
		if !ok || v < 0 || v > 5 {
			return false
		}
		if v > 0 && key != "placeholder" && v >= p["placeholder"] {
			return false
		}
		if v > 0 {
			if seen[v] {
				return false
			}
			seen[v] = true
		}
	}
	return true
}
func posterOrder(settings map[string]string) []string {
	p := posterPriorities(settings)
	out := []string{}
	for k, v := range p {
		if v > 0 {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return p[out[i]] < p[out[j]] })
	return out
}
func orderedPosters(sources []string, settings map[string]string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, provider := range posterOrder(settings) {
		for _, source := range sources {
			u, e := url.Parse(source)
			if e != nil {
				continue
			}
			host := u.Hostname()
			kind := ""
			if strings.HasSuffix(host, ".doubanio.com") {
				kind = "douban"
			}
			if host == "image.tmdb.org" {
				kind = "tmdb"
			}
			if host == "doubaninfo.com" {
				kind = "douying"
			}
			if kind == provider && !seen[source] {
				seen[source] = true
				out = append(out, source)
			}
		}
	}
	return out
}

// Persist quotas so restarting the application does not reset the daily budget.
func (a *App) reservePublicDouban() bool {
	a.posterMu.Lock()
	defer a.posterMu.Unlock()
	var last int64
	json.Unmarshal([]byte(a.settings()["poster_douban_last"]), &last)
	now := time.Now().UnixMilli()
	if now-last < 6100 {
		return false
	}
	a.updateSettings(map[string]string{"poster_douban_last": jsonString(now)})
	return true
}
func (a *App) reserveDouying() bool {
	a.posterMu.Lock()
	defer a.posterMu.Unlock()
	settings := a.settings()
	now := time.Now().UnixMilli()
	if int64(num(settings["poster_douying_cooldown"])) > now {
		return false
	}
	var saved []int64
	json.Unmarshal([]byte(settings["poster_douying_usage"]), &saved)
	active := []int64{}
	for _, stamp := range saved {
		if stamp > now-24*60*60*1000 {
			active = append(active, stamp)
		}
	}
	if len(active) >= 200 || len(active) > 0 && now-active[len(active)-1] < 6100 {
		return false
	}
	active = append(active, now)
	a.updateSettings(map[string]string{"poster_douying_usage": jsonString(active)})
	return true
}

func shortChineseTitle(value string) string {
	value = strings.TrimSpace(value)
	hasChinese := false
	for index, r := range value {
		if r > 127 {
			hasChinese = true
		}
		if hasChinese && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) && index > 0 && value[index-1] == ' ' {
			return strings.TrimSpace(value[:index])
		}
	}
	return value
}
func (a *App) lookupDouying(ctx context.Context, movie Movie, settings map[string]string) (string, string) {
	key, e := openSealed(a.secret, settings["poster_douying_key"])
	if e != nil || key == "" {
		return "", "missing_key"
	}
	if !a.reserveDouying() {
		return "", "rate_limited"
	}
	query := movie.Title + " " + movie.Year
	if movie.DoubanID != "" {
		query = movie.DoubanID
	} else if movie.TMDBID != "" {
		query = "https://www.themoviedb.org/movie/" + movie.TMDBID
	}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://doubaninfo.com/api/v1_douban.php?url="+url.QueryEscape(query), nil)
	if e != nil {
		return "", "unavailable"
	}
	req.Header.Set("X-API-KEY", key)
	response, e := a.posterHTTP.Do(req)
	if e != nil {
		return "", "unavailable"
	}
	defer response.Body.Close()
	if response.StatusCode == 429 {
		a.updateSettings(map[string]string{"poster_douying_cooldown": jsonString(time.Now().Add(time.Minute).UnixMilli())})
		return "", "rate_limited"
	}
	if response.StatusCode == 403 || response.StatusCode == 401 {
		return "", "unauthorized"
	}
	if response.StatusCode != 200 {
		return "", "unavailable"
	}
	var result any
	if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result) != nil {
		return "", "unavailable"
	}
	var find func(any) string
	find = func(value any) string {
		switch v := value.(type) {
		case map[string]any:
			if success, ok := v["success"].(bool); ok && !success {
				return ""
			}
			if matchingTitle(movie.Title, movie.Year, M(v), "chinese_title", "title") || movie.DoubanID != "" && str(v["sid"]) == movie.DoubanID {
				if sources := externalPosters(M{"poster": v["poster"], "cover": v["cover"]}); len(sources) > 0 {
					a.fillMovieSummary(movie, str(first(M(v), "summary", "overview")))
					return sources[0]
				}
			}
			for _, key := range []string{"data", "results", "items", "matches"} {
				if s := find(v[key]); s != "" {
					return s
				}
			}
		case []any:
			for _, item := range v {
				if s := find(item); s != "" {
					return s
				}
			}
		}
		return ""
	}
	if source := find(result); source != "" {
		return source, "ok"
	}
	return "", "not_found"
}
