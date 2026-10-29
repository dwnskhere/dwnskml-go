package ext

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"azml-go/config"
	"azml-go/types"
)

var (
	uploaderTags = []string{
		"Toonworld4all", "SubsPlease", "EMBER", "Erai-raws",
		"HorribleSubs", "AnimeRG", "Judas", "ASW", "Anime Time",
		"Virtuality", "SmallSizedAnime", "DKB", "Cleo", "Reaktor",
		"Commie", "GJM", "Underwater", "FFF", "Doki", "gg", "Chihiro",
		"Coalgirls", "UTW", "Mazui", "Hadena", "Horrible", "Mezashite",
		"Asenshi", "Vivid", "Soldado", "Anime4Life", "Ohys-Raws",
		"CR", "NanDesuKa", "Plex", "Anime-Koi", "AnimeKaizoku",
		"ReinForce", "BlurayDesuYo", "Beatrice-Raws", "SCY", "Nep_Blanc",
		"Anime-Supreme", "HiSub", "NC-Raws", "nekomoe kissaten",
	}

	uploaderTagRegexes []*regexp.Regexp
	titlePatterns      []*regexp.Regexp
	epPatterns         []*regexp.Regexp
	yearRegex          = regexp.MustCompile(`(?i)\s*[\(\[]?\s*(199[0-9]|20[0-2][0-9]|2030)\s*[\)\]]?\s*`)
	seasonRegex        = regexp.MustCompile(`(?i)[Ss](?:eason[\s\.\-]*)?0*(\d+)`)
	qualRegex          = regexp.MustCompile(`(?i)(\d{3,4}[pP]|4K|2160[pP])`)
	mathOffset         = regexp.MustCompile(`\{(episode|season):([+\-])(\d+)\}`)

	regexCache   = make(map[string]*regexp.Regexp)
	regexCacheMu sync.RWMutex
)

func init() {
	for _, tag := range uploaderTags {
		uploaderTagRegexes = append(uploaderTagRegexes, regexp.MustCompile(`(?i)^\[`+regexp.QuoteMeta(tag)+`\]\s*`))
	}

	titlePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\[[Ee]p?(?:isode)?[\s\.\-]*0*\d+\][\s\.\-]*(.+?)[\s\.\-]*(?:\[|@|$)`),
		regexp.MustCompile(`(?i)^(.+?)[\s\.\-]*[Ss]0*(\d+)[\s\.\-]*[Ee]0*(\d+)`),
		regexp.MustCompile(`(?i)^(.+?)[\s\.\-]*[Ss]eason[\s\.\-]*0*(\d+)[\s\.\-]*[Ee]pisode[\s\.\-]*0*(\d+)`),
		regexp.MustCompile(`(?i)^\[CH[-\s]?\d+\][\s\.\-]*(.+?)[\s\.\-]*-`),
		regexp.MustCompile(`(?i)^\[\d+\][\s\.\-]*(.+?)[\s\.\-]*(?:@|$)`),
		regexp.MustCompile(`(?i)^(.+?)[\s\.\-]*(?:Ch(?:apter)?|#)[\s\.\-]*\d+`),
		regexp.MustCompile(`(?i)^(.+?)[\s\.\-]*\[`),
	}

	epPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[\s\.\-]+-[\s\.\-]*0*(\d{1,4})v\d+`),
		regexp.MustCompile(`(?i)\[[Ee]p?(?:isode)?[\s\.\-]*0*(\d{1,4})\]`),
		regexp.MustCompile(`(?i)[Ss]0*(\d+)[Ee]0*(\d+)`),
		regexp.MustCompile(`(?i)[Ee]p(?:isode)?[\s\.\-]*0*(\d{1,4})`),
		regexp.MustCompile(`(?i)[Ee][\s\.\-]*0*(\d{1,4})`),
		regexp.MustCompile(`(?i)[\s\.\-]+-[\s\.\-]*0*(\d{1,4})`),
		regexp.MustCompile(`(?i)[\s\.\-]+0*(\d{1,4})[\s\.\-]+\d{3,4}[pP]`),
		regexp.MustCompile(`(?i)\[0*(\d{1,4})\]`),
	}
}

func getCompiledRegex(pat string) (*regexp.Regexp, error) {
	regexCacheMu.RLock()
	re, ok := regexCache[pat]
	regexCacheMu.RUnlock()
	if ok {
		return re, nil
	}

	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, err
	}

	regexCacheMu.Lock()
	if len(regexCache) > 500 {
		regexCache = make(map[string]*regexp.Regexp)
	}
	regexCache[pat] = re
	regexCacheMu.Unlock()
	return re, nil
}

type ExtractedMeta struct {
	Title   string
	Season  string
	Episode string
	Quality string
	Chapter string
}

func ExtractMetadataFromFilename(filename string) ExtractedMeta {
	meta := ExtractedMeta{
		Title:   "Unknown",
		Season:  "01",
		Episode: "01",
		Quality: "1080p",
		Chapter: "001",
	}

	clean := filename
	for _, re := range uploaderTagRegexes {
		clean = re.ReplaceAllString(clean, "")
	}
	clean = strings.TrimSpace(clean)

	for _, p := range titlePatterns {
		if m := p.FindStringSubmatch(clean); len(m) > 1 {
			title := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(m[1], ".", " "), "-", " "))
			title = yearRegex.ReplaceAllString(title, " ")
			meta.Title = strings.TrimSpace(title)
			if len(m) >= 4 {
				meta.Season = fmt.Sprintf("%02d", mustAtoi(m[2]))
				meta.Episode = fmt.Sprintf("%02d", mustAtoi(m[3]))
			}
			break
		}
	}

	if sm := seasonRegex.FindStringSubmatch(filename); len(sm) > 1 {
		meta.Season = fmt.Sprintf("%02d", mustAtoi(sm[1]))
	}

	for _, ep := range epPatterns {
		if m := ep.FindStringSubmatch(clean); len(m) > 1 {
			target := m[len(m)-1]
			if n := mustAtoi(target); n > 0 && n < 2000 {
				meta.Episode = fmt.Sprintf("%02d", n)
				break
			}
		}
	}

	if qm := qualRegex.FindStringSubmatch(filename); len(qm) > 1 {
		meta.Quality = qm[1]
	}

	return meta
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func ApplyTemplateRename(filename, tmpl string) string {
	if tmpl == "" || !strings.Contains(tmpl, "{") {
		return filename
	}

	meta := ExtractMetadataFromFilename(filename)

	tmpl = mathOffset.ReplaceAllStringFunc(tmpl, func(m string) string {
		parts := mathOffset.FindStringSubmatch(m)
		if len(parts) != 4 {
			return m
		}
		tag := parts[1]
		sign := parts[2]
		offset := mustAtoi(parts[3])

		var orig int
		if tag == "episode" {
			orig = mustAtoi(meta.Episode)
		} else {
			orig = mustAtoi(meta.Season)
		}

		res := orig
		if sign == "+" {
			res += offset
		} else {
			res -= offset
		}
		if res <= 0 {
			res = orig
		}
		valStr := fmt.Sprintf("%02d", res)
		if tag == "episode" {
			meta.Episode = valStr
		} else {
			meta.Season = valStr
		}
		return fmt.Sprintf("{%s}", tag)
	})

	ext := filepath.Ext(filename)
	res := tmpl
	res = strings.ReplaceAll(res, "{title}", meta.Title)
	res = strings.ReplaceAll(res, "{season}", meta.Season)
	res = strings.ReplaceAll(res, "{episode}", meta.Episode)
	res = strings.ReplaceAll(res, "{quality}", meta.Quality)
	res = strings.ReplaceAll(res, "{chapter}", meta.Chapter)

	if !strings.HasSuffix(res, ext) {
		res += ext
	}
	return SanitizeFilename(res)
}

func FormatFilename(filename string, user *types.UserSettings, filePath string) (string, string) {
	cfg := config.Get()
	formatted := filename

	prefix := cfg.LeechFilenamePrefix
	suffix := cfg.LeechFilenameSuffix
	captionTmpl := cfg.LeechFilenameCaption
	remnameAuto := cfg.LeechFilenameRemnameAuto
	remnameRegex := cfg.LeechFilenameRemnameRegex

	if user != nil {
		if user.Prefix != "" {
			prefix = user.Prefix
		}
		if user.Suffix != "" {
			suffix = user.Suffix
		}
		if user.Caption != "" {
			captionTmpl = user.Caption
		}
		if user.RemnameAuto != "" {
			remnameAuto = user.RemnameAuto
		}
		if user.RemnameRegex != "" {
			remnameRegex = user.RemnameRegex
		}
	}

	if user != nil && user.Autorename {
		switch strings.ToLower(strings.TrimSpace(user.RenameMethod)) {
		case "smart":
			// Smart mode is executed by PrepareSmartRename in the shared leech/yt-dlp pipeline.
			// Do not silently route it through template mode if probing/provider lookup is unavailable.
		case "regex":
			if remnameRegex != "" {
				formatted = applyRegexRemname(formatted, remnameRegex)
			} else if remnameAuto != "" { // preserve the historical fallback when regex is unset
				formatted = ApplyTemplateRename(formatted, remnameAuto)
			}
		default:
			if remnameAuto != "" {
				formatted = ApplyTemplateRename(formatted, remnameAuto)
			}
		}
	}

	if prefix != "" {
		formatted = prefix + formatted
	}
	if suffix != "" {
		ext := filepath.Ext(formatted)
		base := strings.TrimSuffix(formatted, ext)
		formatted = base + suffix + ext
	}

	if filePath != "" {
		currentExt := filepath.Ext(formatted)
		if detectedExt := DetectExtension(filePath); detectedExt != "" {
			if currentExt == "" {
				formatted += detectedExt
			} else if (detectedExt == ".mkv" && strings.EqualFold(currentExt, ".mp4")) ||
				(detectedExt == ".webm" && strings.EqualFold(currentExt, ".mp4")) ||
				(detectedExt == ".mp4" && strings.EqualFold(currentExt, ".mkv")) {
				// Prevent container-extension mismatch which corrupts media parsing and previews
				formatted = strings.TrimSuffix(formatted, currentExt) + detectedExt
			}
		}
	}

	formatted = SanitizeFilename(formatted)

	caption := formatted
	if captionTmpl != "" && filePath != "" {
		details, _ := ProbeMedia(filePath)
		fi, _ := os.Stat(filePath)
		sizeStr := "0 B"
		if fi != nil {
			sizeStr = GetReadableFileSize(fi.Size())
		}
		durStr := "0s"
		qualStr := "HD"
		langStr := ""
		subStr := ""
		if details != nil {
			durStr = GetReadableTime(int64(details.Duration))
			qualStr = details.Quality
			langStr = strings.Join(details.AudioLanguages, ", ")
			subStr = strings.Join(details.SubLanguages, ", ")
		}

		capRes := captionTmpl
		capRes = strings.ReplaceAll(capRes, "{filename}", formatted)
		capRes = strings.ReplaceAll(capRes, "{size}", sizeStr)
		capRes = strings.ReplaceAll(capRes, "{duration}", durStr)
		capRes = strings.ReplaceAll(capRes, "{quality}", qualStr)
		capRes = strings.ReplaceAll(capRes, "{languages}", langStr)
		capRes = strings.ReplaceAll(capRes, "{subtitles}", subStr)
		caption = capRes
	}

	return formatted, caption
}

// DetectExtension inspects binary magic bytes and media streams to detect a file's true extension
func DetectExtension(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	header := make([]byte, 512)
	n, _ := f.Read(header)
	if n < 4 {
		return ""
	}

	// EBML Matroska / WebM
	if bytes.HasPrefix(header, []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		if bytes.Contains(header[:n], []byte("webm")) {
			return ".webm"
		}
		return ".mkv"
	}

	// MP4 (ftyp at offset 4)
	if n >= 8 && string(header[4:8]) == "ftyp" {
		return ".mp4"
	}

	// AVI (RIFF ... AVI )
	if n >= 12 && string(header[0:4]) == "RIFF" && string(header[8:12]) == "AVI " {
		return ".avi"
	}

	// FLV
	if bytes.HasPrefix(header, []byte("FLV")) {
		return ".flv"
	}

	// ZIP / 7Z / RAR
	if bytes.HasPrefix(header, []byte{0x50, 0x4B, 0x03, 0x04}) {
		return ".zip"
	}
	if bytes.HasPrefix(header, []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}) {
		return ".7z"
	}
	if bytes.HasPrefix(header, []byte("Rar!")) {
		return ".rar"
	}

	// Audio formats
	if bytes.HasPrefix(header, []byte("ID3")) || (header[0] == 0xFF && (header[1]&0xE0) == 0xE0) {
		return ".mp3"
	}
	if bytes.HasPrefix(header, []byte("fLaC")) {
		return ".flac"
	}
	if bytes.HasPrefix(header, []byte("OggS")) {
		return ".ogg"
	}

	// PDF / Images
	if bytes.HasPrefix(header, []byte("%PDF")) {
		return ".pdf"
	}
	if bytes.HasPrefix(header, []byte{0xFF, 0xD8, 0xFF}) {
		return ".jpg"
	}
	if bytes.HasPrefix(header, []byte{0x89, 0x50, 0x4E, 0x47}) {
		return ".png"
	}

	// Fallback to ProbeMedia if ffprobe is installed
	if details, err := ProbeMedia(filePath); err == nil && details != nil {
		if details.Width > 0 || details.VideoCodec != "" {
			return ".mkv"
		}
		if details.AudioCodec != "" {
			return ".mp3"
		}
	}

	return ""
}

func applyRegexRemname(filename, rules string) string {
	parts := strings.Split(strings.TrimPrefix(rules, "|"), "|")
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	for _, rule := range parts {
		args := strings.Split(rule, ":")
		if len(args) >= 2 {
			pat := args[0]
			rep := args[1]
			re, err := getCompiledRegex(pat)
			if err == nil {
				if len(args) == 3 {
					count := mustAtoi(args[2])
					matches := re.FindAllStringIndex(base, count)
					for i := len(matches) - 1; i >= 0; i-- {
						m := matches[i]
						base = base[:m[0]] + rep + base[m[1]:]
					}
				} else {
					base = re.ReplaceAllString(base, rep)
				}
			}
		} else if len(args) == 1 && args[0] != "" {
			re, err := getCompiledRegex(args[0])
			if err == nil {
				base = re.ReplaceAllString(base, "")
			}
		}
	}
	return base + ext
}

func isVideoExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".mkv", ".webm", ".avi", ".mov", ".flv", ".wmv", ".m4v", ".ts":
		return true
	}
	return false
}

func SplitFile(filePath string, maxSplitSize int64, outDir string, equalSplits bool) ([]string, error) {
	_ = os.MkdirAll(outDir, 0755)
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	fileSize := fi.Size()
	if fileSize <= maxSplitSize {
		return []string{filePath}, nil
	}

	// Safety margin of 5MB so Telegram never rejects with FILE_TOO_BIG (under 2GB / 4GB)
	splitSize := maxSplitSize - (5 * 1024 * 1024)
	if splitSize <= 0 {
		splitSize = maxSplitSize
	}

	partsCount := int((fileSize + maxSplitSize - 1) / maxSplitSize)
	if equalSplits && partsCount > 1 {
		splitSize = (fileSize+int64(partsCount)-1)/int64(partsCount) + 1024
	}

	extName := strings.ToLower(filepath.Ext(filePath))
	details, probeErr := ProbeMedia(filePath)
	if probeErr == nil && details.Duration > 5 && isVideoExtension(extName) {
		parts, vErr := splitVideo(filePath, fileSize, splitSize, details.Duration, outDir, partsCount, equalSplits)
		if vErr == nil && len(parts) > 1 {
			return parts, nil
		}
		// If video split failed, clean up any partial files and fallback to binary split
		for _, p := range parts {
			_ = os.Remove(p)
		}
	}

	return splitBinary(filePath, splitSize, outDir)
}

func splitVideo(filePath string, totalSize int64, splitSize int64, duration int, outDir string, expectedParts int, equalSplits bool) ([]string, error) {
	var parts []string
	ext := filepath.Ext(filePath)
	base := strings.TrimSuffix(filepath.Base(filePath), ext)

	// Equal duration split branch (only when EqualSplits is explicitly requested)
	if equalSplits && expectedParts > 1 {
		partDuration := duration / expectedParts
		for i := 0; i < expectedParts; i++ {
			partPath := filepath.Join(outDir, fmt.Sprintf("%s.part%03d%s", base, i+1, ext))
			start := i * partDuration
			cmd := exec.Command("ffmpeg",
				"-hide_banner", "-loglevel", "error",
				"-ss", strconv.Itoa(start),
				"-i", filePath,
				"-t", strconv.Itoa(partDuration),
				"-map", "0",
				"-c", "copy",
				partPath, "-y",
			)
			if err := cmd.Run(); err == nil {
				parts = append(parts, partPath)
			}
		}
		if len(parts) > 1 {
			return parts, nil
		}
		for _, p := range parts {
			_ = os.Remove(p)
		}
		parts = nil
	}

	// Default branch: Size-based split (-fs splitSize) like AZML
	// Part 1 fills up to ~2GB (splitSize), and Part 2 gets the remaining ~40MB
	startTime := 0
	partIdx := 1
	maxLoops := expectedParts + 5 // safety limit

	for (partIdx <= expectedParts || startTime < duration-4) && partIdx <= maxLoops {
		partPath := filepath.Join(outDir, fmt.Sprintf("%s.part%03d%s", base, partIdx, ext))
		_ = os.Remove(partPath)

		// Try with -map 0 first (preserves all audio/subtitle streams like AZML)
		cmd := exec.Command("ffmpeg",
			"-hide_banner", "-loglevel", "error",
			"-ss", strconv.Itoa(startTime),
			"-i", filePath,
			"-fs", strconv.FormatInt(splitSize, 10),
			"-map", "0",
			"-map_chapters", "-1",
			"-async", "1",
			"-strict", "-2",
			"-c", "copy",
			partPath, "-y",
		)
		err := cmd.Run()
		if err != nil {
			// If -map 0 failed, retry without -map 0
			_ = os.Remove(partPath)
			cmdRetry := exec.Command("ffmpeg",
				"-hide_banner", "-loglevel", "error",
				"-ss", strconv.Itoa(startTime),
				"-i", filePath,
				"-fs", strconv.FormatInt(splitSize, 10),
				"-map_chapters", "-1",
				"-async", "1",
				"-strict", "-2",
				"-c", "copy",
				partPath, "-y",
			)
			if rErr := cmdRetry.Run(); rErr != nil {
				return nil, fmt.Errorf("ffmpeg split failed on part %d: %w", partIdx, rErr)
			}
		}

		pStat, statErr := os.Stat(partPath)
		if statErr != nil || pStat.Size() < 1024 {
			_ = os.Remove(partPath)
			break
		}

		pDetails, pErr := ProbeMedia(partPath)
		if pErr != nil || pDetails.Duration <= 0 {
			return nil, fmt.Errorf("could not probe part %d duration", partIdx)
		}

		// If duration of trailing keyframe fragment is negligible (<= 3s), drop it
		if pDetails.Duration <= 3 && partIdx > 1 {
			_ = os.Remove(partPath)
			break
		}

		parts = append(parts, partPath)

		// Advance startTime by part duration minus 3s overlap (as in AZML for keyframe alignment)
		advance := pDetails.Duration - 3
		if advance <= 0 {
			advance = pDetails.Duration
		}
		startTime += advance

		if startTime >= duration-4 {
			break
		}
		partIdx++
	}

	if len(parts) > 1 {
		return parts, nil
	}
	return nil, fmt.Errorf("size-based video split produced %d parts, expected > 1", len(parts))
}

func splitBinary(filePath string, maxSplitSize int64, outDir string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var parts []string
	base := filepath.Base(filePath)
	partIdx := 1
	buf := make([]byte, 1024*1024)

	for {
		partPath := filepath.Join(outDir, fmt.Sprintf("%s.%03d", base, partIdx))
		out, err := os.Create(partPath)
		if err != nil {
			return nil, err
		}

		written, err := io.CopyBuffer(out, io.LimitReader(f, maxSplitSize), buf)
		out.Close()
		if written > 0 {
			parts = append(parts, partPath)
			partIdx++
		}
		if err != nil || written == 0 {
			break
		}
	}
	return parts, nil
}

func TakeScreenshots(videoPath string, count int) ([]string, error) {
	if count <= 0 {
		count = 3
	}
	details, err := ProbeMedia(videoPath)
	if err != nil {
		return nil, err
	}
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("ss_%s", RandomHex(4)))
	_ = os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	step := details.Duration / (count + 1)
	var imageFiles []string

	for i := 1; i <= count; i++ {
		sec := i * step
		imgPath := filepath.Join(tmpDir, fmt.Sprintf("ss_%d.jpg", i))
		cmd := exec.Command("ffmpeg",
			"-hide_banner", "-loglevel", "error",
			"-ss", strconv.Itoa(sec),
			"-i", videoPath,
			"-vframes", "1",
			"-q:v", "2",
			imgPath, "-y",
		)
		if cmd.Run() == nil {
			imageFiles = append(imageFiles, imgPath)
		}
	}

	var telegraphLinks []string
	for _, img := range imageFiles {
		if link, err := uploadToTelegraph(img); err == nil {
			telegraphLinks = append(telegraphLinks, link)
		}
	}
	return telegraphLinks, nil
}

func uploadToTelegraph(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	_, _ = io.Copy(part, f)
	writer.Close()

	req, _ := http.NewRequest("POST", "https://graph.org/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res []struct {
		Src string `json:"src"`
	}
	if json.NewDecoder(resp.Body).Decode(&res) == nil && len(res) > 0 {
		return "https://graph.org" + res[0].Src, nil
	}
	return "", errors.New("failed to upload image to telegraph")
}

func GenerateThumbnail(filePath, customThumbPath string) string {
	if customThumbPath != "" {
		if _, err := os.Stat(customThumbPath); err == nil {
			outThumb := filepath.Join(os.TempDir(), fmt.Sprintf("thumb_320_%s.jpg", RandomHex(4)))
			cmd := exec.Command("ffmpeg",
				"-hide_banner", "-loglevel", "error",
				"-i", customThumbPath,
				"-vf", "scale=320:320:force_original_aspect_ratio=decrease,scale=trunc(iw/2)*2:trunc(ih/2)*2",
				"-pix_fmt", "yuvj420p",
				"-q:v", "2",
				outThumb, "-y",
			)
			if cmd.Run() == nil {
				return outThumb
			}
			return customThumbPath
		}
	}

	details, _ := ProbeMedia(filePath)
	captureSec := 3
	if details != nil && details.Duration > 4 {
		captureSec = details.Duration / 4
		if captureSec > 120 {
			captureSec = 120
		}
	}

	outThumb := filepath.Join(os.TempDir(), fmt.Sprintf("thumb_%s.jpg", RandomHex(4)))

	// Primary attempt: fast input seeking with even dimension scaling
	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-ss", strconv.Itoa(captureSec),
		"-i", filePath,
		"-vframes", "1",
		"-vf", "scale=320:320:force_original_aspect_ratio=decrease,scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-pix_fmt", "yuvj420p",
		"-q:v", "2",
		outThumb, "-y",
	)
	if cmd.Run() == nil {
		if fi, sErr := os.Stat(outThumb); sErr == nil && fi.Size() > 0 {
			return outThumb
		}
	}

	// Fallback attempt 1: accurate output seeking at 2s
	cmdFallback := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-i", filePath,
		"-ss", "00:00:02",
		"-vframes", "1",
		"-vf", "scale=320:320:force_original_aspect_ratio=decrease,scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-pix_fmt", "yuvj420p",
		"-q:v", "2",
		outThumb, "-y",
	)
	if cmdFallback.Run() == nil {
		if fi, sErr := os.Stat(outThumb); sErr == nil && fi.Size() > 0 {
			return outThumb
		}
	}

	// Fallback attempt 2: frame 0 without seek (guaranteed for short / tricky files)
	cmdFallback0 := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-i", filePath,
		"-vframes", "1",
		"-vf", "scale=320:320:force_original_aspect_ratio=decrease,scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-pix_fmt", "yuvj420p",
		"-q:v", "2",
		outThumb, "-y",
	)
	if cmdFallback0.Run() == nil {
		if fi, sErr := os.Stat(outThumb); sErr == nil && fi.Size() > 0 {
			return outThumb
		}
	}

	return ""
}

func GenerateVideoCover(filePath, customThumbPath string) string {
	if customThumbPath != "" {
		if _, err := os.Stat(customThumbPath); err == nil {
			return customThumbPath
		}
	}

	details, _ := ProbeMedia(filePath)
	captureSec := 3
	if details != nil && details.Duration > 4 {
		captureSec = details.Duration / 4
		if captureSec > 120 {
			captureSec = 120
		}
	}

	outCover := filepath.Join(os.TempDir(), fmt.Sprintf("cover_hd_%s.jpg", RandomHex(4)))

	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-ss", strconv.Itoa(captureSec),
		"-i", filePath,
		"-vframes", "1",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-pix_fmt", "yuvj420p",
		"-q:v", "2",
		outCover, "-y",
	)
	if cmd.Run() == nil {
		if fi, sErr := os.Stat(outCover); sErr == nil && fi.Size() > 0 {
			return outCover
		}
	}

	cmdFallback := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-i", filePath,
		"-ss", "00:00:02",
		"-vframes", "1",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-pix_fmt", "yuvj420p",
		"-q:v", "2",
		outCover, "-y",
	)
	if cmdFallback.Run() == nil {
		if fi, sErr := os.Stat(outCover); sErr == nil && fi.Size() > 0 {
			return outCover
		}
	}

	// Fallback 0: first frame
	cmdFallback0 := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-i", filePath,
		"-vframes", "1",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-pix_fmt", "yuvj420p",
		"-q:v", "2",
		outCover, "-y",
	)
	if cmdFallback0.Run() == nil {
		if fi, sErr := os.Stat(outCover); sErr == nil && fi.Size() > 0 {
			return outCover
		}
	}

	return ""
}
