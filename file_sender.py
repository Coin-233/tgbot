from telegram import Update, InputMediaDocument

async def send_files_as_documents(update: Update, images: list[str], caption_md: str = None):
    try:
        total = len(images)
        if total == 0:
            if caption_md:
                await update.message.reply_text(caption_md, parse_mode="MarkdownV2")
            return

        if total == 1:
            await update.message.reply_document(images[0],
                                                caption=caption_md,
                                                parse_mode="MarkdownV2" if caption_md else None)
            return

        media_group = []
        for i, img_url in enumerate(images):
            current_caption = caption_md if i == 0 and caption_md else None
            media_group.append(
                InputMediaDocument(
                    media=img_url,
                    caption=current_caption,
                    parse_mode="MarkdownV2" if current_caption else None
                )
            )

        for i in range(0, total, 10):
            batch = media_group[i:i + 10]
            await update.message.reply_media_group(batch)

    except Exception as e:
        print(f"send_files_as_documents error: {e}")
        if caption_md:
            try:
                await update.message.reply_text(caption_md, parse_mode="MarkdownV2")
            except Exception as e2:
                print(f"Fallback text reply error: {e2}")