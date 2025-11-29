import re
import os
import requests
import html
import tempfile

PIXIV_SESSID = os.getenv("PHPSESSID", "").strip()


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
            return [], "", "normal"
        illust_id = match.group(1)
        params_str = match.group(2).strip()
        params = params_str.split()

        only_image = "-all" in params
        no_desc = "-des" in params
        no_tag = "-tag" in params

        parse_mode = "normal"
        if "-o" in params:
            parse_mode = "file_only"
        elif "-O" in params:
            parse_mode = "file_with_info"

        page_match = re.search(r"\+([0-9,\-]+)(?=\s|$)", params_str)
        selection_raw = page_match.group(1) if page_match else ""

        api_url = f"https://www.pixiv.net/ajax/illust/{illust_id}"
        artwork_url = f"https://www.pixiv.net/artworks/{illust_id}"

        headers = {
            "User-Agent":
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
            "Referer": artwork_url
        }
        cookies = {}
        if PIXIV_SESSID:
            cookies["PHPSESSID"] = PIXIV_SESSID

        resp = requests.get(api_url,
                            headers=headers,
                            cookies=cookies,
                            timeout=10)
        if resp.status_code != 200:
            return [], "", "normal"

        data = resp.json()
        if data.get("error") or "body" not in data:
            return [], "", "normal"

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
            return [], "", "normal"

        prefix = base_url.rsplit("_p0", 1)[0]
        ext = base_url.split(".")[-1]
        all_image_urls = [f"{prefix}_p{i}.{ext}" for i in range(total_pages)]

        selected_pages = parse_page_selection(selection_raw, total_pages)
        if selected_pages:
            target_urls = [all_image_urls[i - 1] for i in selected_pages]
            suffix = f" {','.join(map(str, selected_pages))}/{total_pages}" if total_pages > 1 else ""
        else:
            target_urls = all_image_urls
            suffix = ""

        local_media_files = []

        for img_url in target_urls:
            try:
                r = requests.get(img_url,
                                 headers=headers,
                                 stream=True,
                                 timeout=20)

                if r.ok and r.headers.get("Content-Type",
                                          "").startswith("image/"):
                    file_ext = f".{ext}"

                    with tempfile.NamedTemporaryFile(delete=False,
                                                     suffix=file_ext) as tmp:
                        for chunk in r.iter_content(8192):
                            tmp.write(chunk)
                        local_media_files.append(tmp.name)
                else:
                    print(
                        f"Pixiv download failed (Status: {r.status_code}, Type: {r.headers.get('Content-Type')}) for {img_url}"
                    )

            except Exception as e:
                print(f"Error downloading pixiv image {img_url}: {e}")
                pass

        if not local_media_files:
            return [], "", "normal"

        if only_image:
            return local_media_files, "", parse_mode

        parts = [title + suffix]
        if not no_desc and desc:
            parts.append(desc)
        if not no_tag and tag_str:
            parts.append(tag_str)

        text = "\n".join(p for p in parts if p).strip()

        return local_media_files, text, parse_mode

    except Exception as e:
        print(f"Pixiv fetch error: {e}")
        return [], "", "normal"
