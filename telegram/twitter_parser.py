import re
import requests

TWITTER_PATTERN = re.compile(
    r'(https?://(?:www\.)?(?:twitter|x)\.com/\w+/status/\d+)')


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
        if "photos" in media:
            for p in media["photos"]:
                if "url" in p:
                    media_urls.append(p["url"])
        elif "all" in media:
            for m in media["all"]:
                if m.get("type") == "photo" and "url" in m:
                    media_urls.append(m["url"])

        return media_urls, text
    except Exception:
        return [], ""
