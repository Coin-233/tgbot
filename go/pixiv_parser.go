package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func MatchPixivURL(text string) bool {
	match, _ := regexp.MatchString(`pixiv\.net/.*(?:artworks/|illust_id=)(\d+)`, text)
	return match
}

func parsePageSelection(selectionRaw string, totalPages int) []int {
	if selectionRaw == "" {
		return nil
	}
	selectedMap := make(map[int]bool)
	parts := strings.Split(selectionRaw, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err1 := strconv.Atoi(bounds[0])
			end, err2 := strconv.Atoi(bounds[1])
			if err1 == nil && err2 == nil {
				if start > end {
					start, end = end, start
				}
				for i := start; i <= end; i++ {
					selectedMap[i] = true
				}
			}
		} else if val, err := strconv.Atoi(part); err == nil {
			selectedMap[val] = true
		}
	}

	var selected []int
	for k := range selectedMap {
		if k >= 1 && k <= totalPages {
			selected = append(selected, k)
		}
	}
	sort.Ints(selected)
	return selected
}

func FetchPixivData(urlStr string, forceOriginal bool) ([]string, string, string) {
	re := regexp.MustCompile(`(?:artworks/|illust_id=)(\d+)(.*)`)
	matches := re.FindStringSubmatch(urlStr)
	if len(matches) < 3 {
		return nil, "", "normal"
	}

	illustID := matches[1]
	paramsStr := strings.TrimSpace(matches[2])
	params := strings.Fields(paramsStr)

	onlyImage := false
	noDesc := false
	noTag := false
	parseMode := "normal"

	for _, p := range params {
		switch p {
		case "-all":
			onlyImage = true
		case "-des":
			noDesc = true
		case "-tag":
			noTag = true
		case "-o":
			parseMode = "file_only"
		case "-O":
			parseMode = "file_with_info"
		}
	}

	if forceOriginal {
		parseMode = "file_only"
	}

	selectionRaw := ""
	rePage := regexp.MustCompile(`\+([0-9,\-]+)(?:\s|$)`)
	if pageMatch := rePage.FindStringSubmatch(paramsStr); len(pageMatch) > 1 {
		selectionRaw = pageMatch[1]
	}

	apiURL := fmt.Sprintf("https://www.pixiv.net/ajax/illust/%s", illustID)
	artworkURL := fmt.Sprintf("https://www.pixiv.net/artworks/%s", illustID)

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", artworkURL)

	if sessid := os.Getenv("PHPSESSID"); sessid != "" {
		req.AddCookie(&http.Cookie{Name: "PHPSESSID", Value: strings.TrimSpace(sessid)})
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil, "", "normal"
	}
	defer resp.Body.Close()

	var data struct {
		Error bool `json:"error"`
		Body  struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			PageCount   int    `json:"pageCount"`
			Tags        struct {
				Tags []struct {
					Tag string `json:"tag"`
				} `json:"tags"`
			} `json:"tags"`
			Urls struct {
				Original string `json:"original"`
				Regular  string `json:"regular"`
			} `json:"urls"`
		} `json:"body"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Error {
		return nil, "", "normal"
	}

	title := escapeMDV2(data.Body.Title)
	desc := htmlToMarkdownV2(data.Body.Description)

	var tagList []string
	for _, t := range data.Body.Tags.Tags {
		tagList = append(tagList, "\\#"+escapeMDV2(t.Tag))
	}
	tagStr := strings.Join(tagList, " ")

	totalPages := data.Body.PageCount
	if totalPages == 0 {
		totalPages = 1
	}

	baseOrig := data.Body.Urls.Original
	baseReg := data.Body.Urls.Regular
	if baseOrig == "" {
		return nil, "", "normal"
	}

	selectedPages := parsePageSelection(selectionRaw, totalPages)
	suffix := ""
	if len(selectedPages) == 0 {
		for i := 1; i <= totalPages; i++ {
			selectedPages = append(selectedPages, i)
		}
	} else if totalPages > 1 {
		pageStrs := make([]string, len(selectedPages))
		for i, v := range selectedPages {
			pageStrs[i] = strconv.Itoa(v)
		}
		suffix = fmt.Sprintf(" %s/%d", strings.Join(pageStrs, ","), totalPages)
	}

	var images []string
	for _, p := range selectedPages {
		pageIdx := p - 1
		currOrigURL := strings.Replace(baseOrig, "_p0", fmt.Sprintf("_p%d", pageIdx), 1)
		currRegURL := strings.Replace(baseReg, "_p0", fmt.Sprintf("_p%d", pageIdx), 1)
		finalURL := currOrigURL

		if parseMode == "normal" && currRegURL != "" {
			headReq, _ := http.NewRequest("HEAD", currOrigURL, nil)
			headReq.Header.Set("User-Agent", "Mozilla/5.0")
			headReq.Header.Set("Referer", artworkURL)
			if sessid := os.Getenv("PHPSESSID"); sessid != "" {
				headReq.AddCookie(&http.Cookie{Name: "PHPSESSID", Value: strings.TrimSpace(sessid)})
			}

			headClient := &http.Client{Timeout: 3 * time.Second}
			headResp, headErr := headClient.Do(headReq)

			if headErr == nil {
				if headResp.ContentLength > 10*1024*1024 {
					finalURL = currRegURL
				}
				headResp.Body.Close()
			}
		}

		images = append(images, finalURL)
	}

	if onlyImage {
		return images, "", parseMode
	}

	var parts []string
	parts = append(parts, title+suffix)
	if !noDesc && desc != "" {
		parts = append(parts, desc)
	}
	if !noTag && tagStr != "" {
		parts = append(parts, tagStr)
	}

	text := strings.Join(parts, "\n")
	return images, strings.TrimSpace(text), parseMode
}
