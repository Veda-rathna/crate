package scheduler_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/cache"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/language"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
	"github.com/TheOutdoorProgrammer/crate/internal/services/scheduler"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

type fakeSchedProvider struct {
	pb.UnimplementedMusicProviderServer
	mu           sync.Mutex
	albums       []*pb.AlbumSummary
	tracks       map[string][]*pb.TrackInfo
	albumDetails map[string]*pb.AlbumDetail
}

func (f *fakeSchedProvider) Info(ctx context.Context, req *pb.InfoRequest) (*pb.InfoResponse, error) {
	return &pb.InfoResponse{Name: "test", DisplayName: "Test", Version: "1.0"}, nil
}

func (f *fakeSchedProvider) GetArtistAlbums(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*pb.AlbumSummary, len(f.albums))
	copy(out, f.albums)
	return &pb.AlbumList{Albums: out}, nil
}

func (f *fakeSchedProvider) GetAlbum(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if detail, ok := f.albumDetails[req.Id]; ok {
		return detail, nil
	}
	return &pb.AlbumDetail{
		Id:     req.Id,
		Title:  "Album " + req.Id,
		Year:   2026,
		Tracks: f.tracks[req.Id],
	}, nil
}

func newTestScheduler(t *testing.T, fp *fakeSchedProvider) (*scheduler.Service, *db.Queries, *provider.Manager) {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	pb.RegisterMusicProviderServer(srv, fp)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	c, err := cache.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	actLog, err := activity.NewLog(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { actLog.Close() })

	queries := db.NewQueries(database)
	mgr := provider.NewManager(c, queries)
	if err := mgr.RegisterProvider(context.Background(), "test", lis.Addr().String()); err != nil {
		t.Fatal(err)
	}

	svc := scheduler.NewService(queries, mgr, actLog, t.TempDir(), time.Hour)
	return svc, queries, mgr
}

func TestDetectNewReleases_PartialDatesSameDayAndCacheBypass(t *testing.T) {
	fp := &fakeSchedProvider{
		albums: []*pb.AlbumSummary{
			{Id: "al-old", Title: "Old Album", Year: 2020, RecordType: "album", Metadata: map[string]string{"release_date": "2020-01-01"}},
		},
		tracks: map[string][]*pb.TrackInfo{
			"al-old":     {{Id: "t-old", Title: "Old Track", TrackNumber: 1, DiscNumber: 1}},
			"al-sameday": {{Id: "t-same", Title: "Same Day Track", TrackNumber: 1, DiscNumber: 1}},
			"al-month":   {{Id: "t-month", Title: "Month Track", TrackNumber: 1, DiscNumber: 1}},
			"al-year":    {{Id: "t-year", Title: "Year Track", TrackNumber: 1, DiscNumber: 1}},
		},
	}
	svc, q, mgr := newTestScheduler(t, fp)
	ctx := context.Background()

	// Seed the 24h cache with only al-old so we prove DetectNewReleases bypasses cache.
	if _, err := mgr.GetArtistAlbums(ctx, "test", "art-partial"); err != nil {
		t.Fatal(err)
	}

	since := "2026-06-15T18:00:00Z"
	artist := &models.Artist{
		Name:                  "Partial Artist",
		Provider:              "test",
		ProviderID:            "art-partial",
		Status:                models.ArtistStatusPartial,
		WatchNewReleases:      true,
		WatchNewReleasesSince: &since,
	}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}

	// Update provider with same-day and partial-date releases while cache still holds only al-old.
	fp.mu.Lock()
	fp.albums = append(fp.albums,
		&pb.AlbumSummary{Id: "al-sameday", Title: "Same Day", Year: 2026, RecordType: "single", Metadata: map[string]string{"release_date": "2026-06-15"}},
		&pb.AlbumSummary{Id: "al-month", Title: "Month Precision", Year: 2026, RecordType: "ep", Metadata: map[string]string{"release_date": "2026-06"}},
		&pb.AlbumSummary{Id: "al-year", Title: "Year Precision", Year: 2026, RecordType: "album", Metadata: map[string]string{"release_date": "2026"}},
	)
	fp.mu.Unlock()

	svc.DetectNewReleases(ctx)

	albums, err := q.ListAlbumsByArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 3 {
		t.Fatalf("expected 3 new releases (same-day, YYYY-MM, YYYY) and 1 old skipped, got %d", len(albums))
	}
	for _, a := range albums {
		if a.ProviderID == "al-old" {
			t.Errorf("did not expect pre-since album al-old on partial artist")
		}
		tracks, _ := q.ListTracksByAlbum(a.ID)
		if len(tracks) != 1 {
			t.Errorf("expected 1 track for %s, got %d", a.Title, len(tracks))
		}
	}
}

func TestDetectNewReleases_WatchedArtistSyncsBackfillAndHealsEmptyAlbum(t *testing.T) {
	fp := &fakeSchedProvider{
		albums: []*pb.AlbumSummary{
			{Id: "al-empty", Title: "Updated Title", Year: 2025, RecordType: "album", Metadata: map[string]string{"release_date": "2025-02-01"}},
			{Id: "al-missed", Title: "Missed Release", Year: 2024, RecordType: "album", Metadata: map[string]string{"release_date": "2024-05-01"}},
		},
		tracks: map[string][]*pb.TrackInfo{
			"al-empty":  {{Id: "t-e1", Title: "Healed Track", TrackNumber: 1, DiscNumber: 1}},
			"al-missed": {{Id: "t-m1", Title: "Missed Track", TrackNumber: 1, DiscNumber: 1}},
		},
	}
	svc, q, _ := newTestScheduler(t, fp)

	since := "2026-06-15T00:00:00Z"
	artist := &models.Artist{
		Name:                  "Full Artist",
		Provider:              "test",
		ProviderID:            "art-full",
		Status:                models.ArtistStatusWatched,
		WatchNewReleases:      true,
		WatchNewReleasesSince: &since,
	}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	// Pre-seed an existing 0-track album (simulating an earlier failed GetAlbum).
	_ = q.CreateAlbum(&models.Album{
		ArtistID:   artist.ID,
		Title:      "Old Title",
		Provider:   "test",
		ProviderID: "al-empty",
		RecordType: "album",
		Status:     models.AlbumStatusWatched,
	})

	svc.DetectNewReleases(context.Background())

	albums, err := q.ListAlbumsByArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 {
		t.Fatalf("expected 2 albums (existing healed + missed added), got %d", len(albums))
	}

	healed, _ := q.FindAlbumByProvider("test", "al-empty")
	if healed == nil || healed.Title != "Updated Title" {
		t.Fatalf("expected al-empty title updated to 'Updated Title', got %+v", healed)
	}
	healedTracks, _ := q.ListTracksByAlbum(healed.ID)
	if len(healedTracks) != 1 || healedTracks[0].Title != "Healed Track" {
		t.Errorf("expected healed track on al-empty, got %+v", healedTracks)
	}
}

func TestDetectNewReleases_LanguageResolverEnrichesTamilAndSkipsDubs(t *testing.T) {
	fp := &fakeSchedProvider{
		albums: []*pb.AlbumSummary{
			{Id: "al-tam", Title: `Aaja Raja (From "KH x RK Reunion")`, Year: 2026, RecordType: "single", Metadata: map[string]string{"release_date": "2026-11-11"}},
			{Id: "al-hin", Title: `Aaja Raja (From "KH x RK Reunion") - Hindi`, Year: 2026, RecordType: "single", Metadata: map[string]string{"release_date": "2026-11-11"}},
		},
		albumDetails: map[string]*pb.AlbumDetail{
			"al-tam": {
				Id:         "al-tam",
				Title:      `Aaja Raja (From "KH x RK Reunion")`,
				ArtistName: "Anirudh Ravichander",
				Year:       2026,
				Metadata:   map[string]string{"text_language": "eng", "text_script": "Latn", "language": "eng", "script": "Latn"},
				Tracks:     []*pb.TrackInfo{{Id: "t-tam", Title: "Aaja Raja", TrackNumber: 1, DiscNumber: 1}},
			},
			"al-hin": {
				Id:         "al-hin",
				Title:      `Aaja Raja (From "KH x RK Reunion") - Hindi`,
				ArtistName: "Anirudh Ravichander",
				Year:       2026,
				Tracks:     []*pb.TrackInfo{{Id: "t-hin", Title: "Aaja Raja - Hindi", TrackNumber: 1, DiscNumber: 1}},
			},
		},
	}
	svc, q, mgr := newTestScheduler(t, fp)

	fakeITunes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resultCount": 1,
			"results": []map[string]any{
				{
					"wrapperType":      "collection",
					"artistName":       "Anirudh Ravichander",
					"collectionName":   `Aaja Raja (From "KH x RK Reunion") - Single`,
					"primaryGenreName": "Tamil",
				},
			},
		})
	}))
	defer fakeITunes.Close()

	svc.SetLanguageResolver(language.NewResolver(mgr.Cache(), q, language.ResolverConfig{
		ITunesBaseURL: fakeITunes.URL,
		ITunesLimiter: rate.NewLimiter(rate.Inf, 10),
	}))

	since := "2026-11-01T00:00:00Z"
	artist := &models.Artist{
		Name:                  "Anirudh Ravichander",
		Provider:              "test",
		ProviderID:            "art-ani",
		Status:                models.ArtistStatusWatched,
		WatchNewReleases:      true,
		WatchNewReleasesSince: &since,
		Languages:             []string{language.Tamil},
	}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}

	svc.DetectNewReleases(context.Background())

	albums, err := q.ListAlbumsByArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || albums[0].ProviderID != "al-tam" {
		t.Fatalf("expected only Tamil album al-tam added (Hindi dub skipped), got %+v", albums)
	}
}

