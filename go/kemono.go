package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"
)

type KemonoCreator struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Service   string `json:"service"`
	Updated   int64  `json:"updated"`
	Favorited int    `json:"favorited"`
	Indexed   int64  `json:"indexed"`
}

type KemonoProfile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Service    string `json:"service"`
	Indexed    string `json:"indexed"`
	Updated    string `json:"updated"`
	PublicID   string `json:"public_id"`
	RelationID int    `json:"relation_id"`
	PostCount  int    `json:"post_count"`
}

var (
	kemonoCache []KemonoCreator
	kemonoMutex sync.RWMutex
)

func startKemonoUpdater() {
	updateKemonoCache()
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		for range ticker.C {
			updateKemonoCache()
		}
	}()
}

func updateKemonoCache() {
	req, err := http.NewRequest("GET", "https://kemono.cr/api/v1/creators", nil)
	if err != nil {
		log.Printf("Kemono req error: %v", err)
		return
	}
	req.Header.Set("Accept", "text/css")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Kemono fetch error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("Kemono fetch bad status: %d", resp.StatusCode)
		return
	}

	var creators []KemonoCreator
	if err := json.NewDecoder(resp.Body).Decode(&creators); err != nil {
		log.Printf("Kemono decode error: %v", err)
		return
	}

	kemonoMutex.Lock()
	kemonoCache = creators
	kemonoMutex.Unlock()
	log.Printf("Kemono cache updated, total creators: %d", len(creators))
}

func isService(s string) bool {
	switch strings.ToLower(s) {
	case "fanbox", "patreon", "fantia", "gumroad", "subscribestar", "dlsite", "discord", "boosty", "afdian":
		return true
	}
	return false
}

func searchKemonoCreators(name, service string) []KemonoCreator {
	kemonoMutex.RLock()
	defer kemonoMutex.RUnlock()

	var results []KemonoCreator
	searchLower := strings.ToLower(name)
	svcLower := strings.ToLower(service)

	for _, c := range kemonoCache {
		if svcLower != "" && strings.ToLower(c.Service) != svcLower {
			continue
		}
		if strings.Contains(strings.ToLower(c.Name), searchLower) {
			results = append(results, c)
		}
	}
	return results
}

func fetchKemonoProfile(service, id string) (*KemonoProfile, error) {
	apiURL := fmt.Sprintf("https://kemono.cr/api/v1/%s/user/%s/profile", service, id)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/css")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	var profile KemonoProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

func handleLookupCommand(c tele.Context) error {
	args := c.Args()
	if len(args) == 0 {
		return c.Reply("请提供要查询的作者名字, 例如：`/lookup 幼月` 或 `/lookup fanbox 幼月`", tele.ModeMarkdownV2)
	}

	var targetService string
	var targetName string

	if len(args) >= 2 && isService(args[0]) {
		targetService = strings.ToLower(args[0])
		targetName = strings.Join(args[1:], " ")
	} else {
		targetName = strings.Join(args, " ")
	}

	results := searchKemonoCreators(targetName, targetService)
	if len(results) == 0 {
		msg := "没有在 Kemono 找到匹配的作者\\."
		return c.Reply(msg, tele.ModeMarkdownV2)
	}

	if len(results) > 1 {
		msgText := fmt.Sprintf("找到 %d 个匹配的作者, 请点击下方按钮查看详情: \n\n", len(results))

		menu := &tele.ReplyMarkup{}
		var rows []tele.Row

		limit := len(results)
		if limit > 10 {
			limit = 10
		}

		for i := 0; i < limit; i++ {
			res := results[i]
			btnText := fmt.Sprintf("%s (%s)", res.Name, res.Service)
			btn := menu.Data(btnText, fmt.Sprintf("k:%s:%s", res.Service, res.ID))
			rows = append(rows, menu.Row(btn))
		}
		menu.Inline(rows...)

		if len(results) > 10 {
			msgText += "_\\(结果过多, 仅显示前 10 个, 请尝试输入更完整的名字\\)_"
		}

		return c.Reply(msgText, tele.ModeMarkdownV2, menu)
	}

	creator := results[0]
	statusMsg, _ := c.Bot().Reply(c.Message(), "找到唯一匹配作者, 正在获取详细信息...")
	return renderKemonoProfile(c.Bot(), statusMsg, creator.Service, creator.ID)
}

func renderKemonoProfile(bot *tele.Bot, msg *tele.Message, service, id string) error {
	profile, err := fetchKemonoProfile(service, id)
	if err != nil {
		_, errEdit := bot.Edit(msg, fmt.Sprintf("获取详细信息失败: %v", err), &tele.SendOptions{ReplyMarkup: nil})
		return errEdit
	}

	updatedStr := profile.Updated
	if idx := strings.Index(updatedStr, "."); idx != -1 {
		updatedStr = updatedStr[:idx]
	}
	updatedStr = strings.Replace(updatedStr, "T", " ", 1)

	userURL := fmt.Sprintf("https://kemono.cr/%s/user/%s", profile.Service, profile.ID)
	userURL = strings.ReplaceAll(userURL, "(", "\\(")
	userURL = strings.ReplaceAll(userURL, ")", "\\)")

	caption := fmt.Sprintf("*%s*\n——————————\n平台: %s\n帖子数量: *%d*\n上次更新: %s\n\n[点击查看 Kemono 作者主页](%s)",
		escapeMDV2(profile.Name),
		escapeMDV2(profile.Service),
		profile.PostCount,
		escapeMDV2(updatedStr),
		userURL,
	)

	opts := &tele.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tele.ModeMarkdownV2,
		ReplyMarkup:           nil,
	}
	_, err = bot.Edit(msg, caption, opts)
	return err
}

func HandleKemonoCallback(c tele.Context, service, id string) error {
	bot := c.Bot()
	msg := c.Message()
	bot.Edit(msg, "正在获取详细信息...", &tele.SendOptions{ReplyMarkup: nil})
	return renderKemonoProfile(bot, msg, service, id)
}
