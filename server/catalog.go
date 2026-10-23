package main

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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
	Libraries      []string `json:"libraries"`
	Genre          string   `json:"genre"`
	Country        string   `json:"country"`
	Year           string   `json:"year"`
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
	if a.syncState.Running {
		a.syncMu.Unlock()
		return false
	}
	a.syncState = syncState{Running: true, StartedAt: nowISO(), Stage: "正在连接飞牛"}
	a.syncMu.Unlock()
	go a.runSync()
	return true
}

func (a *App) runSync() {
	finish := func(err error) {
		a.setSync(func(state *syncState) {
			state.Running = false
			state.FinishedAt = nowISO()
			if err != nil {
				state.Error = err.Error()
				state.Stage = "同步失败"
			} else {
				state.Stage = "同步完成"
			}
		})
		values := map[string]string{"last_sync_at": nowISO(), "last_sync_error": ""}
		if err != nil {
			values["last_sync_error"] = err.Error()
		}
		a.updateSettings(values)
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
	settings := a.settings()
	if settings["metadata_source"] != "fnos" && settings["tmdb_token"] != "" {
		existing := map[string]Movie{}
		for _, movie := range a.allMovies() {
			existing[movie.ID] = movie
		}
		for index := range movies {
			if previous, ok := existing[movies[index].ID]; ok {
				movies[index].TMDBID = previous.TMDBID
				movies[index].TMDBPosterPath = previous.TMDBPosterPath
				if previous.TMDBID != "" {
					movies[index].MetadataSource = "tmdb"
					movies[index].Overview = previous.Overview
					movies[index].Genres = previous.Genres
					movies[index].Rating = previous.Rating
					if movies[index].Year == "" {
						movies[index].Year = previous.Year
					}
				}
			}
		}
		token, decryptErr := openSealed(a.secret, settings["tmdb_token"])
		if decryptErr == nil {
			if tmdb, clientErr := NewTMDBClient(token, settings["tmdb_language"]); clientErr == nil {
				a.setSync(func(state *syncState) {
					state.Stage = "匹配 TMDB 元数据"
					state.Done, state.Total = 0, len(movies)
				})
				_, _ = tmdb.Enrich(ctx, movies, func(done, total int) {
					a.setSync(func(state *syncState) { state.Done, state.Total = done, total })
				})
			}
		}
	}
	a.setSync(func(state *syncState) { state.Stage = "写入本地索引" })
	tx, err := a.db.Begin()
	if err != nil {
		finish(err)
		return
	}
	marker := nowISO()
	for _, movie := range movies {
		_, err = tx.Exec(`INSERT INTO movies(id,source_id,title,year,genres,countries,overview,cover_path,tmdb_id,tmdb_poster_path,metadata_source,library_id,library,watched,duration,rating,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET source_id=excluded.source_id,title=excluded.title,year=excluded.year,genres=excluded.genres,countries=excluded.countries,overview=excluded.overview,cover_path=excluded.cover_path,tmdb_id=excluded.tmdb_id,tmdb_poster_path=excluded.tmdb_poster_path,metadata_source=excluded.metadata_source,library_id=excluded.library_id,library=excluded.library,watched=excluded.watched,duration=excluded.duration,rating=excluded.rating,updated_at=excluded.updated_at`,
			movie.ID, movie.SourceID, movie.Title, movie.Year, jsonString(movie.Genres), jsonString(movie.Countries), movie.Overview, movie.CoverPath, movie.TMDBID, movie.TMDBPosterPath, fallback(movie.MetadataSource, "fnos"), movie.LibraryID, movie.Library, boolInt(movie.Watched), movie.Duration, movie.Rating, marker)
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
	_ = json.Unmarshal([]byte(str(row["genres"])), &genres)
	_ = json.Unmarshal([]byte(str(row["countries"])), &countries)
	return Movie{
		ID: str(row["id"]), SourceID: str(row["source_id"]), Title: str(row["title"]), Year: str(row["year"]), Genres: genres,
		Countries: countries, Overview: str(row["overview"]), CoverPath: str(row["cover_path"]), LibraryID: str(row["library_id"]),
		TMDBID: str(row["tmdb_id"]), TMDBPosterPath: str(row["tmdb_poster_path"]), MetadataSource: fallback(str(row["metadata_source"]), "fnos"),
		Library: str(row["library"]), Watched: num(row["watched"]) != 0, Duration: str(row["duration"]), Rating: str(row["rating"]),
	}
}

func (a *App) movieJSON(movie Movie) M {
	return M{
		"id": movie.ID, "title": movie.Title, "year": movie.Year, "genres": movie.Genres, "countries": movie.Countries,
		"overview": movie.Overview, "libraryId": movie.LibraryID, "library": movie.Library, "watched": movie.Watched,
		"duration": movie.Duration, "rating": movie.Rating, "metadataSource": movie.MetadataSource, "posterUrl": "/api/posters/" + movie.ID,
	}
}

func parseFilters(value any) Filters {
	filters := Filters{}
	b, _ := json.Marshal(value)
	_ = json.Unmarshal(b, &filters)
	return filters
}

func movieMatches(movie Movie, filters Filters) bool {
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

func (a *App) pickMovie(filters Filters, source, scheduleID string) (Movie, error) {
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
	a.exec("INSERT INTO random_history(movie_id,source,schedule_id,created_at) VALUES(?,?,?,?)", movie.ID, source, scheduleID, nowISO())
	a.exec("DELETE FROM random_history WHERE id NOT IN (SELECT id FROM random_history ORDER BY id DESC LIMIT 1000)")
	return movie, nil
}

func (a *App) movieOptions(w http.ResponseWriter) {
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
	respond(w, M{"libraries": libraries, "genres": genreList, "years": yearList, "countries": []string{"华语", "美国", "欧洲", "韩国", "日本", "印度", "其他地区"}})
}

func mapKeys(value map[string]bool) []string {
	out := make([]string, 0, len(value))
	for key := range value {
		out = append(out, key)
	}
	return out
}

func filtersFromQuery(r *http.Request) Filters {
	q := r.URL.Query()
	return Filters{Libraries: q["library"], Genre: q.Get("genre"), Country: q.Get("country"), Year: q.Get("year"), ExcludeWatched: truth(q.Get("excludeWatched"))}
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
	movie, err := a.pickMovie(parseFilters(v["filters"]), "manual", "")
	if err != nil {
		fail(404, err.Error())
	}
	respond(w, M{"movie": a.movieJSON(movie)})
}

func (a *App) playRoute(w http.ResponseWriter, r *http.Request) {
	if !truth(a.getSetting("public_play", "0")) {
		a.require(r)
	}
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
	row := a.one("SELECT cover_path,tmdb_poster_path,updated_at FROM movies WHERE id=?", id)
	if row == nil || (str(row["cover_path"]) == "" && str(row["tmdb_poster_path"]) == "") {
		servePlaceholder(w, id)
		return
	}
	cachePath := filepath.Join(a.dataDir, "posters", id)
	if _, err := os.Stat(cachePath); err != nil {
		a.posterMu.Lock()
		if _, secondErr := os.Stat(cachePath); secondErr != nil {
			err = a.fetchPoster(cachePath, str(row["tmdb_poster_path"]), str(row["cover_path"]))
		}
		a.posterMu.Unlock()
		if err != nil {
			servePlaceholder(w, id)
			return
		}
	}
	file, err := os.Open(cachePath)
	if err != nil {
		servePlaceholder(w, id)
		return
	}
	defer file.Close()
	info, _ := file.Stat()
	head := make([]byte, 512)
	n, _ := file.Read(head)
	_, _ = file.Seek(0, io.SeekStart)
	w.Header().Set("Content-Type", http.DetectContentType(head[:n]))
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	w.Header().Set("ETag", `"`+id+`-`+strconv.FormatInt(info.ModTime().Unix(), 10)+`"`)
	http.ServeContent(w, r, id, info.ModTime(), file)
}

func (a *App) fetchPoster(target, tmdbPath, fnReference string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	var resp *http.Response
	var err error
	if tmdbPath != "" {
		resp, err = fetchTMDBPoster(ctx, tmdbPath)
	}
	if err != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		if fnReference == "" {
			if err != nil {
				return err
			}
			return fmt.Errorf("TMDB 封面接口不可用")
		}
		client, clientErr := a.configuredClient()
		if clientErr != nil {
			return clientErr
		}
		resp, err = client.FetchCover(ctx, fnReference)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("封面接口 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > 15<<20 {
		return fmt.Errorf("封面超过 15 MiB")
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "poster-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	written, err := io.Copy(tmp, io.LimitReader(resp.Body, (15<<20)+1))
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if written > 15<<20 {
		return fmt.Errorf("封面超过 15 MiB")
	}
	return os.Rename(tmpName, target)
}

func servePlaceholder(w http.ResponseWriter, seed string) {
	colors := []string{"#4f8f74", "#695b8f", "#9a6b5b", "#506d8f", "#8a7a4f"}
	color := colors[int(shaHex(seed)[0])%len(colors)]
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 600"><rect width="400" height="600" fill="` + color + `"/><circle cx="200" cy="245" r="70" fill="rgba(255,255,255,.12)"/><path d="M180 205l70 40-70 40z" fill="rgba(255,255,255,.8)"/><text x="200" y="410" fill="white" font-family="sans-serif" font-size="25" text-anchor="middle">今日想看</text></svg>`
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.WriteString(w, svg)
}

func scanSchedule(row M) M {
	filters := M{}
	_ = json.Unmarshal([]byte(str(row["filters_json"])), &filters)
	return M{"id": row["id"], "name": row["name"], "enabled": num(row["enabled"]) != 0, "kind": row["kind"], "time": row["time_of_day"], "timezone": row["timezone"], "weekday": row["weekday"], "dayOfMonth": row["day_of_month"], "filters": filters, "lastRunAt": row["last_run_at"]}
}

var _ sql.Result
