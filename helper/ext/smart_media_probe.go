package ext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type smartFFProbeDocument struct {
	Streams []smartFFProbeStream `json:"streams"`
	Format  struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

type smartFFProbeStream struct {
	Index            int               `json:"index"`
	CodecType        string            `json:"codec_type"`
	CodecName        string            `json:"codec_name"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	PixFmt           string            `json:"pix_fmt"`
	BitsPerRawSample string            `json:"bits_per_raw_sample"`
	BitsPerSample    int               `json:"bits_per_sample"`
	BitRate          string            `json:"bit_rate"`
	Channels         int               `json:"channels"`
	ChannelLayout    string            `json:"channel_layout"`
	Duration         string            `json:"duration"`
	ColorTransfer    string            `json:"color_transfer"`
	ColorPrimaries   string            `json:"color_primaries"`
	Tags             map[string]string `json:"tags"`
	Disposition      map[string]int    `json:"disposition"`
	SideDataList     []map[string]any  `json:"side_data_list"`
}

var (
	lookupSmartFFprobe = exec.LookPath
	runSmartFFprobe    = func(ctx context.Context, binary, filePath string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, "-v", "error", "-show_streams", "-show_format", "-of", "json", filePath)
		return cmd.Output()
	}
)

// ProbeSmartMedia uses ffprobe with caller cancellation and returns only properties actually
// reported by the media container. Missing values remain empty/zero rather than being guessed.
func ProbeSmartMedia(ctx context.Context, filePath string) (SmartMediaProbe, error) {
	probe := SmartMediaProbe{}
	if strings.TrimSpace(filePath) == "" {
		return probe, errors.New("empty media path")
	}
	binary, err := lookupSmartFFprobe("ffprobe")
	if err != nil {
		return probe, fmt.Errorf("ffprobe executable not found: %w", err)
	}
	out, err := runSmartFFprobe(ctx, binary, filePath)
	if err != nil {
		if ctx.Err() != nil {
			return probe, fmt.Errorf("ffprobe stopped: %w", ctx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return probe, fmt.Errorf("ffprobe exited with status %d", exitErr.ExitCode())
		}
		return probe, fmt.Errorf("ffprobe execution failed: %w", err)
	}
	var doc smartFFProbeDocument
	if err := json.Unmarshal(out, &doc); err != nil {
		return probe, fmt.Errorf("decode ffprobe output: %w", err)
	}
	probe.Probed = true
	probe.FormatName = doc.Format.FormatName
	probe.Duration = parseSmartFloat(doc.Format.Duration)
	for _, s := range doc.Streams {
		switch strings.ToLower(s.CodecType) {
		case "video":
			if probe.Width == 0 {
				probe.Width, probe.Height = s.Width, s.Height
			}
			if probe.VideoCodec == "" {
				probe.VideoCodec = strings.ToLower(s.CodecName)
			}
			if probe.PixelFormat == "" {
				probe.PixelFormat = s.PixFmt
			}
			if probe.BitDepth == "" {
				bits := parseSmartInt(s.BitsPerRawSample)
				if bits == 0 {
					bits = s.BitsPerSample
				}
				if bits > 0 {
					probe.BitDepth = fmt.Sprintf("%d-bit", bits)
				} else if depth := bitDepthFromPixelFormat(s.PixFmt); depth != "" {
					probe.BitDepth = depth
				}
			}
			if probe.Duration == 0 {
				probe.Duration = parseSmartFloat(s.Duration)
			}
			if probe.HDR == "" {
				transfer := strings.ToLower(s.ColorTransfer)
				if strings.Contains(transfer, "smpte2084") {
					probe.HDR = "HDR10"
				}
				if strings.Contains(transfer, "arib-std-b67") {
					probe.HDR = "HLG"
				}
				for _, sd := range s.SideDataList {
					sideType := strings.ToLower(fmt.Sprint(sd["side_data_type"]))
					if strings.Contains(sideType, "dolby vision") || strings.Contains(sideType, "dovi") {
						probe.HDR = "Dolby Vision"
					}
				}
			}
		case "audio":
			lang := strings.TrimSpace(s.Tags["language"])
			if strings.EqualFold(lang, "und") {
				lang = ""
			}
			probe.AudioTracks = append(probe.AudioTracks, SmartAudioTrack{
				Codec: strings.ToLower(s.CodecName), Language: lang, Channels: s.Channels,
				ChannelLayout: s.ChannelLayout, Bitrate: parseSmartInt64(s.BitRate),
				Default: s.Disposition["default"] == 1, Forced: s.Disposition["forced"] == 1,
			})
		case "subtitle":
			lang := strings.TrimSpace(s.Tags["language"])
			if strings.EqualFold(lang, "und") {
				lang = ""
			}
			probe.SubtitleTracks = append(probe.SubtitleTracks, SmartSubtitleTrack{
				Language: lang, Default: s.Disposition["default"] == 1, Forced: s.Disposition["forced"] == 1,
			})
		}
	}
	if probe.Height > 0 {
		probe.Quality = smartResolution(probe.Height)
	}
	return probe, nil
}

func parseSmartFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
func parseSmartInt(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
func parseSmartInt64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func bitDepthFromPixelFormat(pixFmt string) string {
	p := strings.ToLower(pixFmt)
	for _, n := range []string{"16", "14", "12", "10"} {
		if strings.Contains(p, n) && (strings.Contains(p, "p") || strings.Contains(p, "le") || strings.Contains(p, "be")) {
			return n + "-bit"
		}
	}
	if p != "" && (strings.HasPrefix(p, "yuv420p") || strings.HasPrefix(p, "yuv422p") || strings.HasPrefix(p, "yuv444p") || p == "nv12") {
		return "8-bit"
	}
	return ""
}

func smartResolution(height int) string {
	switch {
	case height <= 0:
		return ""
	case height <= 480:
		return "480p"
	case height <= 576:
		return "576p"
	case height <= 720:
		return "720p"
	case height <= 1080:
		return "1080p"
	case height <= 1440:
		return "1440p"
	case height <= 2160:
		return "2160p"
	default:
		return fmt.Sprintf("%dp", height)
	}
}

func smartAudioSummary(tracks []SmartAudioTrack, fallback string) string {
	if len(tracks) == 0 {
		return fallback
	}
	seen := map[string]bool{}
	var entries []string
	for _, t := range tracks {
		parts := []string{}
		if t.Language != "" {
			parts = append(parts, strings.ToUpper(t.Language))
		}
		if t.Codec != "" {
			parts = append(parts, strings.ToUpper(t.Codec))
		}
		if t.ChannelLayout != "" {
			parts = append(parts, t.ChannelLayout)
		}
		value := strings.Join(parts, " ")
		if value != "" && !seen[value] {
			entries = append(entries, value)
			seen[value] = true
		}
	}
	if len(entries) == 0 {
		return fallback
	}
	if len(entries) > 3 {
		entries = entries[:3]
		entries = append(entries, "MULTI")
	}
	return strings.Join(entries, "+")
}

func smartSubtitleSummary(tracks []SmartSubtitleTrack, fallback string) string {
	if len(tracks) == 0 {
		return fallback
	}
	seen := map[string]bool{}
	var entries []string
	for _, t := range tracks {
		if t.Language == "" {
			continue
		}
		v := strings.ToUpper(t.Language)
		if !seen[v] {
			entries = append(entries, v)
			seen[v] = true
		}
	}
	if len(entries) == 0 {
		return fallback
	}
	if len(entries) > 5 {
		entries = append(entries[:5], "MULTI")
	}
	return strings.Join(entries, ",")
}
