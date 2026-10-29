package ext

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func disableSmartFFprobe(t *testing.T) {
	t.Helper()
	oldLookup, oldRun := lookupSmartFFprobe, runSmartFFprobe
	lookupSmartFFprobe = func(string) (string, error) { return "", errors.New("ffprobe intentionally disabled in unit test") }
	runSmartFFprobe = func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("ffprobe intentionally disabled in unit test")
	}
	t.Cleanup(func() { lookupSmartFFprobe, runSmartFFprobe = oldLookup, oldRun })
}

func TestParseSmartFilename(t *testing.T) {
	tests := []struct {
		name                   string
		media                  SmartMediaType
		title                  string
		year                   string
		season, start, end     int
		quality, source, codec string
		anime                  bool
	}{
		{"Example.Movie.2024.1080p.WEB-DL.x265.mkv", SmartMediaMovie, "Example Movie", "2024", 0, 0, 0, "1080p", "WEB-DL", "HEVC", false},
		{"Example.Show.S02E05.1080p.WEB-DL.mkv", SmartMediaEpisode, "Example Show", "", 2, 5, 0, "1080p", "WEB-DL", "", false},
		{"Example.Show.S02E05-E06.720p.mkv", SmartMediaEpisodeRange, "Example Show", "", 2, 5, 6, "720p", "", "", false},
		{"Example Anime - 012 [1080p].mkv", SmartMediaEpisode, "Example Anime", "", 0, 12, 0, "1080p", "", "", true},
		{"Naruto Shippuden - E361 [720p BD x265].mkv", SmartMediaEpisode, "Naruto Shippuden", "", 0, 361, 0, "720p", "", "HEVC", true},
		{"Example.Show.S02E05.The.Final.Battle.1080p.WEB-DL.x265-GROUP.mkv", SmartMediaEpisode, "Example Show", "", 2, 5, 0, "1080p", "WEB-DL", "HEVC", false},
		{"Example.Show.S02.COMPLETE.1080p.mkv", SmartMediaSeasonPack, "Example Show", "", 2, 0, 0, "1080p", "", "", false},
		{"Example.Movie.2024.2160p.WEB-DL.DV.HDR.HEVC.mkv", SmartMediaMovie, "Example Movie", "2024", 0, 0, 0, "2160p", "WEB-DL", "HEVC", false},
		{"映画の題名.2022.1080p.WEB-DL.mkv", SmartMediaMovie, "映画の題名", "2022", 0, 0, 0, "1080p", "WEB-DL", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSmartFilename(tt.name)
			if got.MediaType != tt.media {
				t.Fatalf("media type=%q want %q; parsed=%+v", got.MediaType, tt.media, got)
			}
			if got.Title != tt.title {
				t.Errorf("title=%q want %q", got.Title, tt.title)
			}
			if got.Year != tt.year {
				t.Errorf("year=%q want %q", got.Year, tt.year)
			}
			if got.Season != tt.season || got.EpisodeStart != tt.start || got.EpisodeEnd != tt.end {
				t.Errorf("identity season=%d ep=%d-%d want %d %d-%d", got.Season, got.EpisodeStart, got.EpisodeEnd, tt.season, tt.start, tt.end)
			}
			if got.Quality != tt.quality || got.Source != tt.source || got.VideoCodec != tt.codec {
				t.Errorf("technical quality=%q source=%q codec=%q", got.Quality, got.Source, got.VideoCodec)
			}
			if got.IsAnime != tt.anime {
				t.Errorf("anime=%v want %v", got.IsAnime, tt.anime)
			}
		})
	}
}

func TestParserExtractsEpisodeTitleAndReleaseGroup(t *testing.T) {
	p := ParseSmartFilename("Example.Show.S02E05.The.Final.Battle.1080p.WEB-DL.x265-GROUP.mkv")
	if p.EpisodeTitle != "The Final Battle" {
		t.Fatalf("episode title=%q; parsed=%+v", p.EpisodeTitle, p)
	}
	if p.ReleaseGroup != "GROUP" {
		t.Fatalf("release group=%q; parsed=%+v", p.ReleaseGroup, p)
	}
}

func TestParserDoesNotInventYearOrEpisode(t *testing.T) {
	p := ParseSmartFilename("2001 A Space Odyssey.mkv")
	if p.Year != "" {
		t.Fatalf("title-leading year must not be inferred as release year: %+v", p)
	}
	p = ParseSmartFilename("1917.2019.1080p.WEB-DL.mkv")
	if p.Title != "1917" || p.Year != "2019" || p.MediaType != SmartMediaMovie {
		t.Fatalf("numeric movie title/year parsed incorrectly: %+v", p)
	}
	p = ParseSmartFilename("The.2012.Movie.1080p.WEB-DL.mkv")
	if p.Title != "The 2012 Movie" || p.Year != "" {
		t.Fatalf("year-like number inside title was treated as a release year: %+v", p)
	}
	p = ParseSmartFilename("Collection.Volume.12345.mkv")
	if p.MediaType != SmartMediaUnknown {
		t.Fatalf("ambiguous numeric filename should remain unknown, got %+v", p)
	}
}

func TestSplitSuffixPreserved(t *testing.T) {
	for _, tc := range []struct {
		input, suffix, ext string
		after              bool
	}{
		{"Sample.part01.mkv", ".part01", ".mkv", false},
		{"Sample.mkv.001", ".001", ".mkv", true},
		{"Sample.mkv.part01", ".part01", ".mkv", true},
	} {
		p := ParseSmartFilename(tc.input)
		if p.SplitSuffix != tc.suffix || p.Extension != tc.ext || p.SplitSuffixAfterExtension != tc.after {
			t.Errorf("%s => suffix=%q ext=%q after=%v", tc.input, p.SplitSuffix, p.Extension, p.SplitSuffixAfterExtension)
		}
	}
}

func TestAnimeFilenameUsesThreeDigitEpisodeNumber(t *testing.T) {
	p := SmartNameParts{MediaType: SmartMediaEpisode, Title: "Example Anime", IsAnime: true, EpisodeStart: 12, Quality: "1080p", Extension: ".mkv"}
	got := BuildSmartFilename(p, "", "")
	if got != "Example Anime - 012 [1080p].mkv" {
		t.Fatalf("anime filename=%q", got)
	}
}

func TestSmartFilenameBuilderAndFitter(t *testing.T) {
	p := SmartNameParts{MediaType: SmartMediaMovie, Title: "Example Movie", Year: "2024", Quality: "1080p", Source: "WEB-DL", Codec: "HEVC", Extension: ".mkv"}
	got := BuildSmartFilename(p, "", "")
	want := "Example Movie (2024) [1080p] [WEB-DL] [HEVC].mkv"
	if got != want {
		t.Fatalf("BuildSmartFilename()=%q want %q", got, want)
	}
	p = SmartNameParts{MediaType: SmartMediaEpisode, Title: strings.Repeat("映", 80) + " Series", Season: 2, EpisodeStart: 5, EpisodeTitle: "An Episode With a Very Long Title", Quality: "1080p", Source: "WEB-DL", Codec: "HEVC", Extension: ".mkv"}
	short, err := FitSmartFilename(p, "PREFIX-", "-SUFFIX", 60)
	if err != nil {
		t.Fatal(err)
	}
	if n := smartRuneLen(short); n > 60 {
		t.Fatalf("fitted filename length=%d > 60: %q", n, short)
	}
	if !strings.HasSuffix(short, ".mkv") || !strings.Contains(short, "S02E05") {
		t.Fatalf("extension/episode identity lost: %q", short)
	}
}

func TestSmartCaptionOriginalAndUnknownFields(t *testing.T) {
	res := SmartRenameResult{OriginalFilename: "Release.Name.2024.mkv", ProposedFilename: "Canonical Name (2024).mkv", PhysicalFilename: "Canonical Name (2024).mkv", CanonicalTitle: "Canonical Name", Year: "2024", MediaType: SmartMediaMovie}
	caption := BuildSmartCaption(res, "{filename}\nOG: {orig_filename}\n{show_name} {year} {missing}", "")
	want := "Canonical Name (2024).mkv\nOG: Release.Name.2024.mkv\nCanonical Name 2024 "
	if caption != want {
		t.Fatalf("caption=%q want %q", caption, want)
	}
}

func TestSmartCaptionEscapesUntrustedFilenameValues(t *testing.T) {
	res := SmartRenameResult{OriginalFilename: "<script>&.mkv", PhysicalFilename: "<movie>.mkv", ProposedFilename: "<movie>.mkv", CanonicalTitle: "<movie>"}
	got := BuildSmartCaption(res, "{filename}|{orig_filename}|{show_name}", "")
	want := "&lt;movie&gt;.mkv|&lt;script&gt;&amp;.mkv|&lt;movie&gt;"
	if got != want {
		t.Fatalf("caption not escaped: %q want %q", got, want)
	}
	defaultCaption := BuildSmartCaption(res, "", "")
	if strings.Contains(defaultCaption, "<script>") || !strings.Contains(defaultCaption, "&lt;script&gt;") {
		t.Fatalf("default caption not escaped: %q", defaultCaption)
	}
}

func TestProbeSmartMediaParsesStreams(t *testing.T) {
	oldLookup, oldRun := lookupSmartFFprobe, runSmartFFprobe
	t.Cleanup(func() { lookupSmartFFprobe, runSmartFFprobe = oldLookup, oldRun })
	lookupSmartFFprobe = func(string) (string, error) { return "/mock/ffprobe", nil }
	runSmartFFprobe = func(ctx context.Context, binary, path string) ([]byte, error) {
		return []byte(`{"streams":[{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"pix_fmt":"yuv420p10le","color_transfer":"smpte2084"},{"codec_type":"audio","codec_name":"aac","channels":2,"channel_layout":"stereo","tags":{"language":"eng"},"disposition":{"default":1}},{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"spa"},"disposition":{"forced":1}}],"format":{"duration":"123.5","format_name":"matroska"}}`), nil
	}
	got, err := ProbeSmartMedia(context.Background(), "mock.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if got.Quality != "1080p" || got.VideoCodec != "hevc" || got.BitDepth != "10-bit" || got.HDR != "HDR10" {
		t.Fatalf("video metadata incomplete: %+v", got)
	}
	if len(got.AudioTracks) != 1 || got.AudioTracks[0].Language != "eng" || !got.AudioTracks[0].Default {
		t.Fatalf("audio tracks: %+v", got.AudioTracks)
	}
	if len(got.SubtitleTracks) != 1 || !got.SubtitleTracks[0].Forced {
		t.Fatalf("subtitle tracks: %+v", got.SubtitleTracks)
	}
	if got.Duration != 123.5 {
		t.Fatalf("duration=%v", got.Duration)
	}
}

func TestTMDbProviderAdapterUsesMockHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/movie" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("api_key") != "test-token" {
			http.Error(w, "missing API key", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"id": 603, "title": "The Matrix", "release_date": "1999-03-30"}}})
	}))
	defer server.Close()
	r := &smartMetadataResolver{cache: map[string]smartMetadataCacheEntry{}, client: server.Client(), tmdbBase: server.URL, kitsuBase: server.URL, cinemetaBase: server.URL}
	got, err := r.searchTMDb(context.Background(), "The Matrix", "1999", SmartMediaMovie, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "The Matrix" || got.ProviderID != "603" || got.Year != "1999" {
		t.Fatalf("bad provider result: %+v", got)
	}
	if got.Confidence < 0.9 {
		t.Fatalf("expected strong match, got %.3f", got.Confidence)
	}
}

func TestResolverRejectsMovieYearMismatch(t *testing.T) {
	score := smartCandidateScore("Example Movie", "2024", SmartMediaMovie, SmartCanonicalMetadata{Title: "Example Movie", Year: "1999"})
	if score >= 0.78 {
		t.Fatalf("contradictory movie year must not be accepted, score %.3f", score)
	}
}

func TestCandidateScoreRejectsMediaTypeMismatch(t *testing.T) {
	score := smartCandidateScore("Example Movie", "2024", SmartMediaMovie, SmartCanonicalMetadata{Title: "Example Movie", Year: "2024", CanonicalType: "series"})
	if score >= 0.78 {
		t.Fatalf("movie-to-series mismatch must not be accepted, score %.3f", score)
	}
}

func TestSmartAutoRenameCollisionDoesNotOverwrite(t *testing.T) {
	disableSmartFFprobe(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "Example.Movie.2024.1080p.WEB-DL.x265.mkv")
	dest := filepath.Join(dir, "Example Movie (2024) [1080p] [WEB-DL] [HEVC].mkv")
	if err := os.WriteFile(source, []byte("source-data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("existing-data"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := SmartAutoRename(context.Background(), source, SmartRenameOptions{LookupEnabled: false, MaxFilenameChars: 100, ProbeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.RenameStatus != "rename_failed" {
		t.Fatalf("status=%q want rename_failed; result=%+v", result.RenameStatus, result)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source file missing after collision: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing-data" {
		t.Fatalf("existing destination was overwritten: %q", data)
	}
}

func TestSmartAutoRenamePreservesOriginalMetadata(t *testing.T) {
	disableSmartFFprobe(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "Example.Movie.2024.1080p.WEB-DL.x265.mkv")
	if err := os.WriteFile(source, []byte("fake media"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := SmartAutoRename(context.Background(), source, SmartRenameOptions{OriginalFilename: "meta_Example.Movie.2024.1080p.WEB-DL.x265.mkv", LookupEnabled: false, MaxFilenameChars: 100, ProbeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.OriginalFilename != "meta_Example.Movie.2024.1080p.WEB-DL.x265.mkv" {
		t.Fatalf("original name not preserved: %q", result.OriginalFilename)
	}
	if result.PhysicalFilename != "Example Movie (2024) [1080p] [WEB-DL] [HEVC].mkv" {
		t.Fatalf("unexpected normalized filename %q; result=%+v", result.PhysicalFilename, result)
	}
	if result.RenameStatus != "renamed" {
		t.Fatalf("rename status=%q want renamed", result.RenameStatus)
	}
	if _, err := os.Stat(result.PhysicalPath); err != nil {
		t.Fatalf("reported physical path does not exist: %v", err)
	}
}

func TestKitsuAndCinemetaAdaptersUseMockHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/anime"):
			if r.URL.Query().Get("filter[text]") != "Example Anime" {
				http.Error(w, "missing Kitsu title query", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"123","attributes":{"canonicalTitle":"Example Anime","startDate":"2020-01-01","titles":{"en":"Example Anime"}}}]}`))
		case strings.Contains(r.URL.Path, "/catalog/movie/"):
			_, _ = w.Write([]byte(`{"metas":[{"id":"tt123","type":"movie","name":"Example Movie","year":"2024"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	r := &smartMetadataResolver{cache: map[string]smartMetadataCacheEntry{}, client: server.Client(), tmdbBase: server.URL, kitsuBase: server.URL, cinemetaBase: server.URL}

	anime, err := r.searchKitsu(context.Background(), "Example Anime")
	if err != nil {
		t.Fatal(err)
	}
	if anime == nil || anime.Provider != "kitsu" || anime.ProviderID != "123" || anime.Title != "Example Anime" {
		t.Fatalf("unexpected Kitsu response: %+v", anime)
	}
	movie, err := r.searchCinemeta(context.Background(), "Example Movie", SmartMediaMovie)
	if err != nil {
		t.Fatal(err)
	}
	if movie == nil || movie.Provider != "cinemeta" || movie.ProviderID != "tt123" || movie.Year != "2024" {
		t.Fatalf("unexpected Cinemeta response: %+v", movie)
	}
}

func TestProviderPriorityControlsSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/search/movie":
			_, _ = w.Write([]byte(`{"results":[{"id":603,"title":"Example Movie","release_date":"2024-01-01"}]}`))
		case strings.Contains(r.URL.Path, "/catalog/movie/"):
			_, _ = w.Write([]byte(`{"metas":[{"id":"cinemeta-1","type":"movie","name":"Example Movie","year":"2024"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	r := &smartMetadataResolver{cache: map[string]smartMetadataCacheEntry{}, client: server.Client(), tmdbBase: server.URL, kitsuBase: server.URL, cinemetaBase: server.URL}
	p := ParseSmartFilename("Example.Movie.2024.1080p.mkv")
	got, err := r.resolve(context.Background(), p, "test-token", []string{"cinemeta", "tmdb"})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Provider != "cinemeta" {
		t.Fatalf("provider priority not honored: %+v", got)
	}
}

func TestConcurrentResolverRequestsAreCoalesced(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(40 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metas":[{"id":"cinemeta-1","type":"movie","name":"Example Movie","year":"2024"}]}`))
	}))
	defer server.Close()
	r := &smartMetadataResolver{cache: map[string]smartMetadataCacheEntry{}, client: server.Client(), tmdbBase: server.URL, kitsuBase: server.URL, cinemetaBase: server.URL}
	p := ParseSmartFilename("Example.Movie.2024.1080p.WEB-DL.mkv")
	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			got, err := r.resolve(context.Background(), p, "", []string{"cinemeta"})
			if err != nil {
				errs <- err
				return
			}
			if got == nil || got.Provider != "cinemeta" {
				errs <- errors.New("resolver returned no canonical result")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one coalesced HTTP request, got %d", hits.Load())
	}
}

func TestSafeErrorTextRedactsProviderCredentials(t *testing.T) {
	got := safeErrorText(errors.New("request failed https://api.example/search?api_key=supersecret Bearer abc.def-123"))
	if strings.Contains(got, "supersecret") || strings.Contains(got, "abc.def-123") {
		t.Fatalf("sensitive value leaked in log text: %s", got)
	}
}
