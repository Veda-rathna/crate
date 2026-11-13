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

// Account represents an individual arcod.xyz account within Crate's account pool.
type Account struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Password  string    `json:"password"`
	Token     string    `json:"token,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	RateLimitedUntil time.Time     `json:"-"`
	LastUsedAt       time.Time     `json:"-"`
	LastError        string        `json:"-"`
	Limiter          *rate.Limiter `json:"-"`
}

// AccountInfo is the sanitized representation returned across the API.
type AccountInfo struct {
	ID               string     `json:"id"`
	Email            string     `json:"email"`
	HasPassword      bool       `json:"has_password"`
	HasToken         bool       `json:"has_token"`
	CreatedAt        time.Time  `json:"created_at"`
	Status           string     `json:"status"` // "active", "rate_limited", "error"
	RateLimitedUntil *time.Time `json:"rate_limited_until,omitempty"`
	CooldownSeconds  int        `json:"cooldown_seconds"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
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

	accountsMu sync.RWMutex
	accounts   []*Account
}

func NewClient(queries *db.Queries, downloadsDir string) *Client {
	return &Client{
		queries:       queries,
		downloadsDir:  downloadsDir,
		searchLimiter: rate.NewLimiter(rate.Every(defaultSearchRateLimit), 2),
		searchCache:   make(map[string]cachedSearch),
		albumCache:    make(map[string]cachedSearch),
		accounts:      make([]*Account, 0),
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

func (c *Client) getLegacyCredentials() (email, password, token string) {
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

func (c *Client) loadAccountsLocked() {
	if c.queries != nil {
		v, err := c.queries.GetSetting("arcod_accounts")
		if err == nil && strings.TrimSpace(v) != "" {
			var list []*Account
			if err := json.Unmarshal([]byte(v), &list); err == nil && len(list) > 0 {
				for _, acc := range list {
					if acc.Limiter == nil {
						acc.Limiter = rate.NewLimiter(rate.Every(defaultSearchRateLimit), 2)
					}
				}
				c.accounts = list
				return
			}
		}
	}

	// Migrate from legacy single-account settings or environment variables
	email, password, token := c.getLegacyCredentials()
	if email != "" || password != "" || token != "" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		acc := &Account{
			ID:        "acc_" + hex.EncodeToString(b),
			Email:     email,
			Password:  password,
			Token:     token,
			CreatedAt: time.Now(),
			Limiter:   rate.NewLimiter(rate.Every(defaultSearchRateLimit), 2),
		}
		c.accounts = []*Account{acc}
		if c.queries != nil {
			_ = c.saveAccountsLocked()
		}
	}
}

func (c *Client) saveAccountsLocked() error {
	if c.queries == nil {
		return nil
	}
	data, err := json.Marshal(c.accounts)
	if err != nil {
		return err
	}
	return c.queries.SetSetting("arcod_accounts", string(data))
}

func (c *Client) autoCreateAccountLocked(ctx context.Context) (*Account, error) {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	randID := hex.EncodeToString(b)
	email := fmt.Sprintf("crate_user_%s@example.com", randID)
	password := fmt.Sprintf("CrateP@ss_%s!", randID)

	baseURL := c.BaseURL()
	tok, err := c.signup(ctx, baseURL, email, password)
	if err != nil {
		return nil, fmt.Errorf("auto-create arcod account failed: %w", err)
	}

	acc := &Account{
		ID:        "acc_" + randID,
		Email:     email,
		Password:  password,
		Token:     tok,
		CreatedAt: time.Now(),
		Limiter:   rate.NewLimiter(rate.Every(defaultSearchRateLimit), 2),
	}
	c.accounts = append(c.accounts, acc)
	_ = c.saveAccountsLocked()
	return acc, nil
}

// AccountsCount returns the number of configured arcod accounts in the pool.
func (c *Client) AccountsCount() int {
	c.accountsMu.RLock()
	defer c.accountsMu.RUnlock()
	return len(c.accounts)
}

// ListAccounts returns sanitized information for all arcod accounts in the pool.
func (c *Client) ListAccounts(ctx context.Context) []AccountInfo {
	c.accountsMu.Lock()
	if len(c.accounts) == 0 {
		c.loadAccountsLocked()
	}
	accounts := append([]*Account(nil), c.accounts...)
	c.accountsMu.Unlock()

	now := time.Now()
	res := make([]AccountInfo, 0, len(accounts))
	for _, acc := range accounts {
		status := "active"
		cooldownSec := 0
		var rlUntil *time.Time

		if acc.RateLimitedUntil.After(now) {
			status = "rate_limited"
			cooldownSec = int(acc.RateLimitedUntil.Sub(now).Seconds()) + 1
			t := acc.RateLimitedUntil
			rlUntil = &t
		} else if acc.LastError != "" {
			status = "error"
		}

		var lastUsed *time.Time
		if !acc.LastUsedAt.IsZero() {
			t := acc.LastUsedAt
			lastUsed = &t
		}

		res = append(res, AccountInfo{
			ID:               acc.ID,
			Email:            acc.Email,
			HasPassword:      acc.Password != "",
			HasToken:         acc.Token != "",
			CreatedAt:        acc.CreatedAt,
			Status:           status,
			RateLimitedUntil: rlUntil,
			CooldownSeconds:  cooldownSec,
			LastUsedAt:       lastUsed,
			LastError:        acc.LastError,
		})
	}
	return res
}

// AutoCreateAccount registers a new free account on arcod.xyz and adds it to the pool.
func (c *Client) AutoCreateAccount(ctx context.Context) (*AccountInfo, error) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()
	acc, err := c.autoCreateAccountLocked(ctx)
	if err != nil {
		return nil, err
	}
	return &AccountInfo{
		ID:          acc.ID,
		Email:       acc.Email,
		HasPassword: true,
		HasToken:    acc.Token != "",
		CreatedAt:   acc.CreatedAt,
		Status:      "active",
	}, nil
}

// AddAccount adds a manual account (with credentials or token) to the pool.
func (c *Client) AddAccount(ctx context.Context, email, password, token string) (*AccountInfo, error) {
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	token = strings.TrimSpace(token)

	if email == "" {
		return nil, errors.New("email is required")
	}

	// Verify credentials or fetch token if password provided
	if token == "" && password != "" {
		tok, err := c.login(ctx, c.BaseURL(), email, password)
		if err != nil {
			return nil, fmt.Errorf("login verification failed: %w", err)
		}
		token = tok
	}

	b := make([]byte, 4)
	_, _ = rand.Read(b)
	acc := &Account{
		ID:        "acc_" + hex.EncodeToString(b),
		Email:     email,
		Password:  password,
		Token:     token,
		CreatedAt: time.Now(),
		Limiter:   rate.NewLimiter(rate.Every(defaultSearchRateLimit), 2),
	}

	c.accountsMu.Lock()
	if len(c.accounts) == 0 {
		c.loadAccountsLocked()
	}
	c.accounts = append(c.accounts, acc)
	_ = c.saveAccountsLocked()
	c.accountsMu.Unlock()

	return &AccountInfo{
		ID:          acc.ID,
		Email:       acc.Email,
		HasPassword: acc.Password != "",
		HasToken:    acc.Token != "",
		CreatedAt:   acc.CreatedAt,
		Status:      "active",
	}, nil
}

// UpdateAccount updates an existing account in the pool.
func (c *Client) UpdateAccount(ctx context.Context, id, email, password, token string) (*AccountInfo, error) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()

	if len(c.accounts) == 0 {
		c.loadAccountsLocked()
	}

	var target *Account
	for _, acc := range c.accounts {
		if acc.ID == id {
			target = acc
			break
		}
	}
	if target == nil {
		return nil, errors.New("account not found")
	}

	if strings.TrimSpace(email) != "" {
		target.Email = strings.TrimSpace(email)
	}
	if strings.TrimSpace(password) != "" && password != "••••••••" {
		target.Password = strings.TrimSpace(password)
		target.Token = "" // Invalidate old token when password changes
	}
	if strings.TrimSpace(token) != "" && token != "••••••••" {
		target.Token = strings.TrimSpace(token)
	}

	// If token missing and password available, test login
	if target.Token == "" && target.Password != "" {
		tok, err := c.login(ctx, c.BaseURL(), target.Email, target.Password)
		if err == nil {
			target.Token = tok
			target.LastError = ""
		} else {
			target.LastError = err.Error()
		}
	}

	_ = c.saveAccountsLocked()

	return &AccountInfo{
		ID:          target.ID,
		Email:       target.Email,
		HasPassword: target.Password != "",
		HasToken:    target.Token != "",
		CreatedAt:   target.CreatedAt,
		Status:      "active",
		LastError:   target.LastError,
	}, nil
}

// DeleteAccount removes an account from the pool.
func (c *Client) DeleteAccount(id string) error {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()

	if len(c.accounts) == 0 {
		c.loadAccountsLocked()
	}

	idx := -1
	for i, acc := range c.accounts {
		if acc.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errors.New("account not found")
	}

	c.accounts = append(c.accounts[:idx], c.accounts[idx+1:]...)
	return c.saveAccountsLocked()
}

// TestAccount verifies an account's credentials against arcod.xyz.
func (c *Client) TestAccount(ctx context.Context, id string) error {
	c.accountsMu.Lock()
	if len(c.accounts) == 0 {
		c.loadAccountsLocked()
	}
	var target *Account
	for _, acc := range c.accounts {
		if acc.ID == id {
			target = acc
			break
		}
	}
	c.accountsMu.Unlock()

	if target == nil {
		return errors.New("account not found")
	}

	if target.Password == "" && target.Token == "" {
		return errors.New("no password or token configured for account")
	}

	if target.Password != "" {
		tok, err := c.login(ctx, c.BaseURL(), target.Email, target.Password)
		if err != nil {
			c.markAccountError(id, err.Error())
			return fmt.Errorf("login failed: %w", err)
		}
		c.accountsMu.Lock()
		target.Token = tok
		target.LastError = ""
		_ = c.saveAccountsLocked()
		c.accountsMu.Unlock()
	}

	c.markAccountSuccess(id)
	return nil
}

func (c *Client) getNextAvailableAccount(ctx context.Context) (*Account, error) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()

	if len(c.accounts) == 0 {
		c.loadAccountsLocked()
	}
	if len(c.accounts) == 0 {
		if c.cachedToken != "" {
			return &Account{ID: "legacy", Token: c.cachedToken}, nil
		}
		return nil, nil // No account configured
	}

	now := time.Now()
	var available []*Account
	var minCooldown time.Duration = -1

	for _, acc := range c.accounts {
		if acc.RateLimitedUntil.After(now) {
			rem := acc.RateLimitedUntil.Sub(now)
			if minCooldown < 0 || rem < minCooldown {
				minCooldown = rem
			}
		} else {
			available = append(available, acc)
		}
	}

	if len(available) == 0 {
		if minCooldown < 0 {
			minCooldown = defaultRetryAfter
		}
		return nil, &RateLimitError{
			RetryAfter: minCooldown,
			Message:    fmt.Sprintf("all %d arcod accounts are rate limited (ready in %s)", len(c.accounts), minCooldown.Round(time.Second)),
		}
	}

	// Pick least recently used among available accounts to distribute traffic evenly
	best := available[0]
	for _, acc := range available[1:] {
		if acc.LastUsedAt.Before(best.LastUsedAt) {
			best = acc
		}
	}

	best.LastUsedAt = now
	return best, nil
}

func (c *Client) markAccountRateLimited(accountID string, retryAfter time.Duration, msg string) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()
	for _, acc := range c.accounts {
		if acc.ID == accountID {
			acc.RateLimitedUntil = time.Now().Add(retryAfter)
			acc.LastError = msg
			break
		}
	}
}

func (c *Client) markAccountError(accountID string, errStr string) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()
	for _, acc := range c.accounts {
		if acc.ID == accountID {
			acc.LastError = errStr
			break
		}
	}
}

func (c *Client) markAccountSuccess(accountID string) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()
	for _, acc := range c.accounts {
		if acc.ID == accountID {
			acc.LastError = ""
			break
		}
	}
}

func (c *Client) invalidateAccountToken(accountID string) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()
	for _, acc := range c.accounts {
		if acc.ID == accountID {
			acc.Token = ""
			_ = c.saveAccountsLocked()
			break
		}
	}
}

func (c *Client) ensureAccountToken(ctx context.Context, acc *Account) (string, error) {
	c.accountsMu.RLock()
	tok := acc.Token
	email := acc.Email
	password := acc.Password
	c.accountsMu.RUnlock()

	if tok != "" {
		return tok, nil
	}

	if email == "" || password == "" {
		return "", errors.New("arcod account missing email or password")
	}

	newTok, err := c.login(ctx, c.BaseURL(), email, password)
	if err != nil {
		return "", fmt.Errorf("arcod login failed for %s: %w", email, err)
	}

	c.accountsMu.Lock()
	acc.Token = newTok
	_ = c.saveAccountsLocked()
	c.accountsMu.Unlock()

	return newTok, nil
}

// EnsureAuthenticated ensures a valid auth token is available from the pool.
func (c *Client) EnsureAuthenticated(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedToken != "" {
		return c.cachedToken, nil
	}

	acc, err := c.getNextAvailableAccount(ctx)
	if err != nil {
		return "", err
	}
	if acc == nil {
		// Attempt auto-provision
		c.accountsMu.Lock()
		newAcc, aErr := c.autoCreateAccountLocked(ctx)
		c.accountsMu.Unlock()
		if aErr != nil {
			// Fallback to legacy single-account logic if pool is empty
			email, password, token := c.getLegacyCredentials()
			if token != "" {
				c.cachedToken = token
				return token, nil
			}
			if email != "" && password != "" {
				if tok, lErr := c.login(ctx, c.BaseURL(), email, password); lErr == nil && tok != "" {
					c.cachedToken = tok
					return tok, nil
				}
			}
			return "", aErr
		}
		acc = newAcc
	}

	tok, err := c.ensureAccountToken(ctx, acc)
	if err != nil {
		return "", err
	}
	c.cachedToken = tok
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

func (c *Client) doSearch(ctx context.Context, query string, token string) ([]TrackItem, error) {
	baseURL := c.BaseURL()
	searchURL := fmt.Sprintf("%s/api/get-music?q=%s", baseURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		retryAfter := parseRetryAfter(b, resp.Header.Get("Retry-After"))
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

	return items, nil
}

func (c *Client) indexSearchResults(cacheKey string, items []TrackItem) {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()

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
}

// Search queries the arcod music API, rotating across the account pool on 429 rate limits,
// and indexing results for album-level caching.
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
	c.rateMu.Unlock()

	c.accountsMu.RLock()
	accCount := len(c.accounts)
	c.accountsMu.RUnlock()
	if accCount == 0 {
		c.accountsMu.Lock()
		c.loadAccountsLocked()
		accCount = len(c.accounts)
		c.accountsMu.Unlock()
	}

	maxAttempts := accCount
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	triedAccounts := make(map[string]bool)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		acc, err := c.getNextAvailableAccount(ctx)
		var token string
		accountID := ""
		accountEmail := ""

		if err != nil {
			var rlErr *RateLimitError
			if errors.As(err, &rlErr) {
				return nil, err
			}
			// If account auto-provision isn't possible (e.g. test mock), try unauthenticated
		} else if acc != nil {
			if triedAccounts[acc.ID] {
				break
			}
			triedAccounts[acc.ID] = true
			accountID = acc.ID
			accountEmail = acc.Email

			if tok, tErr := c.ensureAccountToken(ctx, acc); tErr == nil && tok != "" {
				token = tok
			} else if tErr != nil {
				c.markAccountError(acc.ID, tErr.Error())
			}

			if acc.Limiter != nil {
				if wErr := acc.Limiter.Wait(ctx); wErr != nil {
					return nil, wErr
				}
			} else if c.searchLimiter != nil {
				if wErr := c.searchLimiter.Wait(ctx); wErr != nil {
					return nil, wErr
				}
			}
		}

		items, err := c.doSearch(ctx, query, token)
		if err == nil {
			if accountID != "" {
				c.markAccountSuccess(accountID)
			}
			c.indexSearchResults(cacheKey, items)
			return items, nil
		}

		var rlErr *RateLimitError
		if errors.As(err, &rlErr) {
			if accountID != "" {
				slog.Warn("arcod: account rate limited (429), rotating to next account",
					"account_id", accountID, "email", accountEmail, "retry_after", rlErr.RetryAfter)
				c.markAccountRateLimited(accountID, rlErr.RetryAfter, rlErr.Error())
			} else {
				c.rateMu.Lock()
				c.rateLimitedUntil = time.Now().Add(rlErr.RetryAfter)
				c.rateMu.Unlock()
			}
			lastErr = err
			continue
		}

		return nil, err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("arcod search failed: all accounts exhausted")
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

var errUnauthorized = errors.New("unauthorized")

func (c *Client) doStreamRequest(ctx context.Context, trackID int64, quality int, token string) (string, error) {
	baseURL := c.BaseURL()
	streamURL := fmt.Sprintf("%s/api/player/stream/%d?quality=%d", baseURL, trackID, quality)

	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", errUnauthorized
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		retryAfter := parseRetryAfter(b, resp.Header.Get("Retry-After"))
		return "", &RateLimitError{
			RetryAfter: retryAfter,
			Message:    fmt.Sprintf("stream rate limited (429): %s", strings.TrimSpace(string(b))),
		}
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

// GetStreamURL retrieves the temporary audio stream URL for a track, rotating through
// accounts on rate limit (429) or token expiration (401).
func (c *Client) GetStreamURL(ctx context.Context, trackID int64, quality int) (string, error) {
	if quality <= 0 {
		quality = DefaultQuality
	}

	c.accountsMu.RLock()
	accCount := len(c.accounts)
	c.accountsMu.RUnlock()
	if accCount == 0 {
		c.accountsMu.Lock()
		c.loadAccountsLocked()
		accCount = len(c.accounts)
		c.accountsMu.Unlock()
	}

	maxAttempts := accCount
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	triedAccounts := make(map[string]bool)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		acc, err := c.getNextAvailableAccount(ctx)
		var token string
		accountID := ""
		accountEmail := ""

		if err != nil {
			var rlErr *RateLimitError
			if errors.As(err, &rlErr) {
				return "", err
			}
			if c.cachedToken != "" {
				token = c.cachedToken
			} else {
				return "", err
			}
		} else if acc != nil {
			if triedAccounts[acc.ID] {
				break
			}
			triedAccounts[acc.ID] = true
			accountID = acc.ID
			accountEmail = acc.Email

			tok, tErr := c.ensureAccountToken(ctx, acc)
			if tErr != nil {
				c.markAccountError(acc.ID, tErr.Error())
				lastErr = tErr
				continue
			}
			token = tok
		}

		streamURL, err := c.doStreamRequest(ctx, trackID, quality, token)
		if err == nil {
			if accountID != "" {
				c.markAccountSuccess(accountID)
			}
			return streamURL, nil
		}

		// Handle 401 Unauthorized: re-login once
		if errors.Is(err, errUnauthorized) {
			if accountID != "" && acc != nil {
				c.invalidateAccountToken(acc.ID)
				if newTok, loginErr := c.ensureAccountToken(ctx, acc); loginErr == nil {
					if retryURL, retryErr := c.doStreamRequest(ctx, trackID, quality, newTok); retryErr == nil {
						c.markAccountSuccess(acc.ID)
						return retryURL, nil
					}
				}
			}
		}

		var rlErr *RateLimitError
		if errors.As(err, &rlErr) {
			if accountID != "" {
				slog.Warn("arcod: account stream rate limited (429), rotating to next account",
					"account_id", accountID, "email", accountEmail, "retry_after", rlErr.RetryAfter)
				c.markAccountRateLimited(accountID, rlErr.RetryAfter, rlErr.Error())
			}
			lastErr = err
			continue
		}

		lastErr = err
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("arcod stream request failed: no available account")
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
