import re
import requests
import html


def match_bilibili_url(text: str):
    return re.search(r"(?:https?://)?t\.bilibili\.com/(\d+)", text)


def html_to_markdown_v2(raw_html: str) -> str:
    if not raw_html:
        return ""
    text = html.unescape(raw_html)
    text = re.sub(r'<\s*br\s*/?\s*>', '\n', text, flags=re.I)

    def link_replacer(m):
        url = m.group(1)
        content = m.group(2)
        content = re.sub(r'([_*[\]()~`>#+\-=|{}.!])', r'\\\1', content)
        url = url.replace(")", "\\)").replace("(", "\\(")
        return f"[{content}]({url})"

    text = re.sub(r'<a [^>]*href=["\']([^"\']+)["\'][^>]*>(.*?)</a>',
                  link_replacer,
                  text,
                  flags=re.I)
    text = re.sub(r'<[^>]+>', '', text)
    text = re.sub(r'\n{3,}', '\n\n', text).strip()
    return text


def fetch_bilibili_data(url: str):
    try:
        match = re.search(r"(?:https?://)?t\.bilibili\.com/(\d+)(.*)", url)
        if not match:
            return [], "", "normal"

        dynamic_id = match.group(1)
        params_str = match.group(2).strip()
        params = params_str.split() if params_str else []

        parse_mode = "normal"
        if "-o" in params:
            parse_mode = "file_only"
        elif "-O" in params:
            parse_mode = "file_with_info"

        api_url = f"https://api.bilibili.com/x/polymer/web-dynamic/v1/detail?id={dynamic_id}&features=itemOpusStyle"

        headers = {
            "User-Agent": ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
                           "AppleWebKit/537.36 (KHTML, like Gecko) "
                           "Chrome/141.0.0.0 Safari/537.36"),
            "Referer":
            f"https://t.bilibili.com/{dynamic_id}",
        }

        resp = requests.get(api_url, headers=headers, timeout=10)
        if resp.status_code != 200:
            return [], "", "normal"

        data = resp.json()
        if data.get("code") != 0:
            return [], "", "normal"

        item = data.get("data", {}).get("item", {})
        modules = item.get("modules", {})
        author = modules.get("module_author", {})
        dynamic = modules.get("module_dynamic", {})
        stats = modules.get("module_stat", {})

        author_name = author.get("name", "")
        pub_time = author.get("pub_time", "")
        author_line = f"{author_name} 发布于 {pub_time}" if author_name else ""

        images = []
        text_content = ""

        major = dynamic.get("major", {})
        major_type = major.get("type")

        if major_type == "MAJOR_TYPE_OPUS":
            opus = major.get("opus", {})
            summary = opus.get("summary", {})
            text_content = summary.get("text", "") or ""
            text_content = html_to_markdown_v2(text_content)
            for pic in opus.get("pics", []):
                if "url" in pic:
                    images.append(pic["url"])

        elif major_type == "MAJOR_TYPE_DRAW":
            draw = major.get("draw", {})
            for img in draw.get("items", []):
                if "src" in img:
                    images.append(img["src"])
            desc = dynamic.get("desc", "")
            if desc:
                text_content = html_to_markdown_v2(desc)

        # like_count = stats.get("like", {}).get("count", 0)
        # forward_count = stats.get("forward", {}).get("count", 0)
        # comment_count = stats.get("comment", {}).get("count", 0)
        # stats_line = f"👍 {like_count} 🔁 {forward_count} 💬 {comment_count}"

        # 要不要加呢
        # parts = [author_line, text_content, stats_line]
        parts = [author_line, text_content]
        text = "\n".join(p for p in parts if p).strip()

        return images, text, parse_mode

    except Exception as e:
        return [], "", "normal"
