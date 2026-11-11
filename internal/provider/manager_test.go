package provider_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"

	"github.com/TheOutdoorProgrammer/crate/internal/cache"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

type stubProvider struct {
	pb.UnimplementedMusicProviderServer
	calls      atomic.Int32
	albumCount atomic.Int32
}

func (s *stubProvider) Info(ctx context.Context, req *pb.InfoRequest) (*pb.InfoResponse, error) {
	return &pb.InfoResponse{Name: "stub", DisplayName: "Stub", Version: "1.0.0"}, nil
}

func (s *stubProvider) GetArtistAlbums(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumList, error) {
	s.calls.Add(1)
	n := int(s.albumCount.Load())
	var albums []*pb.AlbumSummary
	for i := 0; i < n; i++ {
		albums = append(albums, &pb.AlbumSummary{
			Id:    string(rune('a' + i)),
			Title: "Album",
			Year:  2026,
		})
	}
	return &pb.AlbumList{Albums: albums}, nil
}

func (s *stubProvider) GetAlbum(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumDetail, error) {
	s.calls.Add(1)
	if req.Id == "empty" {
		return &pb.AlbumDetail{Id: "empty", Title: "Empty"}, nil
	}
	return &pb.AlbumDetail{
		Id:     req.Id,
		Title:  "Full",
		Tracks: []*pb.TrackInfo{{Id: "t1", Title: "Track 1", TrackNumber: 1, DiscNumber: 1}},
	}, nil
}

func startStubManager(t *testing.T, stub *stubProvider) *provider.Manager {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	pb.RegisterMusicProviderServer(srv, stub)
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

	mgr := provider.NewManager(c, db.NewQueries(database))
	if err := mgr.RegisterProvider(context.Background(), "stub", lis.Addr().String()); err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestManagerFreshBypassesCacheAndSkipsEmptyCache(t *testing.T) {
	stub := &stubProvider{}
	mgr := startStubManager(t, stub)
	ctx := context.Background()

	// Empty discography should NOT be cached
	stub.albumCount.Store(0)
	list, err := mgr.GetArtistAlbums(ctx, "stub", "art1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Albums) != 0 {
		t.Fatalf("expected 0 albums, got %d", len(list.Albums))
	}

	// Next call sees newly populated albums without waiting for TTL
	stub.albumCount.Store(1)
	list, err = mgr.GetArtistAlbums(ctx, "stub", "art1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Albums) != 1 {
		t.Fatalf("expected 1 album (empty response should not poison cache), got %d", len(list.Albums))
	}

	// Cached read returns 1 without calling gRPC
	callsBefore := stub.calls.Load()
	stub.albumCount.Store(2)
	list, err = mgr.GetArtistAlbums(ctx, "stub", "art1")
	if err != nil || len(list.Albums) != 1 || stub.calls.Load() != callsBefore {
		t.Fatalf("expected cached 1 album without new gRPC call, got len=%d calls=%d->%d", len(list.Albums), callsBefore, stub.calls.Load())
	}

	// Fresh read bypasses cache and updates it to 2
	list, err = mgr.GetArtistAlbumsFresh(ctx, "stub", "art1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Albums) != 2 {
		t.Fatalf("expected 2 albums on fresh fetch, got %d", len(list.Albums))
	}

	// Subsequent cached read now returns 2
	list, err = mgr.GetArtistAlbums(ctx, "stub", "art1")
	if err != nil || len(list.Albums) != 2 {
		t.Fatalf("expected refreshed cache to have 2 albums, got %d", len(list.Albums))
	}

	// Empty-track album should not be cached
	callsBefore = stub.calls.Load()
	_, _ = mgr.GetAlbum(ctx, "stub", "empty")
	_, _ = mgr.GetAlbum(ctx, "stub", "empty")
	if stub.calls.Load() != callsBefore+2 {
		t.Errorf("expected empty-track album not to be cached (want %d calls, got %d)", callsBefore+2, stub.calls.Load())
	}
}

