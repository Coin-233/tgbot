import re
import requests
import html


def match_pixiv_url(text: str):
    return re.search(
        r"(?:https?://)?(?:www\.)?pixiv\.net/(?:en/)?artworks/\d+", text)


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


def parse_page_selection(selection_raw: str, total_pages: int):
    if not selection_raw:
        return []
    selected = set()
    for part in selection_raw.split(","):
        part = part.strip()
        if not part:
            continue
        if "-" in part:
            try:
                start, end = map(int, part.split("-", 1))
                if start > end:
                    start, end = end, start
                selected.update(range(start, end + 1))
            except Exception:
                continue
        elif part.isdigit():
            selected.add(int(part))
    selected = sorted(i for i in selected if 1 <= i <= total_pages)
    return selected


def fetch_pixiv_data(url: str):
    try:
        match = re.search(
            r"(?:https?://)?(?:www\.)?pixiv\.net/(?:en/)?artworks/(\d+)(.*)",
            url)
        if not match:
            return [], ""
        illust_id = match.group(1)
        params = match.group(2).strip()

        only_image = "-all" in params
        no_desc = "-des" in params
        no_tag = "-tag" in params

        page_match = re.search(r"\+([0-9,\-]+)(?=\s|$)", params)
        selection_raw = page_match.group(1) if page_match else ""

        api_url = f"https://www.pixiv.net/ajax/illust/{illust_id}"
        headers = {
            "User-Agent":
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
            "Referer": f"https://www.pixiv.net/artworks/{illust_id}"
        }
        resp = requests.get(api_url, headers=headers, timeout=10)
        if resp.status_code != 200:
            return [], ""

        data = resp.json()
        if data.get("error") or "body" not in data:
            return [], ""

        body = data["body"]
        title = body.get("title", "")
        desc = html_to_markdown_v2(body.get("description", ""))
        tags = body.get("tags", {}).get("tags", [])
        tag_str = " ".join([
            f"#{t['tag']}" for t in tags if isinstance(t, dict) and "tag" in t
        ])

        total_pages = int(body.get("pageCount", 1))
        base_url = body.get("urls", {}).get("original", "")
        if not base_url:
            return [], ""

        prefix = base_url.rsplit("_p0", 1)[0]
        ext = base_url.split(".")[-1]
        all_images = [f"{prefix}_p{i}.{ext}" for i in range(total_pages)]

        # 页码选择
        selected_pages = parse_page_selection(selection_raw, total_pages)
        if selected_pages:
            images = [all_images[i - 1] for i in selected_pages]
            suffix = f" {','.join(map(str, selected_pages))}/{total_pages}" if total_pages > 1 else ""
        else:
            images = all_images
            suffix = ""

        # -all 仅图片模式
        if only_image:
            return images, ""

        # 组装
        parts = [title + suffix]
        if not no_desc and desc:
            parts.append(desc)
        if not no_tag and tag_str:
            parts.append(tag_str)

        text = "\n".join(p for p in parts if p).strip()
        return images, text

    except Exception as e:
        print(f"Pixiv fetch error: {e}")
        return [], ""
