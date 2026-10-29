package ext

import (
	"fmt"
	"html"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// SmartCaptionSet contains the two deterministic caption layouts carried over from
// WZMLHK-X. V1 includes richer per-track audio details when ffprobe has them;
// V2 is shorter and uses the summarized audio/subtitle information.
type SmartCaptionSet struct {
	V1 string
	V2 string
}

var smartCaptionTokenRE = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)
var smartCaptionHTMLTagRE = regexp.MustCompile(`<[^>]*>`)
var smartCaptionSpaceRE = regexp.MustCompile(`\s+`)

// BuildSmartCaptions creates both caption variants from the metadata already collected by
// SmartAutoRename. It performs no additional network or media-probe calls.
func BuildSmartCaptions(res SmartRenameResult, prefix, suffix string) SmartCaptionSet {
	return SmartCaptionSet{
		V1: renderSmartCaption(res, prefix, suffix, true),
		V2: renderSmartCaption(res, prefix, suffix, false),
	}
}

func renderSmartCaption(res SmartRenameResult, prefix, suffix string, detailedAudio bool) string {
	title := strings.TrimSpace(res.CanonicalTitle)
	if title == "" {
		title = strings.TrimSpace(res.Parsed.Title)
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(res.OriginalFilename), filepath.Ext(res.OriginalFilename))
	}

	values := make([]string, 0, 24)
	add := func(s string) {
		s = cleanSmartCaptionComponent(s)
		if s == "" {
			return
		}
		for _, old := range values {
			if strings.EqualFold(old, s) {
				return
			}
		}
		values = append(values, s)
	}

	add(title)
	if res.MediaType == SmartMediaEpisode || res.MediaType == SmartMediaEpisodeRange || res.MediaType == SmartMediaSeasonPack {
		switch {
		case res.EpisodeStart > 0 && res.EpisodeEnd > res.EpisodeStart:
			if res.Parsed.IsAnime && res.Season <= 0 {
				add(fmt.Sprintf("E%d-E%d", res.EpisodeStart, res.EpisodeEnd))
			} else if res.Season > 0 {
				add(fmt.Sprintf("S%02dE%02d-E%02d", res.Season, res.EpisodeStart, res.EpisodeEnd))
			} else {
				add(fmt.Sprintf("E%d-E%d", res.EpisodeStart, res.EpisodeEnd))
			}
		case res.EpisodeStart > 0:
			if res.Parsed.IsAnime && res.Season <= 0 {
				add(fmt.Sprintf("E%d", res.EpisodeStart))
			} else if res.Season > 0 {
				add(fmt.Sprintf("S%02dE%02d", res.Season, res.EpisodeStart))
			} else {
				add(fmt.Sprintf("E%d", res.EpisodeStart))
			}
		case res.Season > 0:
			add(fmt.Sprintf("S%02d", res.Season))
		}
	}
	if res.MediaType == SmartMediaMovie {
		add(res.Year)
	}
	add(res.EpisodeTitle)
	add(res.Resolution)
	add(res.Platform)
	add(res.Source)
	add(res.HDR)
	add(res.BitDepth)
	add(res.Encoder)
	add(res.ReleaseGroup)
	add(res.VideoCodec)

	if detailedAudio {
		if audio := renderSmartAudioTracks(res.Probe.AudioTracks); audio != "" {
			add(audio)
		} else {
			add(res.AudioSummary)
		}
	} else {
		add(res.AudioSummary)
	}
	add(res.SubtitleSummary)

	stem := strings.Join(values, ".")
	prefix = cleanSmartCaptionAffix(prefix)
	suffix = cleanSmartCaptionAffix(suffix)
	ext := res.Parsed.Extension
	if ext == "" {
		ext = filepath.Ext(res.PhysicalFilename)
	}
	if ext == "" {
		ext = filepath.Ext(res.OriginalFilename)
	}
	if ext == "" {
		ext = ".mkv"
	}

	// Preserve the original split marker placement. A marker such as .mkv.001
	// is placed after the extension; a marker such as .part001 is placed before it.
	if res.SplitSuffixAfterExtension {
		return prefix + stem + suffix + ext + res.SplitSuffix
	}
	return prefix + stem + suffix + res.SplitSuffix + ext
}

func renderSmartAudioTracks(tracks []SmartAudioTrack) string {
	if len(tracks) == 0 {
		return ""
	}
	items := make([]string, 0, len(tracks))
	for _, track := range tracks {
		parts := make([]string, 0, 4)
		if track.Language != "" {
			parts = append(parts, strings.ToUpper(track.Language))
		}
		if track.Codec != "" {
			parts = append(parts, strings.ToUpper(track.Codec))
		}
		channel := track.ChannelLayout
		if channel == "" && track.Channels > 0 {
			channel = fmt.Sprintf("%dch", track.Channels)
		}
		if channel != "" {
			parts = append(parts, channel)
		}
		if track.Bitrate > 0 {
			if track.Bitrate >= 1000 {
				parts = append(parts, fmt.Sprintf("%dk", track.Bitrate/1000))
			} else {
				parts = append(parts, fmt.Sprintf("%d", track.Bitrate))
			}
		}
		if len(parts) > 0 {
			items = append(items, strings.Join(parts, "."))
		}
	}
	if len(items) == 0 {
		return ""
	}
	if len(items) > 3 {
		items = append(items[:3], "MULTI")
	}
	return "[" + strings.Join(items, ".+. ") + "]"
}

func cleanSmartCaptionComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Match the reference's component cleanup without treating punctuation inside
	// meaningful technical tokens (WEB-DL, x265, S02E03) as a separator.
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsControl(r):
			b.WriteRune(' ')
		case strings.ContainsRune(`<>:"/\\|?*`, r):
			b.WriteRune(' ')
		case r == '.' || r == '_' || unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	value = smartCaptionSpaceRE.ReplaceAllString(b.String(), ".")
	return strings.Trim(value, ".")
}

func cleanSmartCaptionAffix(value string) string {
	value = smartCaptionHTMLTagRE.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, `\s`, " ")
	return value
}

// BuildSmartCaption renders the selected user/global caption template. Existing AZML-Go
// placeholders remain supported; Smart mode additionally exposes original, physical and
// generated names separately and the WZMLHK-X-compatible caption variants.
func BuildSmartCaption(res SmartRenameResult, template, currentPath string) string {
	fallback := res.PhysicalFilename
	if fallback == "" {
		fallback = res.OriginalFilename
	}
	if res.SmartCaptionV1 == "" || res.SmartCaptionV2 == "" {
		caps := BuildSmartCaptions(res, res.FilenamePrefix, res.FilenameSuffix)
		if res.SmartCaptionV1 == "" {
			res.SmartCaptionV1 = caps.V1
		}
		if res.SmartCaptionV2 == "" {
			res.SmartCaptionV2 = caps.V2
		}
	}
	if template == "" {
		caption := res.SmartCaptionV2
		if caption == "" {
			caption = fallback
		}
		if res.OriginalFilename != "" && (!strings.EqualFold(res.OriginalFilename, caption) || !strings.EqualFold(res.OriginalFilename, fallback)) {
			return html.EscapeString(caption) + "\n\nOG FileName: " + html.EscapeString(res.OriginalFilename)
		}
		return html.EscapeString(caption)
	}

	values := smartCaptionTemplateValues(res, fallback, currentPath)
	// Hashing is intentionally opt-in because it requires reading the entire file.
	if strings.Contains(template, "{md5_hash}") && currentPath != "" {
		if hash, err := GetMD5Hash(currentPath); err == nil {
			values["md5_hash"] = hash
		}
	}
	return smartCaptionTokenRE.ReplaceAllStringFunc(template, func(token string) string {
		match := smartCaptionTokenRE.FindStringSubmatch(token)
		if len(match) != 2 {
			return token
		}
		value, ok := values[match[1]]
		if !ok {
			return ""
		}
		return html.EscapeString(value)
	})
}

func smartCaptionTemplateValues(res SmartRenameResult, fallback, currentPath string) map[string]string {
	values := map[string]string{
		"filename":          fallback,
		"physical_filename": fallback,
		"orig_filename":     res.OriginalFilename,
		"original_filename": res.OriginalFilename,
		"prefilename":       res.OriginalFilename,
		"precaption":        "",
		"smart_filename":    res.ProposedFilename,
		"smartcaption":      res.SmartCaptionV2,
		"smartcaptionv1":    res.SmartCaptionV1,
		"smartcaptionv2":    res.SmartCaptionV2,
		"show_name":         res.CanonicalTitle,
		"canonical_title":   res.CanonicalTitle,
		"season":            formatSmartNumber(res.Season),
		"episode":           formatSmartEpisode(res.EpisodeStart, res.EpisodeEnd),
		"episode_start":     formatSmartNumber(res.EpisodeStart),
		"episode_end":       formatSmartNumber(res.EpisodeEnd),
		"year":              res.Year,
		"quality":           res.Resolution,
		"source":            res.Source,
		"platform":          res.Platform,
		"codec":             res.VideoCodec,
		"video_codec":       res.VideoCodec,
		"encoder":           res.Encoder,
		"release_group":     res.ReleaseGroup,
		"audio_summary":     res.AudioSummary,
		"subtitle":          res.SubtitleSummary,
		"subtitles":         res.SubtitleSummary,
		"hdr":               res.HDR,
		"bit_depth":         res.BitDepth,
		"split_suffix":      res.SplitSuffix,
		"provider":          res.Provider,
		"provider_id":       res.ProviderID,
		"match_confidence":  "",
		"languages":         res.AudioSummary,
		"duration":          "",
		"size":              "",
		"mime_type":         "application/octet-stream",
	}
	values["title"] = res.EpisodeTitle
	values["episode_title"] = res.EpisodeTitle
	values["subtitle_summary"] = res.SubtitleSummary
	if res.MatchConfidence > 0 {
		values["match_confidence"] = fmt.Sprintf("%.2f", res.MatchConfidence)
	}
	if res.Probe.Duration > 0 {
		values["duration"] = GetReadableTime(int64(res.Probe.Duration))
	}
	if currentPath != "" {
		if fi, err := os.Stat(currentPath); err == nil {
			values["size"] = GetReadableFileSize(fi.Size())
		}
		if ct := mime.TypeByExtension(filepath.Ext(currentPath)); ct != "" {
			values["mime_type"] = ct
		}
	}
	if values["smart_filename"] == "" {
		values["smart_filename"] = fallback
	}
	if values["smartcaption"] == "" {
		values["smartcaption"] = fallback
	}
	if values["smartcaptionv1"] == "" {
		values["smartcaptionv1"] = fallback
	}
	if values["smartcaptionv2"] == "" {
		values["smartcaptionv2"] = fallback
	}
	return values
}

// AvailableSmartCaptionTokens returns the supported Smart Caption template fields in a stable
// order suitable for menus and documentation.
func AvailableSmartCaptionTokens() []string {
	tokens := []string{
		"filename", "physical_filename", "orig_filename", "original_filename", "prefilename", "precaption", "smart_filename",
		"smartcaption", "smartcaptionv1", "smartcaptionv2", "show_name", "canonical_title", "season", "episode",
		"episode_start", "episode_end", "title", "episode_title", "year", "source", "platform", "quality", "codec", "video_codec",
		"encoder", "release_group", "bit_depth", "hdr", "audio_summary", "subtitle", "subtitle_summary", "subtitles",
		"languages", "split_suffix", "duration", "size", "mime_type", "provider", "provider_id", "match_confidence", "md5_hash",
	}
	sort.Strings(tokens)
	return tokens
}
