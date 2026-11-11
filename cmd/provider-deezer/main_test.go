package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

func startTestServer(t *testing.T, fakeURL string) pb.MusicProviderClient {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	pb.RegisterMusicProviderServer(s, &server{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Inf, 1),
		baseURL: fakeURL,
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

func newFakeDeezerAPI() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/search/artist", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": 1000, "name": "Radiohead", "picture_medium": "http://img/rh.jpg", "nb_album": 9, "nb_fan": 5000000},
				{"id": 1001, "name": "Radiohead Tribute", "picture_medium": "", "nb_album": 1, "nb_fan": 200},
			},
		})
	})

	mux.HandleFunc("/artist/1000", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 1000, "name": "Radiohead", "picture_medium": "http://img/rh.jpg", "nb_fan": 5000000,
		})
	})

	mux.HandleFunc("/artist/1000/albums", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": 2000, "title": "OK Computer", "cover_medium": "http://img/okc.jpg", "release_date": "1997-06-16", "record_type": "album"},
				{"id": 2001, "title": "Kid A", "cover_medium": "http://img/kida.jpg", "release_date": "2000-10-02", "record_type": "album"},
			},
		})
	})

	mux.HandleFunc("/album/2000/tracks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": 3000, "title": "Airbag", "track_position": 1, "disk_number": 1, "duration": 284},
				{"id": 3001, "title": "Paranoid Android", "track_position": 2, "disk_number": 1, "duration": 383},
			},
		})
	})

	mux.HandleFunc("/album/2000", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 2000, "title": "OK Computer", "cover_medium": "http://img/okc.jpg", "release_date": "1997-06-16",
			"artist": map[string]any{"name": "Radiohead"},
		})
	})

	return httptest.NewServer(mux)
}

func TestDeezerInfo(t *testing.T) {
	fake := newFakeDeezerAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.Info(context.Background(), &pb.InfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "deezer" {
		t.Errorf("expected name 'deezer', got %q", resp.Name)
	}
	if resp.DisplayName != "Deezer" {
		t.Errorf("expected display_name 'Deezer', got %q", resp.DisplayName)
	}
}

func TestDeezerSearchArtists(t *testing.T) {
	fake := newFakeDeezerAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "radiohead"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Artists) != 2 {
		t.Fatalf("expected 2 artists, got %d", len(resp.Artists))
	}
	if resp.Artists[0].Name != "Radiohead" {
		t.Errorf("expected 'Radiohead', got %q", resp.Artists[0].Name)
	}
	if resp.Artists[0].Id != "1000" {
		t.Errorf("expected id '1000', got %q", resp.Artists[0].Id)
	}
	if resp.Artists[0].ImageUrl != "http://img/rh.jpg" {
		t.Errorf("expected image url, got %q", resp.Artists[0].ImageUrl)
	}
	if resp.Artists[0].Rank != 5000000 {
		t.Errorf("expected rank 5000000, got %d", resp.Artists[0].Rank)
	}
}

func TestDeezerSearchDeduplicates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/artist", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": 1000, "name": "Radiohead", "nb_fan": 100},
				{"id": 1000, "name": "Radiohead", "nb_fan": 100},
			},
		})
	})
	fake := httptest.NewServer(mux)
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "radiohead"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Artists) != 1 {
		t.Errorf("expected 1 deduplicated artist, got %d", len(resp.Artists))
	}
}

func TestDeezerGetArtist(t *testing.T) {
	fake := newFakeDeezerAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.GetArtist(context.Background(), &pb.EntityRequest{Id: "1000"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "Radiohead" {
		t.Errorf("expected 'Radiohead', got %q", resp.Name)
	}
	if resp.Metadata["fans"] != "5000000" {
		t.Errorf("expected fans metadata, got %v", resp.Metadata)
	}
}

func TestDeezerGetArtistAlbums(t *testing.T) {
	fake := newFakeDeezerAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.GetArtistAlbums(context.Background(), &pb.EntityRequest{Id: "1000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(resp.Albums))
	}
	if resp.Albums[0].Title != "Kid A" {
		t.Errorf("expected newest album 'Kid A' first, got %q", resp.Albums[0].Title)
	}
	if resp.Albums[1].Title != "OK Computer" {
		t.Errorf("expected 'OK Computer' second, got %q", resp.Albums[1].Title)
	}
	if resp.Albums[1].Year != 1997 {
		t.Errorf("expected year 1997, got %d", resp.Albums[1].Year)
	}
	if resp.Albums[1].RecordType != "album" {
		t.Errorf("expected record_type 'album', got %q", resp.Albums[1].RecordType)
	}
	if resp.Albums[1].Metadata["release_date"] != "1997-06-16" {
		t.Errorf("expected release_date metadata, got %v", resp.Albums[1].Metadata)
	}
}

func TestDeezerGetAlbum(t *testing.T) {
	fake := newFakeDeezerAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.GetAlbum(context.Background(), &pb.EntityRequest{Id: "2000"})
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
		t.Errorf("expected track_number 1, got %d", resp.Tracks[0].TrackNumber)
	}
	if resp.Tracks[0].DurationMs != 284000 {
		t.Errorf("expected duration_ms 284000, got %d", resp.Tracks[0].DurationMs)
	}
}

func TestDeezerAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	fake := httptest.NewServer(mux)
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	_, err := client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "test"})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestDeezerGetArtistAlbumsPaginatesAndChecksJSONErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/artist/1000/albums", func(w http.ResponseWriter, r *http.Request) {
		index := r.URL.Query().Get("index")
		var items []map[string]any
		if index == "" || index == "0" {
			for i := 0; i < 100; i++ {
				items = append(items, map[string]any{
					"id":           2000 + i,
					"title":        fmt.Sprintf("Old Album %03d", i),
					"release_date": "2010-01-01",
					"record_type":  "album",
				})
			}
		} else if index == "100" {
			items = append(items, map[string]any{
				"id":           9999,
				"title":        "Brand New 2026 Album",
				"release_date": "2026-11-01",
				"record_type":  "album",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  items,
			"total": 101,
		})
	})
	mux.HandleFunc("/artist/quota/albums", func(w http.ResponseWriter, r *http.Request) {
		// Deezer quota errors return HTTP 200 with an error JSON object
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "QuotaException",
				"message": "Quota limit exceeded",
				"code":    4,
			},
		})
	})

	fake := httptest.NewServer(mux)
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.GetArtistAlbums(context.Background(), &pb.EntityRequest{Id: "1000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Albums) != 101 {
		t.Fatalf("expected 101 paginated albums, got %d", len(resp.Albums))
	}
	if resp.Albums[0].Id != "9999" {
		t.Errorf("expected newest album 9999 first, got %q", resp.Albums[0].Id)
	}

	if _, err := client.GetArtistAlbums(context.Background(), &pb.EntityRequest{Id: "quota"}); err == nil {
		t.Fatal("expected error for Deezer HTTP-200 JSON quota error payload")
	}
}

func TestDeezerGetAlbumMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/album/777", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           777,
			"title":        "Leo",
			"release_date": "2023-10-19",
			"upc":          "3617224112233",
			"label":        "Sony Music South",
			"genres": map[string]any{
				"data": []map[string]any{
					{"id": 1, "name": "Tamil"},
					{"id": 2, "name": "Indian"},
				},
			},
			"artist": map[string]any{"name": "Anirudh Ravichander"},
		})
	})
	mux.HandleFunc("/album/777/tracks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": 1, "title": "Naa Ready", "track_position": 1, "disk_number": 1, "duration": 248},
			},
			"total": 1,
		})
	})

	fake := httptest.NewServer(mux)
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.GetAlbum(context.Background(), &pb.EntityRequest{Id: "777"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Metadata["upc"] != "3617224112233" {
		t.Errorf("expected upc, got %q", resp.Metadata["upc"])
	}
	if resp.Metadata["label"] != "Sony Music South" {
		t.Errorf("expected label Sony Music South, got %q", resp.Metadata["label"])
	}
	if resp.Metadata["genres"] != "Tamil, Indian" {
		t.Errorf("expected genres 'Tamil, Indian', got %q", resp.Metadata["genres"])
	}
}
