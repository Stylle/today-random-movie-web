package main

import (
	"net/url"
	"sort"
	"strings"
)

// Only explicit image references are used. An ID or title is not an image URL.
// No provider API key or FnOS session is needed or exposed to the browser.
func externalPosters(metadata M) []string {
	groups := [3][]string{}
	seen := map[string]bool{}
	var walk func(any, string, int)
	walk = func(value any, key string, depth int) {
		if depth > 12 {
			return
		}
		switch v := value.(type) {
		case M:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				field := strings.ToLower(k)
				if field == "file_path" && strings.Contains(key, "poster") {
					field = "poster_path"
				}
				walk(v[k], field, depth+1)
			}
		case []any:
			for _, item := range v {
				walk(item, key, depth+1)
			}
		case []string:
			for _, item := range v {
				walk(item, key, depth+1)
			}
		case string:
			if !strings.Contains(key, "poster") && !strings.Contains(key, "cover") && key != "url" && key != "src" && key != "image" && key != "original" {
				return
			}
			raw := strings.TrimSpace(v)
			if key == "poster_path" && strings.HasPrefix(raw, "/") && !strings.Contains(raw[1:], "/") {
				raw = "https://image.tmdb.org/t/p/w500" + raw
			}
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
				return
			}
			host := strings.ToLower(u.Hostname())
			group := -1
			if strings.HasSuffix(host, ".doubanio.com") && strings.HasPrefix(u.Path, "/view/photo/") {
				group = 0
			}
			if host == "image.tmdb.org" && strings.HasPrefix(u.Path, "/t/p/") {
				group = 1
			}
			if host == "doubaninfo.com" && strings.HasPrefix(u.Path, "/dbposter/") {
				group = 2
			}
			if group >= 0 && !seen[raw] {
				seen[raw] = true
				groups[group] = append(groups[group], raw)
			}
		}
	}
	walk(metadata, "", 0)
	out := []string{}
	for _, group := range groups {
		out = append(out, group[:min(1, len(group))]...)
	}
	return out
}
