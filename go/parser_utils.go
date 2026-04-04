package main

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

// 转义 MarkdownV2 保留字符
func escapeMDV2(text string) string {
	if text == "" {
		return ""
	}
	// 转义 \ 本身 否则会导致后续转义产生的 \ 被二次转义
	text = strings.ReplaceAll(text, "\\", "\\\\")
	chars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	for _, c := range chars {
		text = strings.ReplaceAll(text, c, "\\"+c)
	}
	return text
}

// HTML 转 Markdown V2 提取链接并安全转义其余所有文本
func htmlToMarkdownV2(rawHtml string) string {
	if rawHtml == "" {
		return ""
	}
	text := html.UnescapeString(rawHtml)

	// 替换 <br> 为换行
	reBr := regexp.MustCompile(`(?i)<\s*br\s*/?\s*>`)
	text = reBr.ReplaceAllString(text, "\n")

	reA := regexp.MustCompile(`(?i)<a [^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	reTags := regexp.MustCompile(`<[^>]+>`)

	var builder strings.Builder
	lastIdx := 0
	matches := reA.FindAllStringSubmatchIndex(text, -1)

	for _, m := range matches {
		// 处理 <a> 标签之前的普通文本 移除残留标签并彻底安全转义
		preText := text[lastIdx:m[0]]
		preText = reTags.ReplaceAllString(preText, "")
		builder.WriteString(escapeMDV2(preText))

		// 处理 <a> 标签本身
		link := text[m[2]:m[3]]
		content := text[m[4]:m[5]]

		// 清理 Pximg / Bili 跳转链接
		if strings.Contains(link, "jump.php") {
			if parsed, err := url.Parse(link); err == nil {
				if q := parsed.Query().Get("url"); q != "" {
					link = q
				}
			}
		}

		content = reTags.ReplaceAllString(content, "")
		content = escapeMDV2(content)

		// URL 中的 ) 和 \ 必须转义
		link = strings.ReplaceAll(link, "\\", "\\\\")
		link = strings.ReplaceAll(link, ")", "\\)")
		link = strings.ReplaceAll(link, "(", "\\(")

		builder.WriteString("[" + content + "](" + link + ")")

		lastIdx = m[1]
	}

	// 处理最后一个 <a> 标签之后的普通文本
	postText := text[lastIdx:]
	postText = reTags.ReplaceAllString(postText, "")
	builder.WriteString(escapeMDV2(postText))

	result := builder.String()

	// 合并多个空行为两个换行
	result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")

	return strings.TrimSpace(result)
}
