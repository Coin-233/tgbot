import os
from telegram import Update, InputMediaDocument


async def send_files_as_documents(update: Update,
                                  images: list[str],
                                  caption_md: str = None,
                                  work_id: str = None):
    try:
        total = len(images)
        if total == 0:
            if caption_md:
                await update.message.reply_text(caption_md,
                                                parse_mode="MarkdownV2")
            return

        if total == 1:
            file_path = images[0]

            ext = os.path.splitext(file_path)[1]
            if not ext:
                ext = ".jpg"
            custom_filename = f"{work_id}{ext}" if work_id else None

            await update.message.reply_document(
                file_path,
                caption=caption_md,
                parse_mode="MarkdownV2" if caption_md else None,
                filename=custom_filename)
            return

        media_group = []
        for i, img_url in enumerate(images):
            current_caption = caption_md if i == 0 and caption_md else None

            ext = os.path.splitext(img_url)[1]
            if not ext:
                ext = ".jpg"
            custom_filename = f"{work_id}_{i+1}{ext}" if work_id else None

            media_group.append(
                InputMediaDocument(
                    media=img_url,
                    caption=current_caption,
                    parse_mode="MarkdownV2" if current_caption else None,
                    filename=custom_filename))

        for i in range(0, total, 10):
            batch = media_group[i:i + 10]
            await update.message.reply_media_group(batch)

    except Exception as e:
        print(f"send_files_as_documents error: {e}")
        if caption_md:
            try:
                await update.message.reply_text(caption_md,
                                                parse_mode="MarkdownV2")
            except Exception as e2:
                print(f"Fallback text reply error: {e2}")
