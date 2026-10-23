package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const tmdbAPIBase = "https://api.themoviedb.org/3"
const tmdbImageBase = "https://image.tmdb.org/t/p/w500"

var tmdbGenres = map[int]string{
	28: "动作", 12: "冒险", 16: "动画", 35: "喜剧", 80: "犯罪", 99: "纪录片", 18: "剧情",
	10751: "家庭", 14: "奇幻", 36: "历史", 27: "恐怖", 10402: "音乐", 9648: "悬疑",
	10749: "爱情", 878: "科幻", 10770: "电视电影", 53: "惊悚", 10752: "战争", 37: "西部",
}

type TMDBClient struct {
	token    string
	language string
	client   *http.Client
}

type tmdbSearchResponse struct {
	Results []tmdbMovie `json:"results"`
}

type tmdbMovie struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Overview      string  `json:"overview"`
	ReleaseDate   string  `json:"release_date"`
	PosterPath    string  `json:"poster_path"`
	GenreIDs      []int   `json:"genre_ids"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int     `json:"vote_count"`
}

func NewTMDBClient(token, language string) (*TMDBClient, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("尚未配置 TMDB API Read Access Token")
	}
	language = strings.TrimSpace(language)
	if language == "" {
		language = "zh-CN"
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          12,
		MaxIdleConnsPerHost:   6,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
	}
	return &TMDBClient{token: token, language: language, client: &http.Client{Timeout: 20 * time.Second, Transport: transport}}, nil
}

func (a *App) configuredTMDB() (*TMDBClient, error) {
	settings := a.settings()
	token, err := openSealed(a.secret, settings["tmdb_token"])
	if err != nil {
		return nil, fmt.Errorf("无法解密 TMDB Token: %w", err)
	}
	return NewTMDBClient(token, settings["tmdb_language"])
}

func (c *TMDBClient) get(ctx context.Context, route string, query url.Values, target any) error {
	rawURL := tmdbAPIBase + route
	if encoded := query.Encode(); encoded != "" {
		rawURL += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fnUserAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("TMDB 接口 HTTP %d: %s", resp.StatusCode, truncate(string(data), 180))
	}
	if err = json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("TMDB 返回格式无效: %w", err)
	}
	return nil
}

func (c *TMDBClient) Test(ctx context.Context) error {
	var result map[string]any
	return c.get(ctx, "/configuration", nil, &result)
}

var titleNoise = regexp.MustCompile(`(?i)[\s\p{P}\p{S}]+`)

func normalizedTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = titleNoise.ReplaceAllString(value, "")
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return -1
	}, value)
}

func releaseYear(value string) string {
	if len(value) >= 4 {
		if _, err := strconv.Atoi(value[:4]); err == nil {
			return value[:4]
		}
	}
	return ""
}

func (c *TMDBClient) Search(ctx context.Context, title, year string) (tmdbMovie, error) {
	query := url.Values{"query": {title}, "language": {c.language}, "include_adult": {"false"}}
	if len(year) == 4 {
		query.Set("year", year)
	}
	result := tmdbSearchResponse{}
	if err := c.get(ctx, "/search/movie", query, &result); err != nil {
		return tmdbMovie{}, err
	}
	want := normalizedTitle(title)
	best, bestScore := tmdbMovie{}, -1
	for _, candidate := range result.Results {
		score := 0
		if normalizedTitle(candidate.Title) == want || normalizedTitle(candidate.OriginalTitle) == want {
			score += 7
		} else if strings.Contains(normalizedTitle(candidate.Title), want) || strings.Contains(want, normalizedTitle(candidate.Title)) {
			score += 3
		}
		if year != "" && releaseYear(candidate.ReleaseDate) == year {
			score += 4
		} else if year == "" {
			score++
		}
		if candidate.PosterPath != "" {
			score++
		}
		if score > bestScore || (score == bestScore && candidate.VoteCount > best.VoteCount) {
			best, bestScore = candidate, score
		}
	}
	if best.ID == 0 || bestScore < 4 {
		return tmdbMovie{}, fmt.Errorf("TMDB 未找到可信匹配")
	}
	return best, nil
}

func applyTMDB(movie *Movie, metadata tmdbMovie) {
	movie.TMDBID = strconv.Itoa(metadata.ID)
	movie.TMDBPosterPath = metadata.PosterPath
	movie.MetadataSource = "tmdb"
	if metadata.Overview != "" {
		movie.Overview = metadata.Overview
	}
	if movie.Year == "" {
		movie.Year = releaseYear(metadata.ReleaseDate)
	}
	genres := make([]string, 0, len(metadata.GenreIDs))
	for _, id := range metadata.GenreIDs {
		if name := tmdbGenres[id]; name != "" {
			genres = append(genres, name)
		}
	}
	if len(genres) > 0 {
		movie.Genres = genres
	}
	if metadata.VoteAverage > 0 {
		movie.Rating = strconv.FormatFloat(metadata.VoteAverage, 'f', 1, 64)
	}
}

func (c *TMDBClient) Enrich(ctx context.Context, movies []Movie, progress func(done, total int)) (int, error) {
	type result struct {
		index    int
		metadata tmdbMovie
		err      error
	}
	jobs := make(chan int)
	results := make(chan result)
	workers := min(4, max(1, len(movies)))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if movies[index].TMDBID != "" && movies[index].TMDBPosterPath != "" {
					results <- result{index: index}
					continue
				}
				metadata, err := c.Search(ctx, movies[index].Title, movies[index].Year)
				results <- result{index: index, metadata: metadata, err: err}
			}
		}()
	}
	go func() {
		for index := range movies {
			jobs <- index
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	matched, failed, done := 0, 0, 0
	for item := range results {
		done++
		if item.err == nil && item.metadata.ID != 0 {
			applyTMDB(&movies[item.index], item.metadata)
			matched++
		} else if movies[item.index].TMDBID == "" {
			failed++
		}
		progress(done, len(movies))
	}
	if failed > 0 && matched == 0 {
		return matched, fmt.Errorf("TMDB 未匹配任何影片（%d 个失败或无结果）", failed)
	}
	return matched, nil
}

func fetchTMDBPoster(ctx context.Context, posterPath string) (*http.Response, error) {
	if !strings.HasPrefix(posterPath, "/") || strings.Contains(posterPath, "..") {
		return nil, fmt.Errorf("TMDB 封面路径无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tmdbImageBase+posterPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/*,*/*;q=0.8")
	req.Header.Set("User-Agent", fnUserAgent)
	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}
