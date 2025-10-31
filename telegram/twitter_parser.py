import re
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

        # FXtwitter
        api_url = f"https://api.fxtwitter.com/{user}/status/{tweet_id}"
        resp = requests.get(api_url, timeout=10)
        if resp.status_code != 200:
            return [], ""

        data = resp.json()
        if "tweet" not in data:
            return [], ""

        tweet = data["tweet"]
        text = re.sub(r'\s+$', '', tweet.get("text", ""))

        media_urls = []
        media = tweet.get("media", {})

        media_list = media.get("all", []) or media.get("photos", [])

        for m in media_list:
            if "url" in m:
                api_media_url = m["url"]

                url_match = IMAGE_URL_PATTERN.search(api_media_url)

                if url_match:
                    filename = url_match.group(1)
                    final_url = f"https://pbs.twimg.com/media/{filename}.png?name=4096x4096"
                    media_urls.append(final_url)
                else:
                    media_urls.append(api_media_url)

        return media_urls, text
    except Exception as e:
        print(f"Twitter parse error: {e}")
        return [], ""
