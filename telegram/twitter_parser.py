import re
import os
import tempfile
import requests

TWITTER_PATTERN = re.compile(
    r'(https?://(?:www\.)?(?:twitter|x)\.com/\w+/status/\d+)')
IMAGE_URL_PATTERN = re.compile(r'https?://pbs\.twimg\.com/media/([^.?]+)')


def match_twitter_url(text: str):
    return TWITTER_PATTERN.search(text)


def fetch_tweet_data(url: str):
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
        media = tweet.get("media", {})
        media_list = media.get("all", [])

        for m in media_list:
            if "url" not in m:
                continue

            api_media_url = m["url"]
            media_type = m.get("type")

            final_url = ""
            suffix = ""

            if media_type == "photo":
                url_match = IMAGE_URL_PATTERN.search(api_media_url)
                if url_match:
                    filename = url_match.group(1)
                    final_url = f"https://pbs.twimg.com/media/{filename}.png?name=4096x4096"
                else:
                    final_url = api_media_url
                suffix = ".png"

            elif media_type == "video" or media_type == "gif":
                final_url = api_media_url
                suffix = ".mp4"

            else:
                continue

            try:
                r = requests.get(final_url, stream=True, timeout=15)

                content_type = r.headers.get("Content-Type", "")
                if not r.ok or (not content_type.startswith("image/")
                                and not content_type.startswith("video/")):
                    print(
                        f"Skipping download, bad status or content type: {r.status_code} / {content_type}"
                    )
                    continue

                with tempfile.NamedTemporaryFile(delete=False,
                                                 suffix=suffix) as tmp:
                    for chunk in r.iter_content(8192):
                        tmp.write(chunk)
                    tmp_path = tmp.name
                media_files.append(tmp_path)
            except Exception as download_e:
                print(f"Failed to download media {final_url}: {download_e}")
                continue

        return media_files, text

    except Exception as e:
        print(f"Twitter parse error: {e}")
        return [], ""
