package ext

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"azml-go/config"
	"azml-go/types"
)

type SmartMediaType string

const (
	SmartMediaMovie        SmartMediaType = "movie"
	SmartMediaEpisode      SmartMediaType = "episode"
	SmartMediaEpisodeRange SmartMediaType = "episode_range"
	SmartMediaSeasonPack   SmartMediaType = "season_pack"
	SmartMediaUnknown      SmartMediaType = "unknown"
)

var (
	sensitiveQueryValueRE     = regexp.MustCompile(`(?i)(api_key=)[^&\s]+`)
	sensitiveBearerRE         = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~-]+`)
	ErrSmartInvalidPath       = errors.New("smart rename: invalid input path")
	ErrSmartUnsupportedMedia  = errors.New("smart rename: unsupported media type")
	ErrSmartProbeFailed       = errors.New("smart rename: media probe failed")
	ErrSmartMetadataLookup    = errors.New("smart rename: metadata lookup failed")
	ErrSmartAmbiguousMatch    = errors.New("smart rename: ambiguous metadata match")
	ErrSmartRenameFailed      = errors.New("smart rename: filesystem rename failed")
	ErrSmartFilenameTooLong   = errors.New("smart rename: filename cannot fit configured length")
	ErrSmartFilenameCollision = errors.New("smart rename: unable to resolve filename collision")
)

type SmartParsedFilename struct {
	OriginalFilename          string
	Title                     string
	Year                      string
	Season                    int
	EpisodeStart              int
	EpisodeEnd                int
	EpisodeTitle              string
	MediaType                 SmartMediaType
	IsAnime                   bool
	Quality                   string
	Source                    string
	Platform                  string
	VideoCodec                string
	AudioHints                string
	SubtitleHints             string
	HDR                       string
	BitDepth                  string
	ReleaseGroup              string
	Encoder                   string
	SplitSuffix               string
	SplitSuffixAfterExtension bool
	Extension                 string
	Confidence                float64
	Evidence                  []string
	Warnings                  []string
}

type SmartAudioTrack struct {
	Codec         string `json:"codec_name"`
	Language      string `json:"language"`
	Channels      int    `json:"channels"`
	ChannelLayout string `json:"channel_layout"`
	Bitrate       int64  `json:"bit_rate"`
	Default       bool   `json:"default"`
	Forced        bool   `json:"forced"`
}

type SmartSubtitleTrack struct {
	Language string `json:"language"`
	Default  bool   `json:"default"`
	Forced   bool   `json:"forced"`
}

type SmartMediaProbe struct {
	Probed         bool
	Width          int
	Height         int
	Duration       float64
	Quality        string
	VideoCodec     string
	PixelFormat    string
	BitDepth       string
	HDR            string
	AudioTracks    []SmartAudioTrack
	SubtitleTracks []SmartSubtitleTrack
	FormatName     string
	Warnings       []string
}

type SmartCanonicalMetadata struct {
	Title         string
	Year          string
	CanonicalType string
	SeriesTitle   string
	EpisodeTitle  string
	Provider      string
	ProviderID    string
	Confidence    float64
	Network       string
}

type SmartRenameOptions struct {
	OriginalFilename string
	Prefix           string
	Suffix           string
	TMDBToken        string
	LookupEnabled    bool
	ProviderPriority []string
	MaxFilenameChars int
	ProbeTimeout     time.Duration
	LookupTimeout    time.Duration
}

type SmartRenameResult struct {
	OriginalFilename          string
	OriginalPath              string
	ProposedFilename          string
	PhysicalFilename          string
	PhysicalPath              string
	FilenamePrefix            string
	FilenameSuffix            string
	SmartCaptionV1            string
	SmartCaptionV2            string
	CanonicalTitle            string
	MediaType                 SmartMediaType
	Year                      string
	Season                    int
	EpisodeStart              int
	EpisodeEnd                int
	EpisodeTitle              string
	Resolution                string
	Source                    string
	Platform                  string
	VideoCodec                string
	AudioSummary              string
	SubtitleSummary           string
	HDR                       string
	BitDepth                  string
	SplitSuffix               string
	SplitSuffixAfterExtension bool
	Provider                  string
	ReleaseGroup              string
	Encoder                   string
	ProviderID                string
	MatchConfidence           float64
	ParseConfidence           float64
	OriginalPreserved         bool
	RenameStatus              string
	Warnings                  []string
	Parsed                    SmartParsedFilename
	Probe                     SmartMediaProbe
}

// SmartAutoRename inspects the input, optionally resolves canonical metadata, and performs a
// collision-safe physical rename. Metadata/probe/provider failures are non-fatal; filesystem
// errors leave the original path tracked and return a warning-bearing result.
func SmartAutoRename(ctx context.Context, inputPath string, opts SmartRenameOptions) (SmartRenameResult, error) {
	res := SmartRenameResult{OriginalPath: inputPath, PhysicalPath: inputPath, PhysicalFilename: filepath.Base(inputPath), FilenamePrefix: opts.Prefix, FilenameSuffix: opts.Suffix, RenameStatus: "unchanged"}
	if strings.TrimSpace(inputPath) == "" {
		return res, ErrSmartInvalidPath
	}
	fi, err := os.Lstat(inputPath)
	if err != nil {
		return res, fmt.Errorf("%w: %v", ErrSmartInvalidPath, err)
	}
	if !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 {
		return res, fmt.Errorf("%w: only regular, non-symlink files are eligible", ErrSmartInvalidPath)
	}
	if !isSmartMediaExtension(filepath.Ext(stripSmartMultipartExtension(filepath.Base(inputPath)))) {
		res.OriginalFilename = filepath.Base(inputPath)
		if strings.TrimSpace(opts.OriginalFilename) != "" {
			res.OriginalFilename = filepath.Base(opts.OriginalFilename)
		}
		res.OriginalPreserved = true
		res.Warnings = append(res.Warnings, ErrSmartUnsupportedMedia.Error()+"; file was not modified")
		res.RenameStatus = "skipped"
		return res, nil
	}
	res.OriginalFilename = filepath.Base(inputPath)
	if strings.TrimSpace(opts.OriginalFilename) != "" {
		res.OriginalFilename = filepath.Base(opts.OriginalFilename)
	}
	res.OriginalPreserved = true
	parsed := ParseSmartFilename(filepath.Base(inputPath))
	res.Parsed = parsed
	res.SplitSuffix = parsed.SplitSuffix
	res.SplitSuffixAfterExtension = parsed.SplitSuffixAfterExtension
	res.ReleaseGroup, res.Encoder = parsed.ReleaseGroup, parsed.Encoder
	res.ParseConfidence = parsed.Confidence
	res.MediaType = parsed.MediaType
	res.Year, res.Season = parsed.Year, parsed.Season
	res.EpisodeStart, res.EpisodeEnd, res.EpisodeTitle = parsed.EpisodeStart, parsed.EpisodeEnd, parsed.EpisodeTitle
	res.Warnings = append(res.Warnings, parsed.Warnings...)

	probeTimeout := opts.ProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 12 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	probe, probeErr := ProbeSmartMedia(probeCtx, inputPath)
	cancel()
	res.Probe = probe
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if probeErr != nil {
		res.Warnings = append(res.Warnings, safeErrorText(fmt.Errorf("%w: %v", ErrSmartProbeFailed, probeErr))+"; using filename hints")
	}
	res.Resolution = parsed.Quality
	if probe.Quality != "" {
		res.Resolution = probe.Quality
	}
	res.VideoCodec = parsed.VideoCodec
	if probe.VideoCodec != "" {
		res.VideoCodec = normalizeSmartCodec(probe.VideoCodec)
	}
	res.HDR = parsed.HDR
	if probe.HDR != "" {
		res.HDR = probe.HDR
	}
	res.BitDepth = parsed.BitDepth
	if probe.BitDepth != "" {
		res.BitDepth = probe.BitDepth
	}
	res.Source = parsed.Source
	res.Platform = parsed.Platform
	res.AudioSummary = smartAudioSummary(probe.AudioTracks, parsed.AudioHints)
	res.SubtitleSummary = smartSubtitleSummary(probe.SubtitleTracks, parsed.SubtitleHints)

	canonicalTitle := parsed.Title
	canonicalYear := parsed.Year
	if opts.LookupEnabled && parsed.Title != "" && parsed.MediaType != SmartMediaUnknown {
		lookupTimeout := opts.LookupTimeout
		if lookupTimeout <= 0 {
			lookupTimeout = 8 * time.Second
		}
		lookupCtx, lookupCancel := context.WithTimeout(ctx, lookupTimeout)
		canonical, lookupErr := ResolveSmartMetadata(lookupCtx, parsed, opts.TMDBToken, opts.ProviderPriority...)
		lookupCancel()
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if lookupErr != nil {
			res.Warnings = append(res.Warnings, safeErrorText(fmt.Errorf("%w: %v", ErrSmartMetadataLookup, lookupErr))+"; using parsed filename metadata")
		} else if canonical == nil {
			res.Warnings = append(res.Warnings, "no sufficiently confident canonical match; using parsed filename metadata")
		}
		if canonical != nil && canonical.Title != "" && canonical.Confidence >= 0.78 {
			canonicalTitle = canonical.Title
			if canonical.Year != "" {
				canonicalYear = canonical.Year
			}
			if canonical.EpisodeTitle != "" {
				res.EpisodeTitle = canonical.EpisodeTitle
			}
			res.Provider, res.ProviderID, res.MatchConfidence = canonical.Provider, canonical.ProviderID, canonical.Confidence
			if canonical.Network != "" && res.Platform == "" {
				res.Platform = canonical.Network
			}
		} else if canonical != nil {
			res.Warnings = append(res.Warnings, "provider returned a low-confidence match; it was rejected")
		}
	}
	res.CanonicalTitle = canonicalTitle
	if canonicalYear != "" {
		res.Year = canonicalYear
	}

	if strings.TrimSpace(canonicalTitle) == "" || parsed.MediaType == SmartMediaUnknown && parsed.Confidence < 0.5 {
		res.Warnings = append(res.Warnings, "insufficient title/media evidence; original filename retained")
		res.PhysicalFilename = filepath.Base(inputPath)
		res.ProposedFilename = filepath.Base(inputPath)
		res.RenameStatus = "skipped"
		return res, nil
	}

	maxChars := opts.MaxFilenameChars
	if maxChars <= 0 {
		maxChars = 60
	}
	parts := SmartNamePartsFromMetadata(parsed, canonicalTitle, canonicalYear, res.EpisodeTitle, res.Resolution, res.Source, res.Platform, res.VideoCodec, res.AudioSummary, res.SubtitleSummary, res.HDR, res.BitDepth)
	proposed, fitErr := FitSmartFilename(parts, opts.Prefix, opts.Suffix, maxChars)
	if fitErr != nil {
		res.Warnings = append(res.Warnings, fitErr.Error()+"; original filename retained")
		res.PhysicalFilename = filepath.Base(inputPath)
		res.ProposedFilename = filepath.Base(inputPath)
		res.RenameStatus = "fallback"
		return res, nil
	}
	res.ProposedFilename = proposed
	if err := ctx.Err(); err != nil {
		return res, err
	}
	current := filepath.Base(inputPath)
	if current == proposed {
		res.PhysicalFilename = current
		res.PhysicalPath = inputPath
		res.RenameStatus = "unchanged"
		return res, nil
	}

	destPath, destName, renameErr := safeRenameSmartFile(inputPath, proposed, maxChars)
	if renameErr != nil {
		res.Warnings = append(res.Warnings, renameErr.Error()+"; original file retained")
		res.PhysicalFilename, res.PhysicalPath = current, inputPath
		res.RenameStatus = "rename_failed"
		return res, nil
	}
	res.PhysicalFilename, res.PhysicalPath = destName, destPath
	res.RenameStatus = "renamed"
	return res, nil
}

func stripSmartMultipartExtension(name string) string {
	core, _, ext := splitSmartSuffix(name)
	if ext != "" {
		return ext
	}
	return filepath.Ext(core)
}

func isSmartMediaExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mkv", ".mp4", ".avi", ".webm", ".flv", ".mov", ".m4v", ".3gp", ".ts", ".m2ts", ".wmv", ".asf", ".divx", ".ogv", ".vob", ".mpg", ".mpeg":
		return true
	default:
		return false
	}
}

// SmartRenameOptionsFromConfig applies existing per-user prefix/suffix precedence.
func SmartRenameOptionsFromConfig(user *types.UserSettings) SmartRenameOptions {
	cfg := config.Get()
	opts := SmartRenameOptions{
		Prefix: cfg.LeechFilenamePrefix, Suffix: cfg.LeechFilenameSuffix,
		TMDBToken: cfg.TMDBAccessToken, LookupEnabled: cfg.SmartRenameLookupEnabled, ProviderPriority: strings.Split(cfg.SmartRenameProviderPriority, ","),
		MaxFilenameChars: cfg.SmartRenameMaxChars,
		ProbeTimeout:     time.Duration(cfg.SmartRenameProbeTimeout) * time.Second,
		LookupTimeout:    time.Duration(cfg.SmartRenameLookupTimeout) * time.Second,
	}
	if user != nil {
		if user.Prefix != "" {
			opts.Prefix = user.Prefix
		}
		if user.Suffix != "" {
			opts.Suffix = user.Suffix
		}
	}
	return opts
}

// PrepareSmartRename is the shared leech/yt-dlp integration entrypoint. It returns the
// canonical result plus a caption generated from the original-name-preserving metadata.
func PrepareSmartRename(ctx context.Context, inputPath string, user *types.UserSettings, originalName ...string) (SmartRenameResult, string, error) {
	opts := SmartRenameOptionsFromConfig(user)
	if len(originalName) > 0 {
		opts.OriginalFilename = originalName[0]
	}
	res, err := SmartAutoRename(ctx, inputPath, opts)
	if err != nil {
		return res, "", err
	}
	caps := BuildSmartCaptions(res, opts.Prefix, opts.Suffix)
	res.SmartCaptionV1, res.SmartCaptionV2 = caps.V1, caps.V2
	captionTemplate := config.Get().LeechFilenameCaption
	if user != nil && user.Caption != "" {
		captionTemplate = user.Caption
	}
	caption := BuildSmartCaption(res, captionTemplate, res.PhysicalPath)
	return res, caption, nil
}

// BuildSmartFallbackCaption preserves caption-template behavior when Smart mode is gated off
// or cannot start. It never renames the file and exposes no fabricated metadata.
func BuildSmartFallbackCaption(currentPath string, user *types.UserSettings, originalName string) string {
	physical := filepath.Base(currentPath)
	if physical == "." || physical == string(filepath.Separator) {
		physical = ""
	}
	if strings.TrimSpace(originalName) == "" {
		originalName = physical
	}
	res := SmartRenameResult{
		OriginalFilename: filepath.Base(originalName), OriginalPath: currentPath,
		ProposedFilename: physical, PhysicalFilename: physical, PhysicalPath: currentPath,
		OriginalPreserved: true, FilenamePrefix: config.Get().LeechFilenamePrefix,
		FilenameSuffix: config.Get().LeechFilenameSuffix, RenameStatus: "unchanged",
	}
	if user != nil {
		if user.Prefix != "" {
			res.FilenamePrefix = user.Prefix
		}
		if user.Suffix != "" {
			res.FilenameSuffix = user.Suffix
		}
	}
	// Parsing filename hints is local-only: the Smart Auto Rename gate being off must
	// prevent probing/provider lookups/physical renaming, not remove useful caption data.
	parsed := ParseSmartFilename(res.OriginalFilename)
	res.Parsed = parsed
	res.MediaType = parsed.MediaType
	res.CanonicalTitle = parsed.Title
	res.Year, res.Season = parsed.Year, parsed.Season
	res.EpisodeStart, res.EpisodeEnd, res.EpisodeTitle = parsed.EpisodeStart, parsed.EpisodeEnd, parsed.EpisodeTitle
	res.Resolution, res.Source, res.Platform = parsed.Quality, parsed.Source, parsed.Platform
	res.VideoCodec, res.HDR, res.BitDepth = parsed.VideoCodec, parsed.HDR, parsed.BitDepth
	res.AudioSummary, res.SubtitleSummary = parsed.AudioHints, parsed.SubtitleHints
	res.ReleaseGroup, res.Encoder = parsed.ReleaseGroup, parsed.Encoder
	res.SplitSuffix, res.SplitSuffixAfterExtension = parsed.SplitSuffix, parsed.SplitSuffixAfterExtension
	caps := BuildSmartCaptions(res, res.FilenamePrefix, res.FilenameSuffix)
	res.SmartCaptionV1, res.SmartCaptionV2 = caps.V1, caps.V2
	template := config.Get().LeechFilenameCaption
	if user != nil && user.Caption != "" {
		template = user.Caption
	}
	return BuildSmartCaption(res, template, currentPath)
}

func safeErrorText(err error) string {
	if err == nil {
		return "unknown error"
	}
	text := strings.TrimSpace(err.Error())
	text = sensitiveQueryValueRE.ReplaceAllString(text, "${1}[redacted]")
	text = sensitiveBearerRE.ReplaceAllString(text, "${1}[redacted]")
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

func formatSmartNumber(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%02d", n)
}
func formatSmartEpisode(start, end int) string {
	if start <= 0 {
		return ""
	}
	if end > start {
		return fmt.Sprintf("%02d-%02d", start, end)
	}
	return fmt.Sprintf("%02d", start)
}

// LogSmartRenameWarnings avoids credentials and provider response bodies in logs.
func LogSmartRenameWarnings(res SmartRenameResult) {
	for _, w := range res.Warnings {
		log.Printf("[Smart AutoRename] %s: %s", res.OriginalFilename, w)
	}
}
