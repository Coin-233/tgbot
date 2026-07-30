package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

type SauceNaoResult struct {
	Similarity string
	Title      string
	Author     string
	URL        string
	Thumbnail  string
}

const (
	searchCacheTTL     = 30 * time.Minute
	sauceImageMaxBytes = 20 * 1024 * 1024
)

type cachedSearch struct {
	results   []SauceNaoResult
	expiresAt time.Time
}

var searchCache = struct {
	sync.Mutex
	entries map[string]cachedSearch
}{entries: make(map[string]cachedSearch)}

func cacheSearchResults(id string, results []SauceNaoResult) {
	now := time.Now()
	searchCache.Lock()
	defer searchCache.Unlock()
	for key, cached := range searchCache.entries {
		if now.After(cached.expiresAt) {
			delete(searchCache.entries, key)
		}
	}
	searchCache.entries[id] = cachedSearch{results: results, expiresAt: now.Add(searchCacheTTL)}
}

func getCachedSearchResults(id string) ([]SauceNaoResult, bool) {
	searchCache.Lock()
	defer searchCache.Unlock()
	cached, ok := searchCache.entries[id]
	if !ok {
		return nil, false
	}
	if time.Now().After(cached.expiresAt) {
		delete(searchCache.entries, id)
		return nil, false
	}
	return cached.results, true
}

// 提取各类图库可能存在的标题和作者字段
func getFlexibleString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch val := v.(type) {
			case string:
				if val != "" {
					return val
				}
			case []interface{}:
				if len(val) > 0 {
					if s, ok := val[0].(string); ok && s != "" {
						return s
					}
				}
			}
		}
	}
	return ""
}

func searchSauceNAO(imageBytes []byte) ([]SauceNaoResult, error) {
	apiKey := os.Getenv("STOKEN")
	if apiKey == "" {
		return nil, fmt.Errorf("未配置 STOKEN")
	}

	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("图片内容为空")
	}
	if len(imageBytes) > sauceImageMaxBytes {
		return nil, fmt.Errorf("图片超过 20MB 限制")
	}

	endpoint := "https://saucenao.com/search.php?output_type=2&numres=6&db=999&api_key=" + url.QueryEscape(apiKey)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "image.jpg")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(imageBytes); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := slowHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SauceNAO HTTP status %d", resp.StatusCode)
	}

	var data struct {
		Results []struct {
			Header struct {
				Similarity string `json:"similarity"`
				Thumbnail  string `json:"thumbnail"`
			} `json:"header"`
			Data map[string]interface{} `json:"data"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var parsedResults []SauceNaoResult
	for _, res := range data.Results {
		sim, _ := strconv.ParseFloat(res.Header.Similarity, 64)
		if sim < 50 {
			continue // 忽略相似度低于 50% 的结果
		}

		// 按照优先级依次寻找标题字段
		title := getFlexibleString(res.Data, "title", "jp_name", "eng_name", "source")
		if title == "" {
			title = "未知"
		}

		// 按照优先级依次寻找作者字段
		author := getFlexibleString(res.Data, "member_name", "author_name", "creator")
		if author == "" {
			author = "未知"
		}

		var sourceURL string
		if extUrls, ok := res.Data["ext_urls"].([]interface{}); ok && len(extUrls) > 0 {
			if s, ok := extUrls[0].(string); ok {
				sourceURL = s
			}
		}

		parsedResults = append(parsedResults, SauceNaoResult{
			Similarity: res.Header.Similarity,
			Title:      title,
			Author:     author,
			URL:        sourceURL,
			Thumbnail:  res.Header.Thumbnail,
		})
	}

	if len(parsedResults) == 0 {
		return nil, fmt.Errorf("QAQ 未找到相似图片")
	}

	return parsedResults, nil
}
