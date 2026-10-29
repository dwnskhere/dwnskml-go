## AZML-Go

<p align="center">
    <a href="https://github.com/aquib4040/azml-go">
        <kbd>
            <img width="250" src="https://i.ibb.co/Mx2TJns0/file-00000000096c7207a8cef40bc07b65ef.png" alt="AZML-Go Logo">
        </kbd>
    </a>
</p>

<p align="center">
    <i>A high-performance, ultra-lightweight Telegram Leech Bot engineered in pure Go. Built on top of the Gotd MTProto framework, featuring multi-bot pool rotation, AIMD upload pacing, and in-place HatchZip extraction.</i>
</p>

<div align="center">

[![](https://img.shields.io/github/go-mod/go-version/aquib4040/azml-go?color=00ADD8&logo=go&labelColor=292c3b)](#)
[![](https://img.shields.io/github/repo-size/aquib4040/azml-go?color=green&label=Repo%20Size&labelColor=292c3b)](#)
[![](https://img.shields.io/github/commit-activity/m/aquib4040/azml-go?logo=github&labelColor=292c3b&label=Github%20Commits)](#)
[![](https://img.shields.io/github/license/aquib4040/azml-go?style=flat&label=License&labelColor=292c3b)](#)
[![](https://img.shields.io/github/stars/aquib4040/azml-go?style=flat&logo=github&label=Stars&labelColor=292c3b&color=yellow)](#)
[![](https://img.shields.io/badge/Telegram%20Channel-Join-9cf?logo=telegram&labelColor=292c3b)](https://t.me/canon_bots)
[![](https://img.shields.io/badge/Support%20Group-Join-9cf?logo=telegram&labelColor=292c3b)](https://t.me/canon_bots)

</div>

---

> [!IMPORTANT]
> **Pure Leech Bot Notice**: `AZML-Go` is exclusively designed for **Leeching** (downloading files from direct links, torrents, yt-dlp, and Telegram to upload straight back to Telegram). **Cloud Drive Mirroring (Google Drive, RClone, Mega) is not included for now** to maintain absolute focus on raw leeching speed, stability, and minimal resource usage.

---

## ⚡ Why AZML-Go? (Why We Wrote This in Go)

Python-based Telegram bots are feature-rich but frequently suffer under heavy multi-task concurrency, high memory consumption, and Python's Global Interpreter Lock (GIL). `AZML-Go` was rewritten from scratch in Go to eliminate these architectural bottlenecks:

| Feature / Metric | Python Bots (AZML / WZML) | AZML-Go |
|---|---|---|
| **Memory Footprint** | ~350 MB – 1 GB+ RAM | **~30 MB – 100 MB RAM** |
| **Concurrency Model** | Python Asyncio + GIL Bottlenecks | **Go Goroutines (True Native Multithreading)** |
| **MTProto Engine** | Pyrogram / Telethon (Python C-bindings) | **Gotd (Pure Go Native MTProto Implementation)** |
| **Binary & Dependencies** | Bulky virtualenvs, wheels, pip conflicts | **Single static compiled binary** |
| **FloodWait Management** | Sequential delays stall entire tasks | **Dynamic Worker Bot Pool & AIMD Pacing** |
| **Disk Space Usage** | 7-Zip requires 2x disk space for extraction | **In-Place HatchZip extraction with 7-Zip fallback** |

- **Goroutine Concurrency Without GIL**: Handle dozens of parallel chunks, active downloads, and uploads simultaneously without event loop starvation or thread-locking.
- **Ultra-Low Memory Footprint**: Idles at ~30 MB RAM and rarely exceeds 100 MB under multi-task workloads. Runs flawlessly on 512 MB free dynos and budget VPS servers.
- **Worker Bot Session Pool**: Dynamic round-robin rotation across multiple worker bots for both downloading and uploading, with automatic failover on FloodWait.
- **Adaptive AIMD Upload Pacing**: Additive Increase / Multiplicative Decrease pacing to sustain maximum transfer speeds right below Telegram's rate-limiting threshold.
- **In-Place HatchZip Extraction**: Extracts large archives in-place to save disk space and prevent storage exhaustion, with transparent fallback to 7-Zip.

---

## 🚀 Features

<details>
  <summary><b>⬇️ Download Engines</b></summary>

- **Aria2c**: High-speed multi-connection downloads for direct links, torrents, and magnet links.
- **Telegram MTProto**: Parallel chunk downloader with worker failover across bot session pools.
- **yt-dlp**: Seamless video and audio extraction from thousands of streaming sites with format selection.
- **Direct HTTP / DDL**: Fast streaming downloads with custom header parsing and filename resolution.

</details>

<details>
  <summary><b>⬆️ Telegram Leech Uploader</b></summary>

- **2 GB & 4 GB (Premium) Splitting**: Automatic splitting of oversized files at exact byte boundaries.
- **Customizable Output**: Per-user thumbnail, prefix, suffix, caption, and custom clean rename regex.
- **Media / Document Modes**: Upload videos as streamable Telegram video messages or uncompressed documents.
- **Channel & Supergroup Routing**: Dedicated dump / bin channel routing with clean `copyMessage` (no forwarded tags in user PMs).

</details>

<details>
  <summary><b>📦 Archive & Compression</b></summary>

- **HatchZip In-Place Extraction**: Replaces archive blocks on-the-fly to fit in tight disk environments.
- **Automatic 7-Zip Fallback**: Robust fallback with full password support and Zip Bomb safety checks.
- **Split Archive Detection**: Native detection of multi-volume `.part01.rar`, `.7z.001`, and `.zip.001` archives.

</details>

---

## 📋 Bot Commands

| Command | Description |
|---|---|
| `/leech [link/reply]` | Download and leech to Telegram |
| `/unzipleech [link/reply]` | Download, extract archive in-place, and leech contents |
| `/zipleech [link/reply]` | Download, compress into `.zip`, and leech |
| `/ytdl [link]` | Download video/audio via yt-dlp and leech to Telegram |
| `/ytdlzip [link]` | Download via yt-dlp, package as zip, and leech |
| `/status` | View real-time progress, speed, ETA, and queue status |
| `/cancel [gid]` | Cancel a specific running task |
| `/cancelall` | Cancel all running tasks |
| `/settings` | Open interactive user settings menu (Thumbnails, Prefixes, Fonts) |
| `/speedtest` | Test server download and upload bandwidth |
| `/ping` | Check bot responsiveness and latency |
| `/stats` | View bot and system resource usage |
| `/help` | Display command usage and instructions |

---

## 🛠️ Deployment Methods

Choose between **Heroku CLI (Server-side Build)**, **Automated GitHub Actions CD**, or **Linux VPS (Docker Compose)**.

---

### Method 1: Deploy on Heroku (Heroku CLI - Server Build)

Deploys `AZML-Go` using Heroku's **Container Stack** where Heroku builds the `Dockerfile` directly on their remote build servers via `heroku.yml` (no local Docker build required).

#### 1. Prerequisites
- Install the [Heroku CLI](https://devcenter.heroku.com/articles/heroku-cli):
  ```bash
  curl https://cli-assets.heroku.com/install.sh | sh
  ```

#### 2. Clone the Repository
```bash
git clone https://github.com/aquib4040/azml-go.git
cd azml-go
```

#### 3. Log In to Heroku
```bash
heroku login
```

#### 4. Create Heroku App with Container Stack
```bash
# If creating a new app:
heroku apps:create your-app-name --stack=container

# Or if linking an existing app:
heroku git:remote -a your-app-name
heroku stack:set container -a your-app-name
```

#### 5. Configure Environment Variables
Set your configuration variables on Heroku:
```bash
# Grab your live App Web URL to use as BASE_URL
APP_URL=$(heroku apps:info -a your-app-name --json | jq -r '.app.web_url')

# Set configuration variables
heroku config:set BOT_TOKEN="your_bot_token" -a your-app-name
heroku config:set OWNER_ID="your_telegram_id" -a your-app-name
heroku config:set TELEGRAM_API="your_api_id" -a your-app-name
heroku config:set TELEGRAM_HASH="your_api_hash" -a your-app-name
heroku config:set BASE_URL="$APP_URL" -a your-app-name
```

#### 6. Deploy Code (Remote Docker Build on Heroku)
When pushing to Heroku git, Heroku detects `heroku.yml` and builds the Docker container directly on their servers:
```bash
git push heroku master
```

#### 7. View Live Logs
```bash
heroku logs --tail -a your-app-name
```

---

### Method 2: Automated GitHub Actions CI/CD (Heroku)

`AZML-Go` includes a continuous deployment workflow ([`.github/workflows/cd.yml`](.github/workflows/cd.yml)) that automatically deploys to Heroku on every push to `master`.

#### What the CD Pipeline Automates:
1. Verifies if your Heroku app exists; if not, it automatically creates it with `--stack=container`.
2. Captures the live Heroku web URL and assigns it as `BASE_URL`.
3. Configures all main environment variables on Heroku.
4. Pushes the code to Heroku's Git repository to trigger the server-side Docker build.

#### Required GitHub Repository Secrets:
Go to your GitHub repository **Settings** ➔ **Secrets and variables** ➔ **Actions** and add:

| Secret Name | Description / Source |
|---|---|
| `HEROKU_API_KEY` | Your Heroku API key (run `heroku auth:token` in terminal) |
| `HEROKU_EMAIL` | Your Heroku account email address |
| `HEROKU_APP_NAME` | Your Heroku app name (e.g. `radiant-savannah-50281`) |
| `BOT_TOKEN` | Telegram Bot token from [@BotFather](https://t.me/BotFather) |
| `OWNER_ID` | Your Telegram User ID |
| `TELEGRAM_API` | Telegram API ID from [my.telegram.org](https://my.telegram.org) |
| `TELEGRAM_HASH` | Telegram API Hash from [my.telegram.org](https://my.telegram.org) |
| `DATABASE_URL` | *(Optional)* MongoDB connection string |

---

### Method 3: Deploy on Linux VPS (Docker Compose - Recommended)

The simplest and most resilient way to run `AZML-Go` on Ubuntu/Debian or any Linux server.

#### 1. Install Docker & Docker Compose
```bash
sudo apt update && sudo apt install -y docker.io docker-compose-v2 git curl
```

#### 2. Clone the Repository
```bash
git clone https://github.com/aquib4040/azml-go.git
cd azml-go
```

#### 3. Setup Environment Variables
Create your `.env` file in the project root:
```bash
cp .env.sample .env # Or create a new .env file
nano .env
```
Fill in the mandatory variables:
```env
BOT_TOKEN=123456789:ABCdefGhIJKlmNoPQRsTUVwxyZ
OWNER_ID=987654321
TELEGRAM_API=1234567
TELEGRAM_HASH=0123456789abcdef0123456789abcdef
DOWNLOAD_DIR=/usr/src/app/downloads/
BASE_URL_PORT=23434
```

#### 4. Build and Start the Container
```bash
docker compose up -d --build
```

#### 5. Monitor Logs and Status
```bash
# View real-time logs
docker compose logs -f

# Check container status
docker compose ps

# Stop or restart the bot
docker compose restart
docker compose down
```

---

## ⚙️ Configuration Variables

For Smart Auto Rename mode, provider setup, supported filename patterns, and fallback behavior, see [Smart Auto Rename documentation](docs/SMART_AUTORENAME.md). For Smart Caption V1/V2 layouts and the full caption-template placeholder reference, see [Smart Caption documentation](docs/SMART_CAPTION.md).

<details>
  <summary><b>Mandatory Variables</b></summary>

| Variable | Type | Description |
|---|---|---|
| `BOT_TOKEN` | `str` | Telegram Bot token obtained from [@BotFather](https://t.me/BotFather) |
| `OWNER_ID` | `int` | Telegram User ID of the bot owner |
| `TELEGRAM_API` | `int` | Telegram API ID from [my.telegram.org](https://my.telegram.org) |
| `TELEGRAM_HASH` | `str` | Telegram API Hash from [my.telegram.org](https://my.telegram.org) |

</details>

<details>
  <summary><b>Telegram Leech Settings</b></summary>

| Variable | Type | Description |
|---|---|---|
| `LEECH_SPLIT_SIZE` | `int` | Split threshold in bytes (default: `2097152000` for 2 GB; up to 4 GB with Premium session) |
| `USER_SESSION_STRING` | `str` | Pyrogram / Gotd user session string (required for 4 GB premium uploads) |
| `AS_DOCUMENT` | `bool` | Default upload format (`true` for Document, `false` for Media) |
| `EQUAL_SPLITS` | `bool` | Split oversized files into equal-sized parts |
| `MEDIA_GROUP` | `bool` | Send split parts together as a Telegram media group |
| `BOT_PM` | `bool` | Send files directly to the user's private chat |
| `LEECH_LOG_ID` | `int` | Telegram Channel/Group ID to forward completed leech logs |
| `LINKS_LOG_ID` | `int` | Telegram Channel/Group ID to forward task links |
| `LEECH_FILENAME_PREFIX`| `str` | Custom prefix prepended to all leeched filenames |
| `LEECH_FILENAME_SUFFIX`| `str` | Custom suffix appended before the file extension |
| `LEECH_FILENAME_CAPTION`| `str` | Custom caption template for leeched files |
| `LEECH_FILENAME_REMNAME`| `str` | Regex pattern to strip unwanted words from filenames |

</details>

<details>
  <summary><b>Worker Bot Pool (Multi-Bot MTProto)</b></summary>

| Variable | Type | Description |
|---|---|---|
| `WORKER_BOT_TOKENS` | `str` | Space or comma-separated bot tokens for download/upload worker rotation |
| `DUMP_CHAT_ID` | `int` | Storage/bin channel ID where worker bots stage chunks and copy messages |

</details>

<details>
  <summary><b>Aria2c & Downloads</b></summary>

| Variable | Type | Description |
|---|---|---|
| `DOWNLOAD_DIR` | `str` | Directory path for downloading files (default: `/usr/src/app/downloads/`) |
| `TORRENT_TIMEOUT` | `int` | Dead torrent timeout in seconds |
| `BASE_URL` | `str` | Public web address for file download or web endpoints |
| `BASE_URL_PORT` | `int` | Port for the web service (default: `23434`) |

</details>

<details>
  <summary><b>Queue & Limits</b></summary>

| Variable | Type | Description |
|---|---|---|
| `QUEUE_ALL` | `int` | Maximum concurrent tasks allowed across the bot |
| `QUEUE_DOWNLOAD` | `int` | Maximum concurrent active download tasks |
| `QUEUE_UPLOAD` | `int` | Maximum concurrent active upload tasks |
| `LEECH_LIMIT` | `int` | Maximum allowed leech task size in GB |
| `TORRENT_LIMIT` | `int` | Maximum allowed torrent task size in GB |
| `DIRECT_LIMIT` | `int` | Maximum allowed direct download size in GB |
| `STORAGE_THRESHOLD` | `int` | Minimum free disk space in GB before blocking new downloads |

</details>

<details>
  <summary><b>Security & Permissions</b></summary>

| Variable | Type | Description |
|---|---|---|
| `AUTHORIZED_CHATS` | `str` | Space-separated chat/group IDs authorized to use the bot |
| `SUDO_USERS` | `str` | Space-separated user IDs granted sudo admin privileges |
| `BLACKLIST_USERS` | `str` | Space-separated user IDs blocked from using the bot |
| `TOKEN_TIMEOUT` | `int` | Shortener token validity duration in seconds |
| `LOGIN_PASS` | `str` | Passphrase to bypass shortener token verification |

</details>

---

## 👥 Authors & Credits

| | |
|:---:|:---:|
| <img width="80" src="https://github.com/aquib4040.png"> | <img width="80" src="https://github.com/isseisenpai0.png"> |
| [`Aquib`](https://github.com/aquib4040) | [`issei`](https://github.com/isseisenpai0) |
| Founder & Author | Maintainer |

- **Gotd Team**: For the pure Go [Gotd MTProto library](https://github.com/gotd/td).
- **AZML Python**: Architectural inspiration and feature design.

---

<p align="center">Licensed under MIT &nbsp;·&nbsp; PRs and contributions welcome</p>
