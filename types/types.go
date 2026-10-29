package types

import (
	"context"
	"sync"
	"time"

	"github.com/gotd/td/tg"
)

type UserSettings struct {
	UserID         int64             `json:"user_id" bson:"_id"`
	Name           string            `json:"name" bson:"name"`
	IsAuth         bool              `json:"is_auth" bson:"is_auth"`
	IsSudo         bool              `json:"is_sudo" bson:"is_sudo"`
	IsBlacklist    bool              `json:"is_blacklist" bson:"is_blacklist"`
	IsPremium      bool              `json:"is_premium" bson:"is_premium"`
	ExpiryTime     int64             `json:"expiry_time" bson:"expiry_time"`
	Token          string            `json:"token" bson:"token"`
	TokenTime      int64             `json:"token_time" bson:"token_time"`
	Prefix         string            `json:"prefix" bson:"prefix"`
	Suffix         string            `json:"suffix" bson:"suffix"`
	RemnameAuto    string            `json:"remname_auto" bson:"remname_auto"`
	RemnameRegex   string            `json:"remname_regex" bson:"remname_regex"`
	RenameMethod   string            `json:"rename_method" bson:"rename_method"` // "auto", "regex", or "smart"
	Autorename     bool              `json:"autorename" bson:"autorename"`
	Caption        string            `json:"caption" bson:"caption"`
	CustomThumb    string            `json:"custom_thumb" bson:"custom_thumb"`
	SplitSize      int64             `json:"split_size" bson:"split_size"`
	EqualSplits    bool              `json:"equal_splits" bson:"equal_splits"`
	MediaGroup     bool              `json:"media_group" bson:"media_group"`
	AsDoc          bool              `json:"as_doc" bson:"as_doc"`
	DumpChatID     int64             `json:"dump_chat_id" bson:"dump_chat_id"`
	Metadata       map[string]string `json:"metadata" bson:"metadata"`
	FFmpegCommands map[string]string `json:"ffmpeg_cmds" bson:"ffmpeg_cmds"`
	AutoThumb      bool              `json:"auto_thumb" bson:"auto_thumb"`
	BotPM          bool              `json:"bot_pm" bson:"bot_pm"`
	StartedInPM    bool              `json:"started_in_pm" bson:"started_in_pm"`
	TopicIDs       []int             `json:"topic_ids" bson:"topic_ids"`
}

type TaskStatus string

const (
	StatusDownloading TaskStatus = "Downloading"
	StatusUploading   TaskStatus = "Uploading"
	StatusSplitting   TaskStatus = "Splitting"
	StatusFFmpeg      TaskStatus = "FFmpeg"
	StatusExtracting  TaskStatus = "Extracting"
	StatusArchiving   TaskStatus = "Archiving"
	StatusQueuedDL    TaskStatus = "QueueDL"
	StatusQueuedUP    TaskStatus = "QueueUp"
	StatusPaused      TaskStatus = "Paused"
)

type DownloadTask interface {
	GID() string
	Name() string
	Size() int64
	DownloadedBytes() int64
	Speed() float64
	ETA() string
	Progress() float64
	Status() TaskStatus
	Engine() string
	Cancel() error
	StartTime() time.Time
	MessageID() int
	ChatID() int64
	UserID() int64
}

type TaskListener interface {
	OnDownloadStart() error
	OnDownloadProgress(current, total int64)
	OnDownloadComplete() error
	OnDownloadError(err string) error
	OnUploadStart() error
	OnUploadProgress(current, total int64)
	OnUploadComplete() error
	OnUploadError(err string) error
	Context() context.Context
	Cancel()
}

type LeechOptions struct {
	CustomName  string
	IsZip       bool
	IsExtract   bool
	Screenshots int
	ThumbURL    string
	ExtractStrm bool
	FFmpegKey   string
	AsDoc       bool
	AsMedia     bool
	UserDump    int64
	Headers     string
	Username    string
	Password    string
	Bulk        bool
	BulkRange   string
	Select      bool
	Multi       int
	MediaGroup  bool
}

type MessageContext struct {
	ChatID      int64
	MessageID   int
	UserID      int64
	SenderName  string
	Username    string
	Text        string
	ReplyToID   int
	ReplyText   string
	ReplyUserID int64
	IsReply     bool
	HasMedia    bool
	MediaType   string
	FileID      string
	FileName    string
	FileSize    int64
	RawMedia    tg.MessageMediaClass
}

type SafeUserMap struct {
	sync.RWMutex
	users map[int64]*UserSettings
}

func NewSafeUserMap() *SafeUserMap {
	return &SafeUserMap{
		users: make(map[int64]*UserSettings),
	}
}

func (m *SafeUserMap) Get(userID int64) *UserSettings {
	m.RLock()
	defer m.RUnlock()
	if u, ok := m.users[userID]; ok {
		return u
	}
	return nil
}

func (m *SafeUserMap) GetOrCreate(userID int64) *UserSettings {
	m.Lock()
	defer m.Unlock()
	if u, ok := m.users[userID]; ok {
		return u
	}
	u := &UserSettings{
		UserID:       userID,
		Autorename:   true,
		RenameMethod: "auto",
		AsDoc:        true,
		Metadata:     make(map[string]string),
	}
	m.users[userID] = u
	return u
}

func (m *SafeUserMap) Set(u *UserSettings) {
	m.Lock()
	defer m.Unlock()
	m.users[u.UserID] = u
}

func (m *SafeUserMap) GetAllUsers() []*UserSettings {
	m.RLock()
	defer m.RUnlock()
	var list []*UserSettings
	for _, u := range m.users {
		list = append(list, u)
	}
	return list
}
