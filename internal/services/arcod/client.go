package arcod

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/matching"
)

const (
	DefaultBaseURL         = "https://arcod.xyz"
	DefaultQuality         = 27 // 27 = Hi-Res / 24-bit FLAC; 6 = 16-bit FLAC
	defaultSearchCacheTTL  = 2 * time.Minute
	defaultAlbumCacheTTL   = 15 * time.Minute
	defaultSearchRateLimit = 5 * time.Second // 12 searches/min steady state (under arcod's 14/min limit)
	defaultRetryAfter      = 60 * time.Second
)

// RateLimitError indicates arcod rejected a search with HTTP 429 or is in active cooldown.
type RateLimitError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("arcod rate limited (retry after %s)", e.RetryAfter)
}

type TrackItem struct {
	ID                  int64   `json:"id"`
	Title               string  `json:"title"`
	Version             string  `json:"version"`
	Duration            int     `json:"duration"`
	TrackNumber         int     `json:"track_number"`
	MaximumBitDepth     int     `json:"maximum_bit_depth"`
	MaximumSamplingRate float64 `json:"maximum_sampling_rate"`
	Hires               bool    `json:"hires"`
	Streamable          bool    `json:"streamable"`
	Artist              string  `json:"artist"`
	AlbumArtist         string  `json:"album_artist,omitempty"`
	Album               string  `json:"album"`
}

// MatchQuery describes the target track metadata for relaxed Arcod matching.
type MatchQuery struct {
	Artist     string
	Title      string
	Album      string
	DurationMs int
}

type SearchResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Tracks struct {
			Total int         `json:"total"`
			Items []rawTrack  `json:"items"`
		} `json:"tracks"`
	} `json:"data"`
}

type rawTrack struct {
	ID                  any     `json:"id"`
	QobuzID             any     `json:"qobuz_id"`
	Title               string  `json:"title"`
	Version             string  `json:"version"`
	Duration            int     `json:"duration"`
	TrackNumber         int     `json:"track_number"`
	MaximumBitDepth     int     `json:"maximum_bit_depth"`
	MaximumSamplingRate float64 `json:"maximum_sampling_rate"`
	Hires               bool    `json:"hires"`
	HiresStreamable     bool    `json:"hires_streamable"`
	Streamable          bool    `json:"streamable"`
	Performer           struct {
		Name string `json:"name"`
		ID   any    `json:"id"`
	} `json:"performer"`
	Album struct {
		ID     any    `json:"id"`
		Title  string `json:"title"`
		Artist struct {
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"album"`
}

type cachedSearch struct {
	items     []TrackItem
	expiresAt time.Time
}

type Client struct {
	queries          *db.Queries
	downloadsDir     string
	httpClient       *http.Client
	mu               sync.Mutex
	cachedToken      string
	rateMu           sync.Mutex
	searchLimiter    *rate.Limiter
	rateLimitedUntil time.Time
	searchCache      map[string]cachedSearch
	albumCache       map[string]cachedSearch
}

func NewClient(queries *db.Queries, downloadsDir string) *Client {
	return &Client{
		queries:       queries,
		downloadsDir:  downloadsDir,
		searchLimiter: rate.NewLimiter(rate.Every(defaultSearchRateLimit), 2),
		searchCache:   make(map[string]cachedSearch),
		albumCache:    make(map[string]cachedSearch),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// SetHTTPClient allows overriding the HTTP client in tests.
func (c *Client) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

// SetSearchLimiter overrides the proactive search rate limiter (useful in unit tests).
func (c *Client) SetSearchLimiter(l *rate.Limiter) {
	c.rateMu.Lock()
	c.searchLimiter = l
	c.rateMu.Unlock()
}

func (c *Client) BaseURL() string {
	if c.queries != nil {
		if v, err := c.queries.GetSetting("arcod_url"); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	if v := os.Getenv("CRATE_ARCOD_URL"); strings.TrimSpace(v) != "" {
		return strings.TrimRight(strings.TrimSpace(v), "/")
	}
	return DefaultBaseURL
}

func (c *Client) IsEnabled() bool {
	if c.queries != nil {
		if v, err := c.queries.GetSetting("arcod_enabled"); err == nil && v != "" {
			return v == "true" || v == "1"
		}
	}
	if v := os.Getenv("CRATE_ARCOD_ENABLED"); v != "" {
		return v == "true" || v == "1"
	}
	// Default enabled
	return true
}

func (c *Client) getCredentials() (email, password, token string) {
	if c.queries != nil {
		email, _ = c.queries.GetSetting("arcod_email")
		password, _ = c.queries.GetSetting("arcod_password")
		token, _ = c.queries.GetSetting("arcod_token")
	}
	if email == "" {
		email = os.Getenv("CRATE_ARCOD_EMAIL")
	}
	if password == "" {
		password = os.Getenv("CRATE_ARCOD_PASSWORD")
	}
	if token == "" {
		token = os.Getenv("CRATE_ARCOD_TOKEN")
	}
	return strings.TrimSpace(email), strings.TrimSpace(password), strings.TrimSpace(token)
}

// EnsureAuthenticated ensures a valid auth token is available.
func (c *Client) EnsureAuthenticated(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedToken != "" {
		return c.cachedToken, nil
	}

	email, password, token := c.getCredentials()
	if token != "" {
		c.cachedToken = token
		return token, nil
	}

	baseURL := c.BaseURL()

	// If email & password provided, log in
	if email != "" && password != "" {
		tok, err := c.login(ctx, baseURL, email, password)
		if err == nil && tok != "" {
			c.cachedToken = tok
			if c.queries != nil {
				_ = c.queries.SetSetting("arcod_token", tok)
			}
			return tok, nil
		}
		slog.Warn("arcod: login with credentials failed, attempting signup", "error", err)
	}

	// Auto-provision a free account if none exists
	if email == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		randID := hex.EncodeToString(b)
		email = fmt.Sprintf("crate_user_%s@example.com", randID)
		password = fmt.Sprintf("CrateP@ss_%s!", randID)
	}

	tok, err := c.signup(ctx, baseURL, email, password)
	if err != nil {
		return "", fmt.Errorf("arcod authentication failed: %w", err)
	}

	c.cachedToken = tok
	if c.queries != nil {
		_ = c.queries.SetSetting("arcod_email", email)
		_ = c.queries.SetSetting("arcod_password", password)
		_ = c.queries.SetSetting("arcod_token", tok)
	}
	return tok, nil
}

func (c *Client) login(ctx context.Context, baseURL, email, password string) (string, error) {
	bodyData, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/auth/login", strings.NewReader(string(bodyData)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("login failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var res struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Token == "" {
		return "", errors.New("empty token in login response")
	}
	return res.Token, nil
}

func (c *Client) signup(ctx context.Context, baseURL, email, password string) (string, error) {
	bodyData, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/auth/signup", strings.NewReader(string(bodyData)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("signup failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var res struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Token == "" {
		return "", errors.New("empty token in signup response")
	}
	return res.Token, nil
}

// Search queries the arcod music API, pacing requests under arcod's 14/min rate limit
// and honoring HTTP 429 retryAfterSeconds cooldowns across callers.
func (c *Client) Search(ctx context.Context, query string) ([]TrackItem, error) {
	cacheKey := strings.ToLower(strings.TrimSpace(query))

	c.rateMu.Lock()
	if entry, ok := c.searchCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		items := append([]TrackItem(nil), entry.items...)
		c.rateMu.Unlock()
		return items, nil
	}
	if remaining := time.Until(c.rateLimitedUntil); remaining > 0 {
		c.rateMu.Unlock()
		return nil, &RateLimitError{
			RetryAfter: remaining,
			Message:    fmt.Sprintf("arcod search rate limit cooldown active (%s remaining)", remaining.Round(time.Second)),
		}
	}
	limiter := c.searchLimiter
	c.rateMu.Unlock()

	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}

	baseURL := c.BaseURL()
	searchURL := fmt.Sprintf("%s/api/get-music?q=%s", baseURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		retryAfter := parseRetryAfter(b, resp.Header.Get("Retry-After"))
		c.rateMu.Lock()
		c.rateLimitedUntil = time.Now().Add(retryAfter)
		c.rateMu.Unlock()
		return nil, &RateLimitError{
			RetryAfter: retryAfter,
			Message:    fmt.Sprintf("search failed (429): %s", strings.TrimSpace(string(b))),
		}
	}

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed (%d): %s", resp.StatusCode, string(b))
	}

	var sr SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	var items []TrackItem
	for _, raw := range sr.Data.Tracks.Items {
		id := parseID(raw.ID)
		if id == 0 {
			id = parseID(raw.QobuzID)
		}
		if id == 0 {
			continue
		}

		artist := raw.Performer.Name
		if artist == "" {
			artist = raw.Album.Artist.Name
		}

		items = append(items, TrackItem{
			ID:                  id,
			Title:               raw.Title,
			Version:             raw.Version,
			Duration:            raw.Duration,
			TrackNumber:         raw.TrackNumber,
			MaximumBitDepth:     raw.MaximumBitDepth,
			MaximumSamplingRate: raw.MaximumSamplingRate,
			Hires:               raw.Hires || raw.HiresStreamable,
			Streamable:          raw.Streamable,
			Artist:              artist,
			AlbumArtist:         raw.Album.Artist.Name,
			Album:               raw.Album.Title,
		})
	}

	c.rateMu.Lock()
	if c.searchCache == nil {
		c.searchCache = make(map[string]cachedSearch)
	}
	if len(c.searchCache) >= 200 {
		now := time.Now()
		for k, v := range c.searchCache {
			if now.After(v.expiresAt) {
				delete(c.searchCache, k)
			}
		}
		if len(c.searchCache) >= 200 {
			c.searchCache = make(map[string]cachedSearch)
		}
	}
	c.searchCache[cacheKey] = cachedSearch{
		items:     append([]TrackItem(nil), items...),
		expiresAt: time.Now().Add(defaultSearchCacheTTL),
	}

	// Index returned tracks into albumCache so other tracks in the same album
	// can be retrieved instantly without burning search quota.
	if c.albumCache == nil {
		c.albumCache = make(map[string]cachedSearch)
	}
	albumGroups := make(map[string][]TrackItem)
	for _, it := range items {
		if strings.TrimSpace(it.Album) != "" {
			k := normalizeAlbumKey(it.Artist, it.Album)
			albumGroups[k] = append(albumGroups[k], it)
			if strings.TrimSpace(it.AlbumArtist) != "" {
				k2 := normalizeAlbumKey(it.AlbumArtist, it.Album)
				if k2 != k {
					albumGroups[k2] = append(albumGroups[k2], it)
				}
			}
		}
	}
	now := time.Now()
	for k, group := range albumGroups {
		c.albumCache[k] = cachedSearch{
			items:     group,
			expiresAt: now.Add(defaultAlbumCacheTTL),
		}
	}
	c.rateMu.Unlock()

	return items, nil
}

func parseRetryAfter(body []byte, headerVal string) time.Duration {
	var payload struct {
		RetryAfterSeconds int `json:"retryAfterSeconds"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.RetryAfterSeconds > 0 {
		return time.Duration(payload.RetryAfterSeconds) * time.Second
	}
	if sec, err := strconv.Atoi(strings.TrimSpace(headerVal)); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return defaultRetryAfter
}

func parseID(val any) int64 {
	switch v := val.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

var (
	qualifierRegex = regexp.MustCompile(`(?i)\s*(\(|\[).*?(remaster|deluxe|bonus|live|mono|stereo|anniversary|version|edit|edition).*?(\)|\])`)
	featRegex      = regexp.MustCompile(`(?i)\s*(feat\.|featuring).*$`)
	punctRegex     = regexp.MustCompile(`[^\p{L}\p{N}\s]`)
)

func cleanString(s string) string {
	s = qualifierRegex.ReplaceAllString(s, "")
	s = featRegex.ReplaceAllString(s, "")
	s = punctRegex.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(s)
}

func normalizeAlbumKey(artist, album string) string {
	primary := matching.PrimaryArtist(artist)
	cleanAlb := matching.StripQualifiers(album)
	return strings.ToLower(strings.TrimSpace(primary + "|" + cleanAlb))
}

// FindBestTrack searches arcod for a track matching artist and title.
// Returns the highest quality FLAC match, or nil if no good match is found.
func (c *Client) FindBestTrack(ctx context.Context, artistName, trackTitle string) (*TrackItem, error) {
	return c.FindBestTrackMatch(ctx, MatchQuery{
		Artist: artistName,
		Title:  trackTitle,
	})
}

// FindBestTrackMatch searches arcod using relaxed metadata matching (preferring
// studio versions, matching album/duration, and selecting the highest FLAC
// bit-depth and sample-rate available).
// It leverages an in-memory album cache and proactive album-level queries to satisfy
// subsequent tracks in the same album with zero additional HTTP search calls.
func (c *Client) FindBestTrackMatch(ctx context.Context, q MatchQuery) (*TrackItem, error) {
	// 1. Check album cache first if album is known
	if strings.TrimSpace(q.Album) != "" {
		albumKey := normalizeAlbumKey(q.Artist, q.Album)
		c.rateMu.Lock()
		entry, ok := c.albumCache[albumKey]
		if ok && time.Now().Before(entry.expiresAt) {
			cachedItems := append([]TrackItem(nil), entry.items...)
			c.rateMu.Unlock()
			if best := pickBestMatch(cachedItems, q); best != nil {
				slog.Debug("arcod: album cache hit for track", "artist", q.Artist, "album", q.Album, "title", q.Title)
				return best, nil
			}
		} else {
			c.rateMu.Unlock()
		}

		// 2. If not yet in album cache, search once by album to pre-fill the cache for all tracks!
		primaryArtist := matching.PrimaryArtist(q.Artist)
		cleanAlb := matching.StripQualifiers(q.Album)
		albumQuery := matching.CleanSearchQuery(primaryArtist, cleanAlb)
		if albumQuery != "" {
			albumResults, err := c.Search(ctx, albumQuery)
			if err != nil {
				return nil, err
			}
			if best := pickBestMatch(albumResults, q); best != nil {
				return best, nil
			}
		}
	}

	// 3. Fall back to track-specific queries
	queries := buildSearchQueries(q)
	for _, query := range queries {
		results, err := c.Search(ctx, query)
		if err != nil {
			return nil, err
		}
		if best := pickBestMatch(results, q); best != nil {
			return best, nil
		}
	}
	return nil, nil
}

func buildSearchQueries(q MatchQuery) []string {
	var queries []string
	seen := make(map[string]bool)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		queries = append(queries, s)
	}

	add(matching.CleanSearchQuery(q.Artist, q.Title))

	primaryArtist := matching.PrimaryArtist(q.Artist)
	coreTitle := matching.StripQualifiers(q.Title)
	for _, sep := range []string{" - ", " – ", " — ", ": "} {
		if lead, _, ok := strings.Cut(coreTitle, sep); ok && strings.TrimSpace(lead) != "" {
			add(matching.CleanSearchQuery(primaryArtist, lead))
		}
	}

	if norm := matching.Normalize(primaryArtist + " " + coreTitle); norm != "" {
		add(norm)
	}

	return queries
}

func pickBestMatch(results []TrackItem, q MatchQuery) *TrackItem {
	var best *TrackItem
	bestScore := -1

	targetContext := strings.TrimSpace(q.Title + " " + q.Album)
	targetSec := q.DurationMs / 1000

	for i := range results {
		t := &results[i]
		if !t.Streamable {
			continue
		}

		// Artist check against performer OR album artist (soundtracks/features).
		artistMatches := matching.ArtistsMatch(q.Artist, t.Artist) ||
			(t.AlbumArtist != "" && matching.ArtistsMatch(q.Artist, t.AlbumArtist))
		if !artistMatches {
			continue
		}

		fullTitle := strings.TrimSpace(t.Title)
		if t.Version != "" && !strings.Contains(strings.ToLower(fullTitle), strings.ToLower(t.Version)) {
			fullTitle = strings.TrimSpace(fullTitle + " (" + t.Version + ")")
		}

		titleQuality := matching.TitleMatchQuality(q.Title, fullTitle)
		if t.Title != fullTitle {
			if q2 := matching.TitleMatchQuality(q.Title, t.Title); q2 > titleQuality {
				titleQuality = q2
			}
		}
		if titleQuality == 0 {
			continue
		}

		// Reject unrequested musical variants (live, remix, karaoke, instrumental, cover, etc.)
		// so studio tracks aren't mis-matched to live/cover versions and can fall back cleanly.
		if mismatch, hardReject := matching.HasUnrequestedVariant(fullTitle, q.Title); mismatch || hardReject {
			continue
		}
		if t.Album != "" {
			if mismatch, hardReject := matching.HasUnrequestedVariant(t.Album, targetContext); mismatch || hardReject {
				continue
			}
		}

		// Score by exact FLAC bit depth & sample rate so highest FLAC always wins.
		bitDepth := t.MaximumBitDepth
		if bitDepth <= 0 {
			bitDepth = 16
		}
		sampleTenths := int(t.MaximumSamplingRate*10 + 0.5)
		if sampleTenths <= 0 {
			sampleTenths = 441
		}
		score := bitDepth*1000 + sampleTenths + titleQuality*30

		if q.Album != "" && t.Album != "" && matching.TitlesMatch(q.Album, t.Album) {
			score += 500
		}

		if t.AlbumArtist != "" && matching.ArtistsMatch(q.Artist, t.AlbumArtist) {
			score += 50
		}

		if targetSec > 0 && t.Duration > 0 {
			diff := t.Duration - targetSec
			if diff < 0 {
				diff = -diff
			}
			switch {
			case diff <= 5:
				score += 1000
			case diff <= 15:
				score += 400
			case diff > 45 && float64(diff) > 0.35*float64(targetSec):
				score -= 20000
			}
		}

		if best == nil || score > bestScore {
			bestScore = score
			best = t
		}
	}

	return best
}

// GetStreamURL retrieves the temporary audio stream URL for a track.
func (c *Client) GetStreamURL(ctx context.Context, trackID int64, quality int) (string, error) {
	if quality <= 0 {
		quality = DefaultQuality
	}

	token, err := c.EnsureAuthenticated(ctx)
	if err != nil {
		return "", err
	}

	baseURL := c.BaseURL()
	streamURL := fmt.Sprintf("%s/api/player/stream/%d?quality=%d", baseURL, trackID, quality)

	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Invalidate cached token and retry once
		c.mu.Lock()
		c.cachedToken = ""
		c.mu.Unlock()

		newToken, err := c.EnsureAuthenticated(ctx)
		if err != nil {
			return "", err
		}

		reqRetry, _ := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
		reqRetry.Header.Set("Authorization", "Bearer "+newToken)
		respRetry, err := c.httpClient.Do(reqRetry)
		if err != nil {
			return "", err
		}
		defer respRetry.Body.Close()
		resp = respRetry
	}

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("arcod stream error (%d): %s", resp.StatusCode, string(b))
	}

	var res struct {
		URL      string `json:"url"`
		MimeType string `json:"mimeType"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.URL == "" {
		return "", errors.New("empty stream URL returned from arcod")
	}
	return res.URL, nil
}

// DownloadTrack downloads the highest available lossless FLAC audio into destPath.
// When requesting DefaultQuality (27 = 24-bit/192kHz), it falls back only to lower
// lossless FLAC tiers (7 = 24-bit/96kHz, 6 = 16-bit/44.1kHz), never lossy formats.
func (c *Client) DownloadTrack(ctx context.Context, trackID int64, quality int, destPath string) (int64, error) {
	qualities := []int{quality}
	if quality <= 0 || quality == DefaultQuality {
		qualities = []int{DefaultQuality, 7, 6}
	}

	var streamURL string
	var lastErr error
	for _, q := range qualities {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		u, err := c.GetStreamURL(ctx, trackID, q)
		if err == nil {
			streamURL = u
			break
		}
		lastErr = err
		slog.Debug("arcod: stream quality unavailable, trying next FLAC tier", "track_id", trackID, "quality", q, "error", err)
	}
	if streamURL == "" {
		return 0, fmt.Errorf("get stream url: %w", lastErr)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("stream fetch failed (%d): %s", resp.StatusCode, string(b))
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return 0, err
	}

	tmpFile := destPath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmpFile)
	}()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return 0, fmt.Errorf("write stream: %w", err)
	}
	_ = f.Close()

	if n < 100 {
		return 0, fmt.Errorf("downloaded file too small (%d bytes)", n)
	}

	// Verify FLAC magic header so lossy/error payloads are never saved as .flac.
	header := make([]byte, 4)
	rf, err := os.Open(tmpFile)
	if err != nil {
		return 0, fmt.Errorf("verify flac header: %w", err)
	}
	_, readErr := io.ReadFull(rf, header)
	_ = rf.Close()
	if readErr != nil || string(header) != "fLaC" {
		return 0, fmt.Errorf("arcod: downloaded file is not valid FLAC (header %q)", string(header))
	}

	if err := os.Rename(tmpFile, destPath); err != nil {
		return 0, fmt.Errorf("rename temp file: %w", err)
	}

	return n, nil
}
