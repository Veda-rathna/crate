package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	_ "modernc.org/sqlite"

	"github.com/TheOutdoorProgrammer/crate/internal/releasedate"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

const (
	spotifyAPIBaseURL   = "https://api.spotify.com/v1"
	spotifyTokenBaseURL = "https://accounts.spotify.com/api/token"
	maxArtistAlbums     = 2000
	maxRateLimitRetries = 3
)

type tokenManager struct {
	clientID     string
	clientSecret string
	tokenURL     string
	httpClient   *http.Client
	mu           sync.Mutex
	token        string
	expiresAt    time.Time
}

func (tm *tokenManager) resolveCredentials() (string, string) {
	clientID := tm.clientID
	if clientID == "" {
		clientID = os.Getenv("SPOTIFY_CLIENT_ID")
		if clientID == "" {
			clientID = os.Getenv("CRATE_SPOTIFY_CLIENT_ID")
		}
	}

	clientSecret := tm.clientSecret
	if clientSecret == "" {
		clientSecret = os.Getenv("SPOTIFY_CLIENT_SECRET")
		if clientSecret == "" {
			clientSecret = os.Getenv("CRATE_SPOTIFY_CLIENT_SECRET")
		}
	}

	if clientID == "" || clientSecret == "" {
		dbPath := os.Getenv("CRATE_DB_PATH")
		if dbPath == "" {
			dbPath = "./crate.db"
		}
		if _, err := os.Stat(dbPath); err == nil {
			if db, err := sql.Open("sqlite", dbPath); err == nil {
				defer db.Close()
				if clientID == "" {
					_ = db.QueryRow("SELECT value FROM settings WHERE key = 'spotify_client_id'").Scan(&clientID)
				}
				if clientSecret == "" {
					_ = db.QueryRow("SELECT value FROM settings WHERE key = 'spotify_client_secret'").Scan(&clientSecret)
				}
			}
		}
	}

	return clientID, clientSecret
}

func (tm *tokenManager) GetToken(ctx context.Context) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.token != "" && time.Now().Add(60*time.Second).Before(tm.expiresAt) {
		return tm.token, nil
	}

	clientID, clientSecret := tm.resolveCredentials()
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("spotify client credentials not configured. Please set SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET or configure them in Crate Settings")
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	tokenURL := tm.tokenURL
	if tokenURL == "" {
		tokenURL = spotifyTokenBaseURL
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token error (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	tm.token = tokenResp.AccessToken
	tm.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return tm.token, nil
}

type imageItem struct {
	URL    string `json:"url"`
	Height int    `json:"height"`
	Width  int    `json:"width"`
}

func bestImage(images []imageItem) string {
	if len(images) == 0 {
		return ""
	}
	for _, img := range images {
		if img.Width >= 250 && img.Width <= 350 {
			return img.URL
		}
	}
	return images[0].URL
}

type server struct {
	pb.UnimplementedMusicProviderServer
	http     *http.Client
	limiter  *rate.Limiter
	baseURL  string
	tokenMgr *tokenManager
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	port := os.Getenv("PORT")
	if port == "" {
		port = "50053"
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	tokenMgr := &tokenManager{
		clientID:     os.Getenv("SPOTIFY_CLIENT_ID"),
		clientSecret: os.Getenv("SPOTIFY_CLIENT_SECRET"),
		httpClient:   httpClient,
	}

	s := grpc.NewServer()
	pb.RegisterMusicProviderServer(s, &server{
		http:     httpClient,
		limiter:  rate.NewLimiter(10, 10),
		baseURL:  spotifyAPIBaseURL,
		tokenMgr: tokenMgr,
	})

	go func() {
		slog.Info("spotify provider starting", "port", port)
		if err := s.Serve(lis); err != nil {
			slog.Error("serve error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("spotify provider shutting down")
	s.GracefulStop()
}

func (s *server) Info(ctx context.Context, req *pb.InfoRequest) (*pb.InfoResponse, error) {
	return &pb.InfoResponse{
		Name:        "spotify",
		DisplayName: "Spotify",
		Version:     "1.0.0",
	}, nil
}

func (s *server) SearchArtists(ctx context.Context, req *pb.SearchRequest) (*pb.ArtistSearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 50 {
		limit = 50
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	var resp struct {
		Artists struct {
			Total int `json:"total"`
			Items []struct {
				ID         string      `json:"id"`
				Name       string      `json:"name"`
				Popularity int         `json:"popularity"`
				Followers  struct {
					Total int `json:"total"`
				} `json:"followers"`
				Genres []string    `json:"genres"`
				Images []imageItem `json:"images"`
			} `json:"items"`
		} `json:"artists"`
	}

	path := fmt.Sprintf("/search?q=%s&type=artist&limit=%d&offset=%d", url.QueryEscape(req.Query), limit, offset)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	var artists []*pb.ArtistResult
	for _, a := range resp.Artists.Items {
		artists = append(artists, &pb.ArtistResult{
			Id:         a.ID,
			Name:       a.Name,
			ImageUrl:   bestImage(a.Images),
			AlbumCount: 0,
			Rank:       int32(a.Popularity),
			Metadata: map[string]string{
				"popularity": strconv.Itoa(a.Popularity),
				"followers":  strconv.Itoa(a.Followers.Total),
				"genres":     strings.Join(a.Genres, ", "),
			},
		})
	}

	sort.Slice(artists, func(i, j int) bool {
		return artists[i].Rank > artists[j].Rank
	})

	return &pb.ArtistSearchResponse{Artists: artists, Total: int32(resp.Artists.Total)}, nil
}

func (s *server) GetArtist(ctx context.Context, req *pb.EntityRequest) (*pb.ArtistDetail, error) {
	var resp struct {
		ID         string      `json:"id"`
		Name       string      `json:"name"`
		Popularity int         `json:"popularity"`
		Followers  struct {
			Total int `json:"total"`
		} `json:"followers"`
		Genres []string    `json:"genres"`
		Images []imageItem `json:"images"`
	}

	if err := s.get(ctx, "/artists/"+req.Id, &resp); err != nil {
		return nil, err
	}

	return &pb.ArtistDetail{
		Id:       resp.ID,
		Name:     resp.Name,
		ImageUrl: bestImage(resp.Images),
		Metadata: map[string]string{
			"popularity": strconv.Itoa(resp.Popularity),
			"followers":  strconv.Itoa(resp.Followers.Total),
			"genres":     strings.Join(resp.Genres, ", "),
		},
	}, nil
}

func (s *server) GetArtistAlbums(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumList, error) {
	type albumEntry struct {
		ID          string      `json:"id"`
		Name        string      `json:"name"`
		AlbumType   string      `json:"album_type"`
		AlbumGroup  string      `json:"album_group"`
		TotalTracks int         `json:"total_tracks"`
		ReleaseDate string      `json:"release_date"`
		Images      []imageItem `json:"images"`
	}

	market := strings.TrimSpace(os.Getenv("SPOTIFY_MARKET"))
	var allItems []albumEntry
	seenIDs := make(map[string]bool)

	for offset := 0; ; {
		var resp struct {
			Total int          `json:"total"`
			Next  string       `json:"next"`
			Items []albumEntry `json:"items"`
		}

		path := fmt.Sprintf("/artists/%s/albums?include_groups=album,single,compilation&limit=50&offset=%d", req.Id, offset)
		if market != "" {
			path += "&market=" + url.QueryEscape(market)
		}
		if err := s.get(ctx, path, &resp); err != nil {
			return nil, err
		}

		for _, item := range resp.Items {
			if seenIDs[item.ID] {
				continue
			}
			seenIDs[item.ID] = true
			allItems = append(allItems, item)
		}

		offset += len(resp.Items)
		if len(resp.Items) == 0 ||
			(resp.Next == "" && (resp.Total == 0 || offset >= resp.Total)) ||
			len(allItems) >= maxArtistAlbums {
			break
		}
	}

	// Deduplicate regional editions by (normalized title, record type, release year)
	// so distinct releases sharing a title across years or types are kept.
	dedupIndex := make(map[string]int)
	var albums []*pb.AlbumSummary
	for _, a := range allItems {
		recordType := "album"
		albumGroup := a.AlbumGroup
		if albumGroup == "" {
			albumGroup = a.AlbumType
		}

		switch albumGroup {
		case "compilation":
			recordType = "compilation"
		case "single":
			if a.TotalTracks >= 4 && a.TotalTracks <= 6 || strings.Contains(strings.ToLower(a.Name), "ep") {
				recordType = "ep"
			} else {
				recordType = "single"
			}
		default:
			recordType = "album"
		}

		year := releasedate.ParseYear(a.ReleaseDate)
		dedupKey := strings.ToLower(strings.TrimSpace(a.Name)) + "|" + recordType + "|" + strconv.Itoa(year)
		summary := &pb.AlbumSummary{
			Id:         a.ID,
			Title:      a.Name,
			CoverUrl:   bestImage(a.Images),
			Year:       int32(year),
			RecordType: recordType,
			Metadata: map[string]string{
				"release_date": a.ReleaseDate,
				"total_tracks": strconv.Itoa(a.TotalTracks),
			},
		}

		if idx, exists := dedupIndex[dedupKey]; exists {
			existingDate := albums[idx].Metadata["release_date"]
			if len(a.ReleaseDate) > len(existingDate) {
				albums[idx] = summary
			}
			continue
		}
		dedupIndex[dedupKey] = len(albums)
		albums = append(albums, summary)
	}

	releasedate.SortAlbumsDescending(albums)
	return &pb.AlbumList{Albums: albums}, nil
}

func (s *server) GetAlbum(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumDetail, error) {
	type spotifyTrack struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		TrackNumber int    `json:"track_number"`
		DiscNumber  int    `json:"disc_number"`
		DurationMs  int    `json:"duration_ms"`
	}
	var albumResp struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		ReleaseDate string `json:"release_date"`
		Artists     []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Images []imageItem `json:"images"`
		Tracks struct {
			Total int            `json:"total"`
			Next  string         `json:"next"`
			Items []spotifyTrack `json:"items"`
		} `json:"tracks"`
	}

	if err := s.get(ctx, "/albums/"+req.Id, &albumResp); err != nil {
		return nil, err
	}

	allTracks := append([]spotifyTrack(nil), albumResp.Tracks.Items...)
	nextURL := albumResp.Tracks.Next
	for offset := len(allTracks); nextURL != "" && len(allTracks) < maxArtistAlbums; {
		var page struct {
			Next  string         `json:"next"`
			Items []spotifyTrack `json:"items"`
		}
		if err := s.get(ctx, fmt.Sprintf("/albums/%s/tracks?limit=50&offset=%d", req.Id, offset), &page); err != nil {
			break
		}
		allTracks = append(allTracks, page.Items...)
		offset += len(page.Items)
		nextURL = page.Next
		if len(page.Items) == 0 {
			break
		}
	}

	artistName := "Unknown"
	if len(albumResp.Artists) > 0 {
		artistName = albumResp.Artists[0].Name
	}

	year := releasedate.ParseYear(albumResp.ReleaseDate)
	var tracks []*pb.TrackInfo
	for _, t := range allTracks {
		tracks = append(tracks, &pb.TrackInfo{
			Id:          t.ID,
			Title:       t.Name,
			TrackNumber: int32(t.TrackNumber),
			DiscNumber:  int32(t.DiscNumber),
			DurationMs:  int32(t.DurationMs),
			Rank:        int32(t.TrackNumber),
		})
	}

	return &pb.AlbumDetail{
		Id:         albumResp.ID,
		Title:      albumResp.Name,
		CoverUrl:   bestImage(albumResp.Images),
		Year:       int32(year),
		ArtistName: artistName,
		Tracks:     tracks,
	}, nil
}

func (s *server) SearchArtistTracks(ctx context.Context, req *pb.ArtistTrackSearchRequest) (*pb.ArtistTrackSearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 50 {
		limit = 50
	}

	q := req.Query
	var artistResp struct {
		Name string `json:"name"`
	}
	if err := s.get(ctx, "/artists/"+req.ArtistId, &artistResp); err == nil && artistResp.Name != "" {
		q = fmt.Sprintf("artist:\"%s\" %s", artistResp.Name, req.Query)
	}

	var resp struct {
		Tracks struct {
			Items []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				DurationMs int    `json:"duration_ms"`
				Album      struct {
					ID          string      `json:"id"`
					Name        string      `json:"name"`
					ReleaseDate string      `json:"release_date"`
					Images      []imageItem `json:"images"`
				} `json:"album"`
			} `json:"items"`
		} `json:"tracks"`
	}

	path := fmt.Sprintf("/search?q=%s&type=track&limit=%d", url.QueryEscape(q), limit)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var tracks []*pb.TrackSearchResult
	for _, t := range resp.Tracks.Items {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		tracks = append(tracks, &pb.TrackSearchResult{
			Id:            t.ID,
			Title:         t.Name,
			DurationMs:    int32(t.DurationMs),
			AlbumId:       t.Album.ID,
			AlbumTitle:    t.Album.Name,
			AlbumCoverUrl: bestImage(t.Album.Images),
			AlbumYear:     int32(releasedate.ParseYear(t.Album.ReleaseDate)),
		})
	}

	return &pb.ArtistTrackSearchResponse{Tracks: tracks}, nil
}

func (s *server) get(ctx context.Context, path string, out any) error {
	for attempt := 0; attempt <= maxRateLimitRetries; attempt++ {
		if err := s.limiter.Wait(ctx); err != nil {
			return err
		}

		token, err := s.tokenMgr.GetToken(ctx)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := s.http.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfterStr := resp.Header.Get("Retry-After")
			resp.Body.Close()
			if attempt == maxRateLimitRetries {
				return fmt.Errorf("spotify API rate limited (429) after %d retries", maxRateLimitRetries)
			}
			waitSec, _ := strconv.Atoi(retryAfterStr)
			if waitSec <= 0 {
				waitSec = 1
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(waitSec) * time.Second):
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("spotify API returned status %d: %s", resp.StatusCode, string(b))
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		return err
	}
	return fmt.Errorf("spotify API request exhausted retries")
}
