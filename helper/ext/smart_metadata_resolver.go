package ext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	tmdbAPIBase     = "https://api.themoviedb.org/3"
	kitsuAPIBase    = "https://kitsu.io/api/edge"
	cinemetaAPIBase = "https://v3-cinemeta.strem.io"
)

type smartMetadataCacheEntry struct {
	value   *SmartCanonicalMetadata
	err     string
	expires time.Time
}
type smartMetadataCall struct {
	done  chan struct{}
	value *SmartCanonicalMetadata
	err   error
}

type smartMetadataResolver struct {
	mu           sync.RWMutex
	cache        map[string]smartMetadataCacheEntry
	inflight     map[string]*smartMetadataCall
	client       *http.Client
	tmdbBase     string
	kitsuBase    string
	cinemetaBase string
}

// smartMetadataProvider keeps each external catalog behind a small, testable adapter.
// Provider-specific HTTP and response models stay out of the orchestration loop.
type smartMetadataProvider interface {
	Name() string
	Search(context.Context, SmartParsedFilename, string) (*SmartCanonicalMetadata, error)
}

type tmdbSmartMetadataProvider struct{ resolver *smartMetadataResolver }

func (p tmdbSmartMetadataProvider) Name() string { return "tmdb" }
func (p tmdbSmartMetadataProvider) Search(ctx context.Context, parsed SmartParsedFilename, token string) (*SmartCanonicalMetadata, error) {
	candidate, err := p.resolver.searchTMDb(ctx, parsed.Title, parsed.Year, parsed.MediaType, token)
	if err != nil || candidate == nil {
		return candidate, err
	}
	if (parsed.MediaType == SmartMediaEpisode || parsed.MediaType == SmartMediaEpisodeRange || parsed.MediaType == SmartMediaSeasonPack) && parsed.Season > 0 && parsed.EpisodeStart > 0 {
		if episodeTitle, episodeErr := p.resolver.tmdbEpisodeTitle(ctx, candidate.ProviderID, parsed.Season, parsed.EpisodeStart, token); episodeErr == nil && episodeTitle != "" {
			candidate.EpisodeTitle = episodeTitle
		}
	}
	return candidate, nil
}

type kitsuSmartMetadataProvider struct{ resolver *smartMetadataResolver }

func (p kitsuSmartMetadataProvider) Name() string { return "kitsu" }
func (p kitsuSmartMetadataProvider) Search(ctx context.Context, parsed SmartParsedFilename, _ string) (*SmartCanonicalMetadata, error) {
	if !parsed.IsAnime {
		return nil, nil
	}
	return p.resolver.searchKitsu(ctx, parsed.Title)
}

type cinemetaSmartMetadataProvider struct{ resolver *smartMetadataResolver }

func (p cinemetaSmartMetadataProvider) Name() string { return "cinemeta" }
func (p cinemetaSmartMetadataProvider) Search(ctx context.Context, parsed SmartParsedFilename, _ string) (*SmartCanonicalMetadata, error) {
	return p.resolver.searchCinemeta(ctx, parsed.Title, parsed.MediaType)
}

var defaultSmartResolver = &smartMetadataResolver{
	cache:    make(map[string]smartMetadataCacheEntry),
	client:   &http.Client{Timeout: 8 * time.Second},
	tmdbBase: tmdbAPIBase, kitsuBase: kitsuAPIBase, cinemetaBase: cinemetaAPIBase,
}

// ResolveSmartMetadata performs optional canonical lookup. A nil result means no sufficiently
// confident provider match was found; callers should retain filename-derived metadata.
func ResolveSmartMetadata(ctx context.Context, parsed SmartParsedFilename, tmdbToken string, providerPriority ...string) (*SmartCanonicalMetadata, error) {
	if strings.TrimSpace(parsed.Title) == "" || parsed.MediaType == SmartMediaUnknown {
		return nil, nil
	}
	priority := normalizeSmartProviderPriority(providerPriority)
	return defaultSmartResolver.resolve(ctx, parsed, tmdbToken, priority)
}

func normalizeSmartProviderPriority(input []string) []string {
	var values []string
	if len(input) == 1 {
		input = strings.Split(input[0], ",")
	}
	seen := map[string]bool{}
	for _, raw := range input {
		name := strings.ToLower(strings.TrimSpace(raw))
		if (name == "tmdb" || name == "kitsu" || name == "cinemeta") && !seen[name] {
			values = append(values, name)
			seen[name] = true
		}
	}
	if len(values) == 0 {
		return []string{"kitsu", "tmdb", "cinemeta"}
	}
	return values
}

func (r *smartMetadataResolver) resolve(ctx context.Context, p SmartParsedFilename, token string, priority []string) (*SmartCanonicalMetadata, error) {
	key := strings.Join([]string{strings.ToLower(strings.TrimSpace(p.Title)), p.Year, string(p.MediaType), strconv.FormatBool(p.IsAnime), strings.TrimSpace(token), strings.Join(priority, ",")}, "|")
	if value, cacheErr, found := r.readCache(key); found {
		return value, cacheErr
	}

	r.mu.Lock()
	if r.inflight == nil {
		r.inflight = make(map[string]*smartMetadataCall)
	}
	if pending, ok := r.inflight[key]; ok {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending.done:
			return pending.value, pending.err
		}
	}
	pending := &smartMetadataCall{done: make(chan struct{})}
	r.inflight[key] = pending
	r.mu.Unlock()

	value, lookupErr := r.resolveProviders(ctx, p, token, priority)
	if ctx.Err() != nil {
		lookupErr = ctx.Err()
	}
	if ctx.Err() == nil {
		ttl := 3 * time.Minute
		if value != nil {
			ttl = 45 * time.Minute
		}
		r.mu.Lock()
		if r.cache == nil {
			r.cache = make(map[string]smartMetadataCacheEntry)
		}
		if len(r.cache) > 500 {
			r.cache = make(map[string]smartMetadataCacheEntry)
		}
		stored := smartMetadataCacheEntry{value: value, expires: time.Now().Add(ttl)}
		if lookupErr != nil {
			stored.err = lookupErr.Error()
		}
		r.cache[key] = stored
		r.mu.Unlock()
	}
	r.mu.Lock()
	pending.value, pending.err = value, lookupErr
	delete(r.inflight, key)
	close(pending.done)
	r.mu.Unlock()
	return value, lookupErr
}

func (r *smartMetadataResolver) readCache(key string) (*SmartCanonicalMetadata, error, bool) {
	r.mu.RLock()
	item, ok := r.cache[key]
	r.mu.RUnlock()
	if !ok {
		return nil, nil, false
	}
	if time.Now().After(item.expires) {
		r.mu.Lock()
		if current, exists := r.cache[key]; exists && current.expires.Equal(item.expires) {
			delete(r.cache, key)
		}
		r.mu.Unlock()
		return nil, nil, false
	}
	if item.err != "" {
		return item.value, errors.New(item.err), true
	}
	return item.value, nil, true
}

func (r *smartMetadataResolver) resolveProviders(ctx context.Context, p SmartParsedFilename, token string, priority []string) (*SmartCanonicalMetadata, error) {
	providers := map[string]smartMetadataProvider{
		"kitsu":    kitsuSmartMetadataProvider{resolver: r},
		"tmdb":     tmdbSmartMetadataProvider{resolver: r},
		"cinemeta": cinemetaSmartMetadataProvider{resolver: r},
	}
	var candidates []SmartCanonicalMetadata
	var errs []string
	for _, name := range priority {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		provider, exists := providers[name]
		if !exists {
			continue
		}
		if provider.Name() == "tmdb" && strings.TrimSpace(token) == "" {
			continue
		}
		candidate, err := provider.Search(ctx, p, token)
		if err != nil {
			errs = append(errs, provider.Name()+": "+safeErrorText(err))
		}
		if candidate != nil {
			candidates = append(candidates, *candidate)
		}
	}
	if len(candidates) == 0 {
		if len(errs) > 0 {
			return nil, errors.New(strings.Join(errs, "; "))
		}
		return nil, nil
	}
	for i := range candidates {
		candidates[i].Confidence = smartCandidateScore(p.Title, p.Year, p.MediaType, candidates[i])
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Confidence > candidates[j].Confidence })
	best := candidates[0]
	if best.Confidence < 0.78 {
		return nil, nil
	}
	if len(candidates) > 1 && candidates[1].Confidence >= 0.78 && mathAbs(candidates[0].Confidence-candidates[1].Confidence) < 0.025 && !strings.EqualFold(candidates[0].Title, candidates[1].Title) {
		return nil, errors.Join(ErrSmartAmbiguousMatch, errors.New("multiple similarly scored canonical candidates"))
	}
	return &best, nil
}

type tmdbSearchResponse struct {
	Results []struct {
		ID            int64  `json:"id"`
		MediaType     string `json:"media_type"`
		Title         string `json:"title"`
		Name          string `json:"name"`
		OriginalTitle string `json:"original_title"`
		OriginalName  string `json:"original_name"`
		ReleaseDate   string `json:"release_date"`
		FirstAirDate  string `json:"first_air_date"`
	} `json:"results"`
}

type kitsuResponse struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			CanonicalTitle string            `json:"canonicalTitle"`
			Titles         map[string]string `json:"titles"`
			StartDate      string            `json:"startDate"`
		} `json:"attributes"`
	} `json:"data"`
}

type cinemetaResponse struct {
	Metas []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Name string `json:"name"`
		Year string `json:"year"`
	} `json:"metas"`
}

func (r *smartMetadataResolver) searchTMDb(ctx context.Context, title, year string, mediaType SmartMediaType, token string) (*SmartCanonicalMetadata, error) {
	endpoint := "/search/movie"
	if mediaType == SmartMediaEpisode || mediaType == SmartMediaEpisodeRange || mediaType == SmartMediaSeasonPack {
		endpoint = "/search/tv"
	}
	if mediaType == SmartMediaUnknown {
		endpoint = "/search/multi"
	}
	q := url.Values{"query": {title}, "include_adult": {"false"}, "language": {"en-US"}}
	if year != "" {
		if endpoint == "/search/movie" {
			q.Set("year", year)
		} else if endpoint == "/search/tv" {
			q.Set("first_air_date_year", year)
		}
	}
	var out tmdbSearchResponse
	if err := r.getJSON(ctx, r.tmdbBase+endpoint+"?"+q.Encode(), token, &out); err != nil {
		return nil, err
	}
	var best *SmartCanonicalMetadata
	for _, item := range out.Results {
		if strings.EqualFold(item.MediaType, "person") {
			continue
		}
		titleValue := firstNonEmpty(item.Title, item.Name, item.OriginalTitle, item.OriginalName)
		if titleValue == "" {
			continue
		}
		y := firstYear(item.ReleaseDate, item.FirstAirDate)
		candidate := SmartCanonicalMetadata{Title: titleValue, Year: y, Provider: "tmdb", ProviderID: strconv.FormatInt(item.ID, 10), CanonicalType: "movie"}
		if endpoint == "/search/tv" || item.MediaType == "tv" {
			candidate.CanonicalType = "series"
			candidate.SeriesTitle = titleValue
		}
		candidate.Confidence = smartCandidateScore(title, year, mediaType, candidate)
		if best == nil || candidate.Confidence > best.Confidence {
			copy := candidate
			best = &copy
		}
	}
	return best, nil
}

func (r *smartMetadataResolver) tmdbEpisodeTitle(ctx context.Context, showID string, season, episode int, token string) (string, error) {
	if showID == "" || season <= 0 || episode <= 0 {
		return "", nil
	}
	var out struct {
		Name string `json:"name"`
	}
	path := fmt.Sprintf("%s/tv/%s/season/%d/episode/%d", r.tmdbBase, url.PathEscape(showID), season, episode)
	if err := r.getJSON(ctx, path, token, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Name), nil
}

func (r *smartMetadataResolver) searchKitsu(ctx context.Context, title string) (*SmartCanonicalMetadata, error) {
	q := url.Values{"filter[text]": {title}, "page[limit]": {"10"}}
	var out kitsuResponse
	if err := r.getJSON(ctx, r.kitsuBase+"/anime?"+q.Encode(), "", &out); err != nil {
		return nil, err
	}
	var best *SmartCanonicalMetadata
	for _, item := range out.Data {
		t := firstNonEmpty(item.Attributes.CanonicalTitle, item.Attributes.Titles["en"], item.Attributes.Titles["en_jp"], item.Attributes.Titles["ja_jp"])
		if t == "" {
			continue
		}
		candidate := SmartCanonicalMetadata{Title: t, Year: firstYear(item.Attributes.StartDate), Provider: "kitsu", ProviderID: item.ID, CanonicalType: "anime"}
		candidate.Confidence = smartCandidateScore(title, "", SmartMediaEpisode, candidate)
		if best == nil || candidate.Confidence > best.Confidence {
			copy := candidate
			best = &copy
		}
	}
	return best, nil
}

func (r *smartMetadataResolver) searchCinemeta(ctx context.Context, title string, mediaType SmartMediaType) (*SmartCanonicalMetadata, error) {
	catalog := "movie"
	if mediaType == SmartMediaEpisode || mediaType == SmartMediaEpisodeRange || mediaType == SmartMediaSeasonPack {
		catalog = "series"
	}
	path := r.cinemetaBase + "/catalog/" + catalog + "/top/search=" + url.PathEscape(title) + ".json"
	var out cinemetaResponse
	if err := r.getJSON(ctx, path, "", &out); err != nil {
		return nil, err
	}
	var best *SmartCanonicalMetadata
	for _, item := range out.Metas {
		if item.Name == "" {
			continue
		}
		candidate := SmartCanonicalMetadata{Title: item.Name, Year: item.Year, Provider: "cinemeta", ProviderID: item.ID, CanonicalType: item.Type}
		if catalog == "series" {
			candidate.SeriesTitle = item.Name
		}
		candidate.Confidence = smartCandidateScore(title, item.Year, mediaType, candidate)
		if best == nil || candidate.Confidence > best.Confidence {
			copy := candidate
			best = &copy
		}
	}
	return best, nil
}

func (r *smartMetadataResolver) getJSON(ctx context.Context, endpoint, token string, out any) error {
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "AZML-Go-SmartAutoRename/1.0")
		if token != "" {
			if len(token) > 50 {
				req.Header.Set("Authorization", "Bearer "+token)
			} else {
				parsed, parseErr := url.Parse(endpoint)
				if parseErr == nil {
					q := parsed.Query()
					q.Set("api_key", token)
					parsed.RawQuery = q.Encode()
					req.URL = parsed
				}
			}
		}
		resp, err := r.client.Do(req)
		if err != nil {
			last = err
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		bodyErr := func() error {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
				return fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
			}
			return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
		}()
		if bodyErr == nil {
			return nil
		}
		last = bodyErr
		// Retry only transient throttling/server responses, once, and remain cancellation-aware.
		if attempt == 0 && (strings.Contains(bodyErr.Error(), "HTTP 429") || strings.Contains(bodyErr.Error(), "HTTP 5")) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		return bodyErr
	}
	if last == nil {
		last = errors.New("provider request failed")
	}
	return last
}

func smartCandidateScore(query, year string, mediaType SmartMediaType, candidate SmartCanonicalMetadata) float64 {
	similarity := smartTitleSimilarity(query, candidate.Title)
	if candidate.SeriesTitle != "" {
		seriesSimilarity := smartTitleSimilarity(query, candidate.SeriesTitle)
		if seriesSimilarity > similarity {
			similarity = seriesSimilarity
		}
	}
	if similarity == 0 {
		return 0
	}
	score := similarity
	canonicalKind := strings.ToLower(strings.TrimSpace(candidate.CanonicalType))
	if canonicalKind != "" {
		isMovieInput := mediaType == SmartMediaMovie
		isMovieCandidate := canonicalKind == "movie" || canonicalKind == "film"
		isSeriesCandidate := canonicalKind == "series" || canonicalKind == "tv" || canonicalKind == "anime"
		if isMovieInput && !isMovieCandidate {
			score -= 0.5
		} else if !isMovieInput && isMovieCandidate && (mediaType == SmartMediaEpisode || mediaType == SmartMediaEpisodeRange || mediaType == SmartMediaSeasonPack) {
			score -= 0.5
		} else if !isMovieInput && !isSeriesCandidate && !isMovieCandidate {
			score -= 0.3
		}
	}
	if year != "" && candidate.Year != "" {
		if year == candidate.Year {
			score += 0.045
		} else if mediaType == SmartMediaMovie {
			score -= 0.28
		} else {
			score -= 0.08
		}
	}
	if candidate.Title == "" {
		score -= 0.25
	}
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}
	return score
}

func smartTitleSimilarity(a, b string) float64 {
	a, b = normalizeSmartTitleCompare(a), normalizeSmartTitleCompare(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	lev := smartLevenshtein([]rune(a), []rune(b))
	maxLen := len([]rune(a))
	if n := len([]rune(b)); n > maxLen {
		maxLen = n
	}
	edited := 1 - float64(lev)/float64(maxLen)
	wordsA, wordsB := map[string]bool{}, map[string]bool{}
	for _, w := range strings.Fields(a) {
		wordsA[w] = true
	}
	for _, w := range strings.Fields(b) {
		wordsB[w] = true
	}
	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}
	union := len(wordsA) + len(wordsB) - intersection
	jaccard := 0.0
	if union > 0 {
		jaccard = float64(intersection) / float64(union)
	}
	return 0.6*edited + 0.4*jaccard
}

func normalizeSmartTitleCompare(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
		} else if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func smartLevenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = minIntSmart(minIntSmart(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
func minIntSmart(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func firstYear(values ...string) string {
	for _, v := range values {
		if len(v) >= 4 {
			if _, err := strconv.Atoi(v[:4]); err == nil {
				n, _ := strconv.Atoi(v[:4])
				if n >= 1900 && n <= 2100 {
					return v[:4]
				}
			}
		}
	}
	return ""
}
