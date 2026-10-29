package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

type Config struct {
	BotToken                    string
	OwnerID                     int64
	OwnerUsername               string
	TelegramAPI                 int
	TelegramHash                string
	UserSessionString           string
	DatabaseURL                 string
	DownloadDir                 string
	CmdSuffix                   string
	AuthorizedChats             []int64
	SudoUsers                   []int64
	BlacklistUsers              []int64
	StatusLimit                 int
	StatusUpdateInterval        int
	AutoDeleteMessageDuration   int
	LeechSplitSize              int64
	EqualSplits                 bool
	AsDocument                  bool
	MediaGroup                  bool
	BotPM                       bool
	CapFont                     string
	LeechFilenamePrefix         string
	LeechFilenameSuffix         string
	LeechFilenameCaption        string
	LeechFilenameRemname        string
	LeechFilenameRemnameAuto    string
	LeechFilenameRemnameRegex   string
	SmartRenameEnabled          bool
	SmartRenameLookupEnabled    bool
	SmartRenameProviderPriority string
	SmartRenameMaxChars         int
	SmartRenameProbeTimeout     int
	SmartRenameLookupTimeout    int
	LeechLogID                  int64
	LinksLogID                  int64
	TorrentTimeout              int
	Aria2Port                   int
	BaseURL                     string
	BaseURLPort                 int
	MegaEmail                   string
	MegaPassword                string
	TokenTimeout                int64
	LoginPass                   string
	Shortener                   string
	ShortenerAPI                string
	ShortenerEnabled            bool
	TokenInPM                   bool
	ScreenshotsMode             bool
	ShowMediaInfo               bool
	SaveMsg                     bool
	SourceLink                  bool
	Metadata                    string
	TitleName                   string
	AuthorName                  string
	AuthorURL                   string
	BotTheme                    string
	ShowExtraCmds               bool
	Timezone                    string
	EnableFFmpegCmds            bool
	FFmpegEncoding              bool
	TMDBAccessToken             string
	AutoThumbnail               bool
	CoverImage                  string
	DDLAPI                      string
	QueueAll                    int
	QueueDownload               int
	QueueUpload                 int
	LeechLimit                  int64 // GB
	TorrentLimit                int64 // GB
	DirectLimit                 int64 // GB
	YtdlpLimit                  int64 // GB
	MegaLimit                   int64 // GB
	ArchiveLimit                int64 // GB
	ExtractLimit                int64 // GB
	DailyLeechLimit             int64 // GB
	DailyTaskLimit              int   // count
	StorageThreshold            int64 // GB
	WorkerBotTokens             []string
	DumpChatID                  int64
}

var (
	GlobalConfig  Config
	configLock    sync.RWMutex
	MaxSplitSize  int64 = 2097152000 // 1.91 GB default
	IsPremiumUser bool  = false
)

func SetIsPremiumUser(isPrem bool) {
	configLock.Lock()
	defer configLock.Unlock()
	IsPremiumUser = isPrem
	if isPrem {
		MaxSplitSize = 4194304000 // 3.91 GB
	} else {
		MaxSplitSize = 2097152000 // 1.91 GB
	}
	if GlobalConfig.LeechSplitSize == 0 || GlobalConfig.LeechSplitSize > MaxSplitSize || GlobalConfig.LeechSplitSize == 2097152000 || GlobalConfig.LeechSplitSize == 4194304000 {
		GlobalConfig.LeechSplitSize = MaxSplitSize
	}
}

func GetMaxSplitSize() int64 {
	configLock.RLock()
	defer configLock.RUnlock()
	return MaxSplitSize
}

func LoadConfig(path string) (*Config, error) {
	configLock.Lock()
	defer configLock.Unlock()

	_ = godotenv.Load(path)
	_ = godotenv.Load(".env")
	_ = godotenv.Load("config.env")

	GlobalConfig = Config{
		BotToken:                    getEnv("BOT_TOKEN", ""),
		OwnerID:                     getEnvInt64("OWNER_ID", 0),
		OwnerUsername:               strings.TrimPrefix(getEnv("OWNER_USERNAME", "exe_aquib"), "@"),
		TelegramAPI:                 getEnvInt("TELEGRAM_API", 0),
		TelegramHash:                getEnv("TELEGRAM_HASH", ""),
		UserSessionString:           getEnv("USER_SESSION_STRING", ""),
		DatabaseURL:                 getEnv("DATABASE_URL", ""),
		DownloadDir:                 cleanDir(getEnv("DOWNLOAD_DIR", "/usr/src/app/downloads/")),
		CmdSuffix:                   getEnv("CMD_SUFFIX", ""),
		AuthorizedChats:             parseIDList(getEnv("AUTHORIZED_CHATS", "")),
		SudoUsers:                   parseIDList(getEnv("SUDO_USERS", "")),
		BlacklistUsers:              parseIDList(getEnv("BLACKLIST_USERS", "")),
		StatusLimit:                 getEnvInt("STATUS_LIMIT", 4),
		StatusUpdateInterval:        getEnvInt("STATUS_UPDATE_INTERVAL", 10),
		AutoDeleteMessageDuration:   getEnvInt("AUTO_DELETE_MESSAGE_DURATION", 60),
		LeechSplitSize:              getEnvInt64("LEECH_SPLIT_SIZE", 2097152000), // ~2GB
		EqualSplits:                 getEnvBool("EQUAL_SPLITS", false),
		AsDocument:                  getEnvBool("AS_DOCUMENT", true),
		MediaGroup:                  getEnvBool("MEDIA_GROUP", false),
		BotPM:                       getEnvBool("BOT_PM", false),
		CapFont:                     getEnv("CAP_FONT", "code"),
		LeechFilenamePrefix:         getEnv("LEECH_FILENAME_PREFIX", ""),
		LeechFilenameSuffix:         getEnv("LEECH_FILENAME_SUFFIX", ""),
		LeechFilenameCaption:        getEnv("LEECH_FILENAME_CAPTION", ""),
		LeechFilenameRemname:        getEnv("LEECH_FILENAME_REMNAME", ""),
		LeechFilenameRemnameAuto:    getEnv("LEECH_FILENAME_REMNAME_AUTO", ""),
		LeechFilenameRemnameRegex:   getEnv("LEECH_FILENAME_REMNAME_REGEX", ""),
		SmartRenameEnabled:          getEnvBool("SMART_AUTORENAME_ENABLED", true),
		SmartRenameLookupEnabled:    getEnvBool("SMART_AUTORENAME_LOOKUP_ENABLED", true),
		SmartRenameProviderPriority: getEnv("SMART_AUTORENAME_PROVIDER_PRIORITY", "kitsu,tmdb,cinemeta"),
		SmartRenameMaxChars:         getEnvInt("SMART_AUTORENAME_MAX_CHARS", 60),
		SmartRenameProbeTimeout:     getEnvInt("SMART_AUTORENAME_PROBE_TIMEOUT", 12),
		SmartRenameLookupTimeout:    getEnvInt("SMART_AUTORENAME_LOOKUP_TIMEOUT", 8),
		LeechLogID:                  getEnvInt64("LEECH_LOG_ID", 0),
		LinksLogID:                  getEnvInt64("LINKS_LOG_ID", 0),
		TorrentTimeout:              getEnvInt("TORRENT_TIMEOUT", 0),
		Aria2Port:                   getEnvInt("ARIA2_PORT", 6810),
		BaseURL:                     getEnv("BASE_URL", ""),
		BaseURLPort:                 getEnvInt("PORT", getEnvInt("BASE_URL_PORT", 23434)),
		MegaEmail:                   getEnv("MEGA_EMAIL", ""),
		MegaPassword:                getEnv("MEGA_PASSWORD", ""),
		TokenTimeout:                getEnvInt64("TOKEN_TIMEOUT", 0),
		LoginPass:                   getEnv("LOGIN_PASS", ""),
		Shortener:                   getEnv("SHORTENER", ""),
		ShortenerAPI:                getEnv("SHORTENER_API", ""),
		ShortenerEnabled:            getEnvBool("SHORTENER_ENABLED", true),
		TokenInPM:                   getEnvBool("TOKEN_IN_PM", true),
		ScreenshotsMode:             getEnvBool("SCREENSHOTS_MODE", false),
		ShowMediaInfo:               getEnvBool("SHOW_MEDIAINFO", false),
		SaveMsg:                     getEnvBool("SAVE_MSG", false),
		SourceLink:                  getEnvBool("SOURCE_LINK", false),
		Metadata:                    getEnv("METADATA", ""),
		TitleName:                   getEnv("TITLE_NAME", "AZML-Go"),
		AuthorName:                  getEnv("AUTHOR_NAME", "AZML-Go"),
		AuthorURL:                   getEnv("AUTHOR_URL", "https://t.me/CANON_BOTS"),
		BotTheme:                    getEnv("BOT_THEME", "minimal"),
		ShowExtraCmds:               getEnvBool("SHOW_EXTRA_CMDS", true),
		Timezone:                    getEnv("TIMEZONE", "Asia/Kolkata"),
		EnableFFmpegCmds:            getEnvBool("ENABLE_FFMPEG_CMDS", true),
		FFmpegEncoding:              getEnvBool("FFMPEG_ENCODING", false),
		TMDBAccessToken:             getEnv("TMDB_ACCESS_TOKEN", ""),
		AutoThumbnail:               getEnvBool("AUTO_THUMBNAIL", false),
		CoverImage:                  getEnv("COVER_IMAGE", "https://graph.org/file/60f9f8bcb97d27f76f5c0.jpg"),
		DDLAPI:                      getEnv("DDL_API", getEnv("THUNDER_API", "")),
		QueueAll:                    getEnvInt("QUEUE_ALL", getEnvInt("BOT_MAX_TASKS", 4)),
		QueueDownload:               getEnvInt("QUEUE_DOWNLOAD", getEnvInt("USER_MAX_TASKS", 2)),
		QueueUpload:                 getEnvInt("QUEUE_UPLOAD", 2),
		LeechLimit:                  getEnvInt64("LEECH_LIMIT", 0),
		TorrentLimit:                getEnvInt64("TORRENT_LIMIT", 0),
		DirectLimit:                 getEnvInt64("DIRECT_LIMIT", 0),
		YtdlpLimit:                  getEnvInt64("YTDLP_LIMIT", 0),
		MegaLimit:                   getEnvInt64("MEGA_LIMIT", 0),
		ArchiveLimit:                getEnvInt64("ARCHIVE_LIMIT", 0),
		ExtractLimit:                getEnvInt64("EXTRACT_LIMIT", 0),
		DailyLeechLimit:             getEnvInt64("DAILY_LEECH_LIMIT", 0),
		DailyTaskLimit:              getEnvInt("DAILY_TASK_LIMIT", 0),
		StorageThreshold:            getEnvInt64("STORAGE_THRESHOLD", 1), // Default 1GB safety cushion
		DumpChatID:                  getEnvInt64("DUMP_CHAT_ID", getEnvInt64("BIN_CHANNEL", getEnvInt64("STORAGE_CHANNEL", getEnvInt64("STORAGE_CHANNELS", getEnvInt64("LEECH_LOG_ID", 0))))),
	}

	// Resolve main bot token
	mainToken := strings.Trim(strings.TrimSpace(getEnv("BOT_TOKEN", getEnv("MAIN_BOT_TOKEN", ""))), `"'`)
	if mainToken == "" {
		for _, candidate := range parseTokenString(getEnv("BOT_TOKENS", getEnv("MULTI_TOKENS", getEnv("MULTI_TOKEN", "")))) {
			if candidate != "" {
				mainToken = candidate
				break
			}
		}
	}
	GlobalConfig.BotToken = mainToken

	// Collect multi-bot worker tokens (strictly excluding the main BotToken)
	seenTokens := make(map[string]bool)
	var workerTokens []string

	tokenSources := []string{
		getEnv("BOT_TOKENS", ""),
		getEnv("MULTI_TOKENS", ""),
		getEnv("MULTI_TOKEN", ""),
		getEnv("TOKENS", ""),
		getEnv("WORKER_TOKENS", ""),
	}

	for _, src := range tokenSources {
		for _, t := range parseTokenString(src) {
			if t != "" && t != mainToken && !seenTokens[t] {
				workerTokens = append(workerTokens, t)
				seenTokens[t] = true
			}
		}
	}

	for i := 1; i <= 100; i++ {
		key := fmt.Sprintf("MULTI_TOKEN%d", i)
		if val := strings.Trim(strings.TrimSpace(os.Getenv(key)), `"'`); val != "" {
			if val != mainToken && !seenTokens[val] {
				workerTokens = append(workerTokens, val)
				seenTokens[val] = true
			}
		}
	}
	GlobalConfig.WorkerBotTokens = workerTokens
	if len(workerTokens) > 0 {
		log.Printf("[Config] Loaded %d Multi-Bot Worker tokens for parallel transfers (Main Bot excluded, Dump: %d)", len(workerTokens), GlobalConfig.DumpChatID)
	}

	if err := os.MkdirAll(GlobalConfig.DownloadDir, 0777); err != nil {
		fallback := filepath.Join(os.TempDir(), "downloads") + string(filepath.Separator)
		if fErr := os.MkdirAll(fallback, 0777); fErr == nil {
			log.Printf("[Config] DownloadDir %s not writable (%v), falling back to %s", GlobalConfig.DownloadDir, err, fallback)
			GlobalConfig.DownloadDir = fallback
		}
	}
	_ = os.MkdirAll("Thumbnails", 0777)

	return &GlobalConfig, nil
}

func Get() *Config {
	configLock.RLock()
	defer configLock.RUnlock()
	return &GlobalConfig
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil {
		return defaultVal
	}
	return n
}

func parseTokenString(raw string) []string {
	var tokens []string
	raw = strings.ReplaceAll(raw, ";", ",")
	raw = strings.ReplaceAll(raw, "\n", ",")
	raw = strings.ReplaceAll(raw, "\r", ",")
	raw = strings.ReplaceAll(raw, "\t", ",")
	for _, part := range strings.Split(raw, ",") {
		for _, token := range strings.Fields(part) {
			token = strings.Trim(strings.TrimSpace(token), `"'`)
			if token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}

func getEnvInt64(key string, defaultVal int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	val = strings.Trim(strings.TrimSpace(val), `"'`)
	if strings.Contains(val, ",") {
		val = strings.TrimSpace(strings.Split(val, ",")[0])
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return defaultVal
	}
	return n
}

func getEnvBool(key string, defaultVal bool) bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if val == "" {
		return defaultVal
	}
	return val == "true" || val == "1" || val == "yes" || val == "t"
}

func parseIDList(val string) []int64 {
	var ids []int64
	parts := strings.Fields(val)
	for _, p := range parts {
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func cleanDir(dir string) string {
	if !strings.HasSuffix(dir, "/") && !strings.HasSuffix(dir, "\\") {
		dir += "/"
	}
	return dir
}

func UpdateConfigVar(key, val string) error {
	configLock.Lock()
	defer configLock.Unlock()

	key = strings.ToUpper(strings.TrimSpace(key))
	switch key {
	case "OWNER_USERNAME":
		GlobalConfig.OwnerUsername = strings.TrimPrefix(strings.TrimSpace(val), "@")
	case "STATUS_LIMIT":
		GlobalConfig.StatusLimit, _ = strconv.Atoi(val)
	case "STATUS_UPDATE_INTERVAL":
		GlobalConfig.StatusUpdateInterval, _ = strconv.Atoi(val)
	case "AUTO_DELETE_MESSAGE_DURATION":
		GlobalConfig.AutoDeleteMessageDuration, _ = strconv.Atoi(val)
	case "LEECH_SPLIT_SIZE":
		valInt, _ := strconv.ParseInt(val, 10, 64)
		if valInt > MaxSplitSize || valInt <= 0 {
			valInt = MaxSplitSize
		}
		GlobalConfig.LeechSplitSize = valInt
	case "AS_DOCUMENT":
		GlobalConfig.AsDocument = val == "true" || val == "1"
	case "EQUAL_SPLITS":
		GlobalConfig.EqualSplits = val == "true" || val == "1"
	case "MEDIA_GROUP":
		GlobalConfig.MediaGroup = val == "true" || val == "1"
	case "CAP_FONT":
		GlobalConfig.CapFont = val
	case "LEECH_FILENAME_PREFIX":
		GlobalConfig.LeechFilenamePrefix = val
	case "LEECH_FILENAME_SUFFIX":
		GlobalConfig.LeechFilenameSuffix = val
	case "LEECH_FILENAME_CAPTION":
		GlobalConfig.LeechFilenameCaption = val
	case "LEECH_FILENAME_REMNAME_AUTO":
		GlobalConfig.LeechFilenameRemnameAuto = val
	case "LEECH_FILENAME_REMNAME_REGEX":
		GlobalConfig.LeechFilenameRemnameRegex = val
	case "LEECH_LOG_ID":
		GlobalConfig.LeechLogID, _ = strconv.ParseInt(val, 10, 64)
	case "TORRENT_TIMEOUT":
		GlobalConfig.TorrentTimeout, _ = strconv.Atoi(val)
	case "SHORTENER":
		GlobalConfig.Shortener = val
	case "SHORTENER_API":
		GlobalConfig.ShortenerAPI = val
	case "SHORTENER_ENABLED":
		GlobalConfig.ShortenerEnabled = val == "true" || val == "1"
	case "TOKEN_TIMEOUT":
		GlobalConfig.TokenTimeout, _ = strconv.ParseInt(val, 10, 64)
	case "SCREENSHOTS_MODE":
		GlobalConfig.ScreenshotsMode = val == "true" || val == "1"
	case "ENABLE_FFMPEG_CMDS":
		GlobalConfig.EnableFFmpegCmds = val == "true" || val == "1"
	case "FFMPEG_ENCODING":
		GlobalConfig.FFmpegEncoding = val == "true" || val == "1"
	case "TMDB_ACCESS_TOKEN":
		GlobalConfig.TMDBAccessToken = val
	case "AUTO_THUMBNAIL":
		GlobalConfig.AutoThumbnail = val == "true" || val == "1"
	case "COVER_IMAGE":
		GlobalConfig.CoverImage = val
	case "DDL_API", "THUNDER_API":
		GlobalConfig.DDLAPI = val
	case "BASE_URL":
		GlobalConfig.BaseURL = val
	case "BASE_URL_PORT":
		GlobalConfig.BaseURLPort, _ = strconv.Atoi(val)
	case "QUEUE_ALL", "BOT_MAX_TASKS":
		GlobalConfig.QueueAll, _ = strconv.Atoi(val)
	case "QUEUE_DOWNLOAD", "USER_MAX_TASKS":
		GlobalConfig.QueueDownload, _ = strconv.Atoi(val)
	case "QUEUE_UPLOAD":
		GlobalConfig.QueueUpload, _ = strconv.Atoi(val)
	case "LEECH_LIMIT":
		GlobalConfig.LeechLimit, _ = strconv.ParseInt(val, 10, 64)
	case "TORRENT_LIMIT":
		GlobalConfig.TorrentLimit, _ = strconv.ParseInt(val, 10, 64)
	case "DIRECT_LIMIT":
		GlobalConfig.DirectLimit, _ = strconv.ParseInt(val, 10, 64)
	case "YTDLP_LIMIT":
		GlobalConfig.YtdlpLimit, _ = strconv.ParseInt(val, 10, 64)
	case "MEGA_LIMIT":
		GlobalConfig.MegaLimit, _ = strconv.ParseInt(val, 10, 64)
	case "ARCHIVE_LIMIT":
		GlobalConfig.ArchiveLimit, _ = strconv.ParseInt(val, 10, 64)
	case "EXTRACT_LIMIT":
		GlobalConfig.ExtractLimit, _ = strconv.ParseInt(val, 10, 64)
	case "DAILY_LEECH_LIMIT":
		GlobalConfig.DailyLeechLimit, _ = strconv.ParseInt(val, 10, 64)
	case "DAILY_TASK_LIMIT":
		GlobalConfig.DailyTaskLimit, _ = strconv.Atoi(val)
	case "STORAGE_THRESHOLD":
		GlobalConfig.StorageThreshold, _ = strconv.ParseInt(val, 10, 64)
	}
	return nil
}

func GetConfigMap() map[string]string {
	configLock.RLock()
	defer configLock.RUnlock()

	return map[string]string{
		"OWNER_USERNAME":               GlobalConfig.OwnerUsername,
		"LEECH_LOG_ID":                 strconv.FormatInt(GlobalConfig.LeechLogID, 10),
		"STATUS_LIMIT":                 strconv.Itoa(GlobalConfig.StatusLimit),
		"STATUS_UPDATE_INTERVAL":       strconv.Itoa(GlobalConfig.StatusUpdateInterval),
		"AUTO_DELETE_MESSAGE_DURATION": strconv.Itoa(GlobalConfig.AutoDeleteMessageDuration),
		"LEECH_SPLIT_SIZE":             strconv.FormatInt(GlobalConfig.LeechSplitSize, 10),
		"AS_DOCUMENT":                  strconv.FormatBool(GlobalConfig.AsDocument),
		"EQUAL_SPLITS":                 strconv.FormatBool(GlobalConfig.EqualSplits),
		"MEDIA_GROUP":                  strconv.FormatBool(GlobalConfig.MediaGroup),
		"AUTO_THUMBNAIL":               strconv.FormatBool(GlobalConfig.AutoThumbnail),
		"LEECH_FILENAME_PREFIX":        GlobalConfig.LeechFilenamePrefix,
		"LEECH_FILENAME_SUFFIX":        GlobalConfig.LeechFilenameSuffix,
		"LEECH_FILENAME_CAPTION":       GlobalConfig.LeechFilenameCaption,
		"TORRENT_TIMEOUT":              strconv.Itoa(GlobalConfig.TorrentTimeout),
		"BASE_URL":                     GlobalConfig.BaseURL,
		"BOT_MAX_TASKS":                strconv.Itoa(GlobalConfig.QueueAll),
		"USER_MAX_TASKS":               strconv.Itoa(GlobalConfig.QueueDownload),
		"LEECH_LIMIT":                  strconv.FormatInt(GlobalConfig.LeechLimit, 10),
		"TORRENT_LIMIT":                strconv.FormatInt(GlobalConfig.TorrentLimit, 10),
		"DIRECT_LIMIT":                 strconv.FormatInt(GlobalConfig.DirectLimit, 10),
		"YTDLP_LIMIT":                  strconv.FormatInt(GlobalConfig.YtdlpLimit, 10),
		"MEGA_LIMIT":                   strconv.FormatInt(GlobalConfig.MegaLimit, 10),
		"DAILY_LEECH_LIMIT":            strconv.FormatInt(GlobalConfig.DailyLeechLimit, 10),
		"DAILY_TASK_LIMIT":             strconv.Itoa(GlobalConfig.DailyTaskLimit),
		"SHORTENER":                    GlobalConfig.Shortener,
		"SHORTENER_API":                GlobalConfig.ShortenerAPI,
		"SHORTENER_ENABLED":            strconv.FormatBool(GlobalConfig.ShortenerEnabled),
		"TOKEN_TIMEOUT":                strconv.FormatInt(GlobalConfig.TokenTimeout, 10),
	}
}
