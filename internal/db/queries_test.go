package db_test

import (
	"testing"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
)

func newTestQueries(t *testing.T) *db.Queries {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return db.NewQueries(database)
}

func seedTrack(t *testing.T, q *db.Queries) int64 {
	t.Helper()
	artist := &models.Artist{Name: "Test Artist", Provider: "test", ProviderID: "a1", Status: models.ArtistStatusWatched}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	album := &models.Album{ArtistID: artist.ID, Title: "Test Album", Provider: "test", ProviderID: "al1", RecordType: "album", Status: models.AlbumStatusWatched}
	if err := q.CreateAlbum(album); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{AlbumID: album.ID, Title: "Test Track", TrackNumber: 1, DiscNumber: 1, DurationMs: 200000, Provider: "test", ProviderID: "t1", Status: models.TrackStatusWanted}
	if err := q.CreateTrack(track); err != nil {
		t.Fatal(err)
	}
	return track.ID
}

func TestEnqueueDownloadSetsSourceUser(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	if err := q.EnqueueDownload(trackID); err != nil {
		t.Fatal(err)
	}

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected source 'user', got %q", downloads[0].Source)
	}
}

func TestReenqueueDownloadSetsSourceScheduler(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	if err := q.ReenqueueDownload(trackID); err != nil {
		t.Fatal(err)
	}

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Source != "scheduler" {
		t.Errorf("expected source 'scheduler', got %q", downloads[0].Source)
	}
}

func TestEnqueueDownloadBatchSetsSourceUser(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	queued, err := q.EnqueueDownloadBatch([]int64{trackID})
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("expected 1 queued, got %d", queued)
	}

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected source 'user', got %q", downloads[0].Source)
	}
}

func TestListDownloadsWithTrackIncludesSource(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	q.EnqueueDownload(trackID)

	downloads, err := q.ListDownloadsWithTrack("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected source 'user', got %q", downloads[0].Source)
	}
	if downloads[0].Track == nil {
		t.Fatal("expected track info")
	}
}

func TestListRetryableDownloadsIncludesSource(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	q.ReenqueueDownload(trackID)
	q.ScheduleRetry(1, "2000-01-01T00:00:00Z", "test error")

	downloads, err := q.ListRetryableDownloads()
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 retryable download, got %d", len(downloads))
	}
	if downloads[0].Source != "scheduler" {
		t.Errorf("expected source 'scheduler', got %q", downloads[0].Source)
	}
}

func TestReenqueueDeletesOldFailedBeforeInsert(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	q.EnqueueDownload(trackID)
	errMsg := "some error"
	q.UpdateDownloadStatus(1, models.DownloadStatusFailed, nil, &errMsg)

	if err := q.ReenqueueDownload(trackID); err != nil {
		t.Fatal(err)
	}

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download after reenqueue (old failed deleted), got %d", len(downloads))
	}
	if downloads[0].Source != "scheduler" {
		t.Errorf("expected source 'scheduler', got %q", downloads[0].Source)
	}
	if downloads[0].Status != models.DownloadStatusPending {
		t.Errorf("expected status 'pending', got %q", downloads[0].Status)
	}
}

func TestReenqueueDoesNotDuplicateActivePending(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	q.EnqueueDownload(trackID)
	q.ReenqueueDownload(trackID)

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Errorf("expected 1 download (no duplicate), got %d", len(downloads))
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected original source 'user' preserved, got %q", downloads[0].Source)
	}
}

func TestArtistLanguages(t *testing.T) {
	q := newTestQueries(t)

	// Create artist with languages
	artist := &models.Artist{
		Name:       "AR Rahman",
		Provider:   "test",
		ProviderID: "arr1",
		Status:     models.ArtistStatusWatched,
		Languages:  []string{"tamil", "hindi"},
	}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}

	// Fetch artist and verify languages
	fetched, err := q.GetArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Languages) != 2 || fetched.Languages[0] != "tamil" || fetched.Languages[1] != "hindi" {
		t.Errorf("expected ['tamil', 'hindi'], got %v", fetched.Languages)
	}

	// Update languages
	if err := q.UpdateArtistLanguages(artist.ID, []string{"tamil", "telugu"}); err != nil {
		t.Fatal(err)
	}

	fetchedAfter, err := q.GetArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetchedAfter.Languages) != 2 || fetchedAfter.Languages[0] != "tamil" || fetchedAfter.Languages[1] != "telugu" {
		t.Errorf("expected ['tamil', 'telugu'], got %v", fetchedAfter.Languages)
	}

	// Add album and track, test GetTrackWithMeta gets ArtistLanguages
	album := &models.Album{ArtistID: artist.ID, Title: "Roja", Provider: "test", ProviderID: "arr_al1", RecordType: "album", Status: models.AlbumStatusWatched}
	if err := q.CreateAlbum(album); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{AlbumID: album.ID, Title: "Chinna Chinna Aasai", TrackNumber: 1, DiscNumber: 1, DurationMs: 200000, Provider: "test", ProviderID: "arr_t1", Status: models.TrackStatusWanted}
	if err := q.CreateTrack(track); err != nil {
		t.Fatal(err)
	}

	trackWithMeta, err := q.GetTrackWithMeta(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trackWithMeta.ArtistLanguages) != 2 || trackWithMeta.ArtistLanguages[0] != "tamil" || trackWithMeta.ArtistLanguages[1] != "telugu" {
		t.Errorf("expected track ArtistLanguages to be ['tamil', 'telugu'], got %v", trackWithMeta.ArtistLanguages)
	}

	// Test ListWantedTracks gets ArtistLanguages
	wanted, err := q.ListWantedTracks()
	if err != nil {
		t.Fatal(err)
	}
	if len(wanted) != 1 || len(wanted[0].ArtistLanguages) != 2 {
		t.Errorf("expected wanted track with ArtistLanguages, got %v", wanted)
	}
}

func TestListAlbumsByArtistOrdersNewestFirst(t *testing.T) {
	q := newTestQueries(t)
	artist := &models.Artist{Name: "Artist", Provider: "test", ProviderID: "a1", Status: models.ArtistStatusWatched}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	y1997, y2026 := 1997, 2026
	_ = q.CreateAlbum(&models.Album{ArtistID: artist.ID, Title: "Old", Year: &y1997, Provider: "test", ProviderID: "al-old", RecordType: "album", Status: models.AlbumStatusWatched})
	_ = q.CreateAlbum(&models.Album{ArtistID: artist.ID, Title: "Unknown", Year: nil, Provider: "test", ProviderID: "al-nil", RecordType: "album", Status: models.AlbumStatusWatched})
	_ = q.CreateAlbum(&models.Album{ArtistID: artist.ID, Title: "Latest", Year: &y2026, Provider: "test", ProviderID: "al-new", RecordType: "album", Status: models.AlbumStatusWatched})

	albums, err := q.ListAlbumsByArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 3 {
		t.Fatalf("expected 3 albums, got %d", len(albums))
	}
	if albums[0].Title != "Latest" || albums[1].Title != "Old" || albums[2].Title != "Unknown" {
		t.Errorf("unexpected album order: [%s, %s, %s]", albums[0].Title, albums[1].Title, albums[2].Title)
	}
}

func TestSetArtistWatchNewReleasesPreservesExistingSince(t *testing.T) {
	q := newTestQueries(t)
	since := "2025-01-01T00:00:00Z"
	artist := &models.Artist{
		Name:                  "Artist",
		Provider:              "test",
		ProviderID:            "a1",
		Status:                models.ArtistStatusWatched,
		WatchNewReleases:      true,
		WatchNewReleasesSince: &since,
	}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	_ = q.SetArtistWatchNewReleases(artist.ID, false)
	_ = q.SetArtistWatchNewReleases(artist.ID, true)

	got, err := q.GetArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WatchNewReleasesSince == nil || *got.WatchNewReleasesSince != since {
		t.Errorf("expected WatchNewReleasesSince preserved as %q, got %v", since, got.WatchNewReleasesSince)
	}
}

func TestArtistSyncPauseAndResume(t *testing.T) {
	q := newTestQueries(t)
	artist := &models.Artist{
		Name:             "Artist 1",
		Provider:         "test",
		ProviderID:       "a1",
		Status:           models.ArtistStatusWatched,
		WatchNewReleases: true,
	}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	if artist.SyncPaused {
		t.Errorf("expected new artist sync_paused to be false")
	}

	if err := q.SetArtistSyncPaused(artist.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := q.GetArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SyncPaused {
		t.Errorf("expected artist sync_paused to be true")
	}

	if err := q.SetArtistSyncPaused(artist.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err = q.GetArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncPaused {
		t.Errorf("expected artist sync_paused to be false after resume")
	}
}

func TestDownloadPauseAndResume(t *testing.T) {
	q := newTestQueries(t)
	t1 := seedTrack(t, q)

	dlID, err := q.EnqueueDownloadReturningID(t1)
	if err != nil {
		t.Fatal(err)
	}

	// Pause single download
	if err := q.PauseDownload(dlID); err != nil {
		t.Fatal(err)
	}
	dl, err := q.GetDownload(dlID)
	if err != nil {
		t.Fatal(err)
	}
	if dl.Status != models.DownloadStatusPaused {
		t.Errorf("expected download status 'paused', got %q", dl.Status)
	}

	// Resume single download
	if err := q.ResumeDownload(dlID); err != nil {
		t.Fatal(err)
	}
	dl, err = q.GetDownload(dlID)
	if err != nil {
		t.Fatal(err)
	}
	if dl.Status != models.DownloadStatusPending {
		t.Errorf("expected download status 'pending', got %q", dl.Status)
	}

	// Pause all
	affected, err := q.PauseAllDownloads()
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Errorf("expected 1 affected, got %d", affected)
	}
	dl, _ = q.GetDownload(dlID)
	if dl.Status != models.DownloadStatusPaused {
		t.Errorf("expected paused after PauseAllDownloads, got %q", dl.Status)
	}

	// Resume all
	resumed, err := q.ResumeAllDownloads()
	if err != nil {
		t.Fatal(err)
	}
	if resumed != 1 {
		t.Errorf("expected 1 resumed, got %d", resumed)
	}
	dl, _ = q.GetDownload(dlID)
	if dl.Status != models.DownloadStatusPending {
		t.Errorf("expected pending after ResumeAllDownloads, got %q", dl.Status)
	}
}
