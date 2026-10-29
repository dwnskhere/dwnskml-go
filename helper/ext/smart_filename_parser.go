package ext

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	smartRangeRE           = regexp.MustCompile(`(?i)(^|[ ._-])S([0-9]{1,2})[ ._-]*E([0-9]{1,4})[ ._-]*(-|–|~|\+|&|,|to|and)[ ._-]*E?([0-9]{1,4})`)
	smartSingleRE          = regexp.MustCompile(`(?i)(^|[ ._-])S([0-9]{1,2})[ ._-]*E([0-9]{1,4})`)
	smartSeasonPackRE      = regexp.MustCompile(`(?i)(^|[ ._-])S([0-9]{1,2})[ ._-]*(COMPLETE|COMPLETED|FULL|PACK|SEASON)\b`)
	smartSeasonWordRE      = regexp.MustCompile(`(?i)(^|[ ._-])Season[ ._-]*([0-9]{1,2})\b`)
	smartYearRE            = regexp.MustCompile(`(19[0-9]{2}|20[0-3][0-9])`)
	smartQualityRE         = regexp.MustCompile(`(?i)(480p|540p|576p|720p|1080p|1440p|2160p|4320p|4k)`)
	smartCodecRE           = regexp.MustCompile(`(?i)(x264|x265|h\.264|h264|h\.265|h265|hevc|av1|vp9|vp8|mpeg-?2|mpeg-?4)`)
	smartSourceRE          = regexp.MustCompile(`(?i)(web[ ._-]?dl|web[ ._-]?rip|blu[ ._-]?ray|bdrip|brrip|hdrip|hdtv|dvdrip|remux|camrip|webrip)`)
	smartOTTRE             = regexp.MustCompile(`(?i)(netflix|\bNF\b|amazon|\bAMZN\b|\bDSNP\b|disney\+?|\bHMAX\b|\bATVP\b|\bHULU\b|\bCR\b|\bZEE5\b|\bJIOCINEMA\b|\bSONYLIV\b)`)
	smartAudioRE           = regexp.MustCompile(`(?i)(truehd|dts[- .]?hd([ .]?ma)?|dts|eac3|ddp?5?\.1|aac(2\.0)?|flac|opus|ac3|atmos|multi[ ._-]?audio|dual[ ._-]?audio)`)
	smartHDRRE             = regexp.MustCompile(`(?i)(dolby[ ._-]?vision|\bDV\b|\bHDR10\+?|\bHDR\b|HLG)`)
	smartBitDepthRE        = regexp.MustCompile(`(?i)(10|12)[ ._-]?bit`)
	smartSubtitleRE        = regexp.MustCompile(`(?i)(multi[ ._-]?subs?|eng(lish)?[ ._-]?subs?|esubs?|softsubs?|subbed)`)
	smartEncoderRE         = regexp.MustCompile(`(?i)(?:encoded[ ._-]*by|encoder[ ._-]*)([a-z0-9][a-z0-9._-]{1,40})`)
	smartReleaseGroupRE    = regexp.MustCompile(`(?i)-([a-z0-9][a-z0-9._]{1,30})$`)
	smartEpisodeTitleCutRE = regexp.MustCompile(`(?i)(^|[ ._-])(480p|540p|576p|720p|1080p|1440p|2160p|4320p|4k|web[ ._-]?dl|web[ ._-]?rip|blu[ ._-]?ray|hdtv|x264|x265|h264|h265|hevc|av1|hdr|dv|10bit|8bit|nf|amzn|dsnp)([ ._-]|$)`)
	smartAnimeEpisodeRE    = regexp.MustCompile(`(?i)[ ._-]+-[ ._-]*E?([0-9]{1,3})([ ._-]+|\[|$)`)
)

// ParseSmartFilename extracts conservative media identity and technical hints from a release filename.
// It deliberately records unknown/ambiguous cases instead of assigning synthetic season or episode values.
func ParseSmartFilename(filename string) SmartParsedFilename {
	p := SmartParsedFilename{OriginalFilename: filepath.Base(filename), MediaType: SmartMediaUnknown}
	base := filepath.Base(filename)
	core, splitSuffix, ext := splitSmartSuffix(base)
	p.Extension, p.SplitSuffix = ext, splitSuffix
	if splitSuffix != "" && ext != "" {
		baseLower, extLower := strings.ToLower(base), strings.ToLower(ext)
		p.SplitSuffixAfterExtension = strings.LastIndex(baseLower, extLower) < strings.LastIndex(baseLower, strings.ToLower(splitSuffix))
	}
	if p.Extension == "" {
		p.Extension = filepath.Ext(core)
		core = strings.TrimSuffix(core, p.Extension)
	}
	p.Title = strings.TrimSpace(core)
	if p.Title == "" {
		p.Warnings = append(p.Warnings, "filename has no usable stem")
		return p
	}

	// Technical hints are intentionally collected independently of title parsing.
	if m := smartQualityRE.FindString(core); m != "" {
		p.Quality = strings.ToLower(m)
		if strings.EqualFold(m, "4k") {
			p.Quality = "2160p"
		}
		p.Evidence = append(p.Evidence, "resolution token in filename")
	}
	if m := smartSourceRE.FindString(core); m != "" {
		p.Source = normalizeSmartSource(m)
		p.Evidence = append(p.Evidence, "source token in filename")
	}
	if m := smartOTTRE.FindString(core); m != "" {
		p.Platform = normalizeSmartPlatform(m)
	}
	if m := smartCodecRE.FindString(core); m != "" {
		p.VideoCodec = normalizeSmartCodec(m)
		p.Evidence = append(p.Evidence, "codec token in filename")
	}
	if m := smartAudioRE.FindString(core); m != "" {
		p.AudioHints = normalizeSmartToken(m)
	}
	if m := smartHDRRE.FindString(core); m != "" {
		p.HDR = normalizeSmartHDR(m)
	}
	if m := smartBitDepthRE.FindStringSubmatch(core); len(m) > 1 {
		p.BitDepth = m[1] + "-bit"
	}
	if smartSubtitleRE.MatchString(core) {
		p.SubtitleHints = normalizeSmartToken(smartSubtitleRE.FindString(core))
	}
	if m := smartEncoderRE.FindStringSubmatch(core); len(m) > 1 {
		p.Encoder = strings.TrimSpace(m[1])
	}
	// Release groups are only inferred when the filename already has release metadata;
	// this avoids classifying ordinary hyphenated titles as group tags.
	if smartQualityRE.MatchString(core) || smartSourceRE.MatchString(core) || smartCodecRE.MatchString(core) {
		if m := smartReleaseGroupRE.FindStringSubmatch(core); len(m) > 1 {
			p.ReleaseGroup = strings.TrimSpace(m[1])
		}
	}

	identityEnd := len(core)
	setIdentityEnd := func(start int) {
		if start >= 0 && start < identityEnd {
			identityEnd = start
		}
	}

	if loc := smartRangeRE.FindStringSubmatchIndex(core); len(loc) >= 8 {
		p.Season = atoiSafe(core[loc[4]:loc[5]])
		p.EpisodeStart = atoiSafe(core[loc[6]:loc[7]])
		// The final capture is episode end; Go regexp indices contain each group pair.
		if len(loc) >= 12 && loc[10] >= 0 {
			p.EpisodeEnd = atoiSafe(core[loc[10]:loc[11]])
		}
		p.MediaType = SmartMediaEpisodeRange
		p.Confidence = 0.92
		p.Evidence = append(p.Evidence, "explicit season/episode range")
		setIdentityEnd(loc[0])
		p.EpisodeTitle = extractSmartEpisodeTitle(core[loc[1]:])
	} else if loc := smartSingleRE.FindStringSubmatchIndex(core); len(loc) >= 8 {
		p.Season = atoiSafe(core[loc[4]:loc[5]])
		p.EpisodeStart = atoiSafe(core[loc[6]:loc[7]])
		p.MediaType = SmartMediaEpisode
		p.Confidence = 0.91
		p.Evidence = append(p.Evidence, "explicit season/episode pattern")
		setIdentityEnd(loc[0])
		p.EpisodeTitle = extractSmartEpisodeTitle(core[loc[1]:])
	} else if loc := smartSeasonPackRE.FindStringSubmatchIndex(core); len(loc) >= 6 {
		p.Season = atoiSafe(core[loc[4]:loc[5]])
		p.MediaType = SmartMediaSeasonPack
		p.Confidence = 0.88
		p.Evidence = append(p.Evidence, "explicit season pack marker")
		setIdentityEnd(loc[0])
	} else if loc := smartSeasonWordRE.FindStringSubmatchIndex(core); len(loc) >= 6 {
		p.Season = atoiSafe(core[loc[4]:loc[5]])
		p.MediaType = SmartMediaSeasonPack
		p.Confidence = 0.80
		p.Evidence = append(p.Evidence, "Season N pattern")
		setIdentityEnd(loc[0])
	} else if loc := smartAnimeEpisodeRE.FindStringSubmatchIndex(core); len(loc) >= 4 {
		ep := atoiSafe(core[loc[2]:loc[3]])
		// A bare " - 2024" is more likely a year than an anime episode.
		matched := core[loc[0]:loc[1]]
		explicitEpisodeLetter := strings.Contains(strings.ToUpper(matched), "E")
		if ep > 0 && ep <= 999 && (explicitEpisodeLetter || p.Quality != "" || strings.Contains(core, "[") || strings.Contains(core, "{")) {
			p.EpisodeStart, p.IsAnime = ep, true
			p.MediaType, p.Confidence = SmartMediaEpisode, 0.78
			p.Evidence = append(p.Evidence, "anime-style episode marker")
			setIdentityEnd(loc[0])
			p.EpisodeTitle = extractSmartEpisodeTitle(core[loc[1]:])
		}
	}

	// Pick a plausible year only when a non-empty title precedes it. This avoids interpreting
	// a title beginning with digits (e.g. "2001 A Space Odyssey") as its release year.
	if p.MediaType == SmartMediaUnknown {
		if idx := smartYearCandidate(core); idx >= 0 {
			m := smartYearRE.FindString(core[idx:])
			if m != "" {
				p.Year = m
				setIdentityEnd(idx)
				p.Evidence = append(p.Evidence, "release year token with preceding title")
			}
		}
	} else if idx := smartYearCandidate(core); idx >= 0 && idx < identityEnd {
		p.Year = smartYearRE.FindString(core[idx:])
		setIdentityEnd(idx)
	}

	// Technical tokens are hard boundaries only when no stronger episode/year boundary was found.
	if p.MediaType == SmartMediaUnknown && p.Year == "" {
		for _, re := range []*regexp.Regexp{smartQualityRE, smartSourceRE, smartCodecRE, smartOTTRE, smartHDRRE, smartAudioRE, smartBitDepthRE, smartSubtitleRE} {
			if loc := re.FindStringIndex(core); loc != nil {
				setIdentityEnd(loc[0])
			}
		}
	}
	if identityEnd > 0 && identityEnd < len(core) {
		p.Title = cleanSmartTitle(core[:identityEnd])
	} else {
		p.Title = cleanSmartTitle(core[:identityEnd])
	}

	if p.MediaType == SmartMediaUnknown && p.Title != "" {
		// A year token is evidence for a movie; otherwise only classify as movie if a
		// recognizable release-quality/source token exists. Otherwise leave it ambiguous.
		if p.Year != "" || (p.Quality != "" && (p.Source != "" || p.VideoCodec != "")) {
			p.MediaType = SmartMediaMovie
			p.Confidence = 0.76
			p.Evidence = append(p.Evidence, "movie-like release metadata")
		}
	}
	if p.Title == "" || strings.EqualFold(p.Title, "unknown") {
		p.Confidence = 0
		p.MediaType = SmartMediaUnknown
		p.Warnings = append(p.Warnings, "could not isolate a reliable title")
	} else if p.Confidence == 0 {
		p.Confidence = 0.45
		p.Warnings = append(p.Warnings, "media type is ambiguous; title-only fallback will be used")
	}
	if p.MediaType == SmartMediaEpisode && p.Season == 0 && p.IsAnime {
		p.Season = 0
	}
	return p
}

func splitSmartSuffix(filename string) (stem, splitSuffix, ext string) {
	base := filepath.Base(filename)
	// part001.mkv / .part001.mkv
	partExt := regexp.MustCompile(`(?i)^(.*?)(\.part[0-9]+)(\.[a-z0-9]{2,5})$`)
	if m := partExt.FindStringSubmatch(base); len(m) == 4 {
		return m[1], m[2], m[3]
	}
	// movie.mkv.001 — preserve the media extension and trailing segment number.
	postPart := regexp.MustCompile(`(?i)^(.*\.(mkv|mp4|avi|webm|mov|m4v|ts|m2ts|wmv))(\.[0-9]{3}|\.part[0-9]+)$`)
	if m := postPart.FindStringSubmatch(base); len(m) == 4 {
		ext = "." + strings.ToLower(m[2])
		return strings.TrimSuffix(m[1], ext), m[3], ext
	}
	ext = filepath.Ext(base)
	return strings.TrimSuffix(base, ext), "", ext
}

func smartYearCandidate(s string) int {
	for _, loc := range smartYearRE.FindAllStringIndex(s, -1) {
		if loc[1] < len(s) {
			r, _ := utf8.DecodeRuneInString(s[loc[1]:])
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
		}
		prefix := strings.TrimSpace(s[:loc[0]])
		if prefix == "" {
			continue
		}
		letters := 0
		for _, r := range prefix {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		// Numeric-title films such as "1917.2019.1080p" have no letters before
		// the release year. Accept a leading 3-4 digit title only when another
		// separately delimited plausible year follows it.
		numericTitlePrefix := regexp.MustCompile(`^[0-9]{3,4}[ ._-]+$`).MatchString(prefix)
		if (letters >= 2 || numericTitlePrefix) && hasSmartYearSuffixEvidence(s[loc[1]:]) {
			return loc[0]
		}
	}
	return -1
}

func hasSmartYearSuffixEvidence(suffix string) bool {
	suffix = strings.TrimLeft(suffix, " ._-()[]{}")
	if suffix == "" {
		return true
	}
	for _, re := range []*regexp.Regexp{smartQualityRE, smartSourceRE, smartCodecRE, smartOTTRE, smartHDRRE, smartAudioRE, smartBitDepthRE, smartSubtitleRE, smartSingleRE, smartRangeRE, smartSeasonPackRE, smartSeasonWordRE} {
		if loc := re.FindStringIndex(suffix); loc != nil && loc[0] == 0 {
			return true
		}
	}
	return regexp.MustCompile(`(?i)^(REPACK|PROPER|REAL|REMUX|REMASTERED|EXTENDED|UNCUT|COMPLETE|FULL|PACK)(?:[ ._-]|$)`).MatchString(suffix)
}

func extractSmartEpisodeTitle(s string) string {
	s = strings.TrimSpace(s)
	cut := len(s)
	// Technical metadata often follows an episode identity inside brackets, e.g.
	// "[1080p]". Search the actual token as well as boundary-aware patterns so a
	// leading bracket cannot be mistaken for the episode title.
	for _, re := range []*regexp.Regexp{smartEpisodeTitleCutRE, smartQualityRE, smartSourceRE, smartCodecRE, smartOTTRE, smartHDRRE, smartAudioRE, smartBitDepthRE, smartSubtitleRE} {
		if loc := re.FindStringIndex(s); loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	if cut < len(s) {
		s = s[:cut]
	}
	s = strings.Trim(s, " ._-[](){}")
	return cleanSmartTitle(s)
}

func cleanSmartTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(".", " ", "_", " ", "\t", " ", "[", " ", "]", " ", "{", " ", "}", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, " -_.()[]{}")
	return s
}

func normalizeSmartSource(s string) string {
	n := strings.ToUpper(strings.NewReplacer(" ", "", ".", "", "_", "", "-", "").Replace(s))
	switch n {
	case "WEBDL":
		return "WEB-DL"
	case "WEBRIP":
		return "WEBRip"
	case "BLURAY":
		return "BluRay"
	case "BDRIP":
		return "BDRip"
	case "BRRIP":
		return "BRRip"
	case "HDRIP":
		return "HDRip"
	case "DVDRIP":
		return "DVDRip"
	case "REMUX":
		return "Remux"
	case "HDTV":
		return "HDTV"
	case "CAMRIP":
		return "CAMRip"
	default:
		return strings.ToUpper(strings.TrimSpace(s))
	}
}

func normalizeSmartCodec(s string) string {
	n := strings.ToLower(strings.NewReplacer(".", "", "-", "").Replace(s))
	switch n {
	case "x264", "h264":
		return "H.264"
	case "x265", "h265", "hevc":
		return "HEVC"
	case "av1":
		return "AV1"
	case "vp9":
		return "VP9"
	case "vp8":
		return "VP8"
	case "mpeg2", "mpeg-2":
		return "MPEG-2"
	case "mpeg4", "mpeg-4":
		return "MPEG-4"
	default:
		return strings.ToUpper(s)
	}
}

func normalizeSmartPlatform(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "nf", "netflix":
		return "Netflix"
	case "amzn", "amazon":
		return "Amazon"
	case "dsnp", "disney", "disney+":
		return "Disney+"
	case "hmax":
		return "HBO Max"
	case "atvp":
		return "Apple TV+"
	case "hulu":
		return "Hulu"
	case "cr":
		return "Crunchyroll"
	case "zee5":
		return "ZEE5"
	case "jiocinema":
		return "JioCinema"
	case "sonyliv":
		return "SonyLIV"
	default:
		return s
	}
}

func normalizeSmartHDR(s string) string {
	v := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "_", ""))
	if strings.Contains(v, "DOLBY") || v == "DV" {
		return "Dolby Vision"
	}
	if strings.Contains(v, "HDR10+") {
		return "HDR10+"
	}
	if strings.Contains(v, "HLG") {
		return "HLG"
	}
	return "HDR"
}

func normalizeSmartToken(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s)), " "))
}

func atoiSafe(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
