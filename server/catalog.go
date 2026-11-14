package main

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Filters struct {
	RatingMin      *float64 `json:"ratingMin,omitempty"`
	RatingMax      *float64 `json:"ratingMax,omitempty"`
	Libraries      []string `json:"libraries"`
	Genre          string   `json:"genre"`
	Country        string   `json:"country"`
	Year           string   `json:"year,omitempty"` // 兼容 v0.3.0 及更早版本保存的单年份规则。
	YearFrom       string   `json:"yearFrom"`
	YearTo         string   `json:"yearTo"`
	ExcludeWatched bool     `json:"excludeWatched"`
}

var countryGroups = map[string][]string{
	"华语": {"中国", "中国大陆", "中国香港", "中国台湾", "香港", "台湾", "CN", "HK", "TW"},
	"美国": {"美国", "US", "USA", "United States"},
	"欧洲": {"英国", "法国", "德国", "西班牙", "意大利", "俄罗斯", "欧洲", "GB", "UK", "FR", "DE", "ES", "IT", "RU"},
	"韩国": {"韩国", "KR", "South Korea"},
	"日本": {"日本", "JP", "Japan"},
	"印度": {"印度", "IN", "India"},
}

func (a *App) configuredClient() (*FnOSClient, error) {
	a.fnMu.Lock()
	defer a.fnMu.Unlock()
	if a.fnClient != nil {
		return a.fnClient, nil
	}
	s := a.settings()
	server := strings.TrimSpace(s["fn_server"])
	if server == "" {
		return nil, fmt.Errorf("尚未配置飞牛地址")
	}
	username, err := openSealed(a.secret, s["fn_username"])
	if err != nil {
		return nil, fmt.Errorf("无法解密飞牛用户名: %w", err)
	}
	pass, err := openSealed(a.secret, s["fn_password"])
	if err != nil {
		return nil, fmt.Errorf("无法解密飞牛密码: %w", err)
	}
	client, err := NewFnOSClient(server, username, pass, truth(s["fn_insecure_tls"]))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err = client.Login(ctx); err != nil {
		return nil, err
	}
	a.fnClient = client
	return client, nil
}

func (a *App) resetClient() {
	a.fnMu.Lock()
	a.fnClient = nil
	a.fnMu.Unlock()
}

func (a *App) setSync(update func(*syncState)) {
	a.syncMu.Lock()
	update(&a.syncState)
	a.syncMu.Unlock()
}

func (a *App) syncSnapshot() syncState {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	return a.syncState
}

func (a *App) startSync() bool {
	a.syncMu.Lock()
	if a.syncState.Running || a.restoring.Load() {
		a.syncMu.Unlock()
		return false
	}
	a.syncState = syncState{Running: true, StartedAt: nowISO(), Stage: "正在连接飞牛"}
	a.syncMu.Unlock()
	a.jobs.Add(1)
	go func() { defer a.jobs.Done(); a.runSync() }()
	return true
}

func (a *App) runSync() {
	finish := func(err error) {

		values := map[string]string{"last_sync_at": nowISO(), "last_sync_error": ""}
		if err != nil {
			values["last_sync_error"] = err.Error()
		} else {
			values["last_sync_success_at"] = nowISO()
		}
		a.updateSettings(values)
		a.setSync(func(state *syncState) {
			state.Running = false
			state.FinishedAt = nowISO()
			if err != nil {
				state.Error = err.Error()
				state.Stage = "同步失败"
			} else {
				state.Error = ""
				state.Stage = "同步完成"
			}
		})
	}
	client, err := a.configuredClient()
	if err != nil {
		finish(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	a.setSync(func(state *syncState) { state.Stage = "读取影片详情" })
	movies, err := client.Movies(ctx, func(done, total int) {
		a.setSync(func(state *syncState) { state.Done, state.Total = done, total })
	})
	if err != nil {
		finish(err)
		return
	}
	if len(movies) == 0 {
		finish(fmt.Errorf("飞牛返回了 0 部影片；为避免误删，现有索引未修改"))
		return
	}
	a.setSync(func(state *syncState) { state.Stage = "写入本地索引" })
	tx, err := a.db.Begin()
	if err != nil {
		finish(err)
		return
	}
	marker := nowISO()
	for _, movie := range movies {
		_, err = tx.Exec(`INSERT INTO movies(id,source_id,title,year,genres,countries,overview,cover_path,library_id,library,watched,duration,rating,updated_at,poster_sources,tmdb_id,douban_id)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET source_id=excluded.source_id,title=excluded.title,year=excluded.year,genres=excluded.genres,countries=excluded.countries,overview=excluded.overview,cover_path=excluded.cover_path,library_id=excluded.library_id,library=excluded.library,watched=excluded.watched,duration=excluded.duration,rating=excluded.rating,updated_at=excluded.updated_at,poster_sources=CASE WHEN excluded.poster_sources='[]' THEN movies.poster_sources ELSE excluded.poster_sources END,tmdb_id=excluded.tmdb_id,douban_id=excluded.douban_id,poster_checked_at=0`,
			movie.ID, movie.SourceID, movie.Title, movie.Year, jsonString(movie.Genres), jsonString(movie.Countries), movie.Overview, movie.CoverPath, movie.LibraryID, movie.Library, boolInt(movie.Watched), movie.Duration, movie.Rating, marker, jsonString(movie.PosterSources), movie.TMDBID, movie.DoubanID)
		if err != nil {
			tx.Rollback()
			finish(err)
			return
		}
	}
	_, err = tx.Exec("DELETE FROM movies WHERE updated_at<>?", marker)
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	finish(err)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func decodeMovie(row M) Movie {
	genres, countries := []string{}, []string{}
	posters := []string{}
	_ = json.Unmarshal([]byte(str(row["poster_sources"])), &posters)
	_ = json.Unmarshal([]byte(str(row["genres"])), &genres)
	_ = json.Unmarshal([]byte(str(row["countries"])), &countries)
	return Movie{
		ID: str(row["id"]), SourceID: str(row["source_id"]), Title: str(row["title"]), Year: str(row["year"]), Genres: genres,
		TMDBID: str(row["tmdb_id"]), DoubanID: str(row["douban_id"]), Countries: countries, PosterSources: posters, Overview: str(row["overview"]), CoverPath: str(row["cover_path"]), LibraryID: str(row["library_id"]),
		Library: str(row["library"]), Watched: num(row["watched"]) != 0, Duration: str(row["duration"]), Rating: str(row["rating"]),
	}
}

func (a *App) movieJSON(movie Movie) M {
	return M{
		"id": movie.ID, "title": movie.Title, "year": movie.Year, "genres": movie.Genres, "countries": movie.Countries,
		"overview": movie.Overview, "libraryId": movie.LibraryID, "library": movie.Library, "watched": movie.Watched,
		"duration": movie.Duration, "rating": movie.Rating, "posterUrl": "/api/posters/" + movie.ID + "?cache=only", "posterSources": orderedPosters(externalPosters(M{"posters": movie.PosterSources, "cover": movie.CoverPath}), a.settings()), "fallbackOrder": posterOrder(a.settings()),
	}
}

func parseFilters(value any) Filters {
	filters := Filters{}
	b, _ := json.Marshal(value)
	_ = json.Unmarshal(b, &filters)
	return filters
}

func movieMatches(movie Movie, filters Filters) bool {
	rating, err := strconv.ParseFloat(strings.TrimSpace(movie.Rating), 64)
	// Missing, invalid and zero ratings represent unrated titles, not poor reviews.
	if err != nil || math.IsNaN(rating) || math.IsInf(rating, 0) || rating <= 0 || rating > 10 {
		rating = 10
	}
	low, high := 0.0, 10.0
	if filters.RatingMin != nil {
		low = *filters.RatingMin
	}
	if filters.RatingMax != nil {
		high = *filters.RatingMax
	}
	if low > high {
		low, high = high, low
	}
	if rating < low || rating > high {
		return false
	}

	if len(filters.Libraries) > 0 {
		found := false
		for _, library := range filters.Libraries {
			found = found || library == movie.LibraryID || library == movie.Library
		}
		if !found {
			return false
		}
	}
	if filters.ExcludeWatched && movie.Watched {
		return false
	}
	if filters.Year != "" && filters.Year != movie.Year {
		return false
	}
	if filters.YearFrom != "" || filters.YearTo != "" {
		movieYear, err := strconv.Atoi(movie.Year)
		if err != nil {
			return false
		}
		from, to := intValue(filters.YearFrom, 0), intValue(filters.YearTo, 0)
		if from > 0 && to > 0 && from > to {
			from, to = to, from
		}
		if (from > 0 && movieYear < from) || (to > 0 && movieYear > to) {
			return false
		}
	}
	if filters.Genre != "" && !containsFold(movie.Genres, filters.Genre) {
		return false
	}
	if filters.Country != "" && filters.Country != "全部" {
		if filters.Country == "其他地区" {
			for group := range countryGroups {
				if countryMatches(movie.Countries, group) {
					return false
				}
			}
			return true
		}
		if !countryMatches(movie.Countries, filters.Country) {
			return false
		}
	}
	return true
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func countryMatches(values []string, group string) bool {
	wants := countryGroups[group]
	if len(wants) == 0 {
		return containsFold(values, group)
	}
	for _, value := range values {
		for _, want := range wants {
			if strings.EqualFold(strings.TrimSpace(value), want) {
				return true
			}
		}
	}
	return false
}

func (a *App) allMovies() []Movie {
	rows := a.rows("SELECT * FROM movies ORDER BY title COLLATE NOCASE")
	movies := make([]Movie, 0, len(rows))
	for _, row := range rows {
		movies = append(movies, decodeMovie(row))
	}
	return movies
}

func (a *App) candidates(filters Filters) []Movie {
	out := []Movie{}
	for _, movie := range a.allMovies() {
		if movieMatches(movie, filters) {
			out = append(out, movie)
		}
	}
	return out
}

func secureIndex(length int) int {
	if length <= 1 {
		return 0
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(length)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(length))
	}
	return int(n.Int64())
}

func (a *App) chooseMovie(filters Filters) (Movie, error) {
	candidates := a.candidates(filters)
	if len(candidates) == 0 {
		return Movie{}, fmt.Errorf("当前筛选条件下没有影片")
	}
	recent := map[string]bool{}
	for _, row := range a.rows("SELECT movie_id FROM random_history ORDER BY id DESC LIMIT 30") {
		recent[str(row["movie_id"])] = true
	}
	fresh := []Movie{}
	for _, movie := range candidates {
		if !recent[movie.ID] {
			fresh = append(fresh, movie)
		}
	}
	if len(fresh) > 0 {
		candidates = fresh
	}
	movie := candidates[secureIndex(len(candidates))]
	return movie, nil
}

func (a *App) pickMovie(filters Filters, source, scheduleID string) (Movie, error) {
	movie, err := a.chooseMovie(filters)
	if err != nil {
		return Movie{}, err
	}
	a.exec("INSERT INTO random_history(movie_id,source,schedule_id,created_at) VALUES(?,?,?,?)", movie.ID, source, scheduleID, nowISO())
	a.exec("DELETE FROM random_history WHERE id NOT IN (SELECT id FROM random_history ORDER BY id DESC LIMIT 1000)")
	return movie, nil
}

func (a *App) movieOptions(w http.ResponseWriter) {
	respond(w, a.movieOptionsData())
}

func (a *App) movieOptionsData() M {
	libraries := []M{}
	for _, row := range a.rows("SELECT library_id,library,COUNT(*) AS count FROM movies GROUP BY library_id,library ORDER BY library") {
		libraries = append(libraries, M{"id": row["library_id"], "name": row["library"], "count": row["count"]})
	}
	genres, years := map[string]bool{}, map[string]bool{}
	for _, movie := range a.allMovies() {
		for _, genre := range movie.Genres {
			genres[genre] = true
		}
		if movie.Year != "" {
			years[movie.Year] = true
		}
	}
	genreList, yearList := mapKeys(genres), mapKeys(years)
	sort.Strings(genreList)
	sort.Sort(sort.Reverse(sort.StringSlice(yearList)))
	return M{"libraries": libraries, "genres": genreList, "years": yearList, "countries": []string{"华语", "美国", "欧洲", "韩国", "日本", "印度", "其他地区"}}
}

func mapKeys(value map[string]bool) []string {
	out := make([]string, 0, len(value))
	for key := range value {
		out = append(out, key)
	}
	return out
}

func queryRating(value string) *float64 {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 10 {
		return nil
	}
	return &n
}
func filtersFromQuery(r *http.Request) Filters {
	q := r.URL.Query()
	return Filters{
		Libraries: q["library"], Genre: q.Get("genre"), Country: q.Get("country"), Year: q.Get("year"),
		RatingMin: queryRating(q.Get("ratingMin")), RatingMax: queryRating(q.Get("ratingMax")), YearFrom: q.Get("yearFrom"), YearTo: q.Get("yearTo"), ExcludeWatched: truth(q.Get("excludeWatched")),
	}
}

func (a *App) listMovies(w http.ResponseWriter, r *http.Request) {
	items := a.candidates(filtersFromQuery(r))
	query := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	if query != "" {
		filtered := items[:0]
		for _, movie := range items {
			if strings.Contains(strings.ToLower(movie.Title), query) {
				filtered = append(filtered, movie)
			}
		}
		items = filtered
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	start := min(len(items), (page-1)*pageSize)
	end := min(len(items), start+pageSize)
	out := []M{}
	for _, movie := range items[start:end] {
		out = append(out, a.movieJSON(movie))
	}
	respond(w, M{"items": out, "total": len(items), "page": page, "pageSize": pageSize})
}

func (a *App) randomMovieRoute(w http.ResponseWriter, r *http.Request) {
	v := readBody(r)
	filters := a.defaultFilters()
	if v["filters"] != nil {
		filters = parseFilters(v["filters"])
	}
	movie, err := a.pickMovie(filters, "manual", "")
	if err != nil {
		fail(404, err.Error())
	}
	respond(w, M{"movie": a.movieJSON(movie)})
}

func (a *App) playRoute(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	row := a.one("SELECT * FROM movies WHERE id=?", id)
	if row == nil {
		fail(404, "影片不存在")
	}
	client, err := a.configuredClient()
	if err != nil {
		fail(503, err.Error())
	}
	respond(w, M{"url": client.PlayURL(str(row["source_id"]))})
}

func (a *App) posterRoute(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/posters/")
	if id == "" || strings.ContainsAny(id, "/\\.") {
		fail(404, "封面不存在")
	}
	if a.one("SELECT id FROM movies WHERE id=?", id) == nil {
		fail(404, "封面不存在")
	}
	cachePath := filepath.Join(a.dataDir, "posters", id)
	file, err := os.Open(cachePath)
	if err != nil {
		if r.URL.Query().Get("strict") == "1" {
			fail(404, "没有已有封面缓存")
		}
		servePlaceholder(w, id)
		return
	}
	defer file.Close()
	info, _ := file.Stat()
	head := make([]byte, 512)
	n, _ := file.Read(head)
	_, _ = file.Seek(0, io.SeekStart)
	w.Header().Set("Content-Type", http.DetectContentType(head[:n]))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("ETag", `"`+id+`-`+strconv.FormatInt(info.ModTime().Unix(), 10)+`"`)
	http.ServeContent(w, r, id, info.ModTime(), file)
}

func servePlaceholder(w http.ResponseWriter, seed string) {
	colors := []string{"#4f8f74", "#695b8f", "#9a6b5b", "#506d8f", "#8a7a4f"}
	color := colors[int(shaHex(seed)[0])%len(colors)]
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 600"><rect width="400" height="600" fill="` + color + `"/><circle cx="200" cy="245" r="70" fill="rgba(255,255,255,.12)"/><path d="M180 205l70 40-70 40z" fill="rgba(255,255,255,.8)"/><text x="200" y="410" fill="white" font-family="sans-serif" font-size="25" text-anchor="middle">今日想看</text></svg>`
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = io.WriteString(w, svg)
}

func scanSchedule(row M) M {
	filters := M{}
	_ = json.Unmarshal([]byte(str(row["filters_json"])), &filters)
	if year := str(filters["year"]); year != "" && str(filters["yearFrom"]) == "" && str(filters["yearTo"]) == "" {
		filters["yearFrom"], filters["yearTo"] = year, year
	}
	delete(filters, "year")
	return M{"id": row["id"], "name": row["name"], "enabled": num(row["enabled"]) != 0, "kind": row["kind"], "time": row["time_of_day"], "timezone": row["timezone"], "weekday": row["weekday"], "dayOfMonth": row["day_of_month"], "filters": filters, "lastRunAt": row["last_run_at"], "notifyChannel": fallback(str(row["notify_channel"]), "webhook")}
}

var _ sql.Result
