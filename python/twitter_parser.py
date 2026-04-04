import re
import os
import tempfile
import requests

TWITTER_PATTERN = re.compile(
    r'(https?://(?:www\.)?(?:twitter|x)\.com/\w+/status/\d+)')
IMAGE_URL_PATTERN = re.compile(r'https?://pbs\.twimg\.com/media/([^.?]+)')


def match_twitter_url(text: str):
    return TWITTER_PATTERN.search(text)


def fetch_tweet_data(url: str, force_original_file_only: bool = False):
    try:
        match = re.search(r"(?:twitter|x)\.com/([^/]+)/status/(\d+)", url)
        if not match:
            return [], ""
        user, tweet_id = match.groups()

        api_url = f"https://api.fxtwitter.com/{user}/status/{tweet_id}"
        resp = requests.get(api_url, timeout=10)
        if resp.status_code != 200:
            return [], ""

        data = resp.json()
        if "tweet" not in data:
            return [], ""

        tweet = data["tweet"]
        text = re.sub(r'\s+$', '', tweet.get("text", ""))

        media_files = []
        media_list = tweet.get("media", {}).get("all", [])

        for m in media_list:
            api_media_url = m.get("url")
            media_type = m.get("type")
            if not api_media_url or not media_type:
                continue

            if media_type == "photo":
                url_match = IMAGE_URL_PATTERN.search(api_media_url)
                if url_match:
                    filename = url_match.group(1)
                    png_url = f"https://pbs.twimg.com/media/{filename}.png?name=4096x4096"
                    jpg_url = f"https://pbs.twimg.com/media/{filename}.jpg?name=large"
                else:
                    alt_match = re.search(r'/media/([^.?]+)', api_media_url)
                    if alt_match:
                        filename = alt_match.group(1)
                        png_url = f"https://pbs.twimg.com/media/{filename}.png?name=4096x4096"
                        jpg_url = f"https://pbs.twimg.com/media/{filename}.jpg?name=large"
                    else:
                        png_url = api_media_url
                        jpg_url = api_media_url

                # 先尝试 PNG
                try:
                    r = requests.get(png_url, stream=True, timeout=15)
                    if r.ok and r.headers.get("Content-Type",
                                              "").startswith("image/"):
                        with tempfile.NamedTemporaryFile(delete=False,
                                                         suffix=".png") as tmp:
                            for chunk in r.iter_content(8192):
                                tmp.write(chunk)
                            tmp_path = tmp.name

                        # 仅普通模式下检测大小
                        if (not force_original_file_only
                            ) and os.path.getsize(tmp_path) > 10 * 1024 * 1024:
                            os.remove(tmp_path)
                            raise Exception
                        media_files.append(tmp_path)
                        continue
                except Exception:
                    pass

                try:
                    r = requests.get(jpg_url, stream=True, timeout=15)
                    if r.ok and r.headers.get("Content-Type",
                                              "").startswith("image/"):
                        with tempfile.NamedTemporaryFile(delete=False,
                                                         suffix=".jpg") as tmp:
                            for chunk in r.iter_content(8192):
                                tmp.write(chunk)
                            tmp_path = tmp.name
                        media_files.append(tmp_path)
                except Exception:
                    pass
                continue

            if media_type in ["video", "gif"]:
                try:
                    r = requests.get(api_media_url, stream=True, timeout=15)
                    if r.ok and r.headers.get("Content-Type",
                                              "").startswith("video/"):
                        with tempfile.NamedTemporaryFile(delete=False,
                                                         suffix=".mp4") as tmp:
                            for chunk in r.iter_content(8192):
                                tmp.write(chunk)
                            tmp_path = tmp.name
                        media_files.append(tmp_path)
                except Exception:
                    pass

        return media_files, text

    except Exception:
        return [], ""
