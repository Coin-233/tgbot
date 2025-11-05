import os
import re
from dotenv import load_dotenv
from telegram import Update, InputMediaPhoto, InputPaidMediaVideo
from telegram.ext import ApplicationBuilder, MessageHandler, CommandHandler, ContextTypes, filters

from twitter_parser import match_twitter_url, fetch_tweet_data
from pixiv_parser import match_pixiv_url, fetch_pixiv_data
from stats_manager import load_stats, save_stats
from file_sender import send_files_as_documents

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

3\. 发送原图
• `\-o`: 仅发送原图文件, 不含任何信息
• `\-O`: 发送原图文件, 并附带作品信息

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


async def send_media(update, images, caption_md, skip_size_check=False):
    try:
        total = len(images)
        if total == 0:
            if caption_md:
                await update.message.reply_text(caption_md,
                                                parse_mode="MarkdownV2")
            return

        MAX_PHOTO_SIZE = 10 * 1024 * 1024  # 10MB 图片限制

        if total == 1:
            file_path = images[0]
            file_size = os.path.getsize(file_path)

            if file_path.endswith(".mp4"):
                await update.message.reply_video(file_path,
                                                 caption=caption_md,
                                                 parse_mode="MarkdownV2")

            elif not skip_size_check and file_size > MAX_PHOTO_SIZE:
                await update.message.reply_document(file_path,
                                                    caption=caption_md,
                                                    parse_mode="MarkdownV2")
            else:
                await update.message.reply_photo(file_path,
                                                 caption=caption_md,
                                                 parse_mode="MarkdownV2")
            return

        media_group = []
        for i, file_path in enumerate(images):
            caption = caption_md if i == 0 else None
            parse_mode = "MarkdownV2" if i == 0 else None

            if file_path.endswith(".mp4"):
                media_group.append(
                    InputMediaVideo(media=file_path,
                                    caption=caption,
                                    parse_mode=parse_mode))
            else:
                media_group.append(
                    InputMediaPhoto(media=file_path,
                                    caption=caption,
                                    parse_mode=parse_mode))

        for i in range(0, total, 10):
            batch = media_group[i:i + 10]
            await update.message.reply_media_group(batch)

    except Exception as e:
        print(f"send_media error: {e}")
        await update.message.reply_text(caption_md, parse_mode="MarkdownV2")


async def handle_message(update: Update, context: ContextTypes.DEFAULT_TYPE):

    msg = update.message
    if not msg:
        return

    text = (msg.text or msg.caption or "").strip()

    if not text:
        return

    is_channel_identity_post = msg.sender_chat is not None and msg.sender_chat.type == 'channel'

    is_auto_forwarded_post = msg.is_automatic_forward is True

    force_original_file_only = is_channel_identity_post or is_auto_forwarded_post

    tw_match = match_twitter_url(text)
    if tw_match:
        parts = text.split()
        twitter_link = ""
        arg_tokens = []

        for i, token in enumerate(parts):
            if "twitter.com/" in token or "x.com/" in token:
                twitter_link = token.strip()
                j = i + 1
                while j < len(parts) and parts[j].startswith("-"):
                    arg_tokens.append(parts[j].strip())
                    j += 1
                break

        if not twitter_link:
            return

        parse_input = f"{twitter_link} {' '.join(arg_tokens)}".strip()

        await handle_twitter(update,
                             parse_input,
                             force_original_file_only=force_original_file_only)
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

        await handle_pixiv(update,
                           parse_input,
                           display_url,
                           force_original_file_only=force_original_file_only)
        return


async def handle_twitter(update: Update,
                         parse_input: str,
                         force_original_file_only: bool = False):
    if "://x.com/" in parse_input:
        parse_input = parse_input.replace("://x.com/", "://twitter.com/")

    parts = parse_input.split()
    url = parts[0]
    params = parts[1:] if len(parts) > 1 else []

    parse_mode = "normal"
    if "-o" in params:
        parse_mode = "file_only"
    elif "-O" in params:
        parse_mode = "file_with_info"

    await update.message.chat.send_action("upload_document")
    fetch_force_original = force_original_file_only or parse_mode in [
        "file_only", "file_with_info"
    ]
    images, tweet_text = fetch_tweet_data(
        url, force_original_file_only=fetch_force_original)

    if not images and not tweet_text:
        await update.message.reply_text("喵~ 这个推文抓不到, 可能被删掉或不公开")
        return

    stats["total_links"] += 1
    stats["total_images"] += len(images)
    save_stats(stats)

    if force_original_file_only or parse_mode == "file_only":
        await send_files_as_documents(update, images, caption_md=None)

    elif parse_mode == "file_with_info":
        caption_md = make_markdown_caption(url, tweet_text)
        await send_files_as_documents(update, images, caption_md=caption_md)

    else:
        caption_md = make_markdown_caption(url, tweet_text)
        await send_media(update,
                         images,
                         caption_md=caption_md,
                         skip_size_check=force_original_file_only)

    for f in images:
        if os.path.exists(f):
            os.remove(f)


async def handle_pixiv(update: Update,
                       parse_input: str,
                       display_url: str,
                       force_original_file_only: bool = False):
    await update.message.chat.send_action("upload_photo")

    images, pixiv_text, parse_mode = fetch_pixiv_data(parse_input)

    if not images:
        await update.message.reply_text("喵~ Pixiv 作品抓不到, 可能被删掉或不公开")
        return

    stats["total_links"] += 1
    stats["total_images"] += len(images)
    save_stats(stats)

    if force_original_file_only or parse_mode == "file_only":
        await send_files_as_documents(update, images, caption_md=None)

    elif parse_mode == "file_with_info":
        caption_md = make_markdown_caption(display_url, pixiv_text)
        await send_files_as_documents(update, images, caption_md=caption_md)

    else:  # parse_mode == "normal"
        caption_md = make_markdown_caption(display_url, pixiv_text)
        await send_media(update,
                         images,
                         caption_md=caption_md,
                         skip_size_check=force_original_file_only)


async def start_command(update: Update, context: ContextTypes.DEFAULT_TYPE):
    await update.message.reply_text(START_MESSAGE_MD, parse_mode="MarkdownV2")


async def stat_command(update: Update, context: ContextTypes.DEFAULT_TYPE):
    msg = (f"统计信息：\n"
           f"• 总解析链接:  {stats['total_links']} 条\n"
           f"• 总解析文件:  {stats['total_images']} 个")
    await update.message.reply_text(msg)


def main():
    app = ApplicationBuilder().token(BOT_TOKEN).build()
    app.add_handler(CommandHandler("start", start_command))
    app.add_handler(CommandHandler("stat", stat_command))
    app.add_handler(
        MessageHandler((filters.TEXT | filters.CAPTION) & ~filters.COMMAND,
                       handle_message))

    app.run_polling()


if __name__ == "__main__":
    main()
