package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	tele "gopkg.in/telebot.v3"
)

const startMessageMD = `发送 *Twitter*, *Pixiv* 或 *Bilibili动态* 链接，机器人将自动解析并发送图片\.

*Pixiv 解析指令*

对于 Pixiv 链接，可在链接后添加以下参数：

1\. 分页 \(\+pages\)
用于获取指定图片，支持多种格式：

• ` + "`\\+1`" + `：第 1 张
• ` + "`\\+1,2,5`" + `：获取第 1、2、5 张
• ` + "`\\+1\\-3`" + `：获取第 1 到第 3 张

2\. 信息去除 \(\- 参数\)
用于移除图片描述信息：

• ` + "`\\-all`" + `：去除简介和 Tag
• ` + "`\\-des`" + `：去除简介
• ` + "`\\-tag`" + `：去除 Tag

3\. 发送原图
• ` + "`\\-o`" + `：仅发送原图文件, 不含任何信息
• ` + "`\\-O`" + `：发送原图文件, 并附带作品信息

混合使用示例：` + "`https://www.pixiv.net/artworks/ID \\+3 \\-des`" + `

*搜图指令*
• ` + "`/s`" + `：回复包含图片的这条消息，或在发送图片时在标题中带上 /s, 进行 SauceNAO 搜图\.

*其他命令*
• ` + "`/stat`" + `：查看总解析统计信息\.`

// 发送聊天状态
func keepSendingAction(c tele.Context, action tele.ChatAction) chan struct{} {
	stopChan := make(chan struct{})
	go func() {
		c.Bot().Send(c.Chat(), action)
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				c.Bot().Send(c.Chat(), action)
			}
		}
	}()
	return stopChan
}

// 组装带链接的 MarkdownV2 文本
func makeMarkdownCaption(url, text string, escapeBody bool) string {
	linkMd := fmt.Sprintf("[%s](%s)", escapeMDV2(url), url)
	if text == "" {
		return linkMd
	}

	bodyMd := text
	if escapeBody {
		bodyMd = escapeMDV2(text)
	}

	var lines []string
	for _, line := range strings.Split(bodyMd, "\n") {
		if line != "" {
			lines = append(lines, "> "+line)
		} else {
			lines = append(lines, ">")
		}
	}
	return linkMd + "\n\n" + strings.Join(lines, "\n")
}

// 发送逻辑
func sendMedia(c tele.Context, images []string, caption string, parseMode string, workID string) error {
	if len(images) == 0 {
		if caption != "" {
			return c.Reply(caption, tele.ModeMarkdownV2)
		}
		return nil
	}

	// 优先尝试直接使用 Telegram 发送
	err := sendMediaBatch(c, images, caption, parseMode, false, workID)
	if err != nil {
		errMsg := strings.ToLower(err.Error())

		// 捕捉防盗链拦截或文件过大错误
		if strings.Contains(errMsg, "webpage_media_empty") ||
			strings.Contains(errMsg, "wrong type") ||
			strings.Contains(errMsg, "failed to get http") ||
			strings.Contains(errMsg, "webpage_curl_failed") ||
			strings.Contains(errMsg, "request entity too large") {

			return sendMediaWithFallback(c, images, caption, parseMode, workID)
		}

		log.Printf("发送媒体失败: %v", err)
		if caption != "" {
			c.Reply(caption, tele.ModeMarkdownV2)
		}
		return err
	}
	return nil
}

// 实际发送批次
func sendMediaBatch(c tele.Context, images []string, caption string, parseMode string, isLocal bool, workID string) error {
	if parseMode == "file_only" {
		caption = ""
	}

	var album tele.Album
	lastIdx := len(images) - 1

	for i, imgPath := range images {
		var file tele.File
		if isLocal {
			file = tele.FromDisk(imgPath)
		} else {
			file = tele.FromURL(imgPath)
		}

		ext := ".jpg"
		if parsedUrl, err := url.Parse(imgPath); err == nil {
			if parsedExt := filepath.Ext(parsedUrl.Path); parsedExt != "" {
				ext = strings.ToLower(parsedExt)
			}
		}

		fileName := fmt.Sprintf("%s%s", workID, ext)
		if len(images) > 1 {
			fileName = fmt.Sprintf("%s_%d%s", workID, i+1, ext)
		}

		isMedia := false
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".mp4":
			isMedia = true
		}

		forceDocument := !isMedia || parseMode == "file_only" || parseMode == "file_with_info"

		if forceDocument {
			doc := &tele.Document{File: file, FileName: fileName}
			if i == lastIdx && caption != "" {
				doc.Caption = caption
			}
			album = append(album, doc)
		} else {
			if ext == ".mp4" {
				video := &tele.Video{File: file, FileName: fileName}
				if i == 0 && caption != "" {
					video.Caption = caption
				}
				album = append(album, video)
			} else {
				photo := &tele.Photo{File: file}
				if i == 0 && caption != "" {
					photo.Caption = caption
				}
				album = append(album, photo)
			}
		}
	}

	if len(album) == 0 {
		return nil
	}

	opts := &tele.SendOptions{
		ReplyTo:   c.Message(),
		ParseMode: tele.ModeMarkdownV2,
	}

	if len(album) == 1 {
		switch v := album[0].(type) {
		case *tele.Photo:
			return c.Send(v, opts)
		case *tele.Video:
			return c.Send(v, opts)
		case *tele.Document:
			return c.Send(v, opts)
		}
	}

	for i := 0; i < len(album); i += 10 {
		end := i + 10
		if end > len(album) {
			end = len(album)
		}
		err := c.SendAlbum(album[i:end], opts)
		if err != nil {
			return err
		}
	}
	return nil
}

// 本地下载并发送回退逻辑
func sendMediaWithFallback(c tele.Context, images []string, caption string, parseMode string, workID string) error {
	var localFiles []string
	defer func() {
		for _, f := range localFiles {
			os.Remove(f)
		}
	}()

	hasLargeFile := false
	for _, imgURL := range images {
		localPath, err := downloadImage(imgURL)
		if err == nil {
			fi, err := os.Stat(localPath)
			if err == nil {
				if fi.Size() > 50*1024*1024 {
					log.Printf("文件超过 50MB 限制被跳过: %s", imgURL)
					os.Remove(localPath)
					sizeMB := float64(fi.Size()) / 1024 / 1024

					sizeStr := strings.ReplaceAll(fmt.Sprintf("%.1f", sizeMB), ".", "\\.")

					caption += fmt.Sprintf("\n\n_有一个文件大小为 %s MB, 超出了 Telegram 机器人的 50MB 限制, 已被跳过\\._", sizeStr)
					continue
				}

				if fi.Size() > 10*1024*1024 {
					hasLargeFile = true
				}
				localFiles = append(localFiles, localPath)
			}
		} else {
			log.Printf("本地下载失败: %s, 错误: %v", imgURL, err)
		}
	}

	if len(localFiles) == 0 {
		if caption != "" {
			return c.Reply(caption+"\n\n_由于源站限制或文件过大, 所有内容发送失败\\._", tele.ModeMarkdownV2)
		}
		return fmt.Errorf("所有文件处理失败")
	}

	if hasLargeFile && parseMode == "normal" {
		parseMode = "file_with_info"
	}

	return sendMediaBatch(c, localFiles, caption, parseMode, true, workID)
}

// 将远端图片流式下载到本地服务器
func downloadImage(imgURL string) (string, error) {
	req, err := http.NewRequest("GET", imgURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	if strings.Contains(imgURL, "pximg.net") || strings.Contains(imgURL, "pixiv.net") {
		req.Header.Set("Referer", "https://www.pixiv.net/")
	} else if strings.Contains(imgURL, "hdslb.com") || strings.Contains(imgURL, "bilibili") {
		req.Header.Set("Referer", "https://t.bilibili.com/")
	} else if strings.Contains(imgURL, "kemono.cr") {
		req.Header.Set("Referer", "https://kemono.cr/")
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("bad status: %d", resp.StatusCode)
	}

	// 使用真实扩展名
	ext := ".jpg"
	if parsedUrl, err := url.Parse(imgURL); err == nil {
		if parsedExt := filepath.Ext(parsedUrl.Path); parsedExt != "" {
			ext = strings.ToLower(parsedExt)
		}
	}

	tmpFile, err := os.CreateTemp("", "tgbot-*"+ext)
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	// 写入本地磁盘
	_, err = io.Copy(tmpFile, resp.Body)
	return tmpFile.Name(), err
}

func main() {
	_ = godotenv.Load()
	loadStats()
	startKemonoUpdater()

	proxy := os.Getenv("PROXY")
	if proxy != "" {
		os.Setenv("HTTP_PROXY", proxy)
		os.Setenv("HTTPS_PROXY", proxy)
	}

	pref := tele.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	bot, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	// 命令: /start
	bot.Handle("/start", func(c tele.Context) error {
		return c.Reply(startMessageMD, tele.ModeMarkdownV2)
	})

	// 命令: /stat
	bot.Handle("/stat", func(c tele.Context) error {
		globalStats.mu.Lock()
		msg := fmt.Sprintf("统计信息：\n• 总解析链接: %d 条\n• 总解析文件: %d 个", globalStats.TotalLinks, globalStats.TotalImages)
		globalStats.mu.Unlock()
		return c.Reply(msg)
	})

	// 命令: /lookup
	bot.Handle("/lookup", handleLookupCommand)

	// 处理文本和图文消息
	bot.Handle(tele.OnText, handleMessage)
	bot.Handle(tele.OnPhoto, handleMessage)

	// 处理带图搜索: /s
	bot.Handle("/s", func(c tele.Context) error {
		targetMsg := c.Message().ReplyTo
		if targetMsg == nil {
			targetMsg = c.Message()
		}

		if targetMsg.Photo == nil {
			return c.Reply("请回复一张图片, 或直接发送带图消息并附带 /s 指令")
		}

		statusMsg, _ := bot.Reply(c.Message(), "正在搜索 SauceNAO...")

		// 获取最高清的图片
		photo := targetMsg.Photo.MediaFile()
		reader, err := bot.File(photo)
		if err != nil {
			bot.Edit(statusMsg, "下载图片失败")
			return nil
		}
		defer reader.Close()

		imgBytes, _ := io.ReadAll(reader)

		results, err := searchSauceNAO(imgBytes)
		if err != nil {
			bot.Edit(statusMsg, "❌ "+err.Error())
			return nil
		}

		searchID := uuid.New().String()[:8]
		searchCache[searchID] = results

		renderSauceNaoPage(bot, statusMsg, searchID, 0)
		return nil
	})

	// 处理翻页回调
	bot.Handle(tele.OnCallback, func(c tele.Context) error {
		data := c.Callback().Data
		data = strings.TrimPrefix(data, "\f")
		if data == "ignore" {
			return c.Respond()
		}

		if strings.HasPrefix(data, "s:") {
			parts := strings.Split(data, ":")
			if len(parts) == 3 {
				searchID := parts[1]
				index, _ := strconv.Atoi(parts[2])
				renderSauceNaoPage(bot, c.Message(), searchID, index)
			}
		}

		if strings.HasPrefix(data, "k:") {
			parts := strings.Split(data, ":")
			if len(parts) == 3 {
				service := parts[1]
				id := parts[2]
				HandleKemonoCallback(c, service, id)
			}
		}
		return c.Respond()
	})

	log.Println("Bot is started.")
	bot.Start()
}

// 路由分发器
func handleMessage(c tele.Context) error {
	text := strings.TrimSpace(c.Text())
	if text == "" || strings.HasPrefix(text, "/s") {
		return nil
	}

	forceOriginal := false
	if c.Message().SenderChat != nil && c.Message().SenderChat.Type == tele.ChatChannel {
		forceOriginal = true
	} else if c.Message().IsForwarded() && c.Message().OriginalChat != nil && c.Message().OriginalChat.Type == tele.ChatChannel {
		forceOriginal = true
	}

	// Twitter
	if MatchTwitterURL(text) {
		url := twitterPattern.FindString(text)

		url = strings.Replace(url, "x.com/", "twitter.com/", 1)

		workID := "twitter"
		if matches := regexp.MustCompile(`status/(\d+)`).FindStringSubmatch(url); len(matches) > 1 {
			workID = matches[1]
		}

		parseMode := "normal"
		if strings.Contains(text, " -o") {
			parseMode = "file_only"
		} else if strings.Contains(text, " -O") {
			parseMode = "file_with_info"
		}

		stopAction := keepSendingAction(c, tele.UploadingDocument)
		defer close(stopAction)

		images, textInfo := FetchTweetData(url, forceOriginal || parseMode != "normal")
		if len(images) == 0 && textInfo == "" {
			return c.Reply("喵~ 这个推文抓不到, 可能被删掉或不公开")
		}

		addStats(1, len(images))
		caption := makeMarkdownCaption(url, textInfo, true)

		if forceOriginal {
			parseMode = "file_only"
		}
		return sendMedia(c, images, caption, parseMode, workID)
	}

	// Pixiv
	if MatchPixivURL(text) {
		stopAction := keepSendingAction(c, tele.UploadingPhoto)
		defer close(stopAction)

		workID := "pixiv"
		if matches := regexp.MustCompile(`(?:artworks/|illust_id=)(\d+)`).FindStringSubmatch(text); len(matches) > 1 {
			workID = matches[1]
		}

		images, textInfo, parseMode := FetchPixivData(text)
		if len(images) == 0 {
			return c.Reply("喵~ Pixiv 作品抓不到, 可能被删掉或不公开")
		}

		addStats(1, len(images))
		caption := makeMarkdownCaption("https://www.pixiv.net/artworks/"+regexp.MustCompile(`(\d+)`).FindString(text), textInfo, false)

		if forceOriginal {
			parseMode = "file_only"
		}
		return sendMedia(c, images, caption, parseMode, workID)
	}

	// Bilibili
	if MatchBilibiliURL(text) {
		stopAction := keepSendingAction(c, tele.UploadingPhoto)
		defer close(stopAction)

		workID := "bilibili"
		if matches := bilibiliPattern.FindStringSubmatch(text); len(matches) > 1 {
			workID = matches[1]
		}

		images, textInfo, parseMode := FetchBilibiliData(text)
		if len(images) == 0 && textInfo == "" {
			return c.Reply("喵~ 这个动态抓不到, 可能被删掉或设为仅自己可见")
		}

		addStats(1, len(images))
		url := bilibiliPattern.FindString(text)
		caption := makeMarkdownCaption(url, textInfo, false)

		if forceOriginal {
			parseMode = "file_only"
		}
		return sendMedia(c, images, caption, parseMode, workID)
	}

	// Kemono
	if MatchKemonoPostURL(text) {
		url := kemonoPostPattern.FindString(text)

		stopAction := keepSendingAction(c, tele.UploadingDocument)
		defer close(stopAction)

		workID := "kemono"
		if matches := kemonoPostPattern.FindStringSubmatch(url); len(matches) >= 4 {
			workID = matches[3]
		}

		images, textInfo, parseMode := FetchKemonoPostData(text)
		if len(images) == 0 && textInfo == "" {
			return c.Reply("喵~ 这个 Kemono 帖子抓不到, 可能已被删掉或不存在")
		}

		addStats(1, len(images))

		caption := makeMarkdownCaption(url, textInfo, false)

		if forceOriginal && parseMode == "normal" {
			parseMode = "file_with_info"
		}
		return sendMedia(c, images, caption, parseMode, workID)
	}

	return nil
}

// 渲染 SauceNAO 搜图页面
func renderSauceNaoPage(bot *tele.Bot, msg *tele.Message, searchID string, index int) {
	results, ok := searchCache[searchID]
	if !ok || len(results) == 0 {
		bot.Edit(msg, "搜索结果已过期")
		return
	}

	total := len(results)
	curr := (index%total + total) % total
	item := results[curr]

	caption := fmt.Sprintf(" *SauceNAO 搜索结果* \\(%d/%d\\)\n——————————————\n相似度: *%s%%*\n标题: %s\n作者: %s",
		curr+1, total, escapeMDV2(item.Similarity), escapeMDV2(item.Title), escapeMDV2(item.Author))

	menu := &tele.ReplyMarkup{}
	prevBtn := menu.Data("⬅️", fmt.Sprintf("s:%s:%d", searchID, curr-1))
	currBtn := menu.Data(fmt.Sprintf("%d/%d", curr+1, total), "ignore")
	nextBtn := menu.Data("➡️", fmt.Sprintf("s:%s:%d", searchID, curr+1))

	row1 := menu.Row(prevBtn, currBtn, nextBtn)

	if item.URL != "" {
		linkBtn := menu.URL("打开来源", item.URL)
		menu.Inline(row1, menu.Row(linkBtn))
	} else {
		menu.Inline(row1)
	}

	photo := &tele.Photo{File: tele.FromURL(item.Thumbnail), Caption: caption}
	_, err := bot.Edit(msg, photo, menu, tele.ModeMarkdownV2)
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		// 缩略图加载失败时 只更新文字和按钮
		bot.Edit(msg, caption+"\n\n_预览图加载失败_", menu, tele.ModeMarkdownV2)
	}
}
