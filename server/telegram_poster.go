package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Telegram fetches public images itself; no FnOS image download is performed.
func (a *App) sendMovieBotPayload(ctx context.Context, token string, payload M) error {
	id := str(payload["_poster_movie"])
	delete(payload, "_poster_movie")
	if id != "" && a.one("SELECT id FROM movies WHERE id=?", id) != nil {
		lookup, cancel := context.WithTimeout(ctx, 10*time.Second)
		result := a.resolvePosters(lookup, id)
		cancel()
		sources, _ := result["posterSources"].([]string)
		for _, provider := range posterOrder(a.settings()) {
			if provider == "placeholder" {
				break
			}
			if provider == "cache" {
				if a.sendCachedBotPhoto(ctx, token, id, payload) == nil {
					return nil
				}
				continue
			}
			if provider == "douying" {
				lookup, stop := context.WithTimeout(ctx, 3*time.Second)
				fallback := a.resolveDouyingFallback(lookup, id)
				stop()
				sources, _ = fallback["posterSources"].([]string)
			}
			for _, source := range sources {
				parsed, e := url.Parse(source)
				if e != nil {
					continue
				}
				kind := "douying"
				if parsed.Hostname() == "image.tmdb.org" {
					kind = "tmdb"
				} else if strings.HasSuffix(parsed.Hostname(), ".doubanio.com") {
					kind = "douban"
				}
				if kind != provider {
					continue
				}
				photo := M{"chat_id": payload["chat_id"], "photo": source, "caption": truncate(str(payload["text"]), 900), "reply_markup": payload["reply_markup"]}
				attempt, stop := context.WithTimeout(ctx, 3*time.Second)
				err := a.botCall(attempt, token, "sendPhoto", photo, nil)
				stop()
				if err == nil {
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
		}
	}
	return a.botCall(ctx, token, "sendMessage", payload, nil)
}

func (a *App) sendCachedBotPhoto(ctx context.Context, token, id string, payload M) error {
	if strings.ContainsAny(id, "/\\.") {
		return movieBotError{}
	}
	name := filepath.Join(a.dataDir, "posters", id)
	info, e := os.Lstat(name)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return movieBotError{}
	}
	file, e := os.Open(name)
	if e != nil {
		return movieBotError{}
	}
	defer file.Close()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("chat_id", str(payload["chat_id"]))
	form.WriteField("caption", truncate(str(payload["text"]), 900))
	markup, _ := json.Marshal(payload["reply_markup"])
	form.WriteField("reply_markup", string(markup))
	photo, e := form.CreateFormFile("photo", "poster.jpg")
	if e != nil {
		return movieBotError{}
	}
	if _, e = io.Copy(photo, io.LimitReader(file, (8<<20)+1)); e != nil {
		return movieBotError{}
	}
	if form.Close() != nil {
		return movieBotError{}
	}
	attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(attempt, "POST", a.tgBase+"/bot"+token+"/sendPhoto", &body)
	if e != nil {
		return movieBotError{}
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	response, e := a.tgHTTP.Do(req)
	if e != nil {
		return movieBotError{}
	}
	defer response.Body.Close()
	var result struct {
		OK bool `json:"ok"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || !result.OK || response.StatusCode != 200 {
		return movieBotError{}
	}
	return nil
}
