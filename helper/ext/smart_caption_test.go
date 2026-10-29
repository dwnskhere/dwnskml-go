package ext

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSmartCaptionsRenderMediaIdentityAndTechnicalMetadata(t *testing.T) {
	res := SmartRenameResult{
		OriginalFilename: "Example.Show.S02E05-E06.720p.WEB-DL.x265.mkv",
		PhysicalFilename: "Example Show S02E05-E06 [720p] [WEB-DL] [HEVC].mkv",
		ProposedFilename: "Example Show S02E05-E06 [720p] [WEB-DL] [HEVC].mkv",
		CanonicalTitle:   "Example Show",
		MediaType:        SmartMediaEpisodeRange,
		Season:           2,
		EpisodeStart:     5,
		EpisodeEnd:       6,
		Resolution:       "720p",
		Source:           "WEB-DL",
		VideoCodec:       "HEVC",
		Parsed:           SmartParsedFilename{Extension: ".mkv"},
		Probe:            SmartMediaProbe{AudioTracks: []SmartAudioTrack{{Language: "eng", Codec: "eac3", ChannelLayout: "5.1", Bitrate: 640000}}},
	}
	caps := BuildSmartCaptions(res, "", "")
	for _, token := range []string{"Example.Show", "S02E05-E06", "720p", "WEB-DL", "HEVC", ".mkv"} {
		if !strings.Contains(caps.V1, token) || !strings.Contains(caps.V2, token) {
			t.Errorf("both caption variants should include %q; v1=%q v2=%q", token, caps.V1, caps.V2)
		}
	}
	if !strings.Contains(caps.V1, "ENG.EAC3.5.1.640k") {
		t.Errorf("v1 should contain per-track audio details, got %q", caps.V1)
	}
	if strings.Contains(caps.V2, "640k") {
		t.Errorf("v2 should use the concise audio summary, got %q", caps.V2)
	}
}

func TestBuildSmartCaptionsPreserveMultipartMarkerPlacement(t *testing.T) {
	res := SmartRenameResult{
		OriginalFilename:          "Movie.2024.mkv.001",
		PhysicalFilename:          "Movie (2024).mkv.001",
		ProposedFilename:          "Movie (2024).mkv.001",
		CanonicalTitle:            "Movie",
		MediaType:                 SmartMediaMovie,
		Year:                      "2024",
		SplitSuffix:               ".001",
		SplitSuffixAfterExtension: true,
		Parsed:                    SmartParsedFilename{Extension: ".mkv", SplitSuffix: ".001", SplitSuffixAfterExtension: true},
	}
	caps := BuildSmartCaptions(res, "", "")
	if !strings.HasSuffix(caps.V2, ".mkv.001") {
		t.Fatalf("extension and multipart marker should be retained in order, got %q", caps.V2)
	}
}

func TestBuildSmartCaptionSupportsRichTokensAndOriginalName(t *testing.T) {
	res := SmartRenameResult{
		OriginalFilename: "Release.Name.2024.<raw>.mkv",
		PhysicalFilename: "Canonical Movie (2024).mkv",
		ProposedFilename: "Canonical Movie (2024).mkv",
		CanonicalTitle:   "Canonical Movie",
		MediaType:        SmartMediaMovie,
		Year:             "2024",
		Resolution:       "1080p",
		Source:           "WEB-DL",
		VideoCodec:       "HEVC",
		Parsed:           SmartParsedFilename{Extension: ".mkv"},
	}
	got := BuildSmartCaption(res, "{orig_filename}|{physical_filename}|{smart_filename}|{show_name}|{year}|{quality}|{source}|{codec}|{smartcaptionv1}|{smartcaptionv2}|{unknown}", "")
	for _, want := range []string{"Release.Name.2024.&lt;raw&gt;.mkv", "Canonical Movie (2024).mkv", "Canonical Movie", "2024", "1080p", "WEB-DL", "HEVC"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered caption %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "{unknown}") {
		t.Fatalf("unknown/unavailable template key should be blank, got %q", got)
	}
}

func TestDefaultSmartCaptionIncludesOriginalNameWhenNormalized(t *testing.T) {
	res := SmartRenameResult{
		OriginalFilename: "Release.Name.2024.1080p.WEB-DL.mkv",
		PhysicalFilename: "Release Name (2024) [1080p] [WEB-DL].mkv",
		ProposedFilename: "Release Name (2024) [1080p] [WEB-DL].mkv",
		CanonicalTitle:   "Release Name",
		MediaType:        SmartMediaMovie,
		Year:             "2024",
		Resolution:       "1080p",
		Source:           "WEB-DL",
		Parsed:           SmartParsedFilename{Extension: ".mkv"},
	}
	got := BuildSmartCaption(res, "", "")
	if !strings.Contains(got, "OG FileName: Release.Name.2024.1080p.WEB-DL.mkv") {
		t.Fatalf("default smart caption should preserve OG filename, got %q", got)
	}
	if !strings.Contains(got, "Release.Name.2024.1080p.WEB-DL.mkv") || !strings.Contains(got, "Release.Name.2024") {
		t.Fatalf("default smart caption should contain smart caption and escaped original, got %q", got)
	}
}

func TestSmartCaptionTokenCatalogIncludesCaptionVariants(t *testing.T) {
	tokens := strings.Join(AvailableSmartCaptionTokens(), ",")
	for _, want := range []string{"orig_filename", "physical_filename", "smartcaption", "smartcaptionv1", "smartcaptionv2", "provider_id"} {
		if !strings.Contains(tokens, want) {
			t.Errorf("caption token catalog missing %q", want)
		}
	}
}

func TestPrepareSmartRenamePopulatesSmartCaptionsAndPreservesIncomingName(t *testing.T) {
	oldLookup, oldRun := lookupSmartFFprobe, runSmartFFprobe
	t.Cleanup(func() { lookupSmartFFprobe, runSmartFFprobe = oldLookup, oldRun })
	lookupSmartFFprobe = func(string) (string, error) { return "/mock/ffprobe", nil }
	runSmartFFprobe = func(ctx context.Context, binary, path string) ([]byte, error) {
		return []byte(`{"streams":[{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"pix_fmt":"yuv420p10le"},{"codec_type":"audio","codec_name":"aac","channels":2,"channel_layout":"stereo","tags":{"language":"eng"}}],"format":{"duration":"120","format_name":"matroska"}}`), nil
	}

	dir := t.TempDir()
	original := "Example.Show.S02E05.1080p.WEB-DL.x265-GROUP.mkv"
	input := filepath.Join(dir, original)
	if err := os.WriteFile(input, []byte("mock media bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, caption, err := PrepareSmartRename(context.Background(), input, nil, original)
	if err != nil {
		t.Fatal(err)
	}
	if result.OriginalFilename != original {
		t.Fatalf("original incoming filename was not preserved: got %q want %q", result.OriginalFilename, original)
	}
	if result.SmartCaptionV1 == "" || result.SmartCaptionV2 == "" {
		t.Fatalf("PrepareSmartRename should populate both caption variants: %+v", result)
	}
	if !strings.Contains(caption, "OG FileName: "+original) {
		t.Fatalf("default caption should include original filename: %q", caption)
	}
	if result.PhysicalFilename == "" || result.PhysicalPath == "" {
		t.Fatalf("physical file details should remain available: %+v", result)
	}
	if _, err := os.Stat(result.PhysicalPath); err != nil {
		t.Fatalf("tracked physical path should exist after rename: %v", err)
	}
}
