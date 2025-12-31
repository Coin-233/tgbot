import re
import os
import requests
import html
import tempfile
import urllib.parse

PIXIV_SESSID = os.getenv("PHPSESSID", "").strip()


def escape_md_v2(text: str) -> str:
    if not text:
        return ""
    for ch in r"_*[]()~`>#+-=|{}.!":
        text = text.replace(ch, "\\" + ch)
    return text

def match_pixiv_url(text: str):
    return re.search(r"pixiv\.net/.*(?:artworks/|illust_id=)(\d+)", text)


def html_to_markdown_v2(raw_html: str) -> str:
    if not raw_html:
        return ""
    text = html.unescape(raw_html)
    text = re.sub(r'<\s*br\s*/?\s*>', '\n', text, flags=re.I)

    def escape_md(t):
        if not t: return ""
        for ch in r"_*[]()~`>#+-=|{}.!":
            t = t.replace(ch, "\\" + ch)
        return t

    # 链接清洗函数
    def clean_url(u):
        if "jump.php" in u:
            try:
                parsed = urllib.parse.urlparse(u)
                qs = urllib.parse.parse_qs(parsed.query)
                if 'url' in qs:
                    return qs['url'][0]
            except:
                pass
        return u
    
    pattern = re.compile(r'<a [^>]*href=["\']([^"\']+)["\'][^>]*>(.*?)</a>', re.I | re.S)
    
    parts = []
    last_idx = 0
    
    for m in pattern.finditer(text):
        pre_text = text[last_idx:m.start()]
        pre_text = re.sub(r'<[^>]+>', '', pre_text) # 去除其他 HTML 标签
        parts.append(escape_md(pre_text))
        
        # 处理链接
        url = clean_url(m.group(1))
        content = m.group(2)
        content = re.sub(r'<[^>]+>', '', content)
        content = escape_md(content)
        
        safe_url = url.replace("\\", "\\\\").replace(")", "\\)")
        
        parts.append(f"[{content}]({safe_url})")
        
        last_idx = m.end()
        
    post_text = text[last_idx:]
    post_text = re.sub(r'<[^>]+>', '', post_text)
    parts.append(escape_md(post_text))
    
    result = "".join(parts)
    result = re.sub(r'\n{3,}', '\n\n', result).strip()
    return result


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


def download_pixiv_images(url_list):
    # 当 telegram 发送 URL 失败时调用
    local_files = []
    headers = {
        "User-Agent":
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
    }

    for img_url in url_list:
        try:
            id_match = re.search(r'/(\d+)_p\d+', img_url)
            if id_match:
                illust_id = id_match.group(1)
                headers[
                    "Referer"] = f"https://www.pixiv.net/artworks/{illust_id}"

            r = requests.get(img_url, headers=headers, stream=True, timeout=20)
            if r.ok and r.headers.get("Content-Type", "").startswith("image/"):
                ext = img_url.split(".")[-1]
                with tempfile.NamedTemporaryFile(delete=False,
                                                 suffix=f".{ext}") as tmp:
                    for chunk in r.iter_content(8192):
                        tmp.write(chunk)
                    local_files.append(tmp.name)
            else:
                print(
                    f"Fallback download failed for {img_url}: {r.status_code}")

        except Exception as e:
            print(f"Fallback download error: {e}")

    return local_files


def fetch_pixiv_data(url: str):
    try:
        match = re.search(r"(?:artworks/|illust_id=)(\d+)(.*)", url)
        if not match: return [], "", "normal"

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

        headers = {"User-Agent": "Mozilla/5.0", "Referer": artwork_url}
        cookies = {}
        if PIXIV_SESSID: cookies["PHPSESSID"] = PIXIV_SESSID

        resp = requests.get(api_url,
                            headers=headers,
                            cookies=cookies,
                            timeout=10)
        if resp.status_code != 200: return [], "", "normal"

        data = resp.json()
        if data.get("error") or "body" not in data: return [], "", "normal"

        body = data["body"]
        title = escape_md_v2(body.get("title", ""))
        desc = html_to_markdown_v2(body.get("description", ""))
        tags = body.get("tags", {}).get("tags", [])
        tag_str = " ".join([
            f"\\#{escape_md_v2(t['tag'])}" for t in tags if isinstance(t, dict) and "tag" in t
        ])

        total_pages = int(body.get("pageCount", 1))

        urls_obj = body.get("urls", {})
        base_url_orig = urls_obj.get("original", "")
        base_url_reg = urls_obj.get("regular", "")

        if not base_url_orig: return [], "", "normal"

        selected_pages = parse_page_selection(selection_raw, total_pages)
        if not selected_pages:
            selected_pages = list(range(1, total_pages + 1))
            suffix = ""
        else:
            suffix = f" {','.join(map(str, selected_pages))}/{total_pages}" if total_pages > 1 else ""

        images = []
        
        for i in selected_pages:
            page_idx = i - 1
            
            curr_orig_url = base_url_orig.replace("_p0", f"_p{page_idx}")
            
            final_url = curr_orig_url

            # 只有在普通模式时才检查大小
            # 如果是 -o / -O 模式 app.py 会以文件发送，不受 10MB 限制，保持原图
            if parse_mode == "normal" and base_url_reg:
                try:
                    head_resp = requests.head(curr_orig_url, headers=headers, timeout=2)
                    if head_resp.ok:
                        size = int(head_resp.headers.get("Content-Length", 0))
                        if size > 10 * 1024 * 1024:
                            final_url = base_url_reg.replace("_p0", f"_p{page_idx}")
                            # print(f"Image too large ({size} bytes), switching to regular: {final_url}")
                except Exception:
                    pass

            images.append(final_url)

        if only_image:
            return images, "", parse_mode

        parts = [title + suffix]
        if not no_desc and desc: parts.append(desc)
        if not no_tag and tag_str: parts.append(tag_str)
        text = "\n".join(p for p in parts if p).strip()

        return images, text, parse_mode

    except Exception as e:
        print(f"Pixiv fetch error: {e}")
        return [], "", "normal"