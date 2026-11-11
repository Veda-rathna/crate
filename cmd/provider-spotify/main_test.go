package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

func startTestSpotifyServer(t *testing.T, fakeURL string, tokenMgr *tokenManager) pb.MusicProviderClient {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	if tokenMgr == nil {
		tokenMgr = &tokenManager{
			token:     "test-valid-token",
			expiresAt: time.Now().Add(1 * time.Hour),
		}
	}
	pb.RegisterMusicProviderServer(s, &server{
		http:     &http.Client{Timeout: 5 * time.Second},
		limiter:  rate.NewLimiter(rate.Inf, 1),
		baseURL:  fakeURL,
		tokenMgr: tokenMgr,
	})
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewMusicProviderClient(conn)
}

func newFakeSpotifyAPI() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-valid-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		searchType := r.URL.Query().Get("type")
		if searchType == "artist" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artists": map[string]any{
					"total": 2,
					"items": []map[string]any{
						{
							"id":         "4Z8W4fKeB5YxbusRsdQVPb",
							"name":       "Radiohead",
							"popularity": 85,
							"followers":  map[string]any{"total": 6000000},
							"genres":     []string{"art rock", "alternative rock"},
							"images": []map[string]any{
								{"url": "http://img/rh_large.jpg", "width": 640, "height": 640},
								{"url": "http://img/rh_medium.jpg", "width": 300, "height": 300},
								{"url": "http://img/rh_small.jpg", "width": 64, "height": 64},
							},
						},
						{
							"id":         "1001",
							"name":       "Radiohead Tribute",
							"popularity": 15,
							"followers":  map[string]any{"total": 500},
							"genres":     []string{"tribute"},
							"images":     []map[string]any{},
						},
					},
				},
			})
			return
		} else if searchType == "track" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tracks": map[string]any{
					"items": []map[string]any{
						{
							"id":          "track1",
							"name":        "Airbag",
							"duration_ms": 284000,
							"album": map[string]any{
								"id":           "alb1",
								"name":         "OK Computer",
								"release_date": "1997-05-21",
								"images": []map[string]any{
									{"url": "http://img/okc_300.jpg", "width": 300, "height": 300},
								},
							},
						},
					},
				},
			})
			return
		}

		http.NotFound(w, r)
	})

	mux.HandleFunc("/artists/4Z8W4fKeB5YxbusRsdQVPb", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "4Z8W4fKeB5YxbusRsdQVPb",
			"name":       "Radiohead",
			"popularity": 85,
			"followers":  map[string]any{"total": 6000000},
			"genres":     []string{"art rock", "alternative rock"},
			"images": []map[string]any{
				{"url": "http://img/rh_medium.jpg", "width": 300, "height": 300},
			},
		})
	})

	mux.HandleFunc("/artists/4Z8W4fKeB5YxbusRsdQVPb/albums", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 3,
			"items": []map[string]any{
				{
					"id":           "alb1",
					"name":         "OK Computer",
					"album_type":   "album",
					"album_group":  "album",
					"total_tracks": 12,
					"release_date": "1997-05-21",
					"images": []map[string]any{
						{"url": "http://img/okc.jpg", "width": 300, "height": 300},
					},
				},
				{
					"id":           "alb2",
					"name":         "Paranoid Android (Single)",
					"album_type":   "single",
					"album_group":  "single",
					"total_tracks": 2,
					"release_date": "1997-05-26",
					"images":       []map[string]any{},
				},
				{
					"id":           "alb3",
					"name":         "Airbag / How Am I Driving? EP",
					"album_type":   "single",
					"album_group":  "single",
					"total_tracks": 7,
					"release_date": "1998-04-21",
					"images":       []map[string]any{},
				},
			},
		})
	})

	mux.HandleFunc("/albums/alb1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "alb1",
			"name":         "OK Computer",
			"release_date": "1997-05-21",
			"artists": []map[string]any{
				{"name": "Radiohead"},
			},
			"images": []map[string]any{
				{"url": "http://img/okc.jpg", "width": 300, "height": 300},
			},
			"tracks": map[string]any{
				"items": []map[string]any{
					{
						"id":           "tr1",
						"name":         "Airbag",
						"track_number": 1,
						"disc_number":  1,
						"duration_ms":  284000,
					},
					{
						"id":           "tr2",
						"name":         "Paranoid Android",
						"track_number": 2,
						"disc_number":  1,
						"duration_ms":  383000,
					},
				},
			},
		})
	})

	return httptest.NewServer(mux)
}

func TestSpotifyInfo(t *testing.T) {
	fake := newFakeSpotifyAPI()
	defer fake.Close()
	client := startTestSpotifyServer(t, fake.URL, nil)

	resp, err := client.Info(context.Background(), &pb.InfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "spotify" {
		t.Errorf("expected name 'spotify', got %q", resp.Name)
	}
	if resp.DisplayName != "Spotify" {
		t.Errorf("expected display_name 'Spotify', got %q", resp.DisplayName)
	}
	if resp.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", resp.Version)
	}
}

func TestSpotifySearchArtists(t *testing.T) {
	fake := newFakeSpotifyAPI()
	defer fake.Close()
	client := startTestSpotifyServer(t, fake.URL, nil)

	resp, err := client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "Radiohead"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Artists) != 2 {
		t.Fatalf("expected 2 artists, got %d", len(resp.Artists))
	}
	if resp.Artists[0].Name != "Radiohead" {
		t.Errorf("expected 'Radiohead', got %q", resp.Artists[0].Name)
	}
	if resp.Artists[0].Id != "4Z8W4fKeB5YxbusRsdQVPb" {
		t.Errorf("expected id '4Z8W4fKeB5YxbusRsdQVPb', got %q", resp.Artists[0].Id)
	}
	if resp.Artists[0].Rank != 85 {
		t.Errorf("expected rank 85, got %d", resp.Artists[0].Rank)
	}
	if resp.Artists[0].ImageUrl != "http://img/rh_medium.jpg" {
		t.Errorf("expected 300px image url, got %q", resp.Artists[0].ImageUrl)
	}
	if resp.Artists[0].Metadata["followers"] != "6000000" {
		t.Errorf("expected followers metadata, got %v", resp.Artists[0].Metadata)
	}
}

func TestSpotifyGetArtist(t *testing.T) {
	fake := newFakeSpotifyAPI()
	defer fake.Close()
	client := startTestSpotifyServer(t, fake.URL, nil)

	resp, err := client.GetArtist(context.Background(), &pb.EntityRequest{Id: "4Z8W4fKeB5YxbusRsdQVPb"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "Radiohead" {
		t.Errorf("expected 'Radiohead', got %q", resp.Name)
	}
	if resp.Metadata["popularity"] != "85" {
		t.Errorf("expected popularity 85, got %v", resp.Metadata)
	}
	if resp.ImageUrl != "http://img/rh_medium.jpg" {
		t.Errorf("expected image url, got %q", resp.ImageUrl)
	}
}

func TestSpotifyGetArtistAlbums(t *testing.T) {
	fake := newFakeSpotifyAPI()
	defer fake.Close()
	client := startTestSpotifyServer(t, fake.URL, nil)

	resp, err := client.GetArtistAlbums(context.Background(), &pb.EntityRequest{Id: "4Z8W4fKeB5YxbusRsdQVPb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Albums) != 3 {
		t.Fatalf("expected 3 albums, got %d", len(resp.Albums))
	}

	// Sorted newest-first: Airbag EP (1998) -> Paranoid Android (1997-05-26) -> OK Computer (1997-05-21)
	if resp.Albums[0].RecordType != "ep" {
		t.Errorf("expected newest album Airbag EP (record_type 'ep') first, got %q (%s)", resp.Albums[0].RecordType, resp.Albums[0].Title)
	}
	if resp.Albums[1].RecordType != "single" {
		t.Errorf("expected record_type 'single', got %q", resp.Albums[1].RecordType)
	}
	if resp.Albums[2].Title != "OK Computer" || resp.Albums[2].Year != 1997 || resp.Albums[2].RecordType != "album" {
		t.Errorf("expected 'OK Computer' (1997 album) third, got %+v", resp.Albums[2])
	}
}

func TestSpotifyGetAlbum(t *testing.T) {
	fake := newFakeSpotifyAPI()
	defer fake.Close()
	client := startTestSpotifyServer(t, fake.URL, nil)

	resp, err := client.GetAlbum(context.Background(), &pb.EntityRequest{Id: "alb1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Title != "OK Computer" {
		t.Errorf("expected 'OK Computer', got %q", resp.Title)
	}
	if resp.ArtistName != "Radiohead" {
		t.Errorf("expected artist 'Radiohead', got %q", resp.ArtistName)
	}
	if resp.Year != 1997 {
		t.Errorf("expected year 1997, got %d", resp.Year)
	}
	if len(resp.Tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(resp.Tracks))
	}
	if resp.Tracks[0].Title != "Airbag" {
		t.Errorf("expected 'Airbag', got %q", resp.Tracks[0].Title)
	}
	if resp.Tracks[0].TrackNumber != 1 {
		t.Errorf("expected track 1, got %d", resp.Tracks[0].TrackNumber)
	}
	if resp.Tracks[0].DurationMs != 284000 {
		t.Errorf("expected 284000 ms, got %d", resp.Tracks[0].DurationMs)
	}
}

func TestSpotifySearchArtistTracks(t *testing.T) {
	fake := newFakeSpotifyAPI()
	defer fake.Close()
	client := startTestSpotifyServer(t, fake.URL, nil)

	resp, err := client.SearchArtistTracks(context.Background(), &pb.ArtistTrackSearchRequest{
		ArtistId: "4Z8W4fKeB5YxbusRsdQVPb",
		Query:    "Airbag",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(resp.Tracks))
	}
	if resp.Tracks[0].Title != "Airbag" {
		t.Errorf("expected 'Airbag', got %q", resp.Tracks[0].Title)
	}
	if resp.Tracks[0].AlbumTitle != "OK Computer" {
		t.Errorf("expected 'OK Computer', got %q", resp.Tracks[0].AlbumTitle)
	}
	if resp.Tracks[0].AlbumYear != 1997 {
		t.Errorf("expected 1997, got %d", resp.Tracks[0].AlbumYear)
	}
}

func TestSpotifyTokenManager_Success(t *testing.T) {
	var requestCount int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "client_credentials" ||
			r.FormValue("client_id") != "test_id" ||
			r.FormValue("client_secret") != "test_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "spotify-tok-12345",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	tm := &tokenManager{
		clientID:     "test_id",
		clientSecret: "test_secret",
		tokenURL:     tokenServer.URL,
		httpClient:   tokenServer.Client(),
	}

	tok1, err := tm.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok1 != "spotify-tok-12345" {
		t.Errorf("expected 'spotify-tok-12345', got %q", tok1)
	}

	// Immediate second call should use cached token without hitting server again
	tok2, err := tm.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok2 != tok1 {
		t.Errorf("expected cached token %q, got %q", tok1, tok2)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("expected 1 HTTP request for cached token, got %d", requestCount)
	}
}

func TestSpotifyTokenManager_MissingCreds(t *testing.T) {
	// Ensure env vars are not set
	t.Setenv("SPOTIFY_CLIENT_ID", "")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "")
	t.Setenv("CRATE_SPOTIFY_CLIENT_ID", "")
	t.Setenv("CRATE_SPOTIFY_CLIENT_SECRET", "")
	t.Setenv("CRATE_DB_PATH", filepath.Join(t.TempDir(), "nonexistent.db"))

	tm := &tokenManager{
		httpClient: &http.Client{},
	}

	_, err := tm.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error when credentials missing")
	}
	if !strings.Contains(err.Error(), "credentials not configured") {
		t.Errorf("expected credentials error message, got: %v", err)
	}
}

func TestSpotifyTokenManager_FromDB(t *testing.T) {
	t.Setenv("SPOTIFY_CLIENT_ID", "")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "")
	t.Setenv("CRATE_SPOTIFY_CLIENT_ID", "")
	t.Setenv("CRATE_SPOTIFY_CLIENT_SECRET", "")

	dbFile := filepath.Join(t.TempDir(), "crate.db")
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO settings (key, value) VALUES ('spotify_client_id', 'db_client_id');
		INSERT INTO settings (key, value) VALUES ('spotify_client_secret', 'db_client_secret');
	`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	t.Setenv("CRATE_DB_PATH", dbFile)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.FormValue("client_id") != "db_client_id" || r.FormValue("client_secret") != "db_client_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "db-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	tm := &tokenManager{
		tokenURL:   tokenServer.URL,
		httpClient: tokenServer.Client(),
	}

	tok, err := tm.GetToken(context.Background())
	if err != nil {
		t.Fatalf("failed to get token from db settings: %v", err)
	}
	if tok != "db-token" {
		t.Errorf("expected 'db-token', got %q", tok)
	}
}

func TestSpotifyAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": {"status": 500, "message": "Internal error"}}`))
	})
	fake := httptest.NewServer(mux)
	defer fake.Close()

	client := startTestSpotifyServer(t, fake.URL, nil)

	_, err := client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestSpotifyGetArtistAlbumsPaginatesPast100AndKeepsSameTitleAcrossYears(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/artists/art-1/albums", func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		var items []map[string]any
		next := ""
		switch offset {
		case "", "0":
			for i := 0; i < 50; i++ {
				items = append(items, map[string]any{
					"id":           fmt.Sprintf("alb-%03d", i),
					"name":         fmt.Sprintf("Catalog Album %03d", i),
					"album_type":   "album",
					"album_group":  "album",
					"total_tracks": 10,
					"release_date": "2010-01-01",
				})
			}
			next = "http://next/1"
		case "50":
			for i := 50; i < 100; i++ {
				items = append(items, map[string]any{
					"id":           fmt.Sprintf("alb-%03d", i),
					"name":         fmt.Sprintf("Catalog Album %03d", i),
					"album_type":   "album",
					"album_group":  "album",
					"total_tracks": 10,
					"release_date": "2015-01-01",
				})
			}
			next = "http://next/2"
		case "100":
			items = append(items,
				map[string]any{
					"id":           "same-title-single-2026",
					"name":         "Echoes",
					"album_type":   "single",
					"album_group":  "single",
					"total_tracks": 1,
					"release_date": "2026-11-05",
				},
				map[string]any{
					"id":           "same-title-album-2024",
					"name":         "Echoes",
					"album_type":   "album",
					"album_group":  "album",
					"total_tracks": 10,
					"release_date": "2024-03-01",
				},
			)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 102,
			"next":  next,
			"items": items,
		})
	})

	fake := httptest.NewServer(mux)
	defer fake.Close()
	client := startTestSpotifyServer(t, fake.URL, nil)

	resp, err := client.GetArtistAlbums(context.Background(), &pb.EntityRequest{Id: "art-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Albums) != 102 {
		t.Fatalf("expected 102 albums (past 100 limit + same-title single/album kept), got %d", len(resp.Albums))
	}
	if resp.Albums[0].Id != "same-title-single-2026" {
		t.Errorf("expected newest release same-title-single-2026 first, got %q", resp.Albums[0].Id)
	}
	if resp.Albums[1].Id != "same-title-album-2024" {
		t.Errorf("expected same-title-album-2024 second, got %q", resp.Albums[1].Id)
	}
}

