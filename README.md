# Poe Herald

A Discord bot that links Path of Exile accounts via OAuth and exposes slash commands. Built with Go.

_This product isn't affiliated with or endorsed by Grinding Gear Games in any way._

## How To Use

A bot invite link will be provided for public usage once the first major version is released!

### Linking

Run **`/link-account`** (in a server or in a DM with the bot). The bot replies with an OAuth link where you verify your GGG account and permissions. When that succeeds, you get a DM confirmation. Then run **`/link-character`** and pick a character from your account for tracking (up to two characters; linking a third starts a swap flow).

### Slash commands

| Command | Where | What it does |
| -------- | ----- | ------------- |
| `/link-account` | Anywhere | Starts OAuth to link your Path of Exile account. If already linked, use `/link-character` instead. |
| `/link-character` | Anywhere | Opens a menu to link a league character for level milestones, deaths, and chat context. Requires a linked account first. |
| `/remove-character` | Anywhere | Ephemeral menu to unlink one of your tracked characters. |
| `/set-channel` | Server only | Pick the guild text channel where the bot posts announcements (link celebrations, milestones, etc.). |
| `/chat` | Anywhere | **`question`** (required): ask build or mechanics questions. Uses wiki RAG plus, when you choose it, a linked character snapshot or a **`https://pobb.in/...`** paste from the follow-up UI. Replies may be sent in DM. Requires `COHERE_API_KEY` on the bot; if missing, chat reports unavailable. |

You can also DM the bot a plain question (no slash) for wiki-grounded answers when RAG is configured.
