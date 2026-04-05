package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var kemonoPostPattern = regexp.MustCompile(`(?:https?://)?(?:www\.)?kemono\.cr/([^/]+)/user/([^/]+)/post/([^/\s]+)`)

func MatchKemonoPostURL(text string) bool {
	return kemonoPostPattern.MatchString(text)
}

func FetchKemonoPostData(urlStr string) ([]string, string, string) {
	matches := kemonoPostPattern.FindStringSubmatch(urlStr)
	if len(matches) < 4 {
		return nil, "", "normal"
	}

	service, userID, postID := matches[1], matches[2], matches[3]

	re := regexp.MustCompile(`(?:kemono\.cr/[^/]+/user/[^/]+/post/[^/\s]+)(.*)`)
	paramMatch := re.FindStringSubmatch(urlStr)
	paramsStr := ""
	if len(paramMatch) > 1 {
		paramsStr = strings.TrimSpace(paramMatch[1])
	}
	params := strings.Fields(paramsStr)

	parseMode := "normal"
	for _, p := range params {
		if p == "-o" {
			parseMode = "file_only"
		} else if p == "-O" {
			parseMode = "file_with_info"
		}
	}

	apiURL := fmt.Sprintf("https://kemono.cr/api/v1/%s/user/%s/post/%s", service, userID, postID)

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Accept", "text/css")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil, "", "normal"
	}
	defer resp.Body.Close()

	var data struct {
		Post struct {
			Title string `json:"title"`
			File  struct {
				Path string `json:"path"`
			} `json:"file"`
			Attachments []struct {
				Path string `json:"path"`
			} `json:"attachments"`
		} `json:"post"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "", "normal"
	}

	var images []string

	if len(data.Post.Attachments) > 0 {
		for _, att := range data.Post.Attachments {
			if att.Path != "" {
				images = append(images, "https://kemono.cr/data"+att.Path)
			}
		}
	} else {
		if data.Post.File.Path != "" {
			images = append(images, "https://kemono.cr/data"+data.Post.File.Path)
		}
	}

	title := strings.TrimSpace(data.Post.Title)
	caption := ""
	if title != "" {
		caption = escapeMDV2(title)
	}

	return images, caption, parseMode
}
