package arcod

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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

