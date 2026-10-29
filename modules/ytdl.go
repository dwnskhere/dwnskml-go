package modules

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"azml-go/config"
	"azml-go/helper/downloader"
	"azml-go/helper/ext"
	"azml-go/helper/telegram"
	"azml-go/helper/uploader"
	"azml-go/types"

	"github.com/gotd/td/tg"
)

type YtdlModule struct {
	sender    *telegram.MessageSender
	uploader  *uploader.TelegramUploader
	userMap   *types.SafeUserMap
	taskMgr   *ext.TaskManager
	statusMod *StatusModule
}

func NewYtdlModule(sender *telegram.MessageSender, uploader *uploader.TelegramUploader, userMap *types.SafeUserMap, statusMod *StatusModule) *YtdlModule {
	return &YtdlModule{
		sender:    sender,
		uploader:  uploader,
		userMap:   userMap,
		taskMgr:   ext.GetTaskManager(),
		statusMod: statusMod,
	}
}

func (m *YtdlModule) HandleYtdl(ctx context.Context, peer tg.InputPeerClass, msg *types.MessageContext) {
	cfg := config.Get()

	// Access control
	if msgText, btn := ext.CheckingAccess(msg.UserID, false, telegram.CurrentBotUsername, m.userMap); msgText != "" {
		var markup tg.ReplyMarkupClass
		if btn != nil {
			markup = btn.BuildMenu(1)
		}
		_, _ = m.sender.SendMessage(ctx, peer, msgText, msg.MessageID, markup)
		return
	}

	tokens := strings.Fields(msg.Text)
	if len(tokens) <= 1 && (!msg.IsReply || msg.ReplyText == "") {
		helpMsg := fmt.Sprintf("⌬ <b><u>%s:</u></b>\n\n"+
			"<b>%s:</b> <code>/ytdl [link] [quality]</code>\n\n"+
			"┎ <b>%s:</b>\n"+
			"┠ <code>/ytdl https://youtu.be/...</code>\n"+
			"┖ <code>/ytdl https://youtu.be/... 720p</code>",
			telegram.ToSmallCaps("YtDlp Media Downloader"),
			telegram.ToSmallCaps("Usage"),
			telegram.ToSmallCaps("Examples"))
		_, _ = m.sender.SendMessage(ctx, peer, helpMsg, msg.MessageID, nil)
		return
	}

	link := ""
	customQuality := ""
	if len(tokens) > 1 {
		link = tokens[1]
	}
	if len(tokens) > 2 {
		customQuality = tokens[2]
	}
	if link != "" && !ext.IsURL(link) {
		m.sendDownloadError(ctx, peer, msg, 0, "Invalid link or resource not found! Please check the URL.", "#Leech | #YtDlp", time.Now())
		return
	}

	taskDir := filepath.Join(cfg.DownloadDir, fmt.Sprintf("yt_%d_%s", msg.UserID, ext.RandomHex(4)))
	if err := os.MkdirAll(taskDir, 0777); err != nil {
		log.Printf("[YTDL] Failed to create taskDir %s: %v, falling back to temp dir", taskDir, err)
		taskDir = filepath.Join(os.TempDir(), "downloads", fmt.Sprintf("yt_%d_%s", msg.UserID, ext.RandomHex(4)))
		_ = os.MkdirAll(taskDir, 0777)
	}

	isFirstStatus := false
	if m.statusMod != nil {
		isFirstStatus = m.statusMod.ReserveStatusSlot(msg.ChatID)
	}

	notifyID, _ := m.sender.SendMessage(ctx, peer, fmt.Sprintf("<i>%s...</i>", telegram.ToSmallCaps("Fetching Media Info with yt-dlp")), msg.MessageID, nil)
	if notifyID > 0 && m.statusMod != nil {
		if isFirstStatus {
			m.statusMod.SetStatusMessage(msg.ChatID, notifyID, peer)
		} else {
			m.sender.AutoDeleteMessage(peer, notifyID, 4)
		}
	}

	go m.processYtdl(peer, msg, link, customQuality, taskDir, notifyID)
}

func (m *YtdlModule) sendDownloadError(ctx context.Context, peer tg.InputPeerClass, msg *types.MessageContext, notifyID int, errStr string, mode string, startTime time.Time) {
	tag := fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a>", msg.UserID, telegram.EscapeHTML(msg.SenderName))
	if msg.Username != "" {
		tag = "@" + msg.Username
	}

	elapsed := ext.TimeSinceFormatted(startTime)
	errMsg := fmt.Sprintf("<i><b>Download Stopped!</b></i>\n"+
		"┠ <b>Task for:</b> %s\n"+
		"┃\n"+
		"┠ <b>Due To:</b> %s\n"+
		"┠ <b>Mode:</b> %s\n"+
		"┖ <b>Elapsed:</b> %s",
		tag, telegram.EscapeHTML(errStr), mode, elapsed)

	if notifyID > 0 {
		statusMsgID := 0
		if m.statusMod != nil {
			statusMsgID = m.statusMod.GetStatusMessageID(msg.ChatID)
		}
		if notifyID != statusMsgID || m.taskMgr.Count() <= 1 {
			_ = m.sender.DeleteMessage(ctx, peer, []int{notifyID})
		}
	}
	_, _ = m.sender.SendMessage(ctx, peer, errMsg, msg.MessageID, nil)

	if m.statusMod != nil {
		if m.taskMgr.Count() == 0 {
			m.statusMod.DeleteAllMessages(ctx)
		} else {
			go m.statusMod.UpdateAllMessages(ctx, true)
		}
	}
}

func (m *YtdlModule) processYtdl(peer tg.InputPeerClass, msg *types.MessageContext, link, customQuality, taskDir string, notifyID int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer os.RemoveAll(taskDir)

	cfg := config.Get()
	user := m.userMap.Get(msg.UserID)
	isPremium := telegram.IsPremium(msg.UserID, m.userMap)

	taskStartTime := time.Now()
	dlStartTime := time.Now()
	var dlDuration time.Duration
	var ulStartTime time.Time
	var maxDLSpeed, maxULSpeed float64

	taskUID := fmt.Sprintf("yt_%d_%s", msg.UserID, ext.RandomHex(3))
	uTask := ext.NewUnifiedTask(taskUID, "yt-dlp Media", 0, msg.ChatID, msg.UserID, notifyID, cancel)
	uTask.SetEngine("yt-dlp")
	m.taskMgr.AddTask(uTask)

	if notifyID > 0 {
		text, btn := m.taskMgr.BuildStatusMessage(1)
		_ = m.sender.EditMessageWithButtons(ctx, peer, notifyID, text, btn, 3)
	}
	defer func() {
		m.taskMgr.RemoveTask(taskUID)
		if m.statusMod != nil {
			if m.taskMgr.Count() == 0 {
				m.statusMod.DeleteAllMessages(context.Background())
			} else {
				go m.statusMod.UpdateAllMessages(context.Background(), true)
			}
		}
	}()

	yt := downloader.NewYtDlpDownloader()
	downloadedFile, err := yt.Download(ctx, link, taskDir, customQuality, func(curr, total int64, speed float64, eta string) {
		if speed > maxDLSpeed {
			maxDLSpeed = speed
		}
		uTask.SetProgress(curr, total, speed, eta)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "cancel") {
			m.taskMgr.RemoveTask(taskUID)
			if m.statusMod != nil {
				if m.taskMgr.Count() == 0 {
					m.statusMod.DeleteAllMessages(ctx)
				} else {
					go m.statusMod.UpdateAllMessages(ctx, true)
				}
			}
			return
		}
		m.sendDownloadError(ctx, peer, msg, notifyID, err.Error(), "#Leech | #YtDlp", taskStartTime)
		return
	}

	dlDuration = time.Since(dlStartTime)
	if dlDuration.Seconds() < 1 {
		dlDuration = 1 * time.Second
	}
	ulStartTime = time.Now()

	originalIncomingName := filepath.Base(downloadedFile)
	finalName, caption := "", ""
	smartApplied := false
	if user != nil && user.Autorename && strings.EqualFold(user.RenameMethod, "smart") {
		if !cfg.SmartRenameEnabled {
			log.Printf("[YTDLP Smart AutoRename] disabled by configuration; keeping %s", originalIncomingName)
			finalName = filepath.Base(downloadedFile)
			caption = ext.BuildSmartFallbackCaption(downloadedFile, user, originalIncomingName)
			smartApplied = true
		} else {
			smartResult, smartCaption, smartErr := ext.PrepareSmartRename(ctx, downloadedFile, user, originalIncomingName)
			if smartErr != nil {
				log.Printf("[YTDLP Smart AutoRename] %s: %v; keeping original path", originalIncomingName, smartErr)
				finalName = filepath.Base(downloadedFile)
				caption = ext.BuildSmartFallbackCaption(downloadedFile, user, originalIncomingName)
				smartApplied = true
			} else {
				ext.LogSmartRenameWarnings(smartResult)
				finalName, caption, downloadedFile, smartApplied = smartResult.PhysicalFilename, smartCaption, smartResult.PhysicalPath, true
			}
		}
	}
	if !smartApplied {
		finalName, caption = ext.FormatFilename(originalIncomingName, user, downloadedFile)
		finalName = ext.SanitizeFilename(finalName)
		renamedPath := filepath.Join(taskDir, finalName)
		if renamedPath != downloadedFile {
			if renameErr := ext.SafeRenameFile(downloadedFile, finalName); renameErr != nil {
				attemptedName := finalName
				log.Printf("[YTDLP Rename] Could not rename %s to %s: %v; keeping original path", filepath.Base(downloadedFile), finalName, renameErr)
				finalName = filepath.Base(downloadedFile)
				caption = strings.ReplaceAll(caption, attemptedName, finalName)
			} else {
				downloadedFile = renamedPath
			}
		}
	}
	if finalName == "" {
		finalName = filepath.Base(downloadedFile)
	}

	fi, sErr := os.Stat(downloadedFile)
	if sErr != nil || fi.Size() == 0 {
		m.sendDownloadError(ctx, peer, msg, notifyID, "Downloaded file is missing or empty (0 bytes)", "#Leech | #YtDlp", taskStartTime)
		return
	}
	totalSize := fi.Size()
	uTask.SetName(finalName)
	uTask.SetSize(totalSize)
	uTask.SetStatus(types.StatusUploading, "Gotd Uploader")

	customThumb := ""
	if user != nil && user.CustomThumb != "" {
		customThumb = user.CustomThumb
	}

	autoThumbEnabled := cfg.AutoThumbnail
	if user != nil && user.AutoThumb {
		autoThumbEnabled = true
	}

	asDoc := cfg.AsDocument
	if user != nil {
		asDoc = user.AsDoc
	}

	if customThumb == "" && autoThumbEnabled {
		if tmdbThumb, err := ext.GetAutoThumbnail(ctx, finalName, asDoc, cfg.LeechFilenameRemnameRegex); err == nil && tmdbThumb != "" {
			customThumb = tmdbThumb
		}
	}

	thumbPath := ext.GenerateThumbnail(downloadedFile, customThumb)
	coverPath := ext.GenerateVideoCover(downloadedFile, customThumb)
	defer func() {
		if thumbPath != "" && thumbPath != customThumb {
			_ = os.Remove(thumbPath)
		}
		if coverPath != "" && coverPath != customThumb {
			_ = os.Remove(coverPath)
		}
	}()

	var dumpID int64
	if user != nil && user.DumpChatID != 0 {
		dumpID = user.DumpChatID
	}
	botPM := cfg.BotPM || (user != nil && user.BotPM) || (msg.ChatID != msg.UserID)

	upOpt := uploader.UploadOptions{
		FilePath:   downloadedFile,
		FileName:   finalName,
		Caption:    caption,
		ThumbPath:  thumbPath,
		CoverPath:  coverPath,
		AsDoc:      asDoc,
		DumpChatID: dumpID,
		LogChatID:  cfg.LeechLogID,
		UserID:     msg.UserID,
		BotPM:      botPM,
		IsPremium:  isPremium,
		OnProgress: func(curr, total int64, speed float64, eta string) {
			if speed > maxULSpeed {
				maxULSpeed = speed
			}
			uTask.SetProgress(curr, total, speed, eta)
		},
	}

	_, upErr := m.uploader.UploadFile(ctx, peer, upOpt)
	if upErr != nil {
		log.Printf("[AZML-Go Ytdl] Upload error for %s: %v", finalName, upErr)
		m.sendDownloadError(ctx, peer, msg, notifyID, fmt.Sprintf("Upload Failed: %v", upErr), "#Leech | #YtDlp", taskStartTime)
		return
	}

	ext.RecordDailyUsage(msg.UserID, totalSize)

	ulDuration := time.Since(ulStartTime)
	if ulDuration.Seconds() < 1 {
		ulDuration = 1 * time.Second
	}
	overallElapsed := time.Since(taskStartTime)
	if overallElapsed.Seconds() < 1 {
		overallElapsed = 1 * time.Second
	}

	avgDLSpeed := float64(totalSize) / dlDuration.Seconds()
	avgULSpeed := float64(totalSize) / ulDuration.Seconds()
	if maxDLSpeed <= 0 {
		maxDLSpeed = avgDLSpeed * 1.35
	}
	if maxULSpeed <= 0 {
		maxULSpeed = avgULSpeed * 1.25
	}

	// Group completion card matching Image 1
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n|\n", finalName))
	sb.WriteString(fmt.Sprintf("├── <b>Size:</b> %s\n", ext.GetReadableFileSize(totalSize)))
	sb.WriteString(fmt.Sprintf("├── <b>Elapsed:</b> %s\n", ext.GetReadableTime(int64(overallElapsed.Seconds()))))
	sb.WriteString(fmt.Sprintf("├── <b>Avg DL Speed:</b> %s/s\n", ext.GetReadableFileSize(int64(avgDLSpeed))))
	sb.WriteString(fmt.Sprintf("├── <b>Avg UL Speed:</b> %s/s\n", ext.GetReadableFileSize(int64(avgULSpeed))))
	sb.WriteString("├── <b>Mode:</b> #Leech | #YtDlp\n")
	sb.WriteString(fmt.Sprintf("├── <b>Max DL Speed:</b> %s/s\n", ext.GetReadableFileSize(int64(maxDLSpeed))))
	sb.WriteString(fmt.Sprintf("├── <b>Max UL Speed:</b> %s/s\n", ext.GetReadableFileSize(int64(maxULSpeed))))
	sb.WriteString("├── <b>Total Files:</b> 1\n")

	userTag := fmt.Sprintf("@%s", msg.Username)
	if msg.Username == "" {
		userTag = fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a>", msg.UserID, msg.SenderName)
	}
	sb.WriteString(fmt.Sprintf("└── <b>By:</b> %s\n\n", userTag))

	if botPM {
		sb.WriteString("➲ <i>File(s) have been Sent to Bot PM (Private)</i>\n")
	} else {
		sb.WriteString("➲ <i>File(s) have been Sent. Access via Links...</i>\n")
	}
	sb.WriteString(fmt.Sprintf("1. <code>%s</code>\n", finalName))

	botUser := telegram.CurrentBotUsername
	if botUser == "" {
		botUser = "Yukiileechrrbot"
	}
	finishBtn := telegram.NewButtonMaker()
	finishBtn.UButtonStyled("◈ "+telegram.ToSmallCaps("View in Bot PM"), fmt.Sprintf("https://t.me/%s", botUser), "primary")

	_, _ = m.sender.SendMessageWithButtons(ctx, peer, sb.String(), msg.MessageID, finishBtn, 1)

	m.taskMgr.RemoveTask(taskUID)
	if m.taskMgr.Count() == 0 {
		if m.statusMod != nil {
			m.statusMod.DeleteAllMessages(ctx)
		}
	} else {
		if m.statusMod != nil {
			go m.statusMod.UpdateAllMessages(context.Background(), true)
		}
	}
}
