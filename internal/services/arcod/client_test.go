package arcod

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"errors"
	"golang.org/x/time/rate"
)

func TestCleanString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Time (2023 Remaster)", "time"},
		{"Get Funky [Deluxe Edition]", "get funky"},
		{"Song Name feat. Someone", "song name"},
		{"It's A Test!", "it s a test"},
	}

	for _, tc := range tests {
		got := cleanString(tc.input)
		if got != tc.want {
			t.Errorf("cleanString(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestClientSearchAndFindBest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/get-music":
			resp := map[string]any{
				"success": true,
				"data": map[string]any{
					"tracks": map[string]any{
						"total": 2,
						"items": []map[string]any{
							{
								"id":                    12345,
								"title":                 "Time",
								"duration":              420,
								"maximum_bit_depth":     24,
								"maximum_sampling_rate": 96.0,
								"hires":                 true,
								"streamable":            true,
								"performer": map[string]any{
									"name": "Pink Floyd",
								},
								"album": map[string]any{
									"title": "The Dark Side of the Moon",
								},
							},
							{
								"id":                    67890,
								"title":                 "Time (Live)",
								"duration":              300,
								"maximum_bit_depth":     16,
								"maximum_sampling_rate": 44.1,
								"hires":                 false,
								"streamable":            true,
								"performer": map[string]any{
									"name": "Pink Floyd",
								},
								"album": map[string]any{
									"title": "Pulse",
								},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := NewClient(nil, "")
	client.SetHTTPClient(ts.Client())
	t.Setenv("CRATE_ARCOD_URL", ts.URL)

	results, err := client.Search(context.Background(), "pink floyd time")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != 12345 || results[0].MaximumBitDepth != 24 {
		t.Errorf("unexpected first result: %+v", results[0])
	}

	best, err := client.FindBestTrack(context.Background(), "Pink Floyd", "Time")
	if err != nil {
		t.Fatalf("FindBestTrack failed: %v", err)
	}
	if best == nil {
		t.Fatal("expected match, got nil")
	}
	if best.ID != 12345 {
		t.Errorf("expected 24-bit studio track 12345, got %d", best.ID)
	}
}

func TestClientDownloadTrack(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/player/stream/12345":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer valid_token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			resp := map[string]any{
				"url":      r.Host + "/stream-file",
				"mimeType": "audio/flac",
			}
			// ensure scheme is in URL
			resp["url"] = "http://" + r.Host + "/stream-file"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/stream-file":
			w.Header().Set("Content-Type", "audio/flac")
			// write fLaC header and dummy bytes
			data := append([]byte("fLaC"), make([]byte, 1024)...)
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	client := NewClient(nil, tmpDir)
	client.SetHTTPClient(ts.Client())
	t.Setenv("CRATE_ARCOD_URL", ts.URL)
	t.Setenv("CRATE_ARCOD_TOKEN", "valid_token")

	destFile := filepath.Join(tmpDir, "test.flac")
	n, err := client.DownloadTrack(context.Background(), 12345, 27, destFile)
	if err != nil {
		t.Fatalf("DownloadTrack failed: %v", err)
	}
	if n < 1000 {
		t.Errorf("downloaded %d bytes, expected >1000", n)
	}

	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("read dest file: %v", err)
	}
	if string(content[:4]) != "fLaC" {
		t.Errorf("expected header fLaC, got %q", string(content[:4]))
	}
}

func TestFindBestTrackMatchHighestFLACAndRelaxed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/get-music" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("q")
		var items []map[string]any
		switch {
		case q == "Pink Floyd Time":
			items = []map[string]any{
				{
					"id": 100, "title": "Time", "version": "Live", "duration": 420,
					"maximum_bit_depth": 24, "maximum_sampling_rate": 192.0, "streamable": true,
					"performer": map[string]any{"name": "Pink Floyd"},
					"album":     map[string]any{"title": "Pulse", "artist": map[string]any{"name": "Pink Floyd"}},
				},
				{
					"id": 101, "title": "Time", "duration": 420,
					"maximum_bit_depth": 24, "maximum_sampling_rate": 96.0, "streamable": true,
					"performer": map[string]any{"name": "Pink Floyd"},
					"album":     map[string]any{"title": "The Dark Side of the Moon", "artist": map[string]any{"name": "Pink Floyd"}},
				},
				{
					"id": 102, "title": "Time", "version": "2011 Remaster", "duration": 420,
					"maximum_bit_depth": 24, "maximum_sampling_rate": 192.0, "streamable": true,
					"performer": map[string]any{"name": "Pink Floyd"},
					"album":     map[string]any{"title": "The Dark Side of the Moon", "artist": map[string]any{"name": "Pink Floyd"}},
				},
			}
		case q == "Beyoncé Dont Hurt Yourself":
			// Only matches via relaxed diacritic & apostrophe matching + album artist fallback
			items = []map[string]any{
				{
					"id": 201, "title": "Don't Hurt Yourself", "duration": 233,
					"maximum_bit_depth": 24, "maximum_sampling_rate": 48.0, "streamable": true,
					"performer": map[string]any{"name": "Jack White"},
					"album":     map[string]any{"title": "Lemonade", "artist": map[string]any{"name": "Beyonce"}},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"tracks": map[string]any{"total": len(items), "items": items}},
		})
	}))
	defer ts.Close()

	client := NewClient(nil, "")
	client.SetHTTPClient(ts.Client())
	t.Setenv("CRATE_ARCOD_URL", ts.URL)

	// Should pick 24-bit/192kHz studio (102) over 24-bit/96kHz (101) and reject Live (100)
	best, err := client.FindBestTrackMatch(context.Background(), MatchQuery{
		Artist:     "Pink Floyd",
		Title:      "Time",
		Album:      "The Dark Side of the Moon",
		DurationMs: 420000,
	})
	if err != nil {
		t.Fatalf("FindBestTrackMatch failed: %v", err)
	}
	if best == nil || best.ID != 102 {
		t.Fatalf("expected 24-bit/192kHz track 102, got %+v", best)
	}

	// Relaxed diacritic + apostrophe + album-artist matching
	best2, err := client.FindBestTrackMatch(context.Background(), MatchQuery{
		Artist: "Beyoncé",
		Title:  "Dont Hurt Yourself (Explicit)",
		Album:  "Lemonade",
	})
	if err != nil {
		t.Fatalf("FindBestTrackMatch relaxed failed: %v", err)
	}
	if best2 == nil || best2.ID != 201 {
		t.Fatalf("expected relaxed track 201, got %+v", best2)
	}
}

func TestDownloadTrackFLACQualityFallbackAndHeaderCheck(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/player/stream/10":
			if r.URL.Query().Get("quality") == "27" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": "http://" + r.Host + "/flac-stream",
			})
		case "/api/player/stream/11":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": "http://" + r.Host + "/bad-stream",
			})
		case "/flac-stream":
			_, _ = w.Write(append([]byte("fLaC"), make([]byte, 512)...))
		case "/bad-stream":
			_, _ = w.Write(append([]byte("ID3\x04"), make([]byte, 512)...))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	client := NewClient(nil, tmpDir)
	client.SetHTTPClient(ts.Client())
	t.Setenv("CRATE_ARCOD_URL", ts.URL)
	t.Setenv("CRATE_ARCOD_TOKEN", "tok")

	// Quality 27 unavailable -> falls back to quality 7 FLAC cleanly
	dest := filepath.Join(tmpDir, "ok.flac")
	if _, err := client.DownloadTrack(context.Background(), 10, DefaultQuality, dest); err != nil {
		t.Fatalf("expected fallback to lower FLAC tier to succeed, got %v", err)
	}

	// Non-FLAC payload must be rejected
	badDest := filepath.Join(tmpDir, "bad.flac")
	if _, err := client.DownloadTrack(context.Background(), 11, DefaultQuality, badDest); err == nil {
		t.Fatal("expected error when stream payload is not fLaC")
	}
	if _, err := os.Stat(badDest); !os.IsNotExist(err) {
		t.Error("invalid non-FLAC file must not be kept on disk")
	}
}

func TestSearchRateLimitCooldownAndCache(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("q") == "trigger429" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"success":false,"error":"Too many searches from this network (14/min).","retryAfterSeconds":60}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"tracks": map[string]any{"total": 0, "items": []any{}}},
		})
	}))
	defer ts.Close()

	client := NewClient(nil, "")
	client.SetHTTPClient(ts.Client())
	client.SetSearchLimiter(rate.NewLimiter(rate.Inf, 1))
	t.Setenv("CRATE_ARCOD_URL", ts.URL)

	// Identical queries hit short-lived cache instead of hammering arcod.
	if _, err := client.Search(context.Background(), "Same Query"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(context.Background(), "same query"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 HTTP search (second cached), got %d", hits.Load())
	}

	// 429 triggers RateLimitError with RetryAfter=60s and stops subsequent calls during cooldown.
	_, err := client.Search(context.Background(), "trigger429")
	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
	if rlErr.RetryAfter != 60*time.Second {
		t.Errorf("expected RetryAfter=60s, got %v", rlErr.RetryAfter)
	}

	hitsBefore := hits.Load()
	_, err = client.Search(context.Background(), "another query")
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected cooldown RateLimitError on follow-up call, got %v", err)
	}
	if hits.Load() != hitsBefore {
		t.Errorf("expected 0 HTTP requests during active cooldown, got %d extra", hits.Load()-hitsBefore)
	}
}

func TestFindBestTrackMatch_AlbumBatchCaching(t *testing.T) {
	var searchCalls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/get-music" {
			searchCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"tracks": map[string]any{
						"total": 3,
						"items": []map[string]any{
							{
								"id":                    101,
								"title":                 "Hukum",
								"duration":              200,
								"maximum_bit_depth":     24,
								"maximum_sampling_rate": 96.0,
								"streamable":            true,
								"performer":             map[string]any{"name": "Anirudh Ravichander"},
								"album":                 map[string]any{"title": "Jailer"},
							},
							{
								"id":                    102,
								"title":                 "Kaavaalaa",
								"duration":              190,
								"maximum_bit_depth":     24,
								"maximum_sampling_rate": 96.0,
								"streamable":            true,
								"performer":             map[string]any{"name": "Anirudh Ravichander"},
								"album":                 map[string]any{"title": "Jailer"},
							},
							{
								"id":                    103,
								"title":                 "Jailer Theme",
								"duration":              150,
								"maximum_bit_depth":     24,
								"maximum_sampling_rate": 96.0,
								"streamable":            true,
								"performer":             map[string]any{"name": "Anirudh Ravichander"},
								"album":                 map[string]any{"title": "Jailer"},
							},
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client := NewClient(nil, "")
	client.SetHTTPClient(ts.Client())
	client.SetSearchLimiter(rate.NewLimiter(rate.Inf, 1))
	t.Setenv("CRATE_ARCOD_URL", ts.URL)

	// First track queries Arcod and populates album cache
	t1, err := client.FindBestTrackMatch(context.Background(), MatchQuery{
		Artist: "Anirudh Ravichander",
		Title:  "Hukum",
		Album:  "Jailer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if t1 == nil || t1.ID != 101 {
		t.Fatalf("expected track 101 Hukum, got %+v", t1)
	}
	if searchCalls.Load() != 1 {
		t.Fatalf("expected 1 search call on first track, got %d", searchCalls.Load())
	}

	// Subsequent tracks in the same album hit the album cache directly without HTTP calls!
	t2, err := client.FindBestTrackMatch(context.Background(), MatchQuery{
		Artist: "Anirudh Ravichander",
		Title:  "Kaavaalaa",
		Album:  "Jailer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if t2 == nil || t2.ID != 102 {
		t.Fatalf("expected track 102 Kaavaalaa from album cache, got %+v", t2)
	}

	t3, err := client.FindBestTrackMatch(context.Background(), MatchQuery{
		Artist: "Anirudh Ravichander",
		Title:  "Jailer Theme",
		Album:  "Jailer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if t3 == nil || t3.ID != 103 {
		t.Fatalf("expected track 103 Jailer Theme from album cache, got %+v", t3)
	}

	// Verify only 1 HTTP request was made across all 3 tracks!
	if searchCalls.Load() != 1 {
		t.Errorf("expected still only 1 HTTP request across 3 tracks of the album, got %d", searchCalls.Load())
	}
}

func TestMultiAccountRateLimitRotation(t *testing.T) {
	var acc1Calls, acc2Calls atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "Bearer token_acc1" {
			acc1Calls.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"success":false,"error":"Too many searches from this network (14/min).","retryAfterSeconds":60}`))
			return
		}
		if auth == "Bearer token_acc2" {
			acc2Calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"tracks": map[string]any{
						"total": 1,
						"items": []map[string]any{
							{
								"id":                    999,
								"title":                 "Rotated Track",
								"duration":              210,
								"maximum_bit_depth":     24,
								"maximum_sampling_rate": 96.0,
								"hires":                 true,
								"streamable":            true,
								"performer": map[string]any{
									"name": "Test Artist",
								},
								"album": map[string]any{
									"title": "Test Album",
								},
							},
						},
					},
				},
			})
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer ts.Close()

	client := NewClient(nil, "")
	client.SetHTTPClient(ts.Client())
	client.SetSearchLimiter(rate.NewLimiter(rate.Inf, 1))
	t.Setenv("CRATE_ARCOD_URL", ts.URL)

	// Add Account 1 and Account 2
	_, err := client.AddAccount(context.Background(), "user1@example.com", "", "token_acc1")
	if err != nil {
		t.Fatalf("failed to add account 1: %v", err)
	}
	_, err = client.AddAccount(context.Background(), "user2@example.com", "", "token_acc2")
	if err != nil {
		t.Fatalf("failed to add account 2: %v", err)
	}

	if client.AccountsCount() != 2 {
		t.Fatalf("expected 2 accounts, got %d", client.AccountsCount())
	}

	// First search should hit Account 1, receive 429, rotate to Account 2, and succeed!
	items, err := client.Search(context.Background(), "rotate-query")
	if err != nil {
		t.Fatalf("search expected to succeed via rotation, got error: %v", err)
	}
	if len(items) != 1 || items[0].ID != 999 {
		t.Fatalf("expected rotated track 999, got %+v", items)
	}

	if acc1Calls.Load() != 1 {
		t.Errorf("expected 1 call on account 1 (rate limited), got %d", acc1Calls.Load())
	}
	if acc2Calls.Load() != 1 {
		t.Errorf("expected 1 call on account 2 (succeeded), got %d", acc2Calls.Load())
	}

	// Verify Account 1 is in rate_limited status and Account 2 is active
	list := client.ListAccounts(context.Background())
	var acc1Info, acc2Info *AccountInfo
	for i := range list {
		if list[i].Email == "user1@example.com" {
			acc1Info = &list[i]
		}
		if list[i].Email == "user2@example.com" {
			acc2Info = &list[i]
		}
	}
	if acc1Info == nil || acc1Info.Status != "rate_limited" {
		t.Errorf("expected account 1 status 'rate_limited', got %+v", acc1Info)
	}
	if acc1Info.CooldownSeconds <= 0 {
		t.Errorf("expected cooldown > 0s, got %d", acc1Info.CooldownSeconds)
	}
	if acc2Info == nil || acc2Info.Status != "active" {
		t.Errorf("expected account 2 status 'active', got %+v", acc2Info)
	}

	// Subsequent search for a different query should immediately pick Account 2 (Account 1 in cooldown)
	items2, err := client.Search(context.Background(), "another-query")
	if err != nil {
		t.Fatalf("subsequent search expected to succeed via account 2, got: %v", err)
	}
	if len(items2) != 1 {
		t.Errorf("expected 1 item, got %d", len(items2))
	}
	if acc1Calls.Load() != 1 {
		t.Errorf("expected account 1 NOT to be called again during cooldown, calls=%d", acc1Calls.Load())
	}
	if acc2Calls.Load() != 2 {
		t.Errorf("expected account 2 to handle second search, got %d calls", acc2Calls.Load())
	}
}

func TestAccountPoolManagement(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/signup":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"token":   "auto_generated_token_123",
			})
		case "/api/auth/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["password"] == "badpass" {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "invalid password"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"token":   "logged_in_token_" + body["email"],
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := NewClient(nil, "")
	client.SetHTTPClient(ts.Client())
	t.Setenv("CRATE_ARCOD_URL", ts.URL)

	// 1. Auto-create multiple accounts (unlimited creation)
	for i := 0; i < 5; i++ {
		acc, err := client.AutoCreateAccount(context.Background())
		if err != nil {
			t.Fatalf("AutoCreateAccount #%d failed: %v", i, err)
		}
		if acc.ID == "" || acc.Email == "" || !acc.HasToken {
			t.Fatalf("unexpected acc info: %+v", acc)
		}
	}

	if client.AccountsCount() != 5 {
		t.Fatalf("expected 5 accounts, got %d", client.AccountsCount())
	}

	// 2. Add manual account
	manualAcc, err := client.AddAccount(context.Background(), "custom@domain.com", "mypass123", "")
	if err != nil {
		t.Fatalf("AddAccount failed: %v", err)
	}
	if client.AccountsCount() != 6 {
		t.Fatalf("expected 6 accounts after manual add, got %d", client.AccountsCount())
	}

	// 3. Test account
	if err := client.TestAccount(context.Background(), manualAcc.ID); err != nil {
		t.Fatalf("TestAccount failed: %v", err)
	}

	// 4. Update account
	updated, err := client.UpdateAccount(context.Background(), manualAcc.ID, "custom_updated@domain.com", "••••••••", "")
	if err != nil {
		t.Fatalf("UpdateAccount failed: %v", err)
	}
	if updated.Email != "custom_updated@domain.com" {
		t.Errorf("expected updated email, got %s", updated.Email)
	}

	// 5. Delete account
	if err := client.DeleteAccount(manualAcc.ID); err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}
	if client.AccountsCount() != 5 {
		t.Fatalf("expected 5 accounts after delete, got %d", client.AccountsCount())
	}
}


