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
	"sync"
)

var (
	pixivURLPattern       = regexp.MustCompile(`pixiv\.net/.*(?:artworks/|illust_id=)(\d+)`)
	pixivArtworkPattern   = regexp.MustCompile(`(?:artworks/|illust_id=)(\d+)(.*)`)
	pixivPageParamPattern = regexp.MustCompile(`\+([0-9,\-]+)(?:\s|$)`)
)

func MatchPixivURL(text string) bool {
	return pixivURLPattern.MatchString(text)
}

func parsePageSelection(selectionRaw string, totalPages int) []int {
	if selectionRaw == "" || totalPages < 1 {
		return nil
	}
	selectedMap := make(map[int]struct{})
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
				if start < 1 {
					start = 1
				}
				if end > totalPages {
					end = totalPages
				}
				for i := start; i <= end; i++ {
					selectedMap[i] = struct{}{}
				}
			}
		} else if val, err := strconv.Atoi(part); err == nil {
			if val >= 1 && val <= totalPages {
				selectedMap[val] = struct{}{}
			}
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
	matches := pixivArtworkPattern.FindStringSubmatch(urlStr)
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
	if pageMatch := pixivPageParamPattern.FindStringSubmatch(paramsStr); len(pageMatch) > 1 {
		selectionRaw = pageMatch[1]
	}

	apiURL := fmt.Sprintf("https://www.pixiv.net/ajax/illust/%s", illustID)
	artworkURL := fmt.Sprintf("https://www.pixiv.net/artworks/%s", illustID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, "", "normal"
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", artworkURL)

	if sessid := os.Getenv("PHPSESSID"); sessid != "" {
		req.AddCookie(&http.Cookie{Name: "PHPSESSID", Value: strings.TrimSpace(sessid)})
	}

	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, "", "normal"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", "normal"
	}

	var data struct {
		Error bool `json:"error"`
		Body  struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			PageCount   int    `json:"pageCount"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
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

	isDimensionInvalid := false
	if data.Body.Width > 0 && data.Body.Height > 0 {
		// 宽+高总和不能超 10000
		if data.Body.Width+data.Body.Height > 10000 {
			isDimensionInvalid = true
		}
		// 宽高比不能超过 1:20
		ratio := float64(data.Body.Height) / float64(data.Body.Width)
		if ratio > 20 || ratio < 0.05 {
			isDimensionInvalid = true
		}
	}

	selectedPages := parsePageSelection(selectionRaw, totalPages)
	suffix := ""
	if selectionRaw != "" && len(selectedPages) == 0 {
		return nil, "", parseMode
	}
	if selectionRaw == "" {
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

	images := make([]string, len(selectedPages))
	regularURLs := make([]string, len(selectedPages))
	for i, p := range selectedPages {
		pageIdx := p - 1
		currOrigURL := strings.Replace(baseOrig, "_p0", fmt.Sprintf("_p%d", pageIdx), 1)
		currRegURL := strings.Replace(baseReg, "_p0", fmt.Sprintf("_p%d", pageIdx), 1)
		images[i] = currOrigURL
		regularURLs[i] = currRegURL
	}

	if parseMode == "normal" {
		if isDimensionInvalid {
			for i, regularURL := range regularURLs {
				if regularURL != "" {
					images[i] = regularURL
				}
			}
		} else {
			downgradeOversizedPixivImages(images, regularURLs, artworkURL)
		}
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

func downgradeOversizedPixivImages(images, regularURLs []string, artworkURL string) {
	jobs := make(chan int)
	workerCount := min(4, len(images))
	var wg sync.WaitGroup

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if regularURLs[i] != "" && pixivImageTooLarge(images[i], artworkURL) {
					images[i] = regularURLs[i]
				}
			}
		}()
	}

	for i := range images {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func pixivImageTooLarge(imageURL, artworkURL string) bool {
	req, err := http.NewRequest("HEAD", imageURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", artworkURL)
	if sessid := strings.TrimSpace(os.Getenv("PHPSESSID")); sessid != "" {
		req.AddCookie(&http.Cookie{Name: "PHPSESSID", Value: sessid})
	}

	resp, err := headHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK && resp.ContentLength > 10*1024*1024
}
