import os
import re
from dotenv import load_dotenv
from telegram import Update, InputMediaPhoto
from telegram.ext import ApplicationBuilder, MessageHandler, CommandHandler, ContextTypes, filters

from twitter_parser import match_twitter_url, fetch_tweet_data
from pixiv_parser import match_pixiv_url, fetch_pixiv_data
from stats_manager import load_stats, save_stats

load_dotenv()
proxy = os.getenv("PROXY", "").strip()
if proxy:
    os.environ["HTTP_PROXY"] = proxy
    os.environ["HTTPS_PROXY"] = proxy
BOT_TOKEN = os.getenv("BOT_TOKEN")
stats = load_stats()
START_MESSAGE_MD = r"""
发送 *Twitter* 或 *Pixiv* 链接，机器人将自动解析并转发图片\.

Pixiv 解析指令

对于 Pixiv 链接，可在链接后添加以下参数：

1\. 分页 \(\+pages\)
用于获取指定图片，支持多种格式：

• `\+1`：第 1 张
• `\+1,2,5`：获取第 1、2、5 张
• `\+1-3`：获取第 1 到第 3 张

2\. 信息去除 \(\- 参数\)
用于移除图片描述信息：

• `\-all`：去除简介和 Tag
• `\-des`：去除简介
• `\-tag`：去除 Tag

混合使用示例: `https://www.pixiv.net/artworks/ID \+3 \-des`

其他命令

• `/stat`：查看总解析统计信息\.
"""


def escape_markdown_v2(text: str) -> str:
    text = text.replace("\\", "\\\\")
    for ch in r"_*[]()~`>#+-=|{}.!":
        text = text.replace(ch, "\\" + ch)
    return text


def make_markdown_caption(display_url: str, text: str):
    link_md = f"[{escape_markdown_v2(display_url)}]({display_url})"
    if not text:
        return link_md
    body_md = escape_markdown_v2(text)
    body_md_lines = "\n".join(
        ["> " + line if line else ">" for line in body_md.splitlines()])
    return f"{link_md}\n\n{body_md_lines}"


async def send_media(update, images, caption_md):
    try:
        if len(images) == 1:
            await update.message.reply_photo(images[0],
                                             caption=caption_md,
                                             parse_mode="MarkdownV2")
        elif len(images) > 1:
            group = []
            for i, img in enumerate(images[:10]):
                if i == 0:
                    group.append(
                        InputMediaPhoto(media=img,
                                        caption=caption_md,
                                        parse_mode="MarkdownV2"))
                else:
                    group.append(InputMediaPhoto(media=img))
            await update.message.reply_media_group(group)
        else:
            await update.message.reply_text(caption_md,
                                            parse_mode="MarkdownV2")
    except Exception:
        await update.message.reply_text(caption_md)


async def handle_message(update: Update, context: ContextTypes.DEFAULT_TYPE):
    text = (update.message.text or "").strip()

    tw_match = match_twitter_url(text)
    if tw_match:
        await handle_twitter(update, tw_match.group(1))
        return

    if match_pixiv_url(text):
        parts = text.split()
        pixiv_link = ""
        arg_tokens = []

        for i, token in enumerate(parts):
            if "pixiv.net/artworks/" in token:
                pixiv_link = token.strip()
                j = i + 1
                while j < len(parts) and (parts[j].startswith("+")
                                          or parts[j].startswith("-")):
                    arg_tokens.append(parts[j].strip())
                    j += 1
                break

        if not pixiv_link:
            return

        parse_input = f"{pixiv_link} {' '.join(arg_tokens)}".strip()

        clean_match = re.search(
            r"https?://(?:www\.)?pixiv\.net/(?:en/)?artworks/\d+", pixiv_link)
        display_url = clean_match.group(0) if clean_match else pixiv_link

        await handle_pixiv(update, parse_input, display_url)
        return


async def handle_twitter(update: Update, url: str):
    # x 域名就是石
    if "://x.com/" in url:
        url = url.replace("://x.com/", "://twitter.com/")

    await update.message.chat.send_action("upload_photo")
    images, tweet_text = fetch_tweet_data(url)

    if not images and not tweet_text:
        await update.message.reply_text("喵~ 这个推文抓不到, 可能被删掉或不公开")
        return

    stats["total_links"] += 1
    stats["total_images"] += len(images)
    save_stats(stats)

    caption_md = make_markdown_caption(url, tweet_text)
    await send_media(update, images, caption_md)


async def handle_pixiv(update: Update, parse_input: str, display_url: str):
    await update.message.chat.send_action("upload_photo")

    images, pixiv_text = fetch_pixiv_data(parse_input)
    if not images and not pixiv_text:
        await update.message.reply_text("喵~ Pixiv 作品抓不到, 可能被删掉或不公开")
        return

    stats["total_links"] += 1
    stats["total_images"] += len(images)
    save_stats(stats)

    caption_md = make_markdown_caption(display_url, pixiv_text)
    await send_media(update, images, caption_md)


async def start_command(update: Update, context: ContextTypes.DEFAULT_TYPE):
    await update.message.reply_text(START_MESSAGE_MD, parse_mode="MarkdownV2")


async def stat_command(update: Update, context: ContextTypes.DEFAULT_TYPE):
    msg = (f"统计信息：\n"
           f"• 总解析链接：{stats['total_links']} 条\n"
           f"• 总解析图片：{stats['total_images']} 张")
    await update.message.reply_text(msg)


def main():
    app = ApplicationBuilder().token(BOT_TOKEN).build()
    app.add_handler(CommandHandler("start", start_command))
    app.add_handler(CommandHandler("stat", stat_command))
    app.add_handler(
        MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))

    app.run_polling()


if __name__ == "__main__":
    main()
