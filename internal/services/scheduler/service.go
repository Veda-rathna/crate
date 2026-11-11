package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/language"
	"github.com/TheOutdoorProgrammer/crate/internal/library"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
	"github.com/TheOutdoorProgrammer/crate/internal/services/downloader"
)

type Service struct {
	queries     *db.Queries
	providers   *provider.Manager
	activityLog *activity.Log
	libraryDir  string
	interval    time.Duration
}

func NewService(queries *db.Queries, providers *provider.Manager, actLog *activity.Log, libraryDir string, interval time.Duration) *Service {
	return &Service{
		queries:     queries,
		providers:   providers,
		activityLog: actLog,
		libraryDir:  libraryDir,
		interval:    interval,
	}
}

func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(s.interval)
	integrityTick := time.NewTicker(24 * time.Hour)
	upgradeTick := time.NewTicker(24 * time.Hour)
	defer tick.Stop()
	defer integrityTick.Stop()
	defer upgradeTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.detectNewReleases(ctx)
			s.queueWantedTracks()
		case <-integrityTick.C:
			s.checkFileIntegrity()
			s.purgeActivityLog()
			s.queries.PurgeExpiredCooldowns()
		case <-upgradeTick.C:
			s.scanForUpgrades()
		}
	}
}

func (s *Service) detectNewReleases(ctx context.Context) {
	artists, err := s.queries.ListWatchedArtists()
	if err != nil {
		slog.Error("scheduler: list watched artists", "error", err)
		return
	}
	for _, artist := range artists {
		if artist.WatchNewReleasesSince == nil {
			continue
		}
		if !s.providers.IsHealthy(artist.Provider) {
			slog.Warn("scheduler: provider unhealthy, skipping artist", "artist", artist.Name, "provider", artist.Provider)
			continue
		}
		sinceTime, _ := time.Parse(time.RFC3339, *artist.WatchNewReleasesSince)
		albumList, err := s.providers.GetArtistAlbums(ctx, artist.Provider, artist.ProviderID)
		if err != nil {
			slog.Error("scheduler: fetch artist albums", "artist", artist.Name, "error", err)
			continue
		}
		added := 0
		for _, pa := range albumList.Albums {
			if existing, _ := s.queries.FindAlbumByProvider(artist.Provider, pa.Id); existing != nil {
				continue
			}
			releaseDate := pa.Metadata["release_date"]
			if releaseDate != "" {
				rd, _ := time.Parse("2006-01-02", releaseDate)
				if rd.Before(sinceTime) {
					continue
				}
			}

			// Check artist language preference on candidate release
			if !language.IsAllLanguages(artist.Languages) {
				summaryLang := language.DetectReleaseSummaryLanguage(pa.Title, pa.Metadata)
				if summaryLang != language.Unknown && !language.MatchesArtistLanguages(artist.Languages, summaryLang) {
					slog.Info("scheduler: skipping release due to language preference",
						"artist", artist.Name, "album", pa.Title, "language", summaryLang, "preferences", artist.Languages)
					continue
				}
			}

			albumDetail, err := s.providers.GetAlbum(ctx, artist.Provider, pa.Id)
			if err != nil {
				continue
			}

			if !language.IsAllLanguages(artist.Languages) {
				detailLang := language.DetectAlbumDetailLanguage(albumDetail)
				if !language.MatchesArtistLanguages(artist.Languages, detailLang) {
					slog.Info("scheduler: skipping release due to language preference",
						"artist", artist.Name, "album", pa.Title, "language", detailLang, "preferences", artist.Languages)
					continue
				}
			}

			year := int(pa.Year)
			var yearPtr *int
			if year > 0 {
				yearPtr = &year
			}
			cover := pa.CoverUrl
			album := models.Album{
				ArtistID:   artist.ID,
				Title:      pa.Title,
				Year:       yearPtr,
				Provider:   artist.Provider,
				ProviderID: pa.Id,
				CoverURL:   strPtrOrNil(cover),
				RecordType: pa.RecordType,
				Status:     models.AlbumStatusWatched,
			}
			if err := s.queries.CreateAlbum(&album); err != nil {
				continue
			}
			for _, pt := range albumDetail.Tracks {
				s.queries.CreateTrack(&models.Track{
					AlbumID:     album.ID,
					Title:       pt.Title,
					TrackNumber: int(pt.TrackNumber),
					DiscNumber:  int(pt.DiscNumber),
					DurationMs:  int(pt.DurationMs),
					Provider:    artist.Provider,
					ProviderID:  pt.Id,
					Status:      models.TrackStatusWanted,
				})
			}
			added++
		}
		if added > 0 {
			slog.Info("scheduler: new releases", "artist", artist.Name, "albums", added)
			s.recordActivity("new_releases", "artist", artist.ID,
				fmt.Sprintf("Detected %d new release(s) for %s", added, artist.Name))
		}
	}
}

func (s *Service) queueWantedTracks() {
	cooldownDays := 7
	if v, err := s.queries.GetSetting("requeue_cooldown_days"); err == nil && v != "" {
		if d, err := strconv.Atoi(v); err == nil && d >= 0 {
			cooldownDays = d
		}
	}

	maxQueue := 50
	if v, err := s.queries.GetSetting("max_auto_queue"); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxQueue = n
		}
	}

	var tracks []models.Track
	var err error
	if cooldownDays > 0 {
		cutoff := time.Now().UTC().Add(-time.Duration(cooldownDays) * 24 * time.Hour).Format(time.RFC3339)
		tracks, err = s.queries.ListWantedTracksWithCooldown(cutoff, maxQueue)
	} else {
		tracks, err = s.queries.ListWantedTracksLimited(maxQueue)
	}
	if err != nil {
		slog.Error("scheduler: list wanted", "error", err)
		return
	}
	queued := 0
	for _, t := range tracks {
		if !language.IsAllLanguages(t.ArtistLanguages) {
			trackLang := language.DetectTrackLanguage(t.AlbumTitle, t.Title)
			if trackLang != language.Unknown && !language.MatchesArtistLanguages(t.ArtistLanguages, trackLang) {
				continue
			}
		}
		if s.queries.ReenqueueDownload(t.ID) == nil {
			queued++
		}
	}
	if queued > 0 {
		slog.Info("scheduler: queued wanted tracks", "count", queued, "limit", maxQueue, "cooldown_days", cooldownDays)
		s.recordActivity("auto_queued", "library", 0,
			fmt.Sprintf("Scheduler queued %d wanted track(s)", queued))
	}
}

func (s *Service) checkFileIntegrity() {
	tracks, err := s.queries.ListOwnedTracksWithPaths()
	if err != nil {
		slog.Error("scheduler: list owned tracks", "error", err)
		return
	}
	reverted := 0
	for _, t := range tracks {
		if t.FilePath == nil {
			continue
		}
		if _, err := os.Stat(library.ResolvePath(s.libraryDir, *t.FilePath)); os.IsNotExist(err) {
			if err := s.queries.UpdateTrackStatus(t.ID, models.TrackStatusWanted); err != nil {
				slog.Error("scheduler: revert track", "track_id", t.ID, "error", err)
				continue
			}
			reverted++
			slog.Warn("scheduler: file missing, reverted to wanted", "track_id", t.ID, "path", *t.FilePath)
		}
	}
	if reverted > 0 {
		slog.Info("scheduler: file integrity check", "reverted", reverted)
		s.recordActivity("file_missing", "library", 0,
			fmt.Sprintf("Integrity check: %d missing track file(s) reverted to wanted", reverted))
	}
}

func (s *Service) scanForUpgrades() {
	tiersJSON, err := s.queries.GetSetting("quality_tiers")
	if err != nil {
		return
	}
	var tiers []models.QualityTier
	if err := json.Unmarshal([]byte(tiersJSON), &tiers); err != nil {
		slog.Error("scheduler: parse quality tiers", "error", err)
		return
	}
	if len(tiers) == 0 {
		return
	}

	lastIDStr, _ := s.queries.GetSetting("upgrade_last_artist_id")
	lastID, _ := strconv.ParseInt(lastIDStr, 10, 64)

	artist, err := s.queries.NextArtistForUpgrade(lastID)
	if err != nil {
		// Wrap around to the beginning
		artist, err = s.queries.NextArtistForUpgrade(0)
		if err != nil {
			return
		}
	}

	_ = s.queries.SetSetting("upgrade_last_artist_id", strconv.FormatInt(artist.ID, 10))

	tracks, err := s.queries.ListOwnedTracksByArtist(artist.ID)
	if err != nil {
		slog.Error("scheduler: list owned tracks for upgrade", "artist", artist.Name, "error", err)
		return
	}

	queued := 0
	for _, t := range tracks {
		if downloader.IsUpgradeable(tiers, &t) {
			if err := s.queries.UpdateTrackStatus(t.ID, models.TrackStatusWanted); err != nil {
				continue
			}
			if err := s.queries.ReenqueueDownload(t.ID); err != nil {
				continue
			}
			queued++
		}
	}
	if queued > 0 {
		slog.Info("scheduler: queued upgrades", "artist", artist.Name, "tracks", queued)
		s.recordActivity("upgrade_queued", "artist", artist.ID,
			fmt.Sprintf("Quality upgrade check queued %d track(s) for %s", queued, artist.Name))
	}
}

func (s *Service) recordActivity(action, entityType string, entityID int64, details string) {
	if s.activityLog != nil {
		s.activityLog.Record(action, entityType, entityID, details)
	}
}

func (s *Service) purgeActivityLog() {
	if s.activityLog == nil {
		return
	}
	days := 30
	if v, err := s.queries.GetSetting("activity_retention_days"); err == nil && v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			days = d
		}
	}
	deleted, err := s.activityLog.Purge(time.Duration(days) * 24 * time.Hour)
	if err != nil {
		slog.Error("scheduler: purge activity log", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("scheduler: purged activity log", "deleted", deleted, "retention_days", days)
	}
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
