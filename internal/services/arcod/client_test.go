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

