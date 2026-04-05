package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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
			strings.Contains(errMsg, "webpage_curl_failed") {

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

		// 提取正确的扩展名并拼接文件名
		ext := ".jpg"
		lowerPath := strings.ToLower(imgPath)
		if strings.Contains(lowerPath, ".png") {
			ext = ".png"
		} else if strings.Contains(lowerPath, ".gif") {
			ext = ".gif"
		} else if strings.Contains(lowerPath, ".mp4") {
			ext = ".mp4"
		}

		fileName := fmt.Sprintf("%s%s", workID, ext)
		if len(images) > 1 {
			fileName = fmt.Sprintf("%s_%d%s", workID, i+1, ext) // 多图加上序号
		}

		if parseMode == "file_only" || parseMode == "file_with_info" {
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

	for i := 0; i < len(album); i += 10 {
		end := i + 10
		if end > len(album) {
			end = len(album)
		}
		opts := &tele.SendOptions{
			ReplyTo:   c.Message(),
			ParseMode: tele.ModeMarkdownV2,
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
	bot := c.Bot()
	bot.Send(c.Chat(), tele.UploadingDocument)

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
			localFiles = append(localFiles, localPath)
			fi, err := os.Stat(localPath)
			if err == nil && fi.Size() > 10*1024*1024 {
				hasLargeFile = true
			}
		} else {
			log.Printf("本地下载失败: %s, 错误: %v", imgURL, err)
		}
	}

	if len(localFiles) == 0 {
		if caption != "" {
			return c.Reply(caption+"\n\n_由于源站限制, 图片下载并发送失败_", tele.ModeMarkdownV2)
		}
		return fmt.Errorf("所有图片下载失败")
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

	// 提取拓展名
	ext := ".jpg"
	if strings.Contains(imgURL, ".png") {
		ext = ".png"
	} else if strings.Contains(imgURL, ".gif") {
		ext = ".gif"
	} else if strings.Contains(imgURL, ".mp4") {
		ext = ".mp4"
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

		bot := c.Bot()
		bot.Send(c.Chat(), tele.UploadingDocument)

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
		bot := c.Bot()
		bot.Send(c.Chat(), tele.UploadingPhoto)

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
		bot := c.Bot()
		bot.Send(c.Chat(), tele.UploadingPhoto)

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
