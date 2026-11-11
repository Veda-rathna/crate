package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"

	"github.com/TheOutdoorProgrammer/crate/internal/releasedate"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

const (
	mbBaseURL       = "https://musicbrainz.org/ws/2"
	maxArtistAlbums = 2000
)

type server struct {
	pb.UnimplementedMusicProviderServer
	http      *http.Client
	limiter   *rate.Limiter
	userAgent string
	baseURL   string
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	port := os.Getenv("PORT")
	if port == "" {
		port = "50051"
	}

	userAgent := os.Getenv("MB_USER_AGENT")
	if userAgent == "" {
		userAgent = "Crate/0.1.0 (https://github.com/TheOutdoorProgrammer/crate)"
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	s := grpc.NewServer()
	pb.RegisterMusicProviderServer(s, &server{
		http:      &http.Client{Timeout: 15 * time.Second},
		limiter:   rate.NewLimiter(1, 1),
		userAgent: userAgent,
		baseURL:   mbBaseURL,
	})

	go func() {
		slog.Info("musicbrainz provider starting", "port", port)
		if err := s.Serve(lis); err != nil {
			slog.Error("serve error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("musicbrainz provider shutting down")
	s.GracefulStop()
}

func (s *server) Info(ctx context.Context, req *pb.InfoRequest) (*pb.InfoResponse, error) {
	return &pb.InfoResponse{
		Name:        "musicbrainz",
		DisplayName: "MusicBrainz",
		Version:     "1.0.0",
	}, nil
}

func (s *server) SearchArtists(ctx context.Context, req *pb.SearchRequest) (*pb.ArtistSearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 25
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	var resp struct {
		Count  int `json:"count"`
		Artists []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Score          int    `json:"score"`
			Country        string `json:"country"`
			Disambiguation string `json:"disambiguation"`
			Type           string `json:"type"`
		} `json:"artists"`
	}

	path := fmt.Sprintf("/artist/?query=%s&limit=%d&offset=%d&fmt=json", url.QueryEscape(req.Query), limit, offset)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	var artists []*pb.ArtistResult
	for _, a := range resp.Artists {
		meta := map[string]string{}
		if a.Country != "" {
			meta["country"] = a.Country
		}
		if a.Disambiguation != "" {
			meta["disambiguation"] = a.Disambiguation
		}
		if a.Type != "" {
			meta["type"] = a.Type
		}
		artists = append(artists, &pb.ArtistResult{
			Id:       a.ID,
			Name:     a.Name,
			Rank:     int32(a.Score),
			Metadata: meta,
		})
	}

	return &pb.ArtistSearchResponse{Artists: artists, Total: int32(resp.Count)}, nil
}

func (s *server) GetArtist(ctx context.Context, req *pb.EntityRequest) (*pb.ArtistDetail, error) {
	var resp struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Country        string `json:"country"`
		Disambiguation string `json:"disambiguation"`
		Type           string `json:"type"`
		LifeSpan       struct {
			Begin string `json:"begin"`
			End   string `json:"end"`
		} `json:"life-span"`
	}

	if err := s.get(ctx, "/artist/"+req.Id+"?fmt=json", &resp); err != nil {
		return nil, err
	}

	meta := map[string]string{}
	if resp.Country != "" {
		meta["country"] = resp.Country
	}
	if resp.Disambiguation != "" {
		meta["disambiguation"] = resp.Disambiguation
	}
	if resp.Type != "" {
		meta["type"] = resp.Type
	}
	if resp.LifeSpan.Begin != "" {
		meta["active_since"] = resp.LifeSpan.Begin
	}

	return &pb.ArtistDetail{
		Id:       resp.ID,
		Name:     resp.Name,
		Metadata: meta,
	}, nil
}

func (s *server) GetArtistAlbums(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumList, error) {
	type mbReleaseGroup struct {
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		PrimaryType      string   `json:"primary-type"`
		SecondaryTypes   []string `json:"secondary-types"`
		FirstReleaseDate string   `json:"first-release-date"`
	}

	var allRGs []mbReleaseGroup
	seen := make(map[string]bool)

	for offset := 0; ; offset += 100 {
		var resp struct {
			ReleaseGroupCount int              `json:"release-group-count"`
			ReleaseGroups     []mbReleaseGroup `json:"release-groups"`
		}

		path := fmt.Sprintf("/release-group?artist=%s&limit=100&offset=%d&fmt=json", req.Id, offset)
		if err := s.get(ctx, path, &resp); err != nil {
			return nil, err
		}

		for _, rg := range resp.ReleaseGroups {
			if seen[rg.ID] {
				continue
			}
			seen[rg.ID] = true
			allRGs = append(allRGs, rg)
		}

		if len(resp.ReleaseGroups) < 100 ||
			(resp.ReleaseGroupCount > 0 && offset+len(resp.ReleaseGroups) >= resp.ReleaseGroupCount) ||
			len(allRGs) >= maxArtistAlbums {
			break
		}
	}

	var albums []*pb.AlbumSummary
	for _, rg := range allRGs {
		year := releasedate.ParseYear(rg.FirstReleaseDate)
		albums = append(albums, &pb.AlbumSummary{
			Id:         rg.ID,
			Title:      rg.Title,
			Year:       int32(year),
			CoverUrl:   "https://coverartarchive.org/release-group/" + rg.ID + "/front-250",
			RecordType: mapMBRecordType(rg.PrimaryType, rg.SecondaryTypes),
			Metadata: map[string]string{
				"release_date": rg.FirstReleaseDate,
			},
		})
	}

	releasedate.SortAlbumsDescending(albums)
	return &pb.AlbumList{Albums: albums}, nil
}

func mapMBRecordType(primary string, secondary []string) string {
	for _, sec := range secondary {
		if sec == "Compilation" {
			return "compilation"
		}
	}
	switch primary {
	case "Single":
		return "single"
	case "EP":
		return "ep"
	case "Compilation":
		return "compilation"
	default:
		return "album"
	}
}

func joinUniqueStrings(items ...string) string {
	seen := make(map[string]struct{}, len(items))
	var out []string
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return strings.Join(out, ", ")
}

func (s *server) GetAlbum(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumDetail, error) {
	type mbRelease struct {
		ID     string `json:"id"`
		Date   string `json:"date"`
		Status string `json:"status"`
	}
	type mbTag struct {
		Name string `json:"name"`
	}
	var rgResp struct {
		ID           string      `json:"id"`
		Title        string      `json:"title"`
		Releases     []mbRelease `json:"releases"`
		Genres       []mbTag     `json:"genres"`
		Tags         []mbTag     `json:"tags"`
		ArtistCredit []struct {
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"artist-credit"`
		FirstReleaseDate string `json:"first-release-date"`
	}

	if err := s.get(ctx, "/release-group/"+req.Id+"?inc=releases+artist-credits+genres+tags&fmt=json", &rgResp); err != nil {
		return nil, err
	}

	artistName := ""
	if len(rgResp.ArtistCredit) > 0 {
		artistName = rgResp.ArtistCredit[0].Artist.Name
	}

	year := releasedate.ParseYear(rgResp.FirstReleaseDate)

	// Prefer Official releases first so a stub/promo release doesn't hide the real tracklist.
	var ordered []mbRelease
	for _, rel := range rgResp.Releases {
		if rel.Status == "Official" {
			ordered = append(ordered, rel)
		}
	}
	for _, rel := range rgResp.Releases {
		if rel.Status != "Official" {
			ordered = append(ordered, rel)
		}
	}

	var rgGenres []string
	for _, g := range rgResp.Genres {
		rgGenres = append(rgGenres, g.Name)
	}
	var rgTags []string
	for _, t := range rgResp.Tags {
		rgTags = append(rgTags, t.Name)
	}

	type mbWorkRel struct {
		TargetType string `json:"target-type"`
		Work       struct {
			Language  string   `json:"language"`
			Languages []string `json:"languages"`
		} `json:"work"`
	}

	var tracks []*pb.TrackInfo
	albumMetadata := map[string]string{}
	if v := joinUniqueStrings(rgGenres...); v != "" {
		albumMetadata["genres"] = v
	}
	if v := joinUniqueStrings(rgTags...); v != "" {
		albumMetadata["tags"] = v
	}

	maxAttempts := 3
	if len(ordered) < maxAttempts {
		maxAttempts = len(ordered)
	}
	for i := 0; i < maxAttempts; i++ {
		var relResp struct {
			Barcode            string `json:"barcode"`
			TextRepresentation struct {
				Language string `json:"language"`
				Script   string `json:"script"`
			} `json:"text-representation"`
			LabelInfo []struct {
				Label struct {
					Name string `json:"name"`
				} `json:"label"`
			} `json:"label-info"`
			Genres []mbTag `json:"genres"`
			Tags   []mbTag `json:"tags"`
			Media  []struct {
				Position int `json:"position"`
				Tracks   []struct {
					ID        string      `json:"id"`
					Title     string      `json:"title"`
					Number    string      `json:"number"`
					Position  int         `json:"position"`
					Length    int         `json:"length"`
					Relations []mbWorkRel `json:"relations"`
					Recording struct {
						Relations []mbWorkRel `json:"relations"`
					} `json:"recording"`
				} `json:"tracks"`
			} `json:"media"`
		}

		if err := s.get(ctx, "/release/"+ordered[i].ID+"?inc=recordings+recording-level-rels+work-rels+labels+genres+tags&fmt=json", &relResp); err != nil {
			continue
		}

		var candidateTracks []*pb.TrackInfo
		var albumWorkLangs []string
		for _, media := range relResp.Media {
			for _, t := range media.Tracks {
				trackNum, _ := strconv.Atoi(t.Number)
				if trackNum == 0 {
					trackNum = t.Position
				}
				var trackWorkLangs []string
				collectRels := func(rels []mbWorkRel) {
					for _, rel := range rels {
						if rel.TargetType != "" && rel.TargetType != "work" {
							continue
						}
						trackWorkLangs = append(trackWorkLangs, rel.Work.Languages...)
						if rel.Work.Language != "" {
							trackWorkLangs = append(trackWorkLangs, rel.Work.Language)
						}
					}
				}
				collectRels(t.Recording.Relations)
				collectRels(t.Relations)
				albumWorkLangs = append(albumWorkLangs, trackWorkLangs...)

				var trackMeta map[string]string
				if joined := joinUniqueStrings(trackWorkLangs...); joined != "" {
					trackMeta = map[string]string{"work_languages": joined}
					// Set single clean language if unambiguous so TrackInfo metadata stays simple.
					parts := strings.Split(joined, ",")
					if len(parts) == 1 {
						trackMeta["language"] = strings.TrimSpace(parts[0])
					}
				}

				candidateTracks = append(candidateTracks, &pb.TrackInfo{
					Id:          t.ID,
					Title:       t.Title,
					TrackNumber: int32(trackNum),
					DiscNumber:  int32(media.Position),
					DurationMs:  int32(t.Length),
					Rank:        int32(t.Position),
					Metadata:    trackMeta,
				})
			}
		}

		if len(candidateTracks) > 0 || len(tracks) == 0 {
			tracks = candidateTracks
			albumMetadata = map[string]string{}
			if relResp.TextRepresentation.Language != "" {
				albumMetadata["language"] = relResp.TextRepresentation.Language
				albumMetadata["text_language"] = relResp.TextRepresentation.Language
			}
			if relResp.TextRepresentation.Script != "" {
				albumMetadata["script"] = relResp.TextRepresentation.Script
				albumMetadata["text_script"] = relResp.TextRepresentation.Script
			}
			if bc := strings.TrimSpace(relResp.Barcode); bc != "" {
				albumMetadata["upc"] = bc
			}
			var labels []string
			for _, li := range relResp.LabelInfo {
				if li.Label.Name != "" {
					labels = append(labels, li.Label.Name)
				}
			}
			if v := joinUniqueStrings(labels...); v != "" {
				albumMetadata["label"] = v
			}
			allGenres := append([]string{}, rgGenres...)
			for _, g := range relResp.Genres {
				allGenres = append(allGenres, g.Name)
			}
			if v := joinUniqueStrings(allGenres...); v != "" {
				albumMetadata["genres"] = v
			}
			allTags := append([]string{}, rgTags...)
			for _, tg := range relResp.Tags {
				allTags = append(allTags, tg.Name)
			}
			if v := joinUniqueStrings(allTags...); v != "" {
				albumMetadata["tags"] = v
			}
			if v := joinUniqueStrings(albumWorkLangs...); v != "" {
				albumMetadata["work_languages"] = v
			}
		}
		if len(tracks) > 0 {
			break
		}
	}

	return &pb.AlbumDetail{
		Id:         rgResp.ID,
		Title:      rgResp.Title,
		Year:       int32(year),
		CoverUrl:   "https://coverartarchive.org/release-group/" + rgResp.ID + "/front-250",
		ArtistName: artistName,
		Tracks:     tracks,
		Metadata:   albumMetadata,
	}, nil
}

func (s *server) SearchArtistTracks(ctx context.Context, req *pb.ArtistTrackSearchRequest) (*pb.ArtistTrackSearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	var resp struct {
		Recordings []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Length int    `json:"length"`
			Releases []struct {
				ID           string `json:"id"`
				Title        string `json:"title"`
				Date         string `json:"date"`
				ReleaseGroup struct {
					ID string `json:"id"`
				} `json:"release-group"`
			} `json:"releases"`
		} `json:"recordings"`
	}

	path := fmt.Sprintf("/recording/?query=arid:%s AND recording:%s&limit=%d&fmt=json",
		req.ArtistId, url.QueryEscape(req.Query), limit)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var tracks []*pb.TrackSearchResult
	for _, rec := range resp.Recordings {
		if seen[rec.ID] {
			continue
		}
		seen[rec.ID] = true

		t := &pb.TrackSearchResult{
			Id:         rec.ID,
			Title:      rec.Title,
			DurationMs: int32(rec.Length),
		}
		if len(rec.Releases) > 0 {
			rel := rec.Releases[0]
			rgID := rel.ReleaseGroup.ID
			if rgID == "" {
				rgID = rel.ID
			}
			t.AlbumId = rgID
			t.AlbumTitle = rel.Title
			t.AlbumCoverUrl = "https://coverartarchive.org/release-group/" + rgID + "/front-250"
			t.AlbumYear = int32(releasedate.ParseYear(rel.Date))
		}
		tracks = append(tracks, t)
	}

	return &pb.ArtistTrackSearchResponse{Tracks: tracks}, nil
}

func (s *server) get(ctx context.Context, path string, out any) error {
	if err := s.limiter.Wait(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("musicbrainz API returned status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
