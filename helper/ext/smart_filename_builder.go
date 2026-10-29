package ext

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type SmartNameParts struct {
	MediaType                 SmartMediaType
	Title                     string
	Year                      string
	Season                    int
	EpisodeStart              int
	EpisodeEnd                int
	EpisodeTitle              string
	IsAnime                   bool
	Quality                   string
	Source                    string
	Platform                  string
	Codec                     string
	Audio                     string
	Subtitles                 string
	HDR                       string
	BitDepth                  string
	Extension                 string
	SplitSuffix               string
	SplitSuffixAfterExtension bool
}

var (
	smartDuplicateSpaceRE = regexp.MustCompile(`\s+`)
	smartUnsafeNameRE     = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)
)

func SmartNamePartsFromMetadata(parsed SmartParsedFilename, canonicalTitle, year, episodeTitle, quality, source, platform, codec, audio, subtitles, hdr, bitDepth string) SmartNameParts {
	return SmartNameParts{
		MediaType: parsed.MediaType, Title: canonicalTitle, Year: year,
		Season: parsed.Season, EpisodeStart: parsed.EpisodeStart, EpisodeEnd: parsed.EpisodeEnd,
		EpisodeTitle: episodeTitle, IsAnime: parsed.IsAnime, Quality: quality,
		Source: source, Platform: platform, Codec: codec, Audio: audio,
		Subtitles: subtitles, HDR: hdr, BitDepth: bitDepth,
		Extension: parsed.Extension, SplitSuffix: parsed.SplitSuffix, SplitSuffixAfterExtension: parsed.SplitSuffixAfterExtension,
	}
}

// BuildSmartFilename renders a normalized filename. Optional technical components are included
// only when known; the extension and multipart marker are retained.
func BuildSmartFilename(p SmartNameParts, prefix, suffix string) string {
	title := cleanSmartTitle(p.Title)
	if title == "" {
		title = "Unknown"
	}
	var base strings.Builder
	base.WriteString(title)
	switch p.MediaType {
	case SmartMediaMovie:
		if p.Year != "" {
			base.WriteString(" (")
			base.WriteString(p.Year)
			base.WriteByte(')')
		}
	case SmartMediaEpisode, SmartMediaEpisodeRange:
		if p.IsAnime && p.Season <= 0 {
			if p.EpisodeStart > 0 {
				base.WriteString(" - ")
				base.WriteString(formatSmartAnimeEpisode(p.EpisodeStart))
			}
		} else if p.Season > 0 && p.EpisodeStart > 0 {
			base.WriteString(" S")
			base.WriteString(formatSmartNumber(p.Season))
			base.WriteString("E")
			base.WriteString(formatSmartNumber(p.EpisodeStart))
			if p.EpisodeEnd > p.EpisodeStart {
				base.WriteString("-E")
				base.WriteString(formatSmartNumber(p.EpisodeEnd))
			}
		}
		if p.EpisodeTitle != "" {
			base.WriteByte(' ')
			base.WriteString(cleanSmartTitle(p.EpisodeTitle))
		}
	case SmartMediaSeasonPack:
		if p.Season > 0 {
			base.WriteString(" - Season ")
			base.WriteString(formatSmartNumber(p.Season))
		}
	}

	technical := []string{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, existing := range technical {
			if strings.EqualFold(existing, s) {
				return
			}
		}
		technical = append(technical, s)
	}
	add(p.Quality)
	add(p.Source)
	add(p.Platform)
	add(p.Codec)
	add(p.HDR)
	add(p.BitDepth)
	add(p.Audio)
	add(p.Subtitles)
	for _, token := range technical {
		base.WriteString(" [")
		base.WriteString(token)
		base.WriteByte(']')
	}

	ext := p.Extension
	if ext == "" || !strings.HasPrefix(ext, ".") || strings.ContainsAny(ext, `/\`) || len(ext) > 12 {
		ext = ".mkv"
	}
	name := base.String()
	if prefix != "" {
		name = prefix + name
	}
	if suffix != "" {
		name += suffix
	}
	name = sanitizeSmartFilename(name)
	if p.SplitSuffixAfterExtension {
		name += ext + p.SplitSuffix
	} else {
		name += p.SplitSuffix + ext
	}
	return name
}

func formatSmartAnimeEpisode(n int) string {
	if n <= 0 {
		return ""
	}
	if n < 1000 {
		return fmt.Sprintf("%03d", n)
	}
	return strconv.Itoa(n)
}

// FitSmartFilename applies a configurable Unicode-code-point budget to the entire final filename,
// including prefixes/suffixes, split markers and extension. It drops optional metadata before
// shortening the title; critical episode/movie identity is retained whenever possible.
func FitSmartFilename(parts SmartNameParts, prefix, suffix string, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 60
	}
	candidate := BuildSmartFilename(parts, prefix, suffix)
	if smartRuneLen(candidate) <= maxChars {
		return candidate, nil
	}

	// Progressive removal order favors identity over optional release details.
	for _, field := range []string{"audio", "subtitles", "platform", "source", "codec", "hdr", "bitdepth", "episodetitle"} {
		clearSmartPart(&parts, field)
		candidate = BuildSmartFilename(parts, prefix, suffix)
		if smartRuneLen(candidate) <= maxChars {
			return candidate, nil
		}
	}

	// Shorten title only as a last resort. Preserve year and episode identity.
	original := []rune(parts.Title)
	for target := len(original) - 1; target >= 1; target-- {
		parts.Title = strings.TrimRight(string(original[:target]), " .-_")
		candidate = BuildSmartFilename(parts, prefix, suffix)
		if smartRuneLen(candidate) <= maxChars {
			return candidate, nil
		}
	}
	return "", ErrSmartFilenameTooLong
}

func clearSmartPart(p *SmartNameParts, field string) {
	switch field {
	case "audio":
		p.Audio = ""
	case "subtitles":
		p.Subtitles = ""
	case "platform":
		p.Platform = ""
	case "source":
		p.Source = ""
	case "codec":
		p.Codec = ""
	case "hdr":
		p.HDR = ""
	case "bitdepth":
		p.BitDepth = ""
	case "episodetitle":
		p.EpisodeTitle = ""
	}
}

func sanitizeSmartFilename(s string) string {
	s = smartUnsafeNameRE.ReplaceAllString(s, "-")
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
			}
			lastSpace = true
		} else {
			b.WriteRune(r)
			lastSpace = false
		}
	}
	s = strings.TrimSpace(smartDuplicateSpaceRE.ReplaceAllString(b.String(), " "))
	s = strings.TrimRight(s, ". ")
	return s
}

// safeRenameSmartFile never overwrites an existing destination. A hard-link-and-unlink operation
// is used where supported because it fails atomically when the target already exists.
func safeRenameSmartFile(sourcePath, proposed string, maxChars int) (string, string, error) {
	if sourcePath == "" || proposed == "" {
		return sourcePath, filepath.Base(sourcePath), ErrSmartInvalidPath
	}
	if filepath.Base(proposed) != proposed || strings.ContainsAny(proposed, `/\\`) {
		return sourcePath, filepath.Base(sourcePath), errors.New("smart rename generated an unsafe path")
	}
	if maxChars > 0 && smartRuneLen(proposed) > maxChars {
		return sourcePath, filepath.Base(sourcePath), ErrSmartFilenameTooLong
	}
	dir := filepath.Dir(sourcePath)
	destPath := filepath.Join(dir, proposed)
	if filepath.Clean(destPath) == filepath.Clean(sourcePath) {
		return sourcePath, filepath.Base(sourcePath), nil
	}
	if _, err := os.Lstat(destPath); err == nil {
		return sourcePath, filepath.Base(sourcePath), ErrSmartFilenameCollision
	} else if !errors.Is(err, os.ErrNotExist) {
		return sourcePath, filepath.Base(sourcePath), err
	}
	if err := os.Link(sourcePath, destPath); err == nil {
		if err := os.Remove(sourcePath); err != nil {
			_ = os.Remove(destPath)
			return sourcePath, filepath.Base(sourcePath), errors.Join(ErrSmartRenameFailed, err)
		}
		return destPath, proposed, nil
	} else {
		// Do not fall back to a potentially overwriting rename if the filesystem cannot hard-link.
		return sourcePath, filepath.Base(sourcePath), errors.Join(ErrSmartRenameFailed, err)
	}
}

// Kept in this file to make the no-overwrite path explicit and testable.
func smartRuneLen(s string) int { return utf8.RuneCountInString(s) }

// SafeRenameFile applies the same no-overwrite, same-directory rename policy to legacy modes.
func SafeRenameFile(sourcePath, proposed string) error {
	_, _, err := safeRenameSmartFile(sourcePath, proposed, 0)
	return err
}
