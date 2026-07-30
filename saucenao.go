package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"time"
)

type SauceNaoResult struct {
	Similarity string
	Title      string
	Author     string
	URL        string
	Thumbnail  string
}

var searchCache = make(map[string][]SauceNaoResult)

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

	url := "https://saucenao.com/search.php?output_type=2&numres=6&db=999&api_key=" + apiKey

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "image.jpg")
	part.Write(imageBytes)
	writer.Close()

	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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
