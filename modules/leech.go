package modules

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"azml-go/config"
	"azml-go/helper/downloader"
	"azml-go/helper/ext"
	"azml-go/helper/pool"
	"azml-go/helper/telegram"
	"azml-go/helper/uploader"
	"azml-go/types"
	"azml-go/web"

	tdDownloader "github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

type LeechModule struct {
	rawAPI     *tg.Client
	userRawAPI *tg.Client
	sender     *telegram.MessageSender
	uploader   *uploader.TelegramUploader
	userMap    *types.SafeUserMap
	taskMgr    *ext.TaskManager
	statusMod  *StatusModule
	sessPool   *pool.SessionPool
	dumpChatID int64
}

func NewLeechModule(rawAPI, userRawAPI *tg.Client, sender *telegram.MessageSender, uploader *uploader.TelegramUploader, userMap *types.SafeUserMap, statusMod *StatusModule, sessPool *pool.SessionPool, dumpChatID int64) *LeechModule {
	return &LeechModule{
		rawAPI:     rawAPI,
		userRawAPI: userRawAPI,
		sender:     sender,
		uploader:   uploader,
		userMap:    userMap,
		taskMgr:    ext.GetTaskManager(),
		statusMod:  statusMod,
		sessPool:   sessPool,
		dumpChatID: dumpChatID,
	}
}

func (m *LeechModule) SetSessionPool(sessPool *pool.SessionPool, dumpChatID int64) {
	m.sessPool = sessPool
	m.dumpChatID = dumpChatID
}

func (m *LeechModule) HandleLeech(ctx context.Context, peer tg.InputPeerClass, msg *types.MessageContext, isZip, isUnzip bool) {
	cfg := config.Get()

	// Access control & token check
	if msgText, btn := ext.CheckingAccess(msg.UserID, false, telegram.CurrentBotUsername, m.userMap); msgText != "" {
		var markup tg.ReplyMarkupClass
		if btn != nil {
			markup = btn.BuildMenu(1)
		}
		_, _ = m.sender.SendMessage(ctx, peer, msgText, msg.MessageID, markup)
		return
	}

	opt, link := parseLeechArgs(msg.Text)
	if isZip {
		opt.IsZip = true
	}
	if isUnzip {
		opt.IsExtract = true
	}
	if opt.Password != "" && !opt.IsZip {
		opt.IsExtract = true
	}

	if link == "" && msg.IsReply && msg.ReplyText != "" && !msg.HasMedia && msg.RawMedia == nil {
		for _, w := range strings.Fields(msg.ReplyText) {
			if strings.HasPrefix(w, "http") || strings.HasPrefix(w, "magnet:") {
				link = w
				break
			}
		}
		if link == "" && (strings.HasPrefix(msg.ReplyText, "http") || strings.HasPrefix(msg.ReplyText, "magnet:")) {
			link = strings.TrimSpace(msg.ReplyText)
		}
	}

	// Bulk Leech Handling (-b / -bulk)
	if opt.Bulk {
		var rawLinks []string
		sourceText := ""
		if msg.IsReply && msg.ReplyText != "" && !msg.HasMedia && msg.RawMedia == nil {
			sourceText = msg.ReplyText
		} else if link != "" {
			sourceText = link
		}

		for _, l := range strings.Split(sourceText, "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "http") || strings.HasPrefix(l, "magnet:") {
				rawLinks = append(rawLinks, l)
			}
		}

		if len(rawLinks) == 0 {
			_, _ = m.sender.SendMessage(ctx, peer, "<b>Reply to a message with links separated by new line for bulk leeching!</b>", msg.MessageID, nil)
			return
		}

		bulkLinks := rawLinks
		rangeStr := opt.BulkRange
		if rangeStr == "" {
			parts := strings.Fields(msg.Text)
			for idx, p := range parts {
				if (p == "-b" || p == "-bulk") && idx+1 < len(parts) && strings.Contains(parts[idx+1], ":") {
					rangeStr = parts[idx+1]
					break
				}
			}
		}
		if rangeStr != "" && strings.Contains(rangeStr, ":") {
			rangeParts := strings.Split(rangeStr, ":")
			start, _ := strconv.Atoi(rangeParts[0])
			end, _ := strconv.Atoi(rangeParts[1])
			if start < len(rawLinks) {
				if end > 0 && end <= len(rawLinks) {
					bulkLinks = rawLinks[start:end]
				} else {
					bulkLinks = rawLinks[start:]
				}
			}
		}

		_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("⌬ <b>Bulk Leech Initiated:</b> <code>%d links</code>", len(bulkLinks)), msg.MessageID, nil)

		go func() {
			for _, bLink := range bulkLinks {
				subTaskDir := filepath.Join(cfg.DownloadDir, fmt.Sprintf("%d_%s", msg.UserID, ext.RandomHex(4)))
				if err := os.MkdirAll(subTaskDir, 0777); err != nil {
					subTaskDir = filepath.Join(os.TempDir(), "downloads", fmt.Sprintf("%d_%s", msg.UserID, ext.RandomHex(4)))
					_ = os.MkdirAll(subTaskDir, 0777)
				}

				isFirstBulk := false
				if m.statusMod != nil {
					isFirstBulk = m.statusMod.ReserveStatusSlot(msg.ChatID)
				}

				nID, _ := m.sender.SendMessage(context.Background(), peer, fmt.Sprintf("<i>Initiating Leech:</i> <code>%s</code>", truncateString(bLink, 40)), msg.MessageID, nil)
				if nID > 0 && m.statusMod != nil {
					if isFirstBulk {
						m.statusMod.SetStatusMessage(msg.ChatID, nID, peer)
					} else {
						m.sender.AutoDeleteMessage(peer, nID, 4)
					}
				}
				m.processLeechPipeline(peer, msg, opt, bLink, subTaskDir, nID)
				time.Sleep(2 * time.Second)
			}
		}()
		return
	}

	// Multi-Message Sequential Leech (-i <count>)
	if opt.Multi > 1 && msg.IsReply && msg.ReplyToID > 0 {
		multiCount := opt.Multi
		baseReplyID := msg.ReplyToID
		go func() {
			for idx := 1; idx < multiCount; idx++ {
				time.Sleep(3 * time.Second)
				targetID := baseReplyID + idx
				res, err := m.rawAPI.MessagesGetMessages(context.Background(), []tg.InputMessageClass{
					&tg.InputMessageID{ID: targetID},
				})
				if err != nil {
					log.Printf("[LEECH -i] Failed to fetch message ID %d: %v", targetID, err)
					continue
				}

				var targetMsg *tg.Message
				switch mRes := res.(type) {
				case *tg.MessagesMessages:
					if len(mRes.Messages) > 0 {
						targetMsg, _ = mRes.Messages[0].(*tg.Message)
					}
				case *tg.MessagesMessagesSlice:
					if len(mRes.Messages) > 0 {
						targetMsg, _ = mRes.Messages[0].(*tg.Message)
					}
				case *tg.MessagesChannelMessages:
					if len(mRes.Messages) > 0 {
						targetMsg, _ = mRes.Messages[0].(*tg.Message)
					}
				}

				if targetMsg == nil {
					continue
				}

				nextMsgCtx := &types.MessageContext{
					ChatID:     msg.ChatID,
					MessageID:  targetMsg.ID,
					UserID:     msg.UserID,
					SenderName: msg.SenderName,
					Text:       targetMsg.Message,
					HasMedia:   targetMsg.Media != nil,
					RawMedia:   targetMsg.Media,
					ReplyToID:  targetID,
					IsReply:    true,
				}

				log.Printf("[LEECH -i] Sequentially triggering download for message ID %d (part %d/%d)", targetID, idx+1, multiCount)
				m.HandleLeech(context.Background(), peer, nextMsgCtx, isZip, isUnzip)
			}
		}()
	}

	if link == "" && !msg.HasMedia && !msg.IsReply {
		helpMsg := fmt.Sprintf("⌬ <b><u>%s:</u></b>\n\n"+
			"<b>%s:</b> <code>/leech [link] [flags]</code>\n\n"+
			"┎ <b>%s:</b>\n"+
			"┠ <code>-b</code> — %s\n"+
			"┠ <code>-n</code> — %s\n"+
			"┠ <code>-z</code> — %s\n"+
			"┠ <code>-e</code> — %s\n"+
			"┠ <code>-p</code> — %s\n"+
			"┠ <code>-ss</code> — %s\n"+
			"┠ <code>-d</code> — %s\n"+
			"┠ <code>-med</code> — %s\n"+
			"┠ <code>-ud</code> — %s\n"+
			"┖ <code>-st</code> — %s",
			telegram.ToSmallCaps("Leech Command Usage"),
			telegram.ToSmallCaps("Usage"),
			telegram.ToSmallCaps("Available Flags"),
			telegram.ToSmallCaps("Bulk Leech (reply to links)"),
			telegram.ToSmallCaps("Custom File Name"),
			telegram.ToSmallCaps("Compress as Zip"),
			telegram.ToSmallCaps("Extract Archive"),
			telegram.ToSmallCaps("Extraction Password"),
			telegram.ToSmallCaps("Generate Screenshots"),
			telegram.ToSmallCaps("Upload as Document"),
			telegram.ToSmallCaps("Upload as Media"),
			telegram.ToSmallCaps("Custom Dump Chat ID"),
			telegram.ToSmallCaps("Extract Video Streams"))
		_, _ = m.sender.SendMessage(ctx, peer, helpMsg, msg.MessageID, nil)
		return
	}

	if link != "" && !ext.IsURL(link) && !ext.IsMagnet(link) && !ext.IsMegaLink(link) && !strings.HasPrefix(link, "tg://") {
		m.sendDownloadError(ctx, peer, msg, 0, "Invalid link or resource not found! Please check the URL.", "#Leech", time.Now())
		return
	}

	// Create user download folder
	taskDir := filepath.Join(cfg.DownloadDir, fmt.Sprintf("%d_%s", msg.UserID, ext.RandomHex(4)))
	if err := os.MkdirAll(taskDir, 0777); err != nil {
		log.Printf("[LEECH] Failed to create taskDir %s: %v, falling back to temp dir", taskDir, err)
		taskDir = filepath.Join(os.TempDir(), "downloads", fmt.Sprintf("%d_%s", msg.UserID, ext.RandomHex(4)))
		_ = os.MkdirAll(taskDir, 0777)
	}

	isFirstStatus := false
	if m.statusMod != nil {
		isFirstStatus = m.statusMod.ReserveStatusSlot(msg.ChatID)
	}

	notifyID, _ := m.sender.SendMessage(ctx, peer, fmt.Sprintf("<i>%s...</i>", telegram.ToSmallCaps("Initiating Leech Task")), msg.MessageID, nil)
	if notifyID > 0 && m.statusMod != nil {
		if isFirstStatus {
			m.statusMod.SetStatusMessage(msg.ChatID, notifyID, peer)
		} else {
			m.sender.AutoDeleteMessage(peer, notifyID, 4)
		}
	}

	go m.processLeechPipeline(peer, msg, opt, link, taskDir, notifyID)
}

func (m *LeechModule) sendDownloadError(ctx context.Context, peer tg.InputPeerClass, msg *types.MessageContext, notifyID int, errStr string, mode string, startTime time.Time) {
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

func (m *LeechModule) processLeechPipeline(peer tg.InputPeerClass, msg *types.MessageContext, opt types.LeechOptions, link, taskDir string, notifyID int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		_ = os.RemoveAll(taskDir)
		log.Printf("[AZML-Go] Cleared directory: %s", taskDir)
	}()

	cfg := config.Get()
	user := m.userMap.Get(msg.UserID)
	isPremium := telegram.IsPremium(msg.UserID, m.userMap)

	var downloadedPath string
	var dlErr error

	// 0. Quota and Task Limits Pre-check
	if limitErr := ext.CheckTaskLimits(msg.UserID, 0, "leech", opt.IsExtract, opt.IsZip, 0); limitErr != "" {
		m.sendDownloadError(ctx, peer, msg, notifyID, "Limit Exceeded: "+limitErr, "#Leech", time.Now())
		return
	}

	// Try DDL API for Telegram links (t.me/... or t.me/c/...) if a link was provided
	if cfg.DDLAPI != "" && link != "" {
		if cid, mid, ok := ext.ParseTelegramLink(link); ok {
			tgChannelID := cid
			if tgChannelID == 0 {
				tgChannelID = msg.ChatID
			}
			if mid != 0 {
				if ddlURL, err := ext.TryDDLAPI(ctx, tgChannelID, mid); err == nil && ddlURL != "" {
					link = ddlURL
				}
			}
		}
	}

	// Try Direct Link Generator for host URLs (Mediafire, Pixeldrain, Solidfiles)
	if ext.IsURL(link) {
		if directURL, err := ext.GenerateDirectLink(link); err == nil && directURL != "" {
			link = directURL
		}
	}

	taskUID := fmt.Sprintf("%d_%s", msg.UserID, ext.RandomHex(3))

	displayName := opt.CustomName
	if displayName == "" {
		if ext.IsMagnet(link) {
			displayName = "Torrent/Magnet"
		} else if ext.IsURL(link) {
			displayName = filepath.Base(link)
			if strings.Contains(displayName, "?") {
				displayName = strings.Split(displayName, "?")[0]
			}
		} else if msg.FileName != "" {
			displayName = msg.FileName
		} else if msg.IsReply {
			displayName = "Telegram File"
		} else {
			displayName = "Leech Task"
		}
	}
	displayName = filepath.Base(displayName)

	uTask := ext.NewUnifiedTask(taskUID, displayName, msg.FileSize, msg.ChatID, msg.UserID, notifyID, cancel)
	m.taskMgr.AddTask(uTask)

	// Instantly render the status progress bar so the user gets an immediate (<1s) response
	if notifyID > 0 && m.statusMod != nil {
		if !m.statusMod.HasStatusMessage(msg.ChatID) {
			m.statusMod.SetStatusMessage(msg.ChatID, notifyID, peer)
			text, btn := m.taskMgr.BuildStatusMessage(1)
			_ = m.sender.EditMessageWithButtons(ctx, peer, notifyID, text, btn, 3)
		} else {
			go m.statusMod.UpdateAllMessages(ctx, true)
		}
	}

	// For HTTP/HTTPS links: probe remote headers to resolve true filename (Content-Disposition) & extension in background
	if ext.IsURL(link) && !ext.IsMagnet(link) && !strings.Contains(strings.ToLower(link), ".torrent") {
		resolvedName, finalURL, resolvedSize, _ := ext.ResolveDownloadMetadata(ctx, link)
		if resolvedName != "" {
			if opt.CustomName == "" {
				opt.CustomName = resolvedName
				displayName = resolvedName
				uTask.SetName(resolvedName)
			}
		}
		if finalURL != "" {
			link = finalURL
		}
		if msg.FileSize == 0 && resolvedSize > 0 {
			msg.FileSize = resolvedSize
			uTask.SetSize(resolvedSize)
		}
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

	taskStartTime := time.Now()
	dlStartTime := time.Now()
	var dlDuration time.Duration
	var ulStartTime time.Time
	var maxDLSpeed float64
	var maxULSpeed float64
	var totalSize int64
	var uploadedNames []string
	modeStr := "#Leech | #Aria2"
	if ext.IsMagnet(link) || strings.HasSuffix(strings.ToLower(link), ".torrent") {
		modeStr = "#Leech | #Torrent"
	} else if ext.IsMegaLink(link) {
		modeStr = "#Leech | #Mega"
	} else if link == "" && (msg.IsReply || msg.HasMedia) {
		modeStr = "#Leech | #Telegram"
	}

	// 0. Queue Check for Download (Sleep v0 Engine)
	queuedDL, qErr := ext.AcquireDownloadQueue(ctx, taskUID, displayName, msg.FileSize, msg.ChatID, msg.UserID, notifyID, cancel)
	if qErr != nil {
		m.sendDownloadError(ctx, peer, msg, notifyID, "Download task cancelled while in queue", modeStr, taskStartTime)
		return
	}
	if queuedDL {
		_ = m.sender.EditMessage(ctx, peer, notifyID, "<i>Starting download from queue...</i>", nil)
	}

	// 1. Download Phase
	if link == "" && (msg.IsReply || msg.HasMedia) {
		var mediaSource tg.MessageMediaClass = msg.RawMedia

		if mediaSource == nil && msg.IsReply && msg.ReplyToID > 0 {
			targetMsg, err := telegram.FetchMessage(ctx, m.rawAPI, peer, msg.ReplyToID)
			if err == nil && targetMsg != nil {
				if targetMsg.Media != nil {
					mediaSource = targetMsg.Media
					msg.HasMedia = true
					msg.RawMedia = targetMsg.Media
					link = ""
				} else if targetMsg.Message != "" {
					for _, tok := range strings.Fields(targetMsg.Message) {
						if strings.HasPrefix(tok, "http") || strings.HasPrefix(tok, "magnet:") {
							link = tok
							break
						}
					}
				}
			} else if err != nil {
				log.Printf("[LEECH] Failed to fetch replied message %d: %v", msg.ReplyToID, err)
				dlErr = fmt.Errorf("failed to fetch replied message: %v", err)
			}
		}

		if mediaSource != nil {
			link = ""
			// Case 1A: Document Media (Videos, Audio, Archives, Files)
			if docMedia, ok := mediaSource.(*tg.MessageMediaDocument); ok {
				if doc, ok := docMedia.Document.(*tg.Document); ok {
					fileName := ""
					for _, attr := range doc.Attributes {
						if fnAttr, ok := attr.(*tg.DocumentAttributeFilename); ok {
							fileName = fnAttr.FileName
							break
						}
					}
					if fileName == "" {
						isVideo := false
						for _, attr := range doc.Attributes {
							if _, ok := attr.(*tg.DocumentAttributeVideo); ok {
								isVideo = true
								break
							}
						}
						if isVideo {
							fileName = fmt.Sprintf("video_%d.mp4", doc.ID)
						} else {
							fileName = fmt.Sprintf("telegram_doc_%d", doc.ID)
						}
					}
					if opt.CustomName != "" {
						origExt := filepath.Ext(fileName)
						fileName = opt.CustomName
						if filepath.Ext(fileName) == "" && origExt != "" {
							fileName += origExt
						}
					}
					fileName = strings.ReplaceAll(fileName, "/", "_")
					fileName = strings.ReplaceAll(fileName, "\\", "_")
					destPath := filepath.Join(taskDir, fileName)
					_ = os.MkdirAll(filepath.Dir(destPath), 0777)
					uTask.SetName(fileName)
					uTask.SetSize(doc.Size)
					var dlSuccess bool

					effectiveDumpChatID := m.dumpChatID
					if effectiveDumpChatID == 0 && cfg != nil {
						effectiveDumpChatID = cfg.DumpChatID
					}
					if effectiveDumpChatID == 0 && cfg != nil {
						effectiveDumpChatID = cfg.LeechLogID
					}

					// Check if Multi-Bot Download is enabled and sessions are active
					if m.sessPool != nil && m.sessPool.ActiveSessionCount() > 0 && effectiveDumpChatID != 0 {
						targetMsgID := msg.ReplyToID
						if targetMsgID == 0 {
							targetMsgID = msg.MessageID
						}

						rawDumpID := pool.RawChannelID(effectiveDumpChatID)
						dumpHash := telegram.ResolveChannelAccessHash(ctx, m.rawAPI, effectiveDumpChatID)
						if dumpHash == 0 {
							dumpHash = telegram.GetChannelAccessHash(rawDumpID)
						}

						if dumpHash != 0 {
							dumpPeer := &tg.InputPeerChannel{
								ChannelID:  rawDumpID,
								AccessHash: dumpHash,
							}
							log.Printf("[LEECH Multi-Bot] Forwarding message %d to dump channel %d for multi-client download...", targetMsgID, effectiveDumpChatID)
							fwdMsgID, fwdErr := telegram.ForwardToChannelWithFloodWait(ctx, m.rawAPI, peer, dumpPeer, targetMsgID)

							if fwdErr == nil && fwdMsgID > 0 {
								uTask.SetStatus(types.StatusDownloading, "Multi-Bot MTProto")
								multiDL := downloader.NewMultiBotDownloader(m.sessPool, cfg.WorkerBotTokens, cfg.TelegramAPI, cfg.TelegramHash)
								dlErr = multiDL.DownloadToFile(ctx, effectiveDumpChatID, int64(fwdMsgID), doc.Size, destPath, func(curr, total int64, speed float64, eta string) {
									uTask.SetProgress(curr, total, speed, eta)
								})
								// Retain forwarded message in dump channel for backup and multi-client access

								if dlErr == nil {
									downloadedPath = destPath
									uTask.SetProgress(doc.Size, doc.Size, 0, "-")
									dlSuccess = true
									log.Printf("[LEECH Multi-Bot] Multi-bot download succeeded for %s (%s)", fileName, ext.GetReadableFileSize(doc.Size))
								} else {
									log.Printf("[LEECH Multi-Bot] Multi-bot download returned error: %v. Falling back to single-client downloader...", dlErr)
								}
							} else {
								log.Printf("[LEECH Multi-Bot] Failed to forward to dump channel (%v, msgID: %d). Falling back to single-client downloader...", fwdErr, fwdMsgID)
							}
						}
					}

					// Fallback to single-client chunk downloader if multi-bot was not active or failed
					if !dlSuccess {
						uTask.SetStatus(types.StatusDownloading, "Telegram MTProto")
						downloadClient := m.rawAPI
						if m.userRawAPI != nil {
							downloadClient = m.userRawAPI
						}
						chunkDL := downloader.NewTelegramChunkDownloader(downloadClient)
						loc := doc.AsInputDocumentFileLocation("")
						if loc != nil {
							refreshRef := func() ([]byte, error) {
								if msg.IsReply && msg.ReplyToID > 0 {
									rMsg, err := telegram.FetchMessage(ctx, m.rawAPI, peer, msg.ReplyToID)
									if err == nil && rMsg != nil {
										if dMedia, ok := rMsg.Media.(*tg.MessageMediaDocument); ok {
											if dDoc, ok := dMedia.Document.(*tg.Document); ok {
												return dDoc.FileReference, nil
											}
										}
									}
								}
								return nil, fmt.Errorf("failed to refresh file reference")
							}

							err := chunkDL.DownloadToFile(ctx, loc, doc.Size, destPath, func(curr, total int64, speed float64, eta string) {
								uTask.SetProgress(curr, total, speed, eta)
							}, refreshRef)

							if err == nil {
								downloadedPath = destPath
								uTask.SetProgress(doc.Size, doc.Size, 0, "-")
								dlErr = nil
							} else {
								log.Printf("[LEECH] MTProto document download error: %v", err)
								dlErr = err
							}
						} else {
							dlErr = fmt.Errorf("failed to get Telegram document file location")
						}
					}
				} else {
					dlErr = fmt.Errorf("replied document is empty or unavailable")
				}
			} else if photoMedia, ok := mediaSource.(*tg.MessageMediaPhoto); ok {
				// Case 1B: Photo Media
				if photo, ok := photoMedia.Photo.(*tg.Photo); ok {
					fileName := fmt.Sprintf("photo_%d.jpg", photo.ID)
					if opt.CustomName != "" {
						fileName = opt.CustomName
						if !strings.HasSuffix(strings.ToLower(fileName), ".jpg") && !strings.HasSuffix(strings.ToLower(fileName), ".png") {
							fileName += ".jpg"
						}
					}
					fileName = strings.ReplaceAll(fileName, "/", "_")
					fileName = strings.ReplaceAll(fileName, "\\", "_")
					destPath := filepath.Join(taskDir, fileName)
					_ = os.MkdirAll(filepath.Dir(destPath), 0777)
					uTask.SetName(fileName)
					uTask.SetStatus(types.StatusDownloading, "Telegram Photo")

					thumbSize := "m"
					if len(photo.Sizes) > 0 {
						switch s := photo.Sizes[len(photo.Sizes)-1].(type) {
						case *tg.PhotoSize:
							thumbSize = s.Type
						case *tg.PhotoSizeProgressive:
							thumbSize = s.Type
						}
					}

					dl := tdDownloader.NewDownloader()
					loc := photo.AsInputPhotoFileLocation(thumbSize)
					if loc != nil {
						_, err := dl.Download(m.rawAPI, loc).ToPath(ctx, destPath)
						if err == nil {
							downloadedPath = destPath
							if fi, statErr := os.Stat(destPath); statErr == nil {
								uTask.SetSize(fi.Size())
								uTask.SetProgress(fi.Size(), fi.Size(), 0, "-")
							}
						} else {
							log.Printf("[LEECH] MTProto photo download error: %v", err)
							dlErr = err
						}
					} else {
						dlErr = fmt.Errorf("failed to get Telegram photo location")
					}
				} else {
					dlErr = fmt.Errorf("replied photo is empty or unavailable")
				}
			} else {
				dlErr = fmt.Errorf("unsupported media in replied message")
			}
		}
	}

	if downloadedPath != "" {
		// Document downloaded directly via MTProto
	} else if downloader.IsGDriveLink(link) {
		gdrive := downloader.GetGDriveClient()
		fileID := downloader.ExtractGDriveID(link)
		if fileID == "" {
			m.sendDownloadError(ctx, peer, msg, notifyID, "Google Drive Error: Unable to extract file/folder ID", modeStr, taskStartTime)
			return
		}

		info, err := gdrive.GetFileInfo(fileID)
		if err != nil {
			m.sendDownloadError(ctx, peer, msg, notifyID, fmt.Sprintf("Google Drive Error: %v", err), modeStr, taskStartTime)
			return
		}

		if info.IsDir {
			files, fErr := gdrive.GetFolderFiles(fileID, "")
			if fErr != nil || len(files) == 0 {
				m.sendDownloadError(ctx, peer, msg, notifyID, "Google Drive Error: No files found in folder", modeStr, taskStartTime)
				return
			}

			filesToDownload := files
			if opt.Select {
				uidStr := fmt.Sprintf("%d", notifyID)
				doneChan := RegisterGDriveSelectCache(uidStr, files, msg.UserID)
				buttons := BuildGDriveSelectionButtons(uidStr)
				_ = m.sender.EditMessage(ctx, peer, notifyID, "Your Google Drive download paused. Choose files from selection web link then press Done Selecting button.", buttons)

				select {
				case ok := <-doneChan:
					if !ok {
						m.sendDownloadError(ctx, peer, msg, notifyID, "Google Drive file selection cancelled by user", modeStr, taskStartTime)
						return
					}
					selectedIDs := web.GDriveStore.ReadSelection(uidStr)
					var filtered []downloader.GDriveFile
					for _, f := range files {
						if !f.IsDir && containsString(selectedIDs, f.ID) {
							filtered = append(filtered, f)
						}
					}
					if len(filtered) == 0 {
						m.sendDownloadError(ctx, peer, msg, notifyID, "No files selected. Download cancelled.", modeStr, taskStartTime)
						return
					}
					filesToDownload = filtered
				case <-ctx.Done():
					return
				}
			}

			uTask.SetStatus(types.StatusDownloading, "Google Drive")
			_ = m.sender.EditMessage(ctx, peer, notifyID, fmt.Sprintf("<i>Downloading %d Google Drive files...</i>", len(filesToDownload)), nil)
			for i, f := range filesToDownload {
				if f.IsDir {
					continue
				}
				uTask.SetName(fmt.Sprintf("[%d/%d] %s", i+1, len(filesToDownload), f.Name))
				subDir := filepath.Join(taskDir, f.Path)
				_ = os.MkdirAll(subDir, 0755)
				_ = gdrive.DownloadFile(ctx, f.ID, subDir, f.Name, func(curr, total int64, speed float64) {
					rem := total - curr
					eta := "-"
					if speed > 0 && rem > 0 {
						eta = ext.GetReadableTime(int64(float64(rem) / speed))
					}
					uTask.SetProgress(curr, total, speed, eta)
				})
			}
			downloadedPath = taskDir
		} else {
			fName := info.Name
			if opt.CustomName != "" {
				fName = opt.CustomName
			}
			uTask.SetName(fName)
			uTask.SetStatus(types.StatusDownloading, "Google Drive")
			dlErr = gdrive.DownloadFile(ctx, fileID, taskDir, fName, func(curr, total int64, speed float64) {
				rem := total - curr
				eta := "-"
				if speed > 0 && rem > 0 {
					eta = ext.GetReadableTime(int64(float64(rem) / speed))
				}
				uTask.SetProgress(curr, total, speed, eta)
			})
			downloadedPath = filepath.Join(taskDir, fName)
		}
	} else if ext.IsMegaLink(link) {
		megaDl := downloader.NewMegaDownloader()
		if opt.Select {
			uidStr := fmt.Sprintf("%d", notifyID)
			mockFiles := []downloader.MegaFileInfo{
				{ID: "0", Name: opt.CustomName, Size: 0, Selected: true},
			}
			doneChan := RegisterMegaSelectCache(uidStr, mockFiles, msg.UserID)
			buttons := BuildMegaSelectionButtons(uidStr)
			_ = m.sender.EditMessage(ctx, peer, notifyID, "Your MEGA download paused. Choose files from selection web link then press Done Selecting button.", buttons)

			select {
			case ok := <-doneChan:
				if !ok {
					m.sendDownloadError(ctx, peer, msg, notifyID, "MEGA file selection cancelled by user", modeStr, taskStartTime)
					return
				}
			case <-ctx.Done():
				return
			}
		}
		uTask.SetStatus(types.StatusDownloading, "Mega")
		downloadedPath, dlErr = megaDl.Download(ctx, link, taskDir, func(cur, total int64, speed float64, eta string) {
			uTask.SetProgress(cur, total, speed, eta)
		})
	} else if ext.IsMagnet(link) || strings.Contains(strings.ToLower(link), ".torrent") || ext.IsURL(link) || (func() bool { fi, err := os.Stat(link); return err == nil && !fi.IsDir() }()) {
		cleanLink := strings.Split(strings.TrimSpace(link), "?")[0]
		isLocalTorrent := false
		if fi, err := os.Stat(link); err == nil && !fi.IsDir() && strings.HasSuffix(strings.ToLower(link), ".torrent") {
			isLocalTorrent = true
		}
		isRemoteTorrent := ext.IsURL(link) && strings.HasSuffix(strings.ToLower(cleanLink), ".torrent")

		aria := downloader.GetAria2Client()

		if isLocalTorrent {
			log.Printf("[LEECH] Adding local .torrent file to Aria2c: %s", link)
			gid, err := aria.AddTorrent(link, taskDir)
			if err != nil {
				m.sendDownloadError(ctx, peer, msg, notifyID, fmt.Sprintf("Torrent Error: %v", err), modeStr, taskStartTime)
				return
			}
			task := downloader.NewAria2DownloadTask(gid, opt.CustomName, taskDir, notifyID, msg.ChatID, msg.UserID)
			downloadedPath, dlErr = task.PollUntilComplete(ctx, func(cur, total int64) {
				uTask.SetProgress(cur, total, task.Speed(), task.ETA())
				if task.Name() != "" {
					uTask.SetName(task.Name())
				}
			})
		} else if isRemoteTorrent {
			log.Printf("[LEECH] Downloading remote .torrent metadata: %s", link)
			torrentDest := filepath.Join(taskDir, fmt.Sprintf("source_%d.torrent", time.Now().UnixNano()))
			if err := ext.DownloadHTTPFile(ctx, link, torrentDest); err == nil {
				gid, tErr := aria.AddTorrent(torrentDest, taskDir)
				if tErr == nil {
					_ = os.Remove(torrentDest)
					task := downloader.NewAria2DownloadTask(gid, opt.CustomName, taskDir, notifyID, msg.ChatID, msg.UserID)
					downloadedPath, dlErr = task.PollUntilComplete(ctx, func(cur, total int64) {
						uTask.SetProgress(cur, total, task.Speed(), task.ETA())
						if task.Name() != "" {
							uTask.SetName(task.Name())
						}
					})
				} else {
					log.Printf("[LEECH] AddTorrent error, fallback to AddURI: %v", tErr)
					gid, aErr := aria.AddURI(link, taskDir, opt.CustomName, nil)
					if aErr != nil {
						m.sendDownloadError(ctx, peer, msg, notifyID, fmt.Sprintf("Download Error: %v", aErr), modeStr, taskStartTime)
						return
					}
					task := downloader.NewAria2DownloadTask(gid, opt.CustomName, taskDir, notifyID, msg.ChatID, msg.UserID)
					downloadedPath, dlErr = task.PollUntilComplete(ctx, func(cur, total int64) {
						uTask.SetProgress(cur, total, task.Speed(), task.ETA())
						if task.Name() != "" {
							uTask.SetName(task.Name())
						}
					})
				}
			} else {
				log.Printf("[LEECH] DownloadHTTPFile error, fallback to AddURI: %v", err)
				gid, aErr := aria.AddURI(link, taskDir, opt.CustomName, nil)
				if aErr != nil {
					m.sendDownloadError(ctx, peer, msg, notifyID, fmt.Sprintf("Download Error: %v", aErr), modeStr, taskStartTime)
					return
				}
				task := downloader.NewAria2DownloadTask(gid, opt.CustomName, taskDir, notifyID, msg.ChatID, msg.UserID)
				downloadedPath, dlErr = task.PollUntilComplete(ctx, func(cur, total int64) {
					uTask.SetProgress(cur, total, task.Speed(), task.ETA())
					if task.Name() != "" {
						uTask.SetName(task.Name())
					}
				})
			}
		} else {
			// All other magnets, torrents, and DDL links are routed through Aria2c with 16 connections
			// Make URI distinct with fragment tag so concurrent duplicate URLs download into separate task directories independently
			ariaLink := link
			if strings.HasPrefix(ariaLink, "http://") || strings.HasPrefix(ariaLink, "https://") {
				if strings.Contains(ariaLink, "#") {
					ariaLink += "_" + taskUID
				} else {
					ariaLink += "#" + taskUID
				}
			}
			gid, err := aria.AddURI(ariaLink, taskDir, opt.CustomName, nil)
			if err != nil {
				// If Aria2c is unreachable or errors on HTTP/HTTPS DDL link, seamlessly fallback to DirectDownloader!
				if ext.IsURL(link) && (strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://")) {
					log.Printf("[LEECH] Aria2c AddURI error: %v, falling back to DirectDownloader for %s", err, link)
					directDl := downloader.NewDirectDownloader()
					dest := filepath.Join(taskDir, "downloaded_file")
					if opt.CustomName != "" {
						dest = filepath.Join(taskDir, opt.CustomName)
					}
					uTask.SetStatus(types.StatusDownloading, "Direct HTTP")
					dlErr = directDl.Download(ctx, link, dest, func(cur, total int64, speed float64, eta string) {
						uTask.SetProgress(cur, total, speed, eta)
					})
					downloadedPath = dest
				} else {
					m.sendDownloadError(ctx, peer, msg, notifyID, fmt.Sprintf("Download Error: %v", err), modeStr, taskStartTime)
					return
				}
			} else {
				if opt.Select {
					cfg := config.Get()
					baseURL := cfg.BaseURL
					if baseURL == "" {
						baseURL = fmt.Sprintf("http://localhost:%d", cfg.BaseURLPort)
					}
					b := telegram.NewButtonMaker()
					b.UButtonStyled("◈ "+telegram.ToSmallCaps("Select Files"), fmt.Sprintf("%s/app/files/%s", strings.TrimSuffix(baseURL, "/"), gid), "primary")
					_ = m.sender.EditMessage(ctx, peer, notifyID, "Your torrent download paused. Choose files from selection web link then press Confirm on web page.", b.BuildMenu(1))
				}

				task := downloader.NewAria2DownloadTask(gid, opt.CustomName, taskDir, notifyID, msg.ChatID, msg.UserID)
				downloadedPath, dlErr = task.PollUntilComplete(ctx, func(cur, total int64) {
					uTask.SetProgress(cur, total, task.Speed(), task.ETA())
					if task.Name() != "" {
						uTask.SetName(task.Name())
					}
				})
			}
		}
	} else if link != "" {
		// Fallback Direct HTTP
		directDl := downloader.NewDirectDownloader()
		dest := filepath.Join(taskDir, "downloaded_file")
		if opt.CustomName != "" {
			dest = filepath.Join(taskDir, opt.CustomName)
		}
		uTask.SetStatus(types.StatusDownloading, "Direct HTTP")
		dlErr = directDl.Download(ctx, link, dest, func(cur, total int64, speed float64, eta string) {
			uTask.SetProgress(cur, total, speed, eta)
		})
		downloadedPath = dest
	} else {
		if dlErr == nil {
			dlErr = fmt.Errorf("no media or valid link found to download")
		}
	}

	if dlErr != nil {
		ext.ReleaseDownload(taskUID)
		if errors.Is(dlErr, context.Canceled) || strings.Contains(strings.ToLower(dlErr.Error()), "cancel") {
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
		m.sendDownloadError(ctx, peer, msg, notifyID, dlErr.Error(), modeStr, taskStartTime)
		return
	}
	ext.ReleaseDownload(taskUID)

	// Critical safeguard: If downloadedPath is a .torrent file, automatically extract and download its real payload!
	if strings.HasSuffix(strings.ToLower(downloadedPath), ".torrent") {
		log.Printf("[LEECH] Downloaded file is a .torrent file (%s), starting real payload download...", downloadedPath)
		torrentFile := downloadedPath
		_ = m.sender.EditMessage(ctx, peer, notifyID, "<i>Processing .torrent metadata & starting payload download...</i>", nil)
		aria := downloader.GetAria2Client()
		gid, err := aria.AddTorrent(torrentFile, taskDir)
		if err == nil {
			_ = os.Remove(torrentFile)
			task := downloader.NewAria2DownloadTask(gid, opt.CustomName, taskDir, notifyID, msg.ChatID, msg.UserID)
			downloadedPath, dlErr = task.PollUntilComplete(ctx, func(cur, total int64) {
				uTask.SetProgress(cur, total, task.Speed(), task.ETA())
				if task.Name() != "" {
					uTask.SetName(task.Name())
				}
			})
			if dlErr != nil {
				_ = m.sender.EditMessage(ctx, peer, notifyID, fmt.Sprintf("<b>Torrent Download Failed:</b> %v", dlErr), nil)
				return
			}
		}
	}

	fi, statErr := os.Stat(downloadedPath)
	if statErr != nil {
		m.sendDownloadError(ctx, peer, msg, notifyID, fmt.Sprintf("Downloaded file not found on disk: %v", statErr), modeStr, taskStartTime)
		return
	}
	if !fi.IsDir() {
		if fi.Size() == 0 {
			m.sendDownloadError(ctx, peer, msg, notifyID, "Downloaded file is empty (0 bytes)", modeStr, taskStartTime)
			return
		}
		uTask.SetSize(fi.Size())
		uTask.SetProgress(fi.Size(), fi.Size(), 0, "-")
	}
	displayName = filepath.Base(downloadedPath)
	uTask.SetName(displayName)

	// 2. Post-processing: Forceful Archive Extraction (/leech -e, -extract, -uz, -p)
	// Reads binary magic bytes (PK, 7z, Rar!, etc.) even if the file extension was disguised or missed
	if opt.IsExtract {
		uTask.SetStatus(types.StatusExtracting, ext.GetExtractEngineName())
		isArch, archType := ext.IsArchive(downloadedPath)
		if isArch {
			log.Printf("[LEECH -e] Detected archive format '%s' via binary header for: %s", archType, downloadedPath)
			extractDir := filepath.Join(taskDir, "extracted")
			if outDir, err := ext.ForceExtractArchive(downloadedPath, extractDir, opt.Password); err == nil {
				_ = os.Remove(downloadedPath)
				_ = os.Remove(downloadedPath + ".hatchzip")
				_ = os.Remove(downloadedPath + ".hatchzip-tail")
				downloadedPath = outDir
			} else {
				log.Printf("[LEECH -e] Extraction error: %v, continuing with original file", err)
				_ = m.sender.EditMessage(ctx, peer, notifyID, fmt.Sprintf("⌬ <b>Extraction Warning:</b> %v\nUploading original archive.", err), nil)
			}
		} else if fi, err := os.Stat(downloadedPath); err == nil && fi.IsDir() {
			_ = filepath.Walk(downloadedPath, func(p string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					if isA, aType := ext.IsArchive(p); isA {
						log.Printf("[LEECH -e] Extracting embedded %s archive: %s", aType, p)
						extDir := filepath.Join(filepath.Dir(p), "extracted")
						if _, eErr := ext.ForceExtractArchive(p, extDir, opt.Password); eErr == nil {
							_ = os.Remove(p)
							_ = os.Remove(p + ".hatchzip")
							_ = os.Remove(p + ".hatchzip-tail")
						}
					}
				}
				return nil
			})
		}
	} else if opt.IsZip {
		uTask.SetStatus(types.StatusArchiving, "7-Zip")
		// Compression requested (-z / -zip)
		zipTarget := filepath.Join(taskDir, filepath.Base(downloadedPath)+".zip")
		if err := ext.CompressToZip(downloadedPath, zipTarget, opt.Password); err == nil {
			_ = os.RemoveAll(downloadedPath)
			downloadedPath = zipTarget
		}
	}

	// 3. Post-processing: Stream extraction if requested
	if opt.ExtractStrm {
		strmDir := filepath.Join(taskDir, "extracted_streams")
		if outputs, err := ext.ExtractStreams(downloadedPath, strmDir, []string{"video", "audio", "subtitle"}); err == nil && len(outputs) > 0 {
			downloadedPath = outputs[0]
		}
	}

	// Custom FFmpeg commands (-ff)
	if opt.FFmpegKey != "" && cfg.EnableFFmpegCmds {
		uTask.SetStatus(types.StatusFFmpeg, "FFmpeg")
		rawCmd := opt.FFmpegKey
		if user != nil && user.FFmpegCommands != nil {
			if preset, ok := user.FFmpegCommands[opt.FFmpegKey]; ok {
				rawCmd = preset
			}
		}
		ffDir := filepath.Join(taskDir, "ff_tmp")
		_ = os.MkdirAll(ffDir, 0755)
		outFFmpeg := filepath.Join(ffDir, filepath.Base(downloadedPath))
		if fErr := ext.ExecuteCustomFFmpeg(downloadedPath, outFFmpeg, rawCmd, cfg.FFmpegEncoding); fErr == nil {
			_ = os.Remove(downloadedPath)
			downloadedPath = outFFmpeg
		} else {
			log.Printf("[FFMPEG] Custom ffmpeg error: %v", fErr)
		}
	}

	// 3. Post-processing: Metadata editing
	// Collect all files to process: single file or directory of files
	var filesToUpload []string
	if fi, err := os.Stat(downloadedPath); err == nil && fi.IsDir() {
		_ = filepath.Walk(downloadedPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			// Smart rename and metadata processing operate only on regular files. Do not
			// follow or process symlinks/special files discovered inside downloaded folders.
			if info.Mode().IsRegular() {
				base := filepath.Base(path)
				if !strings.HasPrefix(base, ".") && !strings.HasSuffix(base, ".aria2") {
					filesToUpload = append(filesToUpload, path)
				}
			}
			return nil
		})
	} else {
		filesToUpload = []string{downloadedPath}
	}

	if len(filesToUpload) == 0 {
		_ = m.sender.EditMessage(ctx, peer, notifyID, "× <b>Download Failed:</b> No payload files found", nil)
		return
	}

	// Queue Check for Upload (Sleep v0 Engine)
	var totalUploadSize int64
	for _, f := range filesToUpload {
		if fi, err := os.Stat(f); err == nil {
			totalUploadSize += fi.Size()
		}
	}

	queuedUP, qUpErr := ext.AcquireUploadQueue(ctx, taskUID, displayName, totalUploadSize, msg.ChatID, msg.UserID, notifyID, cancel)
	if qUpErr != nil {
		_ = m.sender.EditMessage(ctx, peer, notifyID, "× <i>Upload task cancelled while in queue</i>", nil)
		return
	}
	defer ext.ReleaseUpload(taskUID)

	if queuedUP {
		_ = m.sender.EditMessage(ctx, peer, notifyID, "<i>Starting upload from queue...</i>", nil)
	}

	dlDuration = time.Since(dlStartTime)
	if dlDuration.Seconds() < 1 {
		dlDuration = 1 * time.Second
	}
	ulStartTime = time.Now()

	uTask.SetStatus(types.StatusUploading, "MTProto")
	if notifyID > 0 {
		text, btn := m.taskMgr.BuildStatusMessage(1)
		_ = m.sender.EditMessageWithButtons(ctx, peer, notifyID, text, btn, 3)
	}
	if m.statusMod != nil {
		go m.statusMod.UpdateAllMessages(context.Background(), true)
	}

	var dumpID int64
	if opt.UserDump != 0 {
		dumpID = opt.UserDump
	} else if user != nil && user.DumpChatID != 0 {
		dumpID = user.DumpChatID
	}

	for _, curFile := range filesToUpload {
		originalIncomingName := filepath.Base(curFile)
		origBase := filepath.Base(curFile)
		origBase = strings.TrimPrefix(origBase, "meta_")
		origBase = strings.TrimPrefix(origBase, "ff_")

		// Metadata editing
		if user != nil && len(user.Metadata) > 0 {
			uTask.SetStatus(types.StatusFFmpeg, "FFmpeg")
			metaDir := filepath.Join(taskDir, "meta_tmp")
			_ = os.MkdirAll(metaDir, 0755)
			outMeta := filepath.Join(metaDir, origBase)
			if err := ext.EditMetadata(curFile, outMeta, user.Metadata); err == nil {
				_ = os.Remove(curFile)
				curFile = outMeta
			} else {
				log.Printf("[LEECH Metadata] Warning: EditMetadata failed for %s: %v (using original)", origBase, err)
			}
		}

		// File Renaming. Smart mode is shared with yt-dlp and is skipped for explicit custom names.
		finalName, caption := "", ""
		smartApplied := false
		if user != nil && user.Autorename && strings.EqualFold(user.RenameMethod, "smart") && opt.CustomName == "" {
			if !cfg.SmartRenameEnabled {
				log.Printf("[LEECH Smart AutoRename] disabled by configuration; keeping %s", originalIncomingName)
				finalName = filepath.Base(curFile)
				caption = ext.BuildSmartFallbackCaption(curFile, user, originalIncomingName)
				smartApplied = true
			} else {
				smartResult, smartCaption, smartErr := ext.PrepareSmartRename(ctx, curFile, user, originalIncomingName)
				if smartErr != nil {
					log.Printf("[LEECH Smart AutoRename] %s: %v; keeping original path", originalIncomingName, smartErr)
					finalName = filepath.Base(curFile)
					caption = ext.BuildSmartFallbackCaption(curFile, user, originalIncomingName)
					smartApplied = true
				} else {
					ext.LogSmartRenameWarnings(smartResult)
					finalName, caption, curFile, smartApplied = smartResult.PhysicalFilename, smartCaption, smartResult.PhysicalPath, true
				}
			}
		}
		if !smartApplied {
			finalName, caption = ext.FormatFilename(origBase, user, curFile)
			if opt.CustomName != "" && len(filesToUpload) == 1 {
				finalName = opt.CustomName
			}
			if filepath.Ext(finalName) == "" {
				if origExt := filepath.Ext(origBase); origExt != "" {
					finalName += origExt
				} else if detectedExt := ext.DetectExtension(curFile); detectedExt != "" {
					finalName += detectedExt
				}
			}
			finalName = ext.SanitizeFilename(finalName)
			renamedPath := filepath.Join(filepath.Dir(curFile), finalName)
			if renamedPath != curFile {
				if renameErr := ext.SafeRenameFile(curFile, finalName); renameErr != nil {
					attemptedName := finalName
					log.Printf("[LEECH Rename] Could not rename %s to %s: %v; keeping original path", filepath.Base(curFile), finalName, renameErr)
					finalName = filepath.Base(curFile)
					caption = strings.ReplaceAll(caption, attemptedName, finalName)
				} else {
					curFile = renamedPath
				}
			}
		}
		if finalName == "" {
			finalName = filepath.Base(curFile)
		}
		uTask.SetName(finalName)
		uploadedNames = append(uploadedNames, finalName)

		// Screenshots
		var ssLinks []string
		if opt.Screenshots > 0 || cfg.ScreenshotsMode {
			ssCount := opt.Screenshots
			if ssCount <= 0 {
				ssCount = 4
			}
			ssLinks, _ = ext.TakeScreenshots(curFile, ssCount)
		}

		// Thumbnail & HD Cover
		customThumb := ""
		if user != nil && user.CustomThumb != "" {
			customThumb = user.CustomThumb
		}
		autoThumbEnabled := cfg.AutoThumbnail
		if user != nil && user.AutoThumb {
			autoThumbEnabled = true
		}

		// Effective upload as document resolution:
		// 1. Bot-wide default (cfg.AsDocument, default true)
		// 2. User setting overrides if user exists (user.AsDoc)
		// 3. Command flags (-d/-doc forces document, -m/-med/-media forces media)
		asDoc := cfg.AsDocument
		if user != nil {
			asDoc = user.AsDoc
		}
		if opt.AsDoc {
			asDoc = true
		} else if opt.AsMedia {
			asDoc = false
		}

		if customThumb == "" && autoThumbEnabled {
			if tmdbThumb, err := ext.GetAutoThumbnail(ctx, finalName, asDoc, cfg.LeechFilenameRemnameRegex); err == nil && tmdbThumb != "" {
				customThumb = tmdbThumb
			}
		}
		thumbPath := ext.GenerateThumbnail(curFile, customThumb)
		coverPath := ext.GenerateVideoCover(curFile, customThumb)

		// File Splitting
		uTask.SetStatus(types.StatusSplitting, "7-Zip")
		maxSplit := config.GetMaxSplitSize()
		splitSize := cfg.LeechSplitSize
		if user != nil && user.SplitSize > 0 {
			splitSize = user.SplitSize
		}
		if splitSize > maxSplit || splitSize <= 0 {
			splitSize = maxSplit
		}

		equalSplits := cfg.EqualSplits
		if user != nil {
			equalSplits = user.EqualSplits
		}

		splitDir := filepath.Join(taskDir, "splits")
		splitParts, splitErr := ext.SplitFile(curFile, splitSize, splitDir, equalSplits)
		if splitErr != nil || len(splitParts) == 0 {
			splitParts = []string{curFile}
		}

		fi, _ := os.Stat(curFile)
		var itemSize int64
		if fi != nil {
			itemSize = fi.Size()
		}
		totalSize += itemSize

		uTask.SetStatus(types.StatusUploading, "MTProto")
		uTask.SetSize(itemSize)
		uTask.SetProgress(0, itemSize, 0, "-")

		for idx, part := range splitParts {
			partCaption := caption
			if len(splitParts) > 1 {
				partCaption = fmt.Sprintf("<b>Part %d/%d</b>\n%s", idx+1, len(splitParts), caption)
			}
			if len(ssLinks) > 0 {
				partCaption += fmt.Sprintf("\n\n◈ <a href=\"%s\">Screenshots</a>", ssLinks[0])
			}

			botPM := cfg.BotPM || (user != nil && user.BotPM) || (msg.ChatID != msg.UserID)

			upOpt := uploader.UploadOptions{
				FilePath:   part,
				FileName:   filepath.Base(part),
				Caption:    partCaption,
				ThumbPath:  thumbPath,
				CoverPath:  coverPath,
				AsDoc:      asDoc,
				DumpChatID: dumpID,
				LogChatID:  cfg.LeechLogID,
				UserID:     msg.UserID,
				BotPM:      botPM,
				IsPremium:  isPremium,
				GID:        taskUID,
				OnProgress: func(curr, total int64, speed float64, eta string) {
					if speed > maxULSpeed {
						maxULSpeed = speed
					}
					uTask.SetProgress(curr, total, speed, eta)
				},
			}

			_, upErr := m.uploader.UploadFile(ctx, peer, upOpt)
			if upErr != nil {
				log.Printf("[AZML-Go Leech] Upload error for %s: %v", part, upErr)
				m.sendDownloadError(ctx, peer, msg, notifyID, fmt.Sprintf("Upload Failed: %v", upErr), modeStr, taskStartTime)
				return
			}
		}

		if thumbPath != "" && thumbPath != customThumb {
			_ = os.Remove(thumbPath)
		}
		if coverPath != "" && coverPath != customThumb {
			_ = os.Remove(coverPath)
		}
		ext.RecordDailyUsage(msg.UserID, itemSize)
	}

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

	// Build completion message exactly matching Image 1
	var sb strings.Builder
	mainName := displayName
	if len(uploadedNames) > 0 {
		mainName = uploadedNames[0]
	}
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n|\n", mainName))
	sb.WriteString(fmt.Sprintf("├── <b>Size:</b> %s\n", ext.GetReadableFileSize(totalSize)))
	sb.WriteString(fmt.Sprintf("├── <b>Elapsed:</b> %s\n", ext.GetReadableTime(int64(overallElapsed.Seconds()))))
	sb.WriteString(fmt.Sprintf("├── <b>Avg DL Speed:</b> %s/s\n", ext.GetReadableFileSize(int64(avgDLSpeed))))
	sb.WriteString(fmt.Sprintf("├── <b>Avg UL Speed:</b> %s/s\n", ext.GetReadableFileSize(int64(avgULSpeed))))
	sb.WriteString(fmt.Sprintf("├── <b>Mode:</b> %s\n", modeStr))
	sb.WriteString(fmt.Sprintf("├── <b>Max DL Speed:</b> %s/s\n", ext.GetReadableFileSize(int64(maxDLSpeed))))
	sb.WriteString(fmt.Sprintf("├── <b>Max UL Speed:</b> %s/s\n", ext.GetReadableFileSize(int64(maxULSpeed))))
	sb.WriteString(fmt.Sprintf("├── <b>Total Files:</b> %d\n", len(uploadedNames)))

	userTag := fmt.Sprintf("@%s", msg.Username)
	if msg.Username == "" {
		userTag = fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a>", msg.UserID, msg.SenderName)
	}
	sb.WriteString(fmt.Sprintf("└── <b>By:</b> %s\n\n", userTag))

	botPM := cfg.BotPM || (user != nil && user.BotPM) || (msg.ChatID != msg.UserID)
	if botPM {
		sb.WriteString("➲ <i>File(s) have been Sent to Bot PM (Private)</i>\n")
	} else {
		sb.WriteString("➲ <i>File(s) have been Sent. Access via Links...</i>\n")
	}

	for idx, upF := range uploadedNames {
		if idx >= 10 {
			sb.WriteString(fmt.Sprintf("<i>...and %d more file(s)</i>\n", len(uploadedNames)-10))
			break
		}
		sb.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", idx+1, upF))
	}

	botUser := telegram.CurrentBotUsername
	if botUser == "" {
		botUser = "Yukiileechrrbot"
	}
	finishBtn := telegram.NewButtonMaker()
	finishBtn.UButtonStyled("◈ "+telegram.ToSmallCaps("View in Bot PM"), fmt.Sprintf("https://t.me/%s", botUser), "primary")

	// Send completion message into the group / chat where leech was initiated, replying to user's command
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

func splitArgsRespectingQuotes(s string) []string {
	var tokens []string
	var current strings.Builder
	inDouble := false
	inSingle := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '"':
			if !inSingle {
				inDouble = !inDouble
			} else {
				current.WriteByte(ch)
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			} else {
				current.WriteByte(ch)
			}
		case ' ', '\t', '\n', '\r':
			if inDouble || inSingle {
				current.WriteByte(ch)
			} else if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func isLeechFlag(tok string) bool {
	switch tok {
	case "-b", "-bulk", "-z", "-zip", "-e", "-extract", "-uz", "-unzip",
		"-d", "-doc", "-asdoc", "-m", "-med", "-media", "-asmedia",
		"-st", "-streams", "-n", "-name", "-p", "-password",
		"-pssw", "-ss", "-screenshots", "-ud", "-dump", "-ff", "-i", "-s", "-select":
		return true
	default:
		return false
	}
}

func parseLeechArgs(text string) (types.LeechOptions, string) {
	var opt types.LeechOptions
	tokens := splitArgsRespectingQuotes(text)
	if len(tokens) <= 1 {
		return opt, ""
	}

	var link string
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		switch tok {
		case "-b", "-bulk":
			opt.Bulk = true
			if i+1 < len(tokens) && !isLeechFlag(tokens[i+1]) && !ext.IsURL(tokens[i+1]) && !ext.IsMagnet(tokens[i+1]) && strings.Contains(tokens[i+1], ":") {
				opt.BulkRange = tokens[i+1]
				i++
			}
		case "-z", "-zip":
			opt.IsZip = true
		case "-e", "-extract", "-uz", "-unzip":
			opt.IsExtract = true
		case "-d", "-doc", "-asdoc":
			opt.AsDoc = true
		case "-m", "-med", "-media", "-asmedia":
			opt.AsMedia = true
		case "-st", "-streams":
			opt.ExtractStrm = true
		case "-n", "-name":
			var nameParts []string
			for i+1 < len(tokens) {
				nextTok := tokens[i+1]
				if isLeechFlag(nextTok) || ext.IsURL(nextTok) || ext.IsMagnet(nextTok) || ext.IsMegaLink(nextTok) || strings.HasPrefix(nextTok, "tg://") {
					break
				}
				nameParts = append(nameParts, nextTok)
				i++
			}
			if len(nameParts) > 0 {
				opt.CustomName = strings.Join(nameParts, " ")
			}
		case "-p", "-password", "-pssw":
			if i+1 < len(tokens) {
				opt.Password = tokens[i+1]
				i++
			}
		case "-ss", "-screenshots":
			if i+1 < len(tokens) {
				opt.Screenshots, _ = strconv.Atoi(tokens[i+1])
				i++
			}
		case "-ud", "-dump":
			if i+1 < len(tokens) {
				opt.UserDump, _ = strconv.ParseInt(tokens[i+1], 10, 64)
				i++
			}
		case "-ff":
			if i+1 < len(tokens) {
				opt.FFmpegKey = tokens[i+1]
				i++
			}
		case "-i":
			if i+1 < len(tokens) {
				opt.Multi, _ = strconv.Atoi(tokens[i+1])
				i++
			}
		case "-s", "-select":
			opt.Select = true
		default:
			if strings.HasPrefix(tok, "-p") && len(tok) > 2 {
				opt.Password = strings.TrimPrefix(tok, "-p")
			} else if strings.HasPrefix(strings.ToLower(tok), "pssw:") {
				val := strings.TrimPrefix(tok, "pssw:")
				if val != "" {
					opt.Password = val
				} else if i+1 < len(tokens) {
					opt.Password = tokens[i+1]
					i++
				}
			} else if strings.HasPrefix(strings.ToLower(tok), "password:") {
				val := strings.TrimPrefix(tok, "password:")
				if val != "" {
					opt.Password = val
				} else if i+1 < len(tokens) {
					opt.Password = tokens[i+1]
					i++
				}
			} else if ext.IsURL(tok) || ext.IsMagnet(tok) || ext.IsMegaLink(tok) || strings.HasPrefix(tok, "tg://") {
				link = tok
			}
		}
	}
	// If password provided and user didn't specify zip, default to extract
	if opt.Password != "" && !opt.IsZip {
		opt.IsExtract = true
	}
	return opt, link
}
