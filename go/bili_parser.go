package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var bilibiliPattern = regexp.MustCompile(`(?:https?://)?(?:t\.bilibili\.com|(?:www\.)?bilibili\.com/opus)/(\d+)`)

func MatchBilibiliURL(text string) bool {
	return bilibiliPattern.MatchString(text)
}

func FetchBilibiliData(urlStr string) ([]string, string, string) {
	re := regexp.MustCompile(`(?:https?://)?(?:t\.bilibili\.com|(?:www\.)?bilibili\.com/opus)/(\d+)(.*)`)
	matches := re.FindStringSubmatch(urlStr)
	if len(matches) < 3 {
		return nil, "", "normal"
	}

	dynamicID := matches[1]
	paramsStr := strings.TrimSpace(matches[2])
	params := strings.Fields(paramsStr)

	parseMode := "normal"
	for _, p := range params {
		if p == "-o" {
			parseMode = "file_only"
		} else if p == "-O" {
			parseMode = "file_with_info"
		}
	}

	apiURL := fmt.Sprintf("https://api.bilibili.com/x/polymer/web-dynamic/v1/detail?id=%s&features=itemOpusStyle", dynamicID)
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", fmt.Sprintf("https://t.bilibili.com/%s", dynamicID))

	if sess := os.Getenv("SESSDATA"); sess != "" {
		req.AddCookie(&http.Cookie{Name: "SESSDATA", Value: strings.TrimSpace(sess)})
	}
	if jct := os.Getenv("bili_jct"); jct != "" {
		req.AddCookie(&http.Cookie{Name: "bili_jct", Value: strings.TrimSpace(jct)})
	}
	if uid := os.Getenv("DedeUserID"); uid != "" {
		req.AddCookie(&http.Cookie{Name: "DedeUserID", Value: strings.TrimSpace(uid)})
	}
	if uidMd5 := os.Getenv("DedeUserID__ckMd5"); uidMd5 != "" {
		req.AddCookie(&http.Cookie{Name: "DedeUserID__ckMd5", Value: strings.TrimSpace(uidMd5)})
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil, "", "normal"
	}
	defer resp.Body.Close()

	var data struct {
		Code int `json:"code"`
		Data struct {
			Item struct {
				Modules struct {
					Author struct {
						Name    string `json:"name"`
						PubTime string `json:"pub_time"`
					} `json:"module_author"`
					Dynamic struct {
						Major struct {
							Type string `json:"type"`
							Opus struct {
								Summary struct {
									Text string `json:"text"`
								} `json:"summary"`
								Pics []struct {
									URL string `json:"url"`
								} `json:"pics"`
							} `json:"opus"`
							Draw struct {
								Items []struct {
									Src string `json:"src"`
								} `json:"items"`
							} `json:"draw"`
						} `json:"major"`
						Desc struct {
							Text string `json:"text"`
						} `json:"desc"`
					} `json:"module_dynamic"`
				} `json:"modules"`
			} `json:"item"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Code != 0 {
		return nil, "", "normal"
	}

	modules := data.Data.Item.Modules
	authorLine := ""
	if modules.Author.Name != "" {
		authorLine = fmt.Sprintf("*%s* 发布于 %s", escapeMDV2(modules.Author.Name), escapeMDV2(modules.Author.PubTime))
	}

	var images []string
	textContent := ""
	majorType := modules.Dynamic.Major.Type

	if majorType == "MAJOR_TYPE_OPUS" {
		textContent = htmlToMarkdownV2(modules.Dynamic.Major.Opus.Summary.Text)
		for _, pic := range modules.Dynamic.Major.Opus.Pics {
			imgURL := strings.Replace(pic.URL, "http://", "https://", 1)
			images = append(images, imgURL)
		}
	} else if majorType == "MAJOR_TYPE_DRAW" {
		for _, img := range modules.Dynamic.Major.Draw.Items {
			imgURL := strings.Replace(img.Src, "http://", "https://", 1)
			images = append(images, imgURL)
		}
		if modules.Dynamic.Desc.Text != "" {
			textContent = htmlToMarkdownV2(modules.Dynamic.Desc.Text)
		}
	}

	var parts []string
	if authorLine != "" {
		parts = append(parts, authorLine)
	}
	if textContent != "" {
		parts = append(parts, textContent)
	}

	text := strings.Join(parts, "\n")
	return images, strings.TrimSpace(text), parseMode
}
