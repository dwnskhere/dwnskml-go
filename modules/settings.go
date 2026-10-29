package modules

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"azml-go/config"
	"azml-go/helper/ext"
	"azml-go/helper/telegram"
	"azml-go/types"

	tdDownloader "github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

type SettingsModule struct {
	rawAPI  *tg.Client
	sender  *telegram.MessageSender
	userMap *types.SafeUserMap
}

func NewSettingsModule(rawAPI *tg.Client, sender *telegram.MessageSender, userMap *types.SafeUserMap) *SettingsModule {
	return &SettingsModule{
		rawAPI:  rawAPI,
		sender:  sender,
		userMap: userMap,
	}
}

func (m *SettingsModule) HandleSettings(ctx context.Context, peer tg.InputPeerClass, msg *types.MessageContext) {
	u := m.userMap.GetOrCreate(msg.UserID)
	parts := strings.Fields(msg.Text)

	// Check if this was a thumbnail command (/setthumb, /t, /thumb, or /us thumb)
	isThumbCmd := false
	if len(parts) >= 1 {
		cmd := strings.ToLower(parts[0])
		if strings.Contains(cmd, "setthumb") || strings.Contains(cmd, "thumb") || cmd == "/t" || strings.HasPrefix(cmd, "/t@") {
			isThumbCmd = true
		}
	}
	if len(parts) >= 2 {
		subCmd := strings.ToLower(parts[1])
		if subCmd == "thumb" || subCmd == "setthumb" || subCmd == "t" {
			isThumbCmd = true
		}
	}

	if isThumbCmd {
		if msg.IsReply && msg.ReplyToID != 0 && m.rawAPI != nil {
			res, err := m.rawAPI.MessagesGetMessages(ctx, []tg.InputMessageClass{
				&tg.InputMessageID{ID: msg.ReplyToID},
			})
			var targetMsg *tg.Message
			if err == nil {
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
			}
			if targetMsg != nil && targetMsg.Media != nil {
				if photoMedia, ok := targetMsg.Media.(*tg.MessageMediaPhoto); ok {
					if photo, ok := photoMedia.Photo.(*tg.Photo); ok {
						_ = os.MkdirAll("Thumbnails", 0755)
						destThumb := fmt.Sprintf("Thumbnails/%d.jpg", msg.UserID)
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
							_, err := dl.Download(m.rawAPI, loc).ToPath(ctx, destThumb)
							if err == nil {
								u.CustomThumb = destThumb
								_ = ext.DB.SaveUser(u)
								ext.ClearPendingThumbSession(msg.UserID)
								_, _ = m.sender.SendMessage(ctx, peer, "✓ <b>Custom thumbnail saved from replied photo!</b> It will be attached to all your future leeched files.", msg.MessageID, nil)
								return
							}
						}
					}
				} else if docMedia, ok := targetMsg.Media.(*tg.MessageMediaDocument); ok {
					if doc, ok := docMedia.Document.(*tg.Document); ok {
						_ = os.MkdirAll("Thumbnails", 0755)
						destThumb := fmt.Sprintf("Thumbnails/%d.jpg", msg.UserID)
						dl := tdDownloader.NewDownloader()
						loc := doc.AsInputDocumentFileLocation("")
						if loc != nil {
							_, err := dl.Download(m.rawAPI, loc).ToPath(ctx, destThumb)
							if err == nil {
								u.CustomThumb = destThumb
								_ = ext.DB.SaveUser(u)
								ext.ClearPendingThumbSession(msg.UserID)
								_, _ = m.sender.SendMessage(ctx, peer, "✓ <b>Custom thumbnail saved from replied document!</b> It will be attached to all your future leeched files.", msg.MessageID, nil)
								return
							}
						}
					}
				}
			}
		}

		// Activate 60s listener
		ext.SetPendingThumbSession(msg.UserID, msg.ChatID, msg.MessageID, 60*time.Second)
		promptText := "⌬ <b><u>Send Custom Thumbnail</u></b>\n\n" +
			"Please send a <b>photo</b> or image document within the next <b>60 seconds</b>.\n\n" +
			"<i>Once received, it will be automatically saved as your custom thumbnail.</i>\n" +
			"<i>Send /cancel to abort.</i>"
		_, _ = m.sender.SendMessage(ctx, peer, promptText, msg.MessageID, nil)
		return
	}

	// Direct command arguments e.g. /us lprefix <text> or /us doc
	if len(parts) >= 2 {
		subCmd := strings.ToLower(parts[1])
		switch subCmd {
		case "doc", "asdoc":
			u.AsDoc = !u.AsDoc
			_ = ext.DB.SaveUser(u)
			st := "Disabled"
			if u.AsDoc {
				st = "Enabled"
			}
			_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("◈ <b>Upload As Document</b> is now: <b>%s</b>", st), msg.MessageID, nil)
			return
		case "autorename":
			if len(parts) >= 3 {
				u.RemnameAuto = strings.Join(parts[2:], " ")
				_ = ext.DB.SaveUser(u)
				_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ <b>AutoRename Template</b> set to: <code>%s</code>", u.RemnameAuto), msg.MessageID, nil)
				return
			}
			u.Autorename = !u.Autorename
			_ = ext.DB.SaveUser(u)
			st := "Disabled"
			if u.Autorename {
				st = "Enabled"
			}
			_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("◈ <b>AutoRename</b> is now: <b>%s</b>", st), msg.MessageID, nil)
			return
		case "autothumb", "auto_thumb":
			u.AutoThumb = !u.AutoThumb
			_ = ext.DB.SaveUser(u)
			st := "Disabled"
			if u.AutoThumb {
				st = "Enabled"
			}
			_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("◈ <b>Auto Thumbnail</b> is now: <b>%s</b>", st), msg.MessageID, nil)
			return
		case "method", "rename_method":
			u.RenameMethod = nextRenameMethod(u.RenameMethod)
			_ = ext.DB.SaveUser(u)
			_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("• <b>Rename Method</b> is now: <b>%s</b>", strings.Title(u.RenameMethod)), msg.MessageID, nil)
			return
		case "equalsplits", "esplits":
			u.EqualSplits = !u.EqualSplits
			_ = ext.DB.SaveUser(u)
			st := "Disabled"
			if u.EqualSplits {
				st = "Enabled"
			}
			_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("◈ <b>Equal Splits</b> is now: <b>%s</b>", st), msg.MessageID, nil)
			return
		case "mediagroup", "mgroup":
			u.MediaGroup = !u.MediaGroup
			_ = ext.DB.SaveUser(u)
			st := "Disabled"
			if u.MediaGroup {
				st = "Enabled"
			}
			_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("◈ <b>Media Group</b> is now: <b>%s</b>", st), msg.MessageID, nil)
			return
		case "prefix", "lprefix":
			if len(parts) >= 3 {
				val := strings.Join(parts[2:], " ")
				if strings.EqualFold(val, "clear") || strings.EqualFold(val, "none") {
					u.Prefix = ""
					val = "Cleared"
				} else {
					u.Prefix = val
				}
				_ = ext.DB.SaveUser(u)
				_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ <b>Leech Prefix</b> set to: <code>%s</code>", val), msg.MessageID, nil)
				return
			}
		case "suffix", "lsuffix":
			if len(parts) >= 3 {
				val := strings.Join(parts[2:], " ")
				if strings.EqualFold(val, "clear") || strings.EqualFold(val, "none") {
					u.Suffix = ""
					val = "Cleared"
				} else {
					u.Suffix = val
				}
				_ = ext.DB.SaveUser(u)
				_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ <b>Leech Suffix</b> set to: <code>%s</code>", val), msg.MessageID, nil)
				return
			}
		case "caption", "lcaption":
			if len(parts) >= 3 {
				val := strings.Join(parts[2:], " ")
				if strings.EqualFold(val, "clear") || strings.EqualFold(val, "default") {
					u.Caption = ""
					val = "Default"
				} else {
					u.Caption = val
				}
				_ = ext.DB.SaveUser(u)
				_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ <b>Leech Caption</b> set to: <code>%s</code>", val), msg.MessageID, nil)
				return
			}
		case "dump", "ldump":
			if len(parts) >= 3 {
				id, _ := strconv.ParseInt(parts[2], 10, 64)
				u.DumpChatID = id
				_ = ext.DB.SaveUser(u)
				_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ <b>Leech Dump Channel ID</b> set to: <code>%d</code>", u.DumpChatID), msg.MessageID, nil)
				return
			}
		case "meta", "lmeta", "metadata":
			if len(parts) >= 3 {
				if u.Metadata == nil {
					u.Metadata = make(map[string]string)
				}
				val := strings.Join(parts[2:], " ")
				if strings.EqualFold(val, "clear") || strings.EqualFold(val, "none") {
					delete(u.Metadata, "all")
					val = "Cleared"
				} else {
					u.Metadata["all"] = val
				}
				_ = ext.DB.SaveUser(u)
				_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ <b>Leech Metadata</b> set to: <code>%s</code>", val), msg.MessageID, nil)
				return
			}
		case "split", "splitsize", "split_size":
			if len(parts) >= 3 {
				sizeStr := strings.ToUpper(parts[2])
				maxSplit := config.GetMaxSplitSize()
				var bytes int64
				if strings.HasSuffix(sizeStr, "GB") {
					val, _ := strconv.ParseFloat(strings.TrimSuffix(sizeStr, "GB"), 64)
					bytes = int64(val * 1024 * 1024 * 1024)
				} else if strings.HasSuffix(sizeStr, "MB") {
					val, _ := strconv.ParseFloat(strings.TrimSuffix(sizeStr, "MB"), 64)
					bytes = int64(val * 1024 * 1024)
				} else {
					bytes, _ = strconv.ParseInt(sizeStr, 10, 64)
				}
				if bytes > maxSplit || bytes <= 0 {
					bytes = maxSplit
				}
				u.SplitSize = bytes
				_ = ext.DB.SaveUser(u)
				_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ <b>Leech Split Size</b> set to: <code>%s</code>", ext.GetReadableFileSize(u.SplitSize)), msg.MessageID, nil)
				return
			}
		case "leech":
			text, btn := m.buildLeechMenu(u, msg.SenderName)
			_, _ = m.sender.SendMessageWithButtons(ctx, peer, text, msg.MessageID, btn, 2)
			return
		}
	}

	// Default: Open Main Settings panel with one-by-one buttons
	text, btn := m.buildMainMenu(u, msg.SenderName)
	_, _ = m.sender.SendMessageWithButtons(ctx, peer, text, msg.MessageID, btn, 1)
}

func (m *SettingsModule) buildMainMenu(u *types.UserSettings, name string) (string, *telegram.ButtonMaker) {
	if name == "" {
		name = fmt.Sprintf("User_%d", u.UserID)
	}
	text := fmt.Sprintf("⌬ <b><u>%s</u> for <a href=\"tg://user?id=%d\">%s</a></b> (<code>%d</code>)\n\n"+
		"<i>Select a category below to configure your preferences:</i>",
		telegram.ToSmallCaps("User Settings"), u.UserID, name, u.UserID)

	btn := telegram.NewButtonMaker()
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Leech Settings"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.Row()
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Metadata Settings"), fmt.Sprintf("userset %d metadata", u.UserID), "primary")
	btn.Row()
	btn.IButtonStyled("• "+telegram.ToSmallCaps("Reset Setting"), fmt.Sprintf("userset %d reset_all", u.UserID), "danger")
	btn.Row()
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildLeechMenu(u *types.UserSettings, userName string) (string, *telegram.ButtonMaker) {
	if userName == "" {
		userName = fmt.Sprintf("User_%d", u.UserID)
	}
	nameTag := fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a>", u.UserID, userName)

	// Daily leech usage and limit
	_, dailyBytes := ext.GetUserDailyStats(u.UserID)
	dlUsed := ext.GetReadableFileSize(dailyBytes)
	cfg := config.Get()
	dlLimit := "∞"
	if cfg.DailyLeechLimit > 0 {
		dlLimit = ext.GetReadableFileSize(int64(cfg.DailyLeechLimit) * 1024 * 1024 * 1024)
	}
	dlStr := fmt.Sprintf("%s / %s", dlUsed, dlLimit)

	ltype := "MEDIA"
	if u.AsDoc {
		ltype = "DOCUMENT"
	}

	thumbStatus := "Not Exists"
	if u.CustomThumb != "" {
		if _, err := os.Stat(u.CustomThumb); err == nil {
			thumbStatus = "Exists"
		}
	}

	splitSize := ext.GetReadableFileSize(config.GetMaxSplitSize()) + " (Default)"
	if u.SplitSize > 0 {
		splitSize = ext.GetReadableFileSize(u.SplitSize)
	}

	equalSplits := "Disabled"
	if u.EqualSplits {
		equalSplits = "Enabled"
	}

	mediaGroup := "Disabled"
	if u.MediaGroup {
		mediaGroup = "Enabled"
	}

	caption := "Not Exists"
	if u.Caption != "" {
		caption = u.Caption
	}

	prefix := "Not Exists"
	if u.Prefix != "" {
		prefix = u.Prefix
	}

	suffix := "Not Exists"
	if u.Suffix != "" {
		suffix = u.Suffix
	}

	dumps := "Not Exists"
	if u.DumpChatID != 0 {
		dumps = fmt.Sprintf("%d", u.DumpChatID)
	}

	remnameAuto := "Not Exists"
	if u.RemnameAuto != "" {
		remnameAuto = u.RemnameAuto
	}

	remnameRegex := "Not Exists"
	if u.RemnameRegex != "" {
		remnameRegex = u.RemnameRegex
	}

	renameMethod := "Template"
	if u.RenameMethod == "regex" {
		renameMethod = "Regex"
	}
	if u.RenameMethod == "smart" {
		renameMethod = "Smart Auto Rename"
	}

	autorename := "Disabled"
	if u.Autorename {
		autorename = "Enabled"
	}

	autoThumb := "Disabled"
	if u.AutoThumb {
		autoThumb = "Enabled"
	}

	metadata := "Not Exists"
	if u.Metadata != nil && u.Metadata["all"] != "" {
		metadata = u.Metadata["all"]
	}

	// Exact AZML text format with small caps header
	text := fmt.Sprintf("⌬ <b><u>%s :</u></b> %s\n\n"+
		"┎ <b>Daily Leech :</b> <code>%s</code> per day\n"+
		"┠ <b>Leech Type :</b> <i>%s</i>\n"+
		"┠ <b>Custom Thumbnail :</b> <i>%s</i>\n"+
		"┠ <b>Leech Split Size :</b> <code>%s</code>\n"+
		"┠ <b>Equal Splits :</b> <i>%s</i>\n"+
		"┠ <b>Media Group :</b> <i>%s</i>\n"+
		"┠ <b>Leech Caption :</b> <code>%s</code>\n"+
		"┠ <b>Leech Prefix :</b> <code>%s</code>\n"+
		"┠ <b>Leech Suffix :</b> <code>%s</code>\n"+
		"┠ <b>Leech Dumps :</b> <code>%s</code>\n"+
		"┠ <b>AutoRename Template :</b> <code>%s</code>\n"+
		"┠ <b>Regex Remname :</b> <code>%s</code>\n"+
		"┠ <b>Rename Method :</b> <i>%s</i>\n"+
		"┠ <b>AutoRename :</b> <i>%s</i>\n"+
		"┠ <b>Auto Thumbnail :</b> <i>%s</i>\n"+
		"┖ <b>Leech Metadata :</b> <code>%s</code>",
		telegram.ToSmallCaps("Leech Settings"),
		nameTag, dlStr, ltype, thumbStatus, splitSize, equalSplits, mediaGroup,
		caption, prefix, suffix, dumps, remnameAuto, remnameRegex, renameMethod,
		autorename, autoThumb, metadata)

	btn := telegram.NewButtonMaker()

	// Row 1: Document/Media Toggle | Thumbnail
	if u.AsDoc {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Send As Media"), fmt.Sprintf("userset %d doc", u.UserID), "primary")
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Send As Document"), fmt.Sprintf("userset %d doc", u.UserID), "primary")
	}
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Thumbnail"), fmt.Sprintf("userset %d thumb", u.UserID), "primary")
	btn.Row()

	// Row 2: Leech Splits | Leech Caption
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Leech Splits"), fmt.Sprintf("userset %d split_size", u.UserID), "primary")
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Leech Caption"), fmt.Sprintf("userset %d lcaption", u.UserID), "primary")
	btn.Row()

	// Row 3: Leech Prefix | Leech Suffix
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Leech Prefix"), fmt.Sprintf("userset %d lprefix", u.UserID), "primary")
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Leech Suffix"), fmt.Sprintf("userset %d lsuffix", u.UserID), "primary")
	btn.Row()

	// Row 4: AutoRename Template | Regex Remname
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("AutoRename Template"), fmt.Sprintf("userset %d lremname_auto", u.UserID), "primary")
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Regex Remname"), fmt.Sprintf("userset %d lremname_regex", u.UserID), "primary")
	btn.Row()

	// Row 5: Disable/Enable AutoRename | Enable/Disable Auto Thumbnail
	if u.Autorename {
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Disable AutoRename"), fmt.Sprintf("userset %d autorename", u.UserID), "danger")
	} else {
		btn.IButtonStyled("✓ "+telegram.ToSmallCaps("Enable AutoRename"), fmt.Sprintf("userset %d autorename", u.UserID), "success")
	}
	if u.AutoThumb {
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Disable Auto Thumb"), fmt.Sprintf("userset %d auto_thumb", u.UserID), "danger")
	} else {
		btn.IButtonStyled("✓ "+telegram.ToSmallCaps("Enable Auto Thumb"), fmt.Sprintf("userset %d auto_thumb", u.UserID), "success")
	}
	btn.Row()

	// Row 6: Leech Dump | Leech Metadata
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Leech Dump"), fmt.Sprintf("userset %d ldump", u.UserID), "primary")
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Metadata"), fmt.Sprintf("userset %d lmeta", u.UserID), "primary")
	btn.Row()

	// Row 7: Rename Method cycles through legacy Template, Regex, and opt-in Smart modes.
	methodLabel := "Template"
	if u.RenameMethod == "regex" {
		methodLabel = "Regex"
	}
	if u.RenameMethod == "smart" {
		methodLabel = "Smart"
	}
	btn.IButtonStyled("• "+telegram.ToSmallCaps("Rename Method: "+methodLabel), fmt.Sprintf("userset %d rename_method", u.UserID), "primary")
	btn.Row()

	// Footer: Back | Close
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d main", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildThumbnailMenu(u *types.UserSettings, banner ...string) (string, *telegram.ButtonMaker) {
	status := "Not Exists"
	exists := false
	if u.CustomThumb != "" {
		if _, err := os.Stat(u.CustomThumb); err == nil {
			status = "Exists"
			exists = true
		}
	}

	bannerText := ""
	if len(banner) > 0 && banner[0] != "" {
		bannerText = banner[0] + "\n\n"
	}

	text := fmt.Sprintf("%s⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Status:</b> <code>%s</code>\n"+
		"┖ <b>Stored File:</b> <code>%s</code>\n\n"+
		"<i>Custom thumbnail will be embedded into your leeched media files.</i>\n\n"+
		"• Click <b>Change Thumbnail</b> to activate photo listener.\n"+
		"• Alternatively reply to a photo with <code>/us thumb</code>.",
		bannerText, telegram.ToSmallCaps("Custom Thumbnail Settings"), status, u.CustomThumb)

	btn := telegram.NewButtonMaker()
	if exists {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("View Thumbnail"), fmt.Sprintf("userset %d vthumb", u.UserID), "primary")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Delete Thumbnail"), fmt.Sprintf("userset %d dthumb", u.UserID), "danger")
		btn.Row()
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Change Thumbnail"), fmt.Sprintf("userset %d cthumb", u.UserID), "primary")
		btn.Row()
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Set Thumbnail"), fmt.Sprintf("userset %d cthumb", u.UserID), "success")
		btn.Row()
	}
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildSplitSizeMenu(u *types.UserSettings) (string, *telegram.ButtonMaker) {
	splitSize := ext.GetReadableFileSize(config.GetMaxSplitSize()) + " (Default)"
	if u.SplitSize > 0 {
		splitSize = ext.GetReadableFileSize(u.SplitSize)
	}

	eqStatus := "Disabled"
	if u.EqualSplits {
		eqStatus = "Enabled"
	}

	mgStatus := "Disabled"
	if u.MediaGroup {
		mgStatus = "Enabled"
	}

	text := fmt.Sprintf("⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Current Split Size:</b> <code>%s</code>\n"+
		"┠ <b>Equal Splits:</b> <i>%s</i>\n"+
		"┖ <b>Media Group:</b> <i>%s</i>\n\n"+
		"<i>Files larger than this limit will be automatically split before uploading.</i>",
		telegram.ToSmallCaps("Leech Split Settings"), splitSize, eqStatus, mgStatus)

	btn := telegram.NewButtonMaker()

	// Toggles
	if u.EqualSplits {
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Disable Equal Splits"), fmt.Sprintf("userset %d esplits", u.UserID), "danger")
	} else {
		btn.IButtonStyled("✓ "+telegram.ToSmallCaps("Enable Equal Splits"), fmt.Sprintf("userset %d esplits", u.UserID), "success")
	}
	if u.MediaGroup {
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Disable Media Group"), fmt.Sprintf("userset %d mgroup", u.UserID), "danger")
	} else {
		btn.IButtonStyled("✓ "+telegram.ToSmallCaps("Enable Media Group"), fmt.Sprintf("userset %d mgroup", u.UserID), "success")
	}
	btn.Row()

	// Presets
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("2 GB"), fmt.Sprintf("userset %d split_2gb", u.UserID), "primary")
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("4 GB"), fmt.Sprintf("userset %d split_4gb", u.UserID), "primary")
	btn.IButtonStyled("• "+telegram.ToSmallCaps("Default Limit"), fmt.Sprintf("userset %d split_default", u.UserID), "primary")
	btn.Row()

	// Custom
	btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Custom Size"), fmt.Sprintf("userset %d edit_split", u.UserID), "primary")
	btn.Row()
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildCaptionMenu(u *types.UserSettings) (string, *telegram.ButtonMaker) {
	caption := "Not Exists (Default)"
	if u.Caption != "" {
		caption = u.Caption
	}

	text := fmt.Sprintf("⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Current Caption:</b>\n<code>%s</code>\n\n"+
		"➲ <b>Supported Format Tags:</b>\n"+
		"• <code>{filename}</code> / <code>{physical_filename}</code> - Actual filename on disk\n"+
		"• <code>{orig_filename}</code> - Original incoming filename\n"+
		"• <code>{smart_filename}</code> - Proposed normalized filename\n"+
		"• <code>{smartcaption}</code> / <code>{smartcaptionv2}</code> - Detailed Smart Caption\n"+
		"• <code>{smartcaptionv1}</code> - Smart Caption with per-track audio details\n"+
		"• <code>{show_name}</code>, <code>{season}</code>, <code>{episode}</code>, <code>{title}</code>, <code>{year}</code> - Parsed/canonical media identity\n"+
		"• <code>{quality}</code>, <code>{source}</code>, <code>{platform}</code>, <code>{codec}</code>, <code>{encoder}</code> - Release/video details\n"+
		"• <code>{audio_summary}</code>, <code>{languages}</code>, <code>{subtitle}</code>, <code>{subtitles}</code> - Audio and subtitles\n"+
		"• <code>{hdr}</code>, <code>{bit_depth}</code>, <code>{split_suffix}</code> - Additional media details\n"+
		"• <code>{size}</code>, <code>{duration}</code>, <code>{mime_type}</code>, <code>{md5_hash}</code> - File information\n"+
		"Smart metadata tags are populated when Rename Method is <code>smart</code>; unavailable values become empty.",
		telegram.ToSmallCaps("Leech Caption Template"), caption)

	btn := telegram.NewButtonMaker()
	if u.Caption != "" {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Change Caption"), fmt.Sprintf("userset %d edit_lcaption", u.UserID), "primary")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Reset to Default"), fmt.Sprintf("userset %d del_lcaption", u.UserID), "danger")
		btn.Row()
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Set Custom Caption"), fmt.Sprintf("userset %d edit_lcaption", u.UserID), "success")
		btn.Row()
	}
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildPrefixMenu(u *types.UserSettings) (string, *telegram.ButtonMaker) {
	prefix := "Not Exists"
	if u.Prefix != "" {
		prefix = u.Prefix
	}

	text := fmt.Sprintf("⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Current Prefix:</b> <code>%s</code>\n\n"+
		"➲ <b>Description:</b> <i>Text prepended to the start of all your leeched filenames.</i>\n\n"+
		"<b>Example:</b> <code>[AZML] </code>",
		telegram.ToSmallCaps("Leech Prefix Settings"), prefix)

	btn := telegram.NewButtonMaker()
	if u.Prefix != "" {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Change Prefix"), fmt.Sprintf("userset %d edit_lprefix", u.UserID), "primary")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Delete Prefix"), fmt.Sprintf("userset %d del_lprefix", u.UserID), "danger")
		btn.Row()
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Set Prefix"), fmt.Sprintf("userset %d edit_lprefix", u.UserID), "success")
		btn.Row()
	}
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildSuffixMenu(u *types.UserSettings) (string, *telegram.ButtonMaker) {
	suffix := "Not Exists"
	if u.Suffix != "" {
		suffix = u.Suffix
	}

	text := fmt.Sprintf("⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Current Suffix:</b> <code>%s</code>\n\n"+
		"➲ <b>Description:</b> <i>Text appended before extension in all your leeched filenames.</i>\n\n"+
		"<b>Example:</b> <code> @MyChannel</code>",
		telegram.ToSmallCaps("Leech Suffix Settings"), suffix)

	btn := telegram.NewButtonMaker()
	if u.Suffix != "" {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Change Suffix"), fmt.Sprintf("userset %d edit_lsuffix", u.UserID), "primary")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Delete Suffix"), fmt.Sprintf("userset %d del_lsuffix", u.UserID), "danger")
		btn.Row()
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Set Suffix"), fmt.Sprintf("userset %d edit_lsuffix", u.UserID), "success")
		btn.Row()
	}
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildAutoRenameMenu(u *types.UserSettings) (string, *telegram.ButtonMaker) {
	tmpl := "Not Exists"
	if u.RemnameAuto != "" {
		tmpl = u.RemnameAuto
	}

	text := fmt.Sprintf("⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Current Template:</b>\n<code>%s</code>\n\n"+
		"➲ <b>Supported Tags:</b>\n"+
		"• <code>{title}</code> - Media or anime title\n"+
		"• <code>{season}</code> - Season number (e.g. 01)\n"+
		"• <code>{episode}</code> - Episode number (e.g. 05)\n"+
		"• <code>{quality}</code> - Detected resolution (e.g. 1080p)\n\n"+
		"<b>Example:</b>\n<code>{title} - S{season}E{episode} [{quality}]</code>",
		telegram.ToSmallCaps("AutoRename Template Settings"), tmpl)

	btn := telegram.NewButtonMaker()
	if u.RemnameAuto != "" {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Change Template"), fmt.Sprintf("userset %d edit_lremname_auto", u.UserID), "primary")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Delete Template"), fmt.Sprintf("userset %d del_lremname_auto", u.UserID), "danger")
		btn.Row()
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Set Template"), fmt.Sprintf("userset %d edit_lremname_auto", u.UserID), "success")
		btn.Row()
	}
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildRegexMenu(u *types.UserSettings) (string, *telegram.ButtonMaker) {
	regexVal := "Not Exists"
	if u.RemnameRegex != "" {
		regexVal = u.RemnameRegex
	}

	text := fmt.Sprintf("⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Current Regex:</b>\n<code>%s</code>\n\n"+
		"➲ <b>Format:</b> <code>search_regex:replacement</code>\n"+
		"<b>Example:</b> <code>\\[TG\\]:</code> (removes [TG] from filenames)",
		telegram.ToSmallCaps("Regex Remname Settings"), regexVal)

	btn := telegram.NewButtonMaker()
	if u.RemnameRegex != "" {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Change Regex"), fmt.Sprintf("userset %d edit_lremname_regex", u.UserID), "primary")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Delete Regex"), fmt.Sprintf("userset %d del_lremname_regex", u.UserID), "danger")
		btn.Row()
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Set Regex"), fmt.Sprintf("userset %d edit_lremname_regex", u.UserID), "success")
		btn.Row()
	}
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildDumpMenu(u *types.UserSettings) (string, *telegram.ButtonMaker) {
	dumpStr := "Not Exists"
	if u.DumpChatID != 0 {
		dumpStr = fmt.Sprintf("%d", u.DumpChatID)
	}

	text := fmt.Sprintf("⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Dump Channel ID:</b> <code>%s</code>\n\n"+
		"➲ <b>Description:</b> <i>Leeched files will be forwarded to your personal dump channel.</i>\n\n"+
		"<b>Note:</b> Make sure the bot is an <b>Administrator</b> in your channel!",
		telegram.ToSmallCaps("Leech Dump Channel Settings"), dumpStr)

	btn := telegram.NewButtonMaker()
	if u.DumpChatID != 0 {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Change Dump ID"), fmt.Sprintf("userset %d edit_ldump", u.UserID), "primary")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Delete Dump ID"), fmt.Sprintf("userset %d del_ldump", u.UserID), "danger")
		btn.Row()
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Set Dump ID"), fmt.Sprintf("userset %d edit_ldump", u.UserID), "success")
		btn.Row()
	}
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) buildMetadataMenu(u *types.UserSettings) (string, *telegram.ButtonMaker) {
	metaAll := "Not Exists"
	if u.Metadata != nil && u.Metadata["all"] != "" {
		metaAll = u.Metadata["all"]
	}

	text := fmt.Sprintf("⌬ <b><u>%s</u></b>\n\n"+
		"┠ <b>Metadata Text:</b> <code>%s</code>\n\n"+
		"➲ <b>Description:</b> <i>Metadata title and author encoded directly into video streams via FFmpeg.</i>\n\n"+
		"<b>Example:</b> <code>Encoded by TBX TEAM</code>",
		telegram.ToSmallCaps("Leech Video Metadata Settings"), metaAll)

	btn := telegram.NewButtonMaker()
	if metaAll != "Not Exists" {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Change Metadata"), fmt.Sprintf("userset %d edit_lmeta", u.UserID), "primary")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Delete Metadata"), fmt.Sprintf("userset %d del_lmeta", u.UserID), "danger")
		btn.Row()
	} else {
		btn.IButtonStyled("◈ "+telegram.ToSmallCaps("Set Metadata"), fmt.Sprintf("userset %d edit_lmeta", u.UserID), "success")
		btn.Row()
	}
	btn.IButtonStyled("« "+telegram.ToSmallCaps("Back"), fmt.Sprintf("userset %d leech", u.UserID), "primary")
	btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
	btn.Row()

	return text, btn
}

func (m *SettingsModule) HandleCallback(ctx context.Context, peer tg.InputPeerClass, queryID int64, msgID int, data string, fromUserID int64) {
	parts := strings.Fields(data)
	if len(parts) >= 2 {
		targetUID, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil && targetUID != fromUserID {
			_ = m.sender.AnswerCallback(ctx, queryID, "⚠️ It's not your settings panel! Use /us to open your own.", true)
			return
		}
	}
	if len(parts) < 3 {
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)
		return
	}

	targetUID, _ := strconv.ParseInt(parts[1], 10, 64)
	if targetUID != fromUserID {
		_ = m.sender.AnswerCallback(ctx, queryID, "⚠️ It's not your settings panel! Use /us to open your own.", true)
		return
	}

	action := parts[2]
	u := m.userMap.GetOrCreate(targetUID)

	switch action {
	// 1. Navigation
	case "main", "back":
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)
		text, btn := m.buildMainMenu(u, "")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 1)
	case "leech":
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)
		text, btn := m.buildLeechMenu(u, "")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
	case "reset_all":
		u.Prefix = ""
		u.Suffix = ""
		u.Caption = ""
		u.RemnameAuto = ""
		u.RemnameRegex = ""
		u.SplitSize = 0
		u.Autorename = false
		u.AutoThumb = false
		u.EqualSplits = false
		u.MediaGroup = false
		u.AsDoc = config.Get().AsDocument
		if u.CustomThumb != "" {
			_ = os.Remove(u.CustomThumb)
			u.CustomThumb = ""
		}
		go func(usr *types.UserSettings) { _ = ext.DB.SaveUser(usr) }(u)
		_ = m.sender.AnswerCallback(ctx, queryID, "All user settings reset to default!", true)
		text, btn := m.buildMainMenu(u, "")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 1)
	case "close":
		ext.ClearPendingSettingSession(targetUID)
		ext.ClearPendingThumbSession(targetUID)
		_ = m.sender.AnswerCallback(ctx, queryID, "Settings closed", false)
		_ = m.sender.DeleteMessage(ctx, peer, []int{msgID})

	// 2. Direct Toggles (Edit message in-place without glitch)
	case "doc":
		u.AsDoc = !u.AsDoc
		go func(usr *types.UserSettings) { _ = ext.DB.SaveUser(usr) }(u)
		st := "MEDIA"
		if u.AsDoc {
			st = "DOCUMENT"
		}
		_ = m.sender.AnswerCallback(ctx, queryID, fmt.Sprintf("Upload Type set to %s", st), false)
		text, btn := m.buildLeechMenu(u, "")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)

	case "autorename":
		u.Autorename = !u.Autorename
		go func(usr *types.UserSettings) { _ = ext.DB.SaveUser(usr) }(u)
		st := "Disabled"
		if u.Autorename {
			st = "Enabled"
		}
		_ = m.sender.AnswerCallback(ctx, queryID, fmt.Sprintf("AutoRename is now %s", st), false)
		text, btn := m.buildLeechMenu(u, "")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)

	case "auto_thumb":
		u.AutoThumb = !u.AutoThumb
		go func(usr *types.UserSettings) { _ = ext.DB.SaveUser(usr) }(u)
		st := "Disabled"
		if u.AutoThumb {
			st = "Enabled"
		}
		_ = m.sender.AnswerCallback(ctx, queryID, fmt.Sprintf("Auto Thumbnail is now %s", st), false)
		text, btn := m.buildLeechMenu(u, "")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)

	case "rename_method":
		u.RenameMethod = nextRenameMethod(u.RenameMethod)
		go func(usr *types.UserSettings) { _ = ext.DB.SaveUser(usr) }(u)
		_ = m.sender.AnswerCallback(ctx, queryID, fmt.Sprintf("Method: %s", strings.Title(u.RenameMethod)), false)
		text, btn := m.buildLeechMenu(u, "")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)

	case "esplits":
		u.EqualSplits = !u.EqualSplits
		go func(usr *types.UserSettings) { _ = ext.DB.SaveUser(usr) }(u)
		st := "Disabled"
		if u.EqualSplits {
			st = "Enabled"
		}
		_ = m.sender.AnswerCallback(ctx, queryID, fmt.Sprintf("Equal Splits is now %s", st), false)
		text, btn := m.buildSplitSizeMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)

	case "mgroup":
		u.MediaGroup = !u.MediaGroup
		go func(usr *types.UserSettings) { _ = ext.DB.SaveUser(usr) }(u)
		st := "Disabled"
		if u.MediaGroup {
			st = "Enabled"
		}
		_ = m.sender.AnswerCallback(ctx, queryID, fmt.Sprintf("Media Group is now %s", st), false)
		text, btn := m.buildSplitSizeMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)

	// 3. Thumbnail Submenu
	case "thumb":
		ext.ClearPendingThumbSession(targetUID)
		text, btn := m.buildThumbnailMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "cthumb":
		chatID := telegram.PeerToChatID(peer)
		ext.SetPendingThumbSession(targetUID, chatID, msgID, 120*time.Second)
		prompt := "⌬ <b><u>Send Custom Thumbnail</u></b>\n\n" +
			"Please send a <b>photo</b> or image document within the next <b>2 minutes</b>.\n\n" +
			"<i>Once received, it will be automatically saved as your custom thumbnail and this menu will update.</i>"
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d thumb", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send your photo...", false)

	case "vthumb":
		if u.CustomThumb == "" {
			_ = m.sender.AnswerCallback(ctx, queryID, "No thumbnail configured!", true)
			return
		}
		if _, err := os.Stat(u.CustomThumb); err != nil {
			_ = m.sender.AnswerCallback(ctx, queryID, "Thumbnail file not found on disk!", true)
			return
		}
		_, err := m.sender.SendLocalPhoto(ctx, peer, u.CustomThumb, "⌬ <b>Your Custom Thumbnail</b>", 0)
		if err != nil {
			_ = m.sender.AnswerCallback(ctx, queryID, fmt.Sprintf("Error sending thumbnail: %v", err), true)
		} else {
			_ = m.sender.AnswerCallback(ctx, queryID, "Thumbnail sent!", false)
		}

	case "dthumb":
		ext.ClearPendingThumbSession(targetUID)
		if u.CustomThumb != "" {
			_ = os.Remove(u.CustomThumb)
		}
		dest := fmt.Sprintf("Thumbnails/%d.jpg", targetUID)
		_ = os.Remove(dest)
		u.CustomThumb = ""
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildThumbnailMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "✓ Custom thumbnail deleted!", false)

	// 4. Split Size Submenu
	case "split_size":
		text, btn := m.buildSplitSizeMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "split_2gb":
		u.SplitSize = 2000 * 1024 * 1024
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildSplitSizeMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Split size set to 2GB", false)

	case "split_4gb":
		u.SplitSize = 4000 * 1024 * 1024
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildSplitSizeMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Split size set to 4GB", false)

	case "split_default":
		u.SplitSize = 0
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildSplitSizeMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Reset to default limit", false)

	case "edit_split":
		ext.SetPendingSettingSession(targetUID, "split_size", 0, msgID, 60*time.Second)
		prompt := "⌬ <b><u>Set Custom Split Size</u></b>\n\n" +
			"Send custom size (e.g. <code>1.5GB</code>, <code>500MB</code>, <code>2000000000</code>) within <b>60 seconds</b>:"
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d split_size", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send new split size...", false)

	// 5. Caption Submenu
	case "lcaption":
		text, btn := m.buildCaptionMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "edit_lcaption":
		ext.SetPendingSettingSession(targetUID, "lcaption", 0, msgID, 60*time.Second)
		prompt := "⌬ <b><u>Set Custom Leech Caption</u></b>\n\n" +
			"Send new caption template within <b>60 seconds</b>.\n\n" +
			"<b>Basic:</b> <code>{filename}</code>, <code>{physical_filename}</code>, <code>{orig_filename}</code>, <code>{size}</code>, <code>{duration}</code>\n" +
			"<b>Smart:</b> <code>{smart_filename}</code>, <code>{smartcaption}</code>, <code>{smartcaptionv1}</code>, <code>{smartcaptionv2}</code>\n" +
			"<b>Media:</b> <code>{show_name}</code>, <code>{season}</code>, <code>{episode}</code>, <code>{title}</code>, <code>{year}</code>, <code>{quality}</code>, <code>{source}</code>, <code>{codec}</code>, <code>{audio_summary}</code>, <code>{subtitle}</code>, <code>{hdr}</code>, <code>{bit_depth}</code>\n\n" +
			"Smart-specific tags require Rename Method = <code>smart</code>. Unavailable metadata is blank."
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d lcaption", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send new caption template...", false)

	case "del_lcaption":
		u.Caption = ""
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildCaptionMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Caption reset to default", false)

	// 6. Prefix Submenu
	case "lprefix":
		text, btn := m.buildPrefixMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "edit_lprefix":
		ext.SetPendingSettingSession(targetUID, "lprefix", 0, msgID, 60*time.Second)
		prompt := "⌬ <b><u>Set Leech Prefix</u></b>\n\n" +
			"Send prefix text to prepend to filenames within <b>60 seconds</b> (e.g. <code>[AZML] </code>):"
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d lprefix", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send new prefix...", false)

	case "del_lprefix":
		u.Prefix = ""
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildPrefixMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Prefix deleted", false)

	// 7. Suffix Submenu
	case "lsuffix":
		text, btn := m.buildSuffixMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "edit_lsuffix":
		ext.SetPendingSettingSession(targetUID, "lsuffix", 0, msgID, 60*time.Second)
		prompt := "⌬ <b><u>Set Leech Suffix</u></b>\n\n" +
			"Send suffix text to append before file extension within <b>60 seconds</b> (e.g. <code> @MyChannel</code>):"
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d lsuffix", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send new suffix...", false)

	case "del_lsuffix":
		u.Suffix = ""
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildSuffixMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Suffix deleted", false)

	// 8. AutoRename Template Submenu
	case "lremname_auto":
		text, btn := m.buildAutoRenameMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "edit_lremname_auto":
		ext.SetPendingSettingSession(targetUID, "lremname_auto", 0, msgID, 60*time.Second)
		prompt := "⌬ <b><u>Set AutoRename Template</u></b>\n\n" +
			"Send rename template within <b>60 seconds</b>.\n\n" +
			"<b>Tags:</b> <code>{title}</code>, <code>{season}</code>, <code>{episode}</code>, <code>{quality}</code>\n" +
			"<b>Example:</b> <code>{title} - S{season}E{episode} [{quality}]</code>"
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d lremname_auto", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send new autorename template...", false)

	case "del_lremname_auto":
		u.RemnameAuto = ""
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildAutoRenameMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "AutoRename template deleted", false)

	// 9. Regex Remname Submenu
	case "lremname_regex":
		text, btn := m.buildRegexMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "edit_lremname_regex":
		ext.SetPendingSettingSession(targetUID, "lremname_regex", 0, msgID, 60*time.Second)
		prompt := "⌬ <b><u>Set Regex Remname</u></b>\n\n" +
			"Send regex pattern and replacement separated by colon within <b>60 seconds</b>.\n\n" +
			"<b>Format:</b> <code>search_pattern:replacement</code>\n" +
			"<b>Example:</b> <code>\\[TG\\]:</code>"
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d lremname_regex", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send new regex rule...", false)

	case "del_lremname_regex":
		u.RemnameRegex = ""
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildRegexMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Regex rule deleted", false)

	// 10. Dump Channel Submenu
	case "ldump":
		text, btn := m.buildDumpMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "edit_ldump":
		ext.SetPendingSettingSession(targetUID, "ldump", 0, msgID, 60*time.Second)
		prompt := "⌬ <b><u>Set Leech Dump Channel ID</u></b>\n\n" +
			"Send your Channel ID within <b>60 seconds</b> (e.g. <code>-1001234567890</code>).\n\n" +
			"<i>Make sure the bot has Admin rights with Post Messages permission!</i>"
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d ldump", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send Dump Channel ID...", false)

	case "del_ldump":
		u.DumpChatID = 0
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildDumpMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Dump channel cleared", false)

	// 11. Metadata Submenu
	case "lmeta", "metadata":
		text, btn := m.buildMetadataMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)

	case "edit_lmeta":
		ext.SetPendingSettingSession(targetUID, "lmeta", 0, msgID, 60*time.Second)
		prompt := "⌬ <b><u>Set Video Leech Metadata</u></b>\n\n" +
			"Send metadata text within <b>60 seconds</b> (e.g. <code>Encoded by TBX TEAM</code>):"
		btn := telegram.NewButtonMaker()
		btn.IButtonStyled("« "+telegram.ToSmallCaps("Cancel"), fmt.Sprintf("userset %d lmeta", u.UserID), "danger")
		btn.IButtonStyled("× "+telegram.ToSmallCaps("Close"), fmt.Sprintf("userset %d close", u.UserID), "danger")
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, prompt, btn, 1)
		_ = m.sender.AnswerCallback(ctx, queryID, "Send metadata text...", false)

	case "del_lmeta":
		if u.Metadata != nil {
			delete(u.Metadata, "all")
		}
		_ = ext.DB.SaveUser(u)
		text, btn := m.buildMetadataMenu(u)
		_ = m.sender.EditMessageWithButtons(ctx, peer, msgID, text, btn, 2)
		_ = m.sender.AnswerCallback(ctx, queryID, "Metadata cleared", false)

	default:
		_ = m.sender.AnswerCallback(ctx, queryID, "", false)
	}
}

// HandleSettingUpdated refreshes and edits the prompt message back to the corresponding submenu
func (m *SettingsModule) HandleSettingUpdated(ctx context.Context, peer tg.InputPeerClass, promptMsgID int, u *types.UserSettings, key string) {
	if promptMsgID <= 0 || u == nil {
		return
	}
	var text string
	var btn *telegram.ButtonMaker
	switch key {
	case "lprefix":
		text, btn = m.buildPrefixMenu(u)
	case "lsuffix":
		text, btn = m.buildSuffixMenu(u)
	case "lcaption":
		text, btn = m.buildCaptionMenu(u)
	case "lremname_auto":
		text, btn = m.buildAutoRenameMenu(u)
	case "lremname_regex":
		text, btn = m.buildRegexMenu(u)
	case "ldump":
		text, btn = m.buildDumpMenu(u)
	case "lmeta", "metadata":
		text, btn = m.buildMetadataMenu(u)
	case "split_size", "edit_split":
		text, btn = m.buildSplitSizeMenu(u)
	case "thumb", "cthumb":
		text, btn = m.buildThumbnailMenu(u, "✓ <b>Custom thumbnail saved successfully!</b>")
	default:
		text, btn = m.buildLeechMenu(u, "")
	}
	_ = m.sender.EditMessageWithButtons(ctx, peer, promptMsgID, text, btn, 2)
}

func nextRenameMethod(current string) string {
	switch strings.ToLower(strings.TrimSpace(current)) {
	case "auto", "":
		return "regex"
	case "regex":
		return "smart"
	default:
		return "auto"
	}
}

func (m *SettingsModule) HandleAutoRename(ctx context.Context, peer tg.InputPeerClass, msg *types.MessageContext) {
	u := m.userMap.GetOrCreate(msg.UserID)
	parts := strings.SplitN(strings.TrimSpace(msg.Text), " ", 2)

	// --- No argument: show current template or help ---
	if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
		current := u.RemnameAuto
		if current == "" {
			current = config.Get().LeechFilenameRemnameAuto
		}

		if current != "" {
			text := fmt.Sprintf("⌬ <b><u>%s :</u></b>\n\n"+
				"<code>%s</code>\n\n"+
				"<b>Usage:</b> <code>/ar [template]</code>\n"+
				"<b>Clear:</b> <code>/ar off</code>\n"+
				"<b>Mode:</b> <code>/ar mode auto|regex|smart</code>",
				telegram.ToSmallCaps("Current AutoRename Template"),
				telegram.EscapeHTML(current))
			_, _ = m.sender.SendMessage(ctx, peer, text, msg.MessageID, nil)
		} else {
			text := fmt.Sprintf("⌬ <b><u>%s :</u></b>\n\n"+
				"<b>No AutoRename template set.</b>\n\n"+
				"<b>Usage:</b> <code>/ar [animeshrine.xyz] S0{season}E{episode} Dr. Stone {quality}.mkv</code>\n\n"+
				"<b>Available tags:</b>\n"+
				"  • <code>{title}</code> — parsed title\n"+
				"  • <code>{season}</code> — season number\n"+
				"  • <code>{episode}</code> — episode number\n"+
				"  • <code>{quality}</code> — video quality (e.g. 1080p)\n"+
				"  • <code>{codec}</code> — video codec\n"+
				"  • <code>{audio}</code> — audio codec\n"+
				"  • <code>{chapter}</code> — chapter number\n"+
				"  • <code>{episode:+N}</code> — episode with offset\n\n"+
				"<b>Rename modes:</b> <code>/ar mode auto|regex|smart</code>\n"+
				"<b>Clear:</b> <code>/ar off</code>",
				telegram.ToSmallCaps("AutoRename Guide"))
			_, _ = m.sender.SendMessage(ctx, peer, text, msg.MessageID, nil)
		}
		return
	}

	template := strings.TrimSpace(parts[1])
	lowerTemplate := strings.ToLower(template)
	if strings.HasPrefix(lowerTemplate, "mode ") {
		mode := strings.TrimSpace(strings.TrimPrefix(lowerTemplate, "mode "))
		if mode != "auto" && mode != "regex" && mode != "smart" {
			_, _ = m.sender.SendMessage(ctx, peer, "Invalid rename mode. Use <code>/ar mode auto</code>, <code>/ar mode regex</code>, or <code>/ar mode smart</code>.", msg.MessageID, nil)
			return
		}
		u.RenameMethod = mode
		u.Autorename = true
		_ = ext.DB.SaveUser(u)
		_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ Rename mode set to <b>%s</b>.", strings.Title(mode)), msg.MessageID, nil)
		return
	}
	if lowerTemplate == "smart" || lowerTemplate == "regex" || lowerTemplate == "auto" {
		u.RenameMethod = lowerTemplate
		u.Autorename = true
		_ = ext.DB.SaveUser(u)
		_, _ = m.sender.SendMessage(ctx, peer, fmt.Sprintf("✓ Rename mode set to <b>%s</b>.", strings.Title(lowerTemplate)), msg.MessageID, nil)
		return
	}

	// --- Clear template ---
	switch strings.ToLower(template) {
	case "off", "clear", "reset", "del", "none":
		u.RemnameAuto = ""
		_ = ext.DB.SaveUser(u)
		_, _ = m.sender.SendMessage(ctx, peer, "✓ <b>AutoRename template cleared!</b>", msg.MessageID, nil)
		return

	default:
		u.RemnameAuto = template
		u.Autorename = true
		u.RenameMethod = "auto"
		_ = ext.DB.SaveUser(u)
		text := fmt.Sprintf("✓ <b>AutoRename template saved!</b>\n\n<code>%s</code>", telegram.EscapeHTML(template))
		_, _ = m.sender.SendMessage(ctx, peer, text, msg.MessageID, nil)
		return
	}
}
