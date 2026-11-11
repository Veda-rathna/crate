package language

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
	"golang.org/x/time/rate"
)

const (
	defaultITunesBaseURL = "https://itunes.apple.com"
	positiveCacheTTL     = 30 * 24 * time.Hour
	negativeCacheTTL     = 24 * time.Hour
)

// CacheStore is a minimal key-value cache interface satisfied by *cache.Cache.
type CacheStore interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte, ttl time.Duration)
}

// SettingsReader reads persisted application settings, satisfied by *db.Queries.
type SettingsReader interface {
	AllSettings() (map[string]string, error)
}

// ResolverConfig overrides external endpoints or rate limits (primarily for testing).
type ResolverConfig struct {
	HTTPClient    *http.Client
	ITunesBaseURL string
	ITunesLimiter *rate.Limiter
	LLMLimiter    *rate.Limiter
}

// Resolver enriches album language detection using local metadata, Apple Music/iTunes India lookup,
// and an optional OpenAI-compatible HTTP LLM tie-breaker. Results are cached in cache.db.
type Resolver struct {
	cache         CacheStore
	settings      SettingsReader
	httpClient    *http.Client
	itunesBaseURL string
	itunesLimiter *rate.Limiter
	llmLimiter    *rate.Limiter
}

// NewResolver creates a lightweight language Resolver using cache and settings stores.
func NewResolver(cache CacheStore, settings SettingsReader, cfg ...ResolverConfig) *Resolver {
	r := &Resolver{
		cache:         cache,
		settings:      settings,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		itunesBaseURL: defaultITunesBaseURL,
		itunesLimiter: rate.NewLimiter(rate.Every(3*time.Second), 2),
		llmLimiter:    rate.NewLimiter(rate.Every(1*time.Second), 3),
	}
	if len(cfg) > 0 {
		c := cfg[0]
		if c.HTTPClient != nil {
			r.httpClient = c.HTTPClient
		}
		if c.ITunesBaseURL != "" {
			r.itunesBaseURL = strings.TrimRight(c.ITunesBaseURL, "/")
		}
		if c.ITunesLimiter != nil {
			r.itunesLimiter = c.ITunesLimiter
		}
		if c.LLMLimiter != nil {
			r.llmLimiter = c.LLMLimiter
		}
	}
	return r
}

type resolverOptions struct {
	allowUnknown bool
	llmEnabled   bool
	llmURL       string
	llmAPIKey    string
	llmModel     string
}

func (r *Resolver) readOptions() resolverOptions {
	if r == nil || r.settings == nil {
		return resolverOptions{}
	}
	all, err := r.settings.AllSettings()
	if err != nil {
		return resolverOptions{}
	}
	return resolverOptions{
		allowUnknown: strings.EqualFold(strings.TrimSpace(all["lang_allow_unknown"]), "true"),
		llmEnabled:   strings.EqualFold(strings.TrimSpace(all["lang_llm_enabled"]), "true"),
		llmURL:       strings.TrimRight(strings.TrimSpace(all["lang_llm_url"]), "/"),
		llmAPIKey:    strings.TrimSpace(all["lang_llm_api_key"]),
		llmModel:     strings.TrimSpace(all["lang_llm_model"]),
	}
}

// AllowUnknown reports whether unknown-language albums are allowed when filtering artist releases.
func (r *Resolver) AllowUnknown() bool {
	return r.readOptions().allowUnknown
}

// MatchesSummary checks an album summary quickly before fetching full album details.
// Summaries with Unknown language return true so detailed/iTunes/LLM resolution can run on AlbumDetail.
func (r *Resolver) MatchesSummary(artistLangs []string, title string, metadata map[string]string) (allowed bool, detected string) {
	if IsAllLanguages(artistLangs) {
		return true, All
	}
	detected = DetectReleaseSummaryLanguage(title, metadata)
	if detected == Unknown {
		return true, Unknown
	}
	// If summary language came purely from ambiguous MusicBrainz eng+Latn, defer decision until AlbumDetail.
	if metadata != nil && Normalize(detected) == English &&
		strings.EqualFold(metadata["text_script"], "Latn") &&
		detectAuthoritativeMetadata(metadata) == Unknown {
		return true, Unknown
	}
	return MatchesArtistLanguagesWithPolicy(artistLangs, detected, r.AllowUnknown()), detected
}

// CheckAlbum evaluates an AlbumDetail against an artist's language filters using local detection,
// iTunes India lookup, and optional LLM tie-breaker.
func (r *Resolver) CheckAlbum(ctx context.Context, artistName string, artistLangs []string, album *pb.AlbumDetail) (allowed bool, detected string) {
	if IsAllLanguages(artistLangs) {
		return true, DetectAlbumDetailLanguage(album)
	}
	opts := r.readOptions()
	detected = r.resolveWithOptions(ctx, opts, artistName, artistLangs, album)
	return MatchesArtistLanguagesWithPolicy(artistLangs, detected, opts.allowUnknown), detected
}

// ResolveAlbumLanguage returns the best detected language for an album.
func (r *Resolver) ResolveAlbumLanguage(ctx context.Context, artistName string, artistLangs []string, album *pb.AlbumDetail) string {
	return r.resolveWithOptions(ctx, r.readOptions(), artistName, artistLangs, album)
}

func (r *Resolver) resolveWithOptions(ctx context.Context, opts resolverOptions, artistName string, artistLangs []string, album *pb.AlbumDetail) string {
	if album == nil {
		return Unknown
	}

	localLang := DetectAlbumDetailLanguage(album)
	ambiguousEng := localLang == English && IsAmbiguousEnglishMetadata(album)

	// Fast path: conclusive local detection or artist accepts all languages.
	if IsAllLanguages(artistLangs) {
		return localLang
	}
	if localLang != Unknown && !ambiguousEng {
		return localLang
	}

	effArtist := strings.TrimSpace(artistName)
	if effArtist == "" {
		effArtist = strings.TrimSpace(album.ArtistName)
	}

	// Tier 2: iTunes / Apple Music India genre & catalog check.
	if itunesLang := r.lookupITunes(ctx, effArtist, album); itunesLang != Unknown {
		return itunesLang
	}

	// Tier 3: Optional lightweight HTTP OpenAI-compatible LLM tie-breaker.
	if opts.llmEnabled && opts.llmURL != "" && opts.llmModel != "" {
		if llmLang := r.classifyWithLLM(ctx, opts, effArtist, album); llmLang != Unknown {
			return llmLang
		}
	}

	// If MusicBrainz text_language was eng+Latn and the artist actually wants English, keep English;
	// for Indic-filtered artists, avoid letting unverified Latn transliteration pretend to be English.
	if ambiguousEng {
		for _, l := range artistLangs {
			if Normalize(l) == English {
				return English
			}
		}
		return Unknown
	}
	return localLang
}

type cachedLangVerdict struct {
	Language string `json:"language"`
}

func cacheHashKey(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(strings.ToLower(strings.TrimSpace(p))))
		h.Write([]byte{0})
	}
	return prefix + hex.EncodeToString(h.Sum(nil))
}

func (r *Resolver) getCachedVerdict(key string) (string, bool) {
	if r == nil || r.cache == nil {
		return "", false
	}
	raw, ok := r.cache.Get(key)
	if !ok {
		return "", false
	}
	var v cachedLangVerdict
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	return v.Language, true
}

func (r *Resolver) setCachedVerdict(key, lang string, ttl time.Duration) {
	if r == nil || r.cache == nil {
		return
	}
	b, err := json.Marshal(cachedLangVerdict{Language: lang})
	if err != nil {
		return
	}
	r.cache.Set(key, b, ttl)
}

type itunesResponse struct {
	ResultCount int            `json:"resultCount"`
	Results     []itunesResult `json:"results"`
}

type itunesResult struct {
	WrapperType      string `json:"wrapperType"`
	CollectionType   string `json:"collectionType"`
	ArtistName       string `json:"artistName"`
	CollectionName   string `json:"collectionName"`
	PrimaryGenreName string `json:"primaryGenreName"`
}

func (r *Resolver) lookupITunes(ctx context.Context, artistName string, album *pb.AlbumDetail) string {
	upc := cleanUPC(album.Metadata["upc"])
	key := cacheHashKey("langres:v1:itunes:", upc, artistName, album.Title)
	if cached, ok := r.getCachedVerdict(key); ok {
		return cached
	}

	var resolved string
	var hadNetworkSuccess bool

	// 1. Exact UPC lookup when available.
	if upc != "" {
		resp, err := r.fetchITunes(ctx, fmt.Sprintf("%s/lookup?upc=%s&country=in", r.itunesBaseURL, url.QueryEscape(upc)))
		if err == nil {
			hadNetworkSuccess = true
			resolved = extractITunesAlbumLanguage(resp.Results, artistName, album.Title, true)
		}
	}

	// 2. Artist + album search fallback in Apple Music India store.
	if resolved == "" || resolved == Unknown {
		coreTitle := normalizeAlbumCoreTitle(album.Title)
		queryTerm := strings.TrimSpace(artistName + " " + coreTitle)
		if queryTerm != "" {
			searchURL := fmt.Sprintf("%s/search?term=%s&entity=album&country=in&limit=5", r.itunesBaseURL, url.QueryEscape(queryTerm))
			resp, err := r.fetchITunes(ctx, searchURL)
			if err == nil {
				hadNetworkSuccess = true
				if lang := extractITunesAlbumLanguage(resp.Results, artistName, album.Title, false); lang != Unknown {
					resolved = lang
				}
			}
		}
	}

	if resolved == "" {
		resolved = Unknown
	}
	if resolved != Unknown {
		r.setCachedVerdict(key, resolved, positiveCacheTTL)
	} else if hadNetworkSuccess {
		r.setCachedVerdict(key, Unknown, negativeCacheTTL)
	}
	return resolved
}

func (r *Resolver) fetchITunes(ctx context.Context, endpoint string) (*itunesResponse, error) {
	if r.itunesLimiter != nil {
		if err := r.itunesLimiter.Wait(ctx); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Crate/1.0")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("itunes http %d", resp.StatusCode)
	}
	var out itunesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 512*1024)).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

var coreTitleStripRE = regexp.MustCompile(`(?i)\s*[\(\[](?:[^\]\)]*(?:original motion picture soundtrack|motion picture soundtrack|soundtrack|ost|deluxe|edition|single|ep|from\s+"[^"]*")[^\]\)]*)[\)\]]`)
var trailingDashBoilerplateRE = regexp.MustCompile(`(?i)\s*[\-\x{2013}]\s*(?:original motion picture soundtrack|motion picture soundtrack|soundtrack|ost|single|ep)\s*$`)
var nonAlnumSpaceRE = regexp.MustCompile(`[^a-z0-9\p{L}]+`)

func cleanUPC(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeAlbumCoreTitle(title string) string {
	s := coreTitleStripRE.ReplaceAllString(title, " ")
	s = trailingDashBoilerplateRE.ReplaceAllString(s, " ")
	s = nonAlnumSpaceRE.ReplaceAllString(strings.ToLower(s), " ")
	return strings.TrimSpace(s)
}

func normalizeSimple(s string) string {
	return strings.TrimSpace(nonAlnumSpaceRE.ReplaceAllString(strings.ToLower(s), " "))
}

func artistMatches(target, candidate string) bool {
	t := normalizeSimple(target)
	c := normalizeSimple(candidate)
	if t == "" || c == "" {
		return true
	}
	if t == c || strings.Contains(c, t) || strings.Contains(t, c) {
		return true
	}
	// Accept if primary artist word matches (e.g. "Anirudh Ravichander" vs "Anirudh")
	tParts := strings.Fields(t)
	cParts := strings.Fields(c)
	if len(tParts) > 0 && len(cParts) > 0 && len(tParts[0]) >= 4 && tParts[0] == cParts[0] {
		return true
	}
	return false
}

func extractITunesAlbumLanguage(results []itunesResult, artistName, targetTitle string, isExactUPC bool) string {
	targetCore := normalizeAlbumCoreTitle(targetTitle)
	targetTag := DetectStructuredTags(targetTitle)

	for _, item := range results {
		if item.WrapperType != "" && item.WrapperType != "collection" {
			continue
		}
		if !isExactUPC {
			if !artistMatches(artistName, item.ArtistName) {
				continue
			}
			candCore := normalizeAlbumCoreTitle(item.CollectionName)
			if targetCore != "" && candCore != targetCore {
				continue
			}
			// Guard against matching a foreign dub (e.g. "Leo (Telugu)") when target was unlabelled "Leo".
			candTag := DetectStructuredTags(item.CollectionName)
			if candTag != Unknown && targetTag == Unknown {
				continue
			}
		}

		if tagLang := DetectStructuredTags(item.CollectionName); tagLang != Unknown {
			return tagLang
		}
		if genreLang := detectITunesGenre(item.PrimaryGenreName); genreLang != Unknown {
			return genreLang
		}
	}
	return Unknown
}

func detectITunesGenre(genre string) string {
	// Apple Music India assigns clear language genres: "Tamil", "Telugu", "Malayalam", "Hindi", "Kannada", "Bollywood", etc.
	if lang := matchUniquePattern(genre, genreKeywordPatterns); lang != Unknown {
		return lang
	}
	norm := Normalize(genre)
	for _, supported := range SupportedLanguages {
		if norm == supported {
			return supported
		}
	}
	return Unknown
}

func (r *Resolver) classifyWithLLM(ctx context.Context, opts resolverOptions, artistName string, album *pb.AlbumDetail) string {
	key := cacheHashKey("langres:v1:llm:", artistName, album.Title, album.Metadata["upc"], album.Metadata["label"])
	if cached, ok := r.getCachedVerdict(key); ok {
		return cached
	}

	if r.llmLimiter != nil {
		if err := r.llmLimiter.Wait(ctx); err != nil {
			return Unknown
		}
	}

	endpoint := opts.llmURL
	if !strings.HasSuffix(strings.ToLower(endpoint), "/chat/completions") {
		endpoint += "/chat/completions"
	}

	var trackSample []string
	for i, t := range album.Tracks {
		if i >= 6 || t == nil {
			break
		}
		if title := strings.TrimSpace(t.Title); title != "" {
			trackSample = append(trackSample, title)
		}
	}

	var promptParts []string
	promptParts = append(promptParts, fmt.Sprintf("Artist: %s", artistName))
	promptParts = append(promptParts, fmt.Sprintf("Album: %s", album.Title))
	if album.Year > 0 {
		promptParts = append(promptParts, fmt.Sprintf("Year: %d", album.Year))
	}
	for _, k := range []string{"label", "genres", "tags", "copyrights"} {
		if v := strings.TrimSpace(album.Metadata[k]); v != "" {
			promptParts = append(promptParts, fmt.Sprintf("%s: %s", k, v))
		}
	}
	if len(trackSample) > 0 {
		promptParts = append(promptParts, "Tracks: "+strings.Join(trackSample, " | "))
	}

	payload := map[string]any{
		"model":       opts.llmModel,
		"temperature": 0,
		"max_tokens":  16,
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "Classify the primary vocal/release language of the given music release into ONE lowercase word from: " +
					"tamil, telugu, malayalam, hindi, kannada, english, other, unknown. " +
					"Romanized South Indian soundtrack titles or Latin-script transliterations belong to their regional film language (e.g. tamil, telugu, malayalam, hindi, kannada), NOT english, unless the release is genuinely sung in English. " +
					"Reply with ONLY that single word and nothing else.",
			},
			{
				"role":    "user",
				"content": strings.Join(promptParts, "\n"),
			},
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return Unknown
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return Unknown
	}
	req.Header.Set("Content-Type", "application/json")
	if opts.llmAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+opts.llmAPIKey)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return Unknown
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Unknown
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&completion); err != nil {
		return Unknown
	}
	if len(completion.Choices) == 0 {
		return Unknown
	}

	verdict := parseLLMVerdict(completion.Choices[0].Message.Content)
	if verdict != "" && verdict != Unknown {
		r.setCachedVerdict(key, verdict, positiveCacheTTL)
	} else {
		r.setCachedVerdict(key, Unknown, negativeCacheTTL)
	}
	return verdict
}

func parseLLMVerdict(raw string) string {
	token := strings.TrimSpace(strings.ToLower(raw))
	token = strings.Trim(token, ".,!?:;\"'`\n\r\t ")
	fields := strings.Fields(token)
	if len(fields) > 0 {
		token = strings.Trim(fields[0], ".,!?:;\"'`")
	}
	switch token {
	case "other", "instrumental", "multilingual":
		return "other"
	case "unknown", "unsure", "":
		return Unknown
	}
	norm := Normalize(token)
	for _, s := range SupportedLanguages {
		if norm == s {
			return s
		}
	}
	return Unknown
}
