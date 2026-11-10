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

	"github.com/TheOutdoorProgrammer/crate/internal/db"
)

const (
	DefaultBaseURL = "https://arcod.xyz"
	DefaultQuality = 27 // 27 = Hi-Res / 24-bit FLAC; 6 = 16-bit FLAC
)

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
	Album               string  `json:"album"`
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

type Client struct {
	queries      *db.Queries
	downloadsDir string
	httpClient   *http.Client
	mu           sync.Mutex
	cachedToken  string
}

func NewClient(queries *db.Queries, downloadsDir string) *Client {
	return &Client{
		queries:      queries,
		downloadsDir: downloadsDir,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// SetHTTPClient allows overriding the HTTP client in tests.
func (c *Client) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
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

// Search queries the arcod music API.
func (c *Client) Search(ctx context.Context, query string) ([]TrackItem, error) {
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
			Album:               raw.Album.Title,
		})
	}
	return items, nil
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

// FindBestTrack searches arcod for a track matching artist and title.
// Returns the highest quality match (prefers Hi-Res FLAC), or nil if no good match found.
func (c *Client) FindBestTrack(ctx context.Context, artistName, trackTitle string) (*TrackItem, error) {
	cleanArtist := cleanString(artistName)
	cleanTitle := cleanString(trackTitle)

	query := fmt.Sprintf("%s %s", artistName, trackTitle)
	results, err := c.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	var best *TrackItem
	bestScore := -1

	for i := range results {
		t := &results[i]
		if !t.Streamable {
			continue
		}

		itemArtist := cleanString(t.Artist)
		itemTitle := cleanString(t.Title)

		// Check artist match
		artistMatches := itemArtist == cleanArtist ||
			strings.Contains(itemArtist, cleanArtist) ||
			strings.Contains(cleanArtist, itemArtist)

		if !artistMatches {
			continue
		}

		// Check title match
		titleMatches := itemTitle == cleanTitle ||
			strings.HasPrefix(itemTitle, cleanTitle) ||
			strings.HasPrefix(cleanTitle, itemTitle)

		if !titleMatches {
			continue
		}

		// Score: Prefer FLAC bit depth and sample rate
		score := 50
		if t.MaximumBitDepth >= 24 {
			score += 50
		} else if t.MaximumBitDepth >= 16 {
			score += 30
		}

		if t.MaximumSamplingRate >= 96.0 {
			score += 15
		} else if t.MaximumSamplingRate >= 44.1 {
			score += 10
		}

		if score > bestScore {
			bestScore = score
			best = t
		}
	}

	return best, nil
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

// DownloadTrack downloads the lossless FLAC audio directly into destPath.
func (c *Client) DownloadTrack(ctx context.Context, trackID int64, quality int, destPath string) (int64, error) {
	streamURL, err := c.GetStreamURL(ctx, trackID, quality)
	if err != nil {
		return 0, fmt.Errorf("get stream url: %w", err)
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

	// Verify FLAC magic header
	header := make([]byte, 4)
	rf, err := os.Open(tmpFile)
	if err == nil {
		_, _ = io.ReadFull(rf, header)
		_ = rf.Close()
		if string(header) != "fLaC" {
			slog.Warn("arcod: downloaded file header is not fLaC", "header", string(header))
		}
	}

	if err := os.Rename(tmpFile, destPath); err != nil {
		return 0, fmt.Errorf("rename temp file: %w", err)
	}

	return n, nil
}
