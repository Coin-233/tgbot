package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	twitterPattern = regexp.MustCompile(`(?:https?://)?(?:www\.)?(?:twitter|x)\.com/\w+/status/\d+`)
	imgURLPattern  = regexp.MustCompile(`https?://pbs\.twimg\.com/media/([^.?]+)`)
)

func MatchTwitterURL(text string) bool {
	return twitterPattern.MatchString(text)
}

func FetchTweetData(urlStr string, forceOriginalFileOnly bool) ([]string, string) {
	re := regexp.MustCompile(`(?:twitter|x)\.com/([^/]+)/status/(\d+)`)
	matches := re.FindStringSubmatch(urlStr)
	if len(matches) < 3 {
		return nil, ""
	}
	user, tweetID := matches[1], matches[2]

	apiURL := fmt.Sprintf("https://api.fxtwitter.com/%s/status/%s", user, tweetID)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil || resp.StatusCode != 200 {
		return nil, ""
	}
	defer resp.Body.Close()

	var data struct {
		Tweet struct {
			Text  string `json:"text"`
			Media struct {
				All []struct {
					URL  string `json:"url"`
					Type string `json:"type"`
				} `json:"all"`
			} `json:"media"`
		} `json:"tweet"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, ""
	}

	text := strings.TrimRight(data.Tweet.Text, " \t\n\r")
	var mediaFiles []string

	for _, m := range data.Tweet.Media.All {
		if m.URL == "" || m.Type == "" {
			continue
		}

		if m.Type == "photo" {
			match := imgURLPattern.FindStringSubmatch(m.URL)
			if len(match) > 1 {
				filename := match[1]
				// 默认使用原图质量
				jpgURL := fmt.Sprintf("https://pbs.twimg.com/media/%s.jpg?name=orig", filename)
				mediaFiles = append(mediaFiles, jpgURL)
			} else {
				mediaFiles = append(mediaFiles, m.URL)
			}
		} else if m.Type == "video" || m.Type == "gif" {
			mediaFiles = append(mediaFiles, m.URL)
		}
	}

	return mediaFiles, text
}
