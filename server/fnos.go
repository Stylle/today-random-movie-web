package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	fnSecret     = "16CCEB3D-AB42-077D-36A1-F355324E4237"
	fnSignPrefix = "NDzZTVxnRKP8Z0jXg1VAMonaG8akvh"
	fnClientVer  = "629"
	fnUserAgent  = "Mozilla/5.0 (compatible; TodayRandomMovie/0.1; +https://github.com/Stylle/today-random-movie-web)"
)

var genreNames = map[int]string{
	1: "动作", 2: "冒险", 3: "动画", 4: "喜剧", 5: "犯罪", 6: "纪录片", 7: "剧情", 8: "家庭",
	9: "科幻奇幻", 10: "历史", 11: "恐怖", 12: "音乐", 13: "悬疑", 14: "爱情", 16: "电视电影",
	17: "惊悚", 18: "战争与政治", 19: "西部", 30: "动作冒险", 31: "儿童", 32: "新闻", 33: "真人秀",
	35: "肥皂剧", 36: "脱口秀", 101: "体育", 106: "其他", 109: "电视剧", 110: "综艺",
}

var countryNames = map[string]string{
	"CN": "中国", "HK": "中国香港", "TW": "中国台湾", "JP": "日本", "KR": "韩国", "US": "美国",
	"GB": "英国", "FR": "法国", "DE": "德国", "CA": "加拿大", "AU": "澳大利亚", "IN": "印度",
	"RU": "俄罗斯", "ES": "西班牙", "IT": "意大利", "TH": "泰国", "SG": "新加坡", "BR": "巴西", "MX": "墨西哥",
}

type Movie struct {
	ID        string   `json:"id"`
	SourceID  string   `json:"-"`
	Title     string   `json:"title"`
	Year      string   `json:"year"`
	Genres    []string `json:"genres"`
	Countries []string `json:"countries"`
	Overview  string   `json:"overview"`
	CoverPath string   `json:"-"`
	LibraryID string   `json:"libraryId"`
	Library   string   `json:"library"`
	Watched   bool     `json:"watched"`
	Duration  string   `json:"duration"`
	Rating    string   `json:"rating"`
}

type FnOSClient struct {
	server    string
	origin    string
	webBase   string
	apiBases  []string
	username  string
	password  string
	token     string
	client    *http.Client
	requestMu sync.Mutex
}

func NewFnOSClient(server, username, password string, insecureTLS bool) (*FnOSClient, error) {
	server = strings.TrimSpace(server)
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		server = "http://" + server
	}
	u, err := validateTarget(server, "fnos")
	if err != nil {
		return nil, err
	}
	origin := u.Scheme + "://" + u.Host
	webPath := strings.TrimSuffix(u.EscapedPath(), "/")
	webBase := origin + webPath
	apiBases := []string{}
	if strings.HasSuffix(strings.ToLower(webPath), "/api/v1") {
		apiBases = append(apiBases, webBase)
	} else if webPath != "" {
		apiBases = append(apiBases, webBase+"/api/v1", origin+"/api/v1")
	} else {
		apiBases = append(apiBases, origin+"/v/api/v1", origin+"/api/v1")
		webBase = origin + "/v"
	}
	client := restrictedClient("fnos", 30*time.Second)
	jar, _ := cookiejar.New(nil)
	client.Jar = jar
	if insecureTLS {
		if transport, ok := client.Transport.(*http.Transport); ok {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- explicit per-admin compatibility option for private NAS certificates.
		}
	}
	return &FnOSClient{server: server, origin: origin, webBase: webBase, apiBases: unique(apiBases), username: username, password: password, client: client}, nil
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func authx(method, rawURL string, body []byte) string {
	u, _ := url.Parse(rawURL)
	digestInput := body
	if strings.EqualFold(method, http.MethodGet) {
		pairs := u.Query()
		keys := make([]string, 0, len(pairs))
		for key := range pairs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ordered := url.Values{}
		for _, key := range keys {
			for _, value := range pairs[key] {
				ordered.Add(key, value)
			}
		}
		digestInput = []byte(ordered.Encode())
	}
	bodySum := md5.Sum(digestInput)
	nonce := strconv.Itoa(100000 + rand.IntN(900000))
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	raw := strings.Join([]string{fnSignPrefix, u.EscapedPath(), nonce, timestamp, hex.EncodeToString(bodySum[:]), fnSecret}, "_")
	sum := md5.Sum([]byte(raw))
	return "nonce=" + nonce + "&timestamp=" + timestamp + "&sign=" + hex.EncodeToString(sum[:])
}

func (c *FnOSClient) request(ctx context.Context, method, route string, payload any) (any, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	var last error
	for _, base := range c.apiBases {
		rawURL := strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(route, "/")
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("User-Agent", fnUserAgent)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Origin", c.origin)
		req.Header.Set("Referer", strings.TrimSuffix(c.webBase, "/")+"/")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.token != "" {
			req.Header.Set("Authorization", c.token)
			req.Header.Set("X-Trim-Client", "web")
			req.Header.Set("X-Trim-Client-Version", fnClientVer)
			req.Header.Set("authx", authx(method, rawURL, body))
		}
		resp, err := c.client.Do(req)
		if err != nil {
			last = err
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			last = fmt.Errorf("飞牛接口 HTTP %d: %s", resp.StatusCode, truncate(string(data), 240))
			continue
		}
		if len(data) == 0 {
			return M{}, nil
		}
		var result any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err = decoder.Decode(&result); err != nil {
			last = fmt.Errorf("飞牛返回的不是 JSON: %w", err)
			continue
		}
		return result, nil
	}
	if last == nil {
		last = fmt.Errorf("没有可用的飞牛 API 地址")
	}
	return nil, last
}

func truncate(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}

func first(value M, keys ...string) any {
	for _, key := range keys {
		if item, ok := value[key]; ok && item != nil && str(item) != "" {
			return item
		}
	}
	return nil
}

func nestedItems(value any) []M {
	switch typed := value.(type) {
	case []any:
		out := []M{}
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				out = append(out, M(m))
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"data", "items", "results", "list", "libraries", "medias", "movies"} {
			if result := nestedItems(typed[key]); len(result) > 0 {
				return result
			}
		}
	}
	return nil
}

func (c *FnOSClient) Login(ctx context.Context) error {
	if c.username == "" && c.password == "" {
		return nil
	}
	// Some FnOS versions establish a cookie on the web entry page before login.
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.webBase, "/")+"/", nil); err == nil {
		req.Header.Set("User-Agent", fnUserAgent)
		if resp, requestErr := c.client.Do(req); requestErr == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
		}
	}
	var last error
	for _, route := range []string{"/login", "/user/loginByPassword"} {
		for _, payload := range []M{
			{"username": c.username, "password": c.password},
			{"userName": c.username, "passWord": c.password},
			{"account": c.username, "password": c.password},
			{"loginName": c.username, "password": c.password},
		} {
			result, err := c.request(ctx, http.MethodPost, route, payload)
			if err != nil {
				last = err
				continue
			}
			m, ok := result.(map[string]any)
			if !ok {
				last = fmt.Errorf("登录返回格式无法识别")
				continue
			}
			nested, _ := m["data"].(map[string]any)
			token := str(first(M(m), "token", "accessToken", "jwt", "session"))
			if token == "" && nested != nil {
				token = str(first(M(nested), "token", "accessToken", "jwt", "session"))
			}
			code := str(first(M(m), "code", "status", "errno"))
			success, hasSuccess := m["success"].(bool)
			if token != "" || (hasSuccess && success) || code == "0" || code == "200" || first(M(m), "user", "username", "account", "uid") != nil {
				c.token = token
				return nil
			}
			last = fmt.Errorf("登录失败: %s", str(first(M(m), "message", "msg", "error", "errorMessage")))
		}
	}
	if last == nil {
		last = fmt.Errorf("登录失败")
	}
	return last
}

func (c *FnOSClient) Libraries(ctx context.Context) ([]M, error) {
	var last error
	for _, route := range []string{"/mdb/list", "/mediadb/list"} {
		result, err := c.request(ctx, http.MethodGet, route, nil)
		if err != nil {
			last = err
			continue
		}
		items := nestedItems(result)
		if len(items) == 0 {
			continue
		}
		out := []M{}
		for _, item := range items {
			category := strings.ToLower(str(first(item, "category", "type")))
			if category != "tv" && category != "series" && category != "电视剧" {
				out = append(out, item)
			}
		}
		return out, nil
	}
	return nil, last
}

func (c *FnOSClient) Movies(ctx context.Context, progress func(done, total int)) ([]Movie, error) {
	var result any
	var err error
	for _, payload := range []M{{}, {"page": 1, "pageSize": 10000}} {
		result, err = c.request(ctx, http.MethodPost, "/item/list", payload)
		if err == nil && len(nestedItems(result)) > 0 {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	items := nestedItems(result)
	type pending struct {
		item M
		guid string
	}
	queue := []pending{}
	for _, item := range items {
		kind := strings.ToLower(str(first(item, "mediaType", "type", "itemType", "category")))
		if strings.Contains(kind, "series") || strings.Contains(kind, "episode") || kind == "tv" || kind == "电视剧" {
			continue
		}
		guid := str(first(item, "guid", "id", "mediaId"))
		queue = append(queue, pending{item: item, guid: guid})
	}
	progress(0, len(queue))
	details := make([]M, len(queue))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var doneMu sync.Mutex
	done := 0
	workers := min(8, max(1, len(queue)))
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if queue[index].guid != "" {
					value, e := c.request(ctx, http.MethodGet, "/item/"+url.PathEscape(queue[index].guid), nil)
					if e == nil {
						if m, ok := value.(map[string]any); ok {
							if nested, ok := m["data"].(map[string]any); ok {
								details[index] = M(nested)
							} else {
								details[index] = M(m)
							}
						}
					}
				}
				doneMu.Lock()
				done++
				progress(done, len(queue))
				doneMu.Unlock()
			}
		}()
	}
	for index := range queue {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	out := make([]Movie, 0, len(queue))
	for index, item := range queue {
		merged := M{}
		for key, value := range item.item {
			merged[key] = value
		}
		for key, value := range details[index] {
			merged[key] = value
		}
		genres := stringSlice(first(merged, "genres", "genre", "tags"), genreNames)
		countries := stringSlice(first(merged, "production_countries", "origin_country", "country", "region", "area"), nil)
		for i, country := range countries {
			if name := countryNames[country]; name != "" {
				countries[i] = name
			}
		}
		year := str(first(merged, "year", "releaseYear", "release_date", "first_air_date", "premiereDate"))
		if len(year) > 4 {
			year = year[:4]
		}
		watched := truth(first(merged, "watched", "isWatched", "played", "viewed")) || num(first(merged, "playCount", "viewCount", "watchedCount")) > 0
		guid := item.guid
		if guid == "" {
			guid = str(first(merged, "guid", "id", "mediaId"))
		}
		id := shaHex(guid)
		if guid == "" {
			id = shaHex(str(first(merged, "title", "name")) + "|" + year)
		}
		out = append(out, Movie{
			ID: id[:24], SourceID: guid, Title: fallback(str(first(item.item, "title", "name", "movieName")), "未命名影片"),
			Year: year, Genres: genres, Countries: countries, Overview: str(first(merged, "overview", "description", "plot", "summary")),
			CoverPath: coverReference(first(merged, "cover", "poster", "posters", "posterUrl", "image", "thumb")),
			LibraryID: str(first(item.item, "libraryId", "library_id", "ancestor_guid", "sourceId")),
			Library:   fallback(str(first(item.item, "libraryName", "library", "sourceName", "ancestor_name")), "未分类媒体库"),
			Watched:   watched, Duration: str(first(merged, "duration", "runtime")), Rating: str(first(merged, "rating", "score", "vote_average")),
		})
	}
	return out, nil
}

func coverReference(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		for _, item := range typed {
			if result := coverReference(item); result != "" {
				return result
			}
		}
	case map[string]any:
		return str(first(M(typed), "url", "path", "src", "original"))
	}
	return ""
}

func fallback(value, alternate string) string {
	if strings.TrimSpace(value) == "" {
		return alternate
	}
	return value
}

func stringSlice(value any, numericNames map[int]string) []string {
	values := []any{}
	switch typed := value.(type) {
	case []any:
		values = typed
	case string:
		for _, item := range strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == '/' || r == '|' || r == '、' }) {
			values = append(values, strings.TrimSpace(item))
		}
	case nil:
		return []string{}
	default:
		values = []any{typed}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, item := range values {
		name := strings.TrimSpace(str(item))
		if numericNames != nil {
			if n, err := strconv.Atoi(name); err == nil && numericNames[n] != "" {
				name = numericNames[n]
			}
		}
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func (c *FnOSClient) CoverURL(reference string) (string, error) {
	if reference == "" {
		return "", fmt.Errorf("影片没有封面")
	}
	if u, err := url.Parse(reference); err == nil && u.IsAbs() {
		if !strings.EqualFold(u.Hostname(), mustURL(c.server).Hostname()) {
			return "", fmt.Errorf("封面地址不属于已配置的飞牛主机")
		}
		return u.String(), nil
	}
	clean := "/" + strings.TrimPrefix(reference, "/")
	if strings.Contains(clean, "/sys/img/") {
		return c.origin + clean, nil
	}
	return strings.TrimSuffix(c.apiBases[0], "/") + "/sys/img/" + strings.TrimPrefix(path.Clean(clean), "/"), nil
}

func mustURL(value string) *url.URL {
	u, _ := url.Parse(value)
	return u
}

func (c *FnOSClient) PlayURL(guid string) string {
	return strings.TrimSuffix(c.webBase, "/") + "/video/" + url.PathEscape(guid)
}

func (c *FnOSClient) FetchCover(ctx context.Context, reference string) (*http.Response, error) {
	rawURL, err := c.CoverURL(reference)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("User-Agent", fnUserAgent)
	req.Header.Set("Referer", strings.TrimSuffix(c.webBase, "/")+"/")
	if c.token != "" {
		req.Header.Set("Authorization", c.token)
		req.Header.Set("X-Trim-Client", "web")
		req.Header.Set("X-Trim-Client-Version", fnClientVer)
		req.Header.Set("authx", authx(http.MethodGet, rawURL, nil))
	}
	return c.client.Do(req)
}
