package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/language"
	"github.com/TheOutdoorProgrammer/crate/internal/matching"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/services/arcod"
	"github.com/TheOutdoorProgrammer/crate/internal/services/slskd"
)

const defaultMaxConcurrentSlskd = 10

const (
	SourceArcod = "arcod"
	SourceSlskd = "slskd"
)

var DefaultSourcePriority = []string{SourceArcod, SourceSlskd}

// ValidateSourcePriority parses and normalizes a JSON array of download source names.
// Unknown sources or empty lists are rejected; omitted known sources are appended in default order.
func ValidateSourcePriority(raw string) ([]string, error) {
	var parsed []string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("must be a JSON array of source names")
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("must include at least one source")
	}
	seen := make(map[string]bool)
	var out []string
	for _, s := range parsed {
		norm := strings.ToLower(strings.TrimSpace(s))
		if norm != SourceArcod && norm != SourceSlskd {
			return nil, fmt.Errorf("unknown download source %q", s)
		}
		if seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	for _, def := range DefaultSourcePriority {
		if !seen[def] {
			out = append(out, def)
		}
	}
	return out, nil
}

var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func sanitizeFilename(s string) string {
	return strings.TrimSpace(unsafeFilenameChars.ReplaceAllString(s, "_"))
}

var supportedExts = map[string]bool{
	".flac": true,
	".mp3":  true,
	".ogg":  true,
	".opus": true,
	".aac":  true,
	".m4a":  true,
	".wav":  true,
}

var losslessExts = map[string]bool{
	".flac": true,
	".wav":  true,
}

type Organizer interface {
	Organize(track *models.Track) error
}

type PostDownloadNotifier interface {
	TriggerScan(ctx context.Context)
}

type ArcodProvider interface {
	Search(ctx context.Context, query string) ([]arcod.TrackItem, error)
	FindBestTrack(ctx context.Context, artistName, trackTitle string) (*arcod.TrackItem, error)
	FindBestTrackMatch(ctx context.Context, q arcod.MatchQuery) (*arcod.TrackItem, error)
	DownloadTrack(ctx context.Context, trackID int64, quality int, destPath string) (int64, error)
	IsEnabled() bool
}

type arcodCacheEntry struct {
	results  []ManualSearchResult
	cachedAt time.Time
}

type Service struct {
	queries          *db.Queries
	slskd            *slskd.Client
	organizer        Organizer
	activityLog      *activity.Log
	notifiers        []PostDownloadNotifier
	arcod            ArcodProvider
	downloadsDir     string
	arcodSearchCache sync.Map
}

func NewService(queries *db.Queries, slskdClient *slskd.Client, org Organizer, actLog *activity.Log) *Service {
	return &Service{queries: queries, slskd: slskdClient, organizer: org, activityLog: actLog}
}

func (s *Service) SetArcod(a ArcodProvider) {
	s.arcod = a
}

func (s *Service) Arcod() ArcodProvider {
	return s.arcod
}

func (s *Service) SetDownloadsDir(dir string) {
	s.downloadsDir = dir
}

func (s *Service) getDownloadsDir() string {
	if s.downloadsDir != "" {
		return s.downloadsDir
	}
	if v := os.Getenv("CRATE_DOWNLOADS_DIR"); v != "" {
		return v
	}
	return "./downloads"
}

func (s *Service) AddNotifier(n PostDownloadNotifier) {
	s.notifiers = append(s.notifiers, n)
}

func (s *Service) Notifiers() []PostDownloadNotifier {
	return s.notifiers
}

func (s *Service) CancelTransfer(ctx context.Context, d *models.DownloadQueueItem) {
	if d.SlskdSearchID == nil {
		return
	}
	if strings.HasPrefix(*d.SlskdSearchID, "arcod|") {
		return
	}
	switch d.Status {
	case models.DownloadStatusDownloading:
		parts := strings.SplitN(*d.SlskdSearchID, "|", 2)
		if len(parts) == 2 {
			_ = s.slskd.CancelDownload(ctx, parts[0], parts[1])
		}
	case models.DownloadStatusSearching:
		_ = s.slskd.DeleteSearch(ctx, *d.SlskdSearchID)
	}
}

type ProgressItem struct {
	DownloadID       int64   `json:"download_id,omitempty"`
	TrackID          int64   `json:"track_id,omitempty"`
	Username         string  `json:"username"`
	Filename         string  `json:"filename"`
	PercentComplete  float64 `json:"percent_complete"`
	AverageSpeed     float64 `json:"average_speed_bps"`
	BytesTransferred int64   `json:"bytes_transferred"`
	Size             int64   `json:"size"`
	State            string  `json:"state"`
}

func (s *Service) GetProgress(ctx context.Context) ([]ProgressItem, error) {
	activeTransfers := make(map[string]models.DownloadQueueItem)
	downloading, _ := s.queries.ListDownloads("downloading")
	for _, d := range downloading {
		if d.SlskdSearchID != nil {
			activeTransfers[*d.SlskdSearchID] = d
		}
	}

	var items []ProgressItem
	for _, d := range downloading {
		if d.SlskdSearchID != nil && strings.HasPrefix(*d.SlskdSearchID, "arcod|") {
			track, _ := s.queries.GetTrackWithMeta(d.TrackID)
			title := "Unknown"
			if track != nil {
				title = fmt.Sprintf("%s - %s", track.ArtistName, track.Title)
			}
			items = append(items, ProgressItem{
				DownloadID:      d.ID,
				TrackID:         d.TrackID,
				Username:        "arcod (Direct FLAC)",
				Filename:        title + ".flac",
				PercentComplete: 50,
				State:           "Downloading",
			})
		}
	}

	if !s.isSlskdEnabled() {
		return items, nil
	}

	dirs, err := s.slskd.GetAllDownloads(ctx)
	if err != nil {
		return nil, err
	}
	for _, ud := range dirs {
		for _, dir := range ud.Directories {
			for _, f := range dir.Files {
				if strings.Contains(f.State, "Completed") {
					continue
				}
				transferKey := ud.Username + "|" + f.ID
				dl, ok := activeTransfers[transferKey]
				if !ok {
					continue
				}
				items = append(items, ProgressItem{
					DownloadID:       dl.ID,
					TrackID:          dl.TrackID,
					Username:         ud.Username,
					Filename:         filepath.Base(f.Filename),
					PercentComplete:  f.PercentComplete,
					AverageSpeed:     f.AverageSpeed,
					BytesTransferred: f.BytesTransferred,
					Size:             f.Size,
					State:            f.State,
				})
			}
		}
	}
	return items, nil
}

func (s *Service) IsQueuePaused() bool {
	if s.queries == nil {
		return false
	}
	v, err := s.queries.GetSetting("downloads_paused")
	return err == nil && v == "true"
}

func (s *Service) CancelAllActive(ctx context.Context) {
	downloading, _ := s.queries.ListDownloads("downloading")
	for _, d := range downloading {
		s.CancelTransfer(ctx, &d)
	}
	searching, _ := s.queries.ListDownloads("searching")
	for _, d := range searching {
		s.CancelTransfer(ctx, &d)
	}
}

// Run processes the download queue on a ticker. Call in a goroutine.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	if s.IsQueuePaused() {
		return
	}

	retryable, err := s.queries.ListRetryableDownloads()
	if err == nil {
		for _, d := range retryable {
			if d.Attempts >= 4 && d.Source == "scheduler" {
				slog.Info("downloader: removing exhausted scheduler download", "download_id", d.ID)
				s.queries.DeleteDownload(d.ID)
				continue
			}
			slog.Info("downloader: retrying", "download_id", d.ID, "attempts", d.Attempts)
			s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusPending, nil, d.Error)
		}
	}

	maxConcurrent := s.getMaxConcurrent()
	activeCount, err := s.queries.CountActiveDownloads()
	if err != nil {
		slog.Error("downloader: count active", "error", err)
		activeCount = 0
	}
	slots := maxConcurrent - activeCount

	pending, err := s.queries.ListDownloads("pending")
	if err != nil {
		slog.Error("downloader: list pending", "error", err)
		return
	}
	for _, d := range pending {
		if slots <= 0 {
			break
		}
		if d.Attempts >= 4 {
			if d.Source == "scheduler" {
				slog.Info("downloader: removing exhausted scheduler download", "download_id", d.ID)
				s.queries.DeleteDownload(d.ID)
				continue
			}
			reason := "exceeded maximum retry attempts"
			if d.Error != nil && *d.Error != "" {
				reason += ": " + *d.Error
			}
			s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &reason)
			continue
		}
		if err := s.process(ctx, d); err != nil {
			var rlErr *arcod.RateLimitError
			if errors.As(err, &rlErr) {
				slog.Info("downloader: pausing pending batch for arcod rate limit cooldown", "retry_after", rlErr.RetryAfter)
				break
			}
			slog.Error("downloader: process", "download_id", d.ID, "error", err)
		}
		slots--
	}

	searching, err := s.queries.ListDownloads("searching")
	if err != nil {
		return
	}
	for _, d := range searching {
		if err := s.checkSearch(ctx, d); err != nil {
			slog.Error("downloader: check search", "download_id", d.ID, "error", err)
		}
	}

	downloading, err := s.queries.ListDownloads("downloading")
	if err != nil {
		return
	}
	for _, d := range downloading {
		if err := s.checkDownload(ctx, d); err != nil {
			slog.Error("downloader: check download", "download_id", d.ID, "error", err)
		}
	}

	organizing, err := s.queries.ListDownloads("organizing")
	if err != nil {
		return
	}
	for _, d := range organizing {
		if err := s.retryOrganize(d); err != nil {
			slog.Error("downloader: retry organize", "download_id", d.ID, "error", err)
		}
	}
}

func (s *Service) getCooldownDuration() time.Duration {
	if v, err := s.queries.GetSetting("shadow_ban_duration_minutes"); err == nil && v != "" {
		if mins, err := strconv.Atoi(v); err == nil && mins > 0 {
			return time.Duration(mins) * time.Minute
		}
	}
	return 60 * time.Minute
}

func (s *Service) getScoringConfig() scoringConfig {
	cfg := scoringConfig{fallbackEnabled: true}
	if tiersJSON, err := s.queries.GetSetting("quality_tiers"); err == nil && tiersJSON != "" {
		var tiers []models.QualityTier
		if json.Unmarshal([]byte(tiersJSON), &tiers) == nil {
			cfg.tiers = tiers
		}
	}
	if v, err := s.queries.GetSetting("quality_fallback_enabled"); err == nil && v == "false" {
		cfg.fallbackEnabled = false
	}
	if kwJSON, err := s.queries.GetSetting("negative_keywords"); err == nil && kwJSON != "" {
		var keywords []string
		if json.Unmarshal([]byte(kwJSON), &keywords) == nil {
			cfg.negativeKeywords = keywords
		}
	}
	if v, err := s.queries.GetSetting("lang_allow_unknown"); err == nil && strings.EqualFold(strings.TrimSpace(v), "true") {
		cfg.allowUnknown = true
	}
	return cfg
}

func (s *Service) getMaxConcurrent() int {
	v, err := s.queries.GetSetting("max_concurrent_slskd")
	if err != nil {
		return defaultMaxConcurrentSlskd
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultMaxConcurrentSlskd
	}
	return n
}

func (s *Service) isArcodEnabled() bool {
	if s.arcod == nil || !s.arcod.IsEnabled() {
		return false
	}
	if s.queries != nil {
		if v, err := s.queries.GetSetting("arcod_enabled"); err == nil && (v == "false" || v == "0") {
			return false
		}
	}
	return true
}

func (s *Service) isSlskdEnabled() bool {
	if s.slskd == nil {
		return false
	}
	if s.queries != nil {
		if v, err := s.queries.GetSetting("slskd_enabled"); err == nil && (v == "false" || v == "0") {
			return false
		}
	}
	return true
}

func (s *Service) sourcePriority() []string {
	if s.queries != nil {
		if raw, err := s.queries.GetSetting("download_source_priority"); err == nil && strings.TrimSpace(raw) != "" {
			if p, err := ValidateSourcePriority(raw); err == nil {
				return p
			}
		}
	}
	return append([]string(nil), DefaultSourcePriority...)
}

func (s *Service) sourceRank(name string) int {
	for i, src := range s.sourcePriority() {
		if src == name {
			return i
		}
	}
	return len(DefaultSourcePriority)
}

func (s *Service) enabledSources() []string {
	var enabled []string
	for _, src := range s.sourcePriority() {
		switch src {
		case SourceArcod:
			if s.isArcodEnabled() {
				enabled = append(enabled, src)
			}
		case SourceSlskd:
			if s.isSlskdEnabled() {
				enabled = append(enabled, src)
			}
		}
	}
	return enabled
}

func (s *Service) shouldFallbackToArcodAfterSlskd() bool {
	return s.isArcodEnabled() && s.sourceRank(SourceSlskd) < s.sourceRank(SourceArcod)
}

type arcodOutcome int

const (
	arcodDownloaded arcodOutcome = iota
	arcodOrganizePending
	arcodNoMatch
	arcodRateLimited
	arcodFailed
)

func (s *Service) deferRateLimitedRetry(d models.DownloadQueueItem, err error) error {
	wait := 60 * time.Second
	var rlErr *arcod.RateLimitError
	if errors.As(err, &rlErr) && rlErr.RetryAfter > 0 {
		wait = rlErr.RetryAfter + 2*time.Second
	}
	retryAt := time.Now().UTC().Add(wait).Format(time.RFC3339)
	slog.Info("downloader: arcod rate limited, deferring retry without counting attempt",
		"download_id", d.ID, "retry_in", wait.Round(time.Second))
	_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
	return s.queries.ScheduleDeferredRetry(d.ID, retryAt, err.Error())
}

// process kicks off a download attempt across enabled sources in priority order.
func (s *Service) process(ctx context.Context, d models.DownloadQueueItem) error {
	track, err := s.queries.GetTrackWithMeta(d.TrackID)
	if err != nil {
		return err
	}

	sources := s.enabledSources()
	if len(sources) == 0 {
		errStr := "all download sources are disabled"
		_ = s.queries.UpdateTrackStatus(track.ID, models.TrackStatusWanted)
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
	}

	if sources[0] == SourceArcod {
		outcome, arcodErr := s.tryArcodDownload(ctx, d, track)
		if outcome == arcodDownloaded || outcome == arcodOrganizePending {
			return nil
		}
		if outcome == arcodRateLimited {
			if deferErr := s.deferRateLimitedRetry(d, arcodErr); deferErr != nil {
				return deferErr
			}
			return arcodErr
		}
		if s.isSlskdEnabled() {
			return s.startSlskdSearch(ctx, d, track)
		}
		if outcome == arcodNoMatch {
			errStr := "no match found on arcod"
			_ = s.queries.UpdateTrackStatus(track.ID, models.TrackStatusWanted)
			return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
		}
		msg := "arcod download failed"
		if arcodErr != nil {
			msg = arcodErr.Error()
		}
		return s.failWithRetry(d, msg)
	}

	// Primary is slskd.
	if err := s.startSlskdSearch(ctx, d, track); err != nil {
		if s.isArcodEnabled() {
			outcome, arcodErr := s.tryArcodDownload(ctx, d, track)
			if outcome == arcodDownloaded || outcome == arcodOrganizePending {
				return nil
			}
			if outcome == arcodRateLimited {
				if deferErr := s.deferRateLimitedRetry(d, arcodErr); deferErr != nil {
					return deferErr
				}
				return arcodErr
			}
		}
		return s.failWithRetry(d, err.Error())
	}
	return nil
}

func (s *Service) startSlskdSearch(ctx context.Context, d models.DownloadQueueItem, track *models.Track) error {
	query := matching.CleanSearchQuery(track.ArtistName, track.Title)
	slog.Info("downloader: searching", "query", query, "track_id", track.ID)
	s.logActivity("search_started", "track", track.ID,
		fmt.Sprintf("Searching for: %s - %s", track.ArtistName, track.Title))

	search, err := s.slskd.StartSearch(ctx, query)
	if err != nil {
		return err
	}

	return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusSearching, &search.ID, nil)
}

func (s *Service) tryArcodDownload(ctx context.Context, d models.DownloadQueueItem, track *models.Track) (arcodOutcome, error) {
	slog.Info("downloader: checking arcod source", "artist", track.ArtistName, "title", track.Title, "album", track.AlbumTitle, "track_id", track.ID)
	arcodTrack, err := s.arcod.FindBestTrackMatch(ctx, arcod.MatchQuery{
		Artist:     track.ArtistName,
		Title:      track.Title,
		Album:      track.AlbumTitle,
		DurationMs: track.DurationMs,
	})
	if err != nil {
		var rlErr *arcod.RateLimitError
		if errors.As(err, &rlErr) {
			slog.Warn("downloader: arcod search rate limited", "retry_after", rlErr.RetryAfter, "track_id", track.ID)
			return arcodRateLimited, err
		}
		slog.Warn("downloader: arcod search error", "error", err, "track_id", track.ID)
		return arcodFailed, err
	}
	if arcodTrack == nil {
		slog.Info("downloader: no match on arcod", "artist", track.ArtistName, "title", track.Title)
		return arcodNoMatch, nil
	}

	slog.Info("downloader: match found on arcod, downloading lossless FLAC", "track_id", track.ID, "arcod_id", arcodTrack.ID, "title", arcodTrack.Title)
	s.logActivity("download_started", "track", track.ID,
		fmt.Sprintf("Downloading FLAC from arcod: %s - %s", track.ArtistName, track.Title))

	destFileName := sanitizeFilename(fmt.Sprintf("%d - %s - %s.flac", track.ID, track.ArtistName, track.Title))
	destPath := filepath.Join(s.getDownloadsDir(), destFileName)

	arcodKey := fmt.Sprintf("arcod|%d", arcodTrack.ID)
	_ = s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusDownloading, &arcodKey, nil)
	_ = s.queries.UpdateTrackStatus(track.ID, models.TrackStatusDownloading)
	_ = s.queries.UpdateTrackDownloadedFrom(track.ID, "arcod")
	_ = s.queries.UpdateTrackDownloadedFilename(track.ID, destFileName)
	_ = s.queries.UpdateTrackQuality(track.ID, "flac", 0)

	_, err = s.arcod.DownloadTrack(ctx, arcodTrack.ID, arcod.DefaultQuality, destPath)
	if err != nil {
		slog.Warn("downloader: arcod stream download failed", "error", err, "track_id", track.ID)
		_ = os.Remove(destPath)
		_ = s.queries.UpdateTrackStatus(track.ID, models.TrackStatusWanted)
		return arcodFailed, err
	}

	if err := s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusOrganizing, &arcodKey, nil); err != nil {
		slog.Error("downloader: update status to organizing failed", "error", err)
		return arcodFailed, err
	}

	trackCopy := *track
	trackCopy.FilePath = &destFileName
	if err := s.organizer.Organize(&trackCopy); err != nil {
		slog.Error("downloader: organize arcod download failed, will retry", "filename", destFileName, "track_id", track.ID, "error", err)
		return arcodOrganizePending, nil // remain in organizing state so retryOrganize handles it
	}

	s.logActivity("download_complete", "track", track.ID,
		fmt.Sprintf("Downloaded lossless FLAC from arcod: %s - %s", track.ArtistName, track.Title))
	for _, n := range s.notifiers {
		n.TriggerScan(ctx)
	}
	_ = s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusComplete, &arcodKey, nil)
	return arcodDownloaded, nil
}

// checkSearch polls a search and initiates download when results are ready.
func (s *Service) checkSearch(ctx context.Context, d models.DownloadQueueItem) error {
	if d.SlskdSearchID == nil {
		errStr := "missing search id"
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
	}

	search, err := s.slskd.GetSearch(ctx, *d.SlskdSearchID)
	if err != nil {
		return err
	}

	if !search.IsComplete {
		if d.LastAttempt != nil && time.Since(mustParseTime(*d.LastAttempt)) < 5*time.Minute {
			return nil
		}
	}

	track, err := s.queries.GetTrackWithMeta(d.TrackID)
	if err != nil {
		return err
	}

	cfg := s.getScoringConfig()
	cfg.excludeNegative = true
	best := pickBestFile(search.Responses, track, s.queries, cfg)
	if best == nil {
		_ = s.slskd.DeleteSearch(ctx, *d.SlskdSearchID)
		if s.shouldFallbackToArcodAfterSlskd() {
			outcome, arcodErr := s.tryArcodDownload(ctx, d, track)
			if outcome == arcodDownloaded || outcome == arcodOrganizePending {
				return nil
			}
			if outcome == arcodRateLimited {
				return s.deferRateLimitedRetry(d, arcodErr)
			}
		}
		errStr := "no suitable file found"
		s.logActivity("download_failed", "track", track.ID,
			fmt.Sprintf("No results for: %s - %s", track.ArtistName, track.Title))
		_ = s.queries.UpdateTrackStatus(track.ID, models.TrackStatusWanted)
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
	}

	slog.Info("downloader: starting download",
		"username", best.username,
		"filename", best.file.Filename,
		"track_id", track.ID,
	)
	s.logActivity("download_started", "track", track.ID,
		fmt.Sprintf("Downloading from %s: %s", best.username, filepath.Base(best.file.Filename)))

	_ = s.queries.UpdateTrackDownloadedFrom(track.ID, best.username)
	_ = s.queries.UpdateTrackDownloadedFilename(track.ID, best.file.Filename)

	ext := strings.ToLower(filepath.Ext(best.file.Filename))
	if format := strings.TrimPrefix(ext, "."); format != "" {
		_ = s.queries.UpdateTrackQuality(track.ID, format, best.file.BitRate)
	}

	transfer, err := s.slskd.StartDownload(ctx, best.username, best.file.Filename, best.file.Size)
	if err != nil {
		dur := s.getCooldownDuration()
		s.queries.CooldownUser(best.username, "download failed: "+err.Error(), dur)
		s.logActivity("user_cooldown", "track", track.ID,
			fmt.Sprintf("Temporarily blocked peer %s (%s): %s", best.username, dur, err.Error()))
		slog.Info("downloader: download start failed, shadow banning user", "username", best.username, "error", err)
		return s.failWithRetry(d, err.Error())
	}

	_ = s.slskd.DeleteSearch(ctx, *d.SlskdSearchID)

	// Store transfer ID in slskd_search_id field (reuse the column)
	transferKey := best.username + "|" + transfer.ID
	if err := s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusDownloading, &transferKey, nil); err != nil {
		return err
	}
	return s.queries.UpdateTrackStatus(track.ID, models.TrackStatusDownloading)
}

// checkDownload polls an active transfer and marks complete when done.
func (s *Service) checkDownload(ctx context.Context, d models.DownloadQueueItem) error {
	if d.SlskdSearchID == nil {
		return nil
	}
	if strings.HasPrefix(*d.SlskdSearchID, "arcod|") {
		return nil
	}

	parts := strings.SplitN(*d.SlskdSearchID, "|", 2)
	if len(parts) != 2 {
		return nil
	}
	username, transferID := parts[0], parts[1]

	transfer, err := s.slskd.GetDownload(ctx, username, transferID)
	if err != nil {
		// Transfer may have been cleaned up; mark failed so it retries
		errStr := err.Error()
		_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
	}

	state := transfer.State
	switch {
	case strings.Contains(state, "Succeeded"):
		rawName := transfer.Filename
		if idx := strings.LastIndexAny(rawName, `/\`); idx >= 0 {
			rawName = rawName[idx+1:]
		}
		if err := s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusOrganizing, d.SlskdSearchID, nil); err != nil {
			return err
		}
		slog.Info("downloader: organizing", "filename", rawName, "track_id", d.TrackID)
		track, err := s.queries.GetTrackWithMeta(d.TrackID)
		if err != nil {
			return err
		}
		track.FilePath = &rawName

		if err := s.organizer.Organize(track); err != nil {
			slog.Error("downloader: organize failed, will retry", "filename", rawName, "track_id", d.TrackID, "error", err)
			return err
		}
		s.logActivity("download_complete", "track", d.TrackID,
			fmt.Sprintf("Downloaded: %s - %s from %s", track.ArtistName, track.Title, username))
		for _, n := range s.notifiers {
			n.TriggerScan(ctx)
		}
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusComplete, d.SlskdSearchID, nil)

	case strings.Contains(state, "Errored"), strings.Contains(state, "Rejected"), strings.Contains(state, "Cancelled"):
		_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
		_ = s.queries.BlacklistFile(username, transfer.Filename, "transfer "+state)
		slog.Info("downloader: blacklisted file", "username", username, "filename", filepath.Base(transfer.Filename))
		s.logActivity("download_failed", "track", d.TrackID,
			fmt.Sprintf("Transfer failed: %s from %s (blacklisted)", state, username))
		return s.failWithRetry(d, "transfer failed: "+state)
	}

	if transfer.BytesTransferred > d.LastProgressBytes {
		_ = s.queries.UpdateDownloadProgress(d.ID, transfer.BytesTransferred)
		return nil
	}

	timeout := staleTimeoutForState(state)
	if d.LastAttempt != nil {
		lastProgress := mustParseTime(*d.LastAttempt)
		if !lastProgress.IsZero() && time.Since(lastProgress) > timeout {
			_ = s.slskd.CancelDownload(ctx, username, transferID)
			_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)

			reason := fmt.Sprintf("stale transfer (%s): no progress for %s", state, timeout)
			if strings.Contains(state, "Queued") || strings.Contains(state, "Requested") {
				s.queries.CooldownUser(username, reason, s.getCooldownDuration())
				slog.Info("downloader: stale queued transfer, shadow banning user", "username", username, "state", state, "download_id", d.ID)
			} else {
				_ = s.queries.BlacklistFile(username, transfer.Filename, reason)
				slog.Info("downloader: stale transfer, blacklisting file", "username", username, "filename", filepath.Base(transfer.Filename), "download_id", d.ID)
			}
			s.logActivity("download_failed", "track", d.TrackID,
				fmt.Sprintf("Stale transfer (%s, %s) from %s", state, timeout, username))
			return s.failWithRetry(d, reason)
		}
	}
	return nil
}

// retryOrganize is called for downloads stuck in "organizing" (e.g. after a crash).
// It re-derives the filename from the slskd transfer key and retries the move.
func (s *Service) retryOrganize(d models.DownloadQueueItem) error {
	if d.SlskdSearchID == nil {
		return nil
	}
	parts := strings.SplitN(*d.SlskdSearchID, "|", 2)
	if len(parts) != 2 {
		return nil
	}
	// The transfer key is "username|transferID". We stored it before transitioning
	// to organizing, so parse the original slskd filename from the transfer record.
	// We don't have the filename directly, so look it up from the track if available,
	// otherwise fall back to a no-op (the file is either already moved or needs a manual fix).
	track, err := s.queries.GetTrackWithMeta(d.TrackID)
	if err != nil {
		return err
	}
	if track.FilePath == nil {
		// No filename recorded yet; nothing to retry
		return nil
	}
	slog.Info("downloader: retrying organize", "filename", *track.FilePath, "track_id", d.TrackID)
	if err := s.organizer.Organize(track); err != nil {
		slog.Error("downloader: retry organize failed", "filename", *track.FilePath, "track_id", d.TrackID, "error", err)
		return err
	}
	return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusComplete, d.SlskdSearchID, nil)
}

func (s *Service) failWithRetry(d models.DownloadQueueItem, errMsg string) error {
	backoff := retryDelay(d.Attempts)
	// 5m + 15m + 30m + 1h = 1h50m cumulative at attempt 4; stop after ~2h
	if d.Attempts >= 4 {
		if s.shouldFallbackToArcodAfterSlskd() {
			if track, err := s.queries.GetTrackWithMeta(d.TrackID); err == nil && track != nil {
				outcome, _ := s.tryArcodDownload(context.Background(), d, track)
				if outcome == arcodDownloaded || outcome == arcodOrganizePending {
					return nil
				}
			}
		}
		_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
		if d.Source == "scheduler" {
			slog.Info("downloader: removing exhausted scheduler download", "download_id", d.ID)
			return s.queries.DeleteDownload(d.ID)
		}
		slog.Warn("downloader: giving up after 2h of retries", "download_id", d.ID, "attempts", d.Attempts)
		errFinal := errMsg + " (giving up after 2h)"
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errFinal)
	}
	retryAt := time.Now().UTC().Add(backoff).Format(time.RFC3339)
	slog.Info("downloader: scheduling retry", "download_id", d.ID, "attempts", d.Attempts, "retry_in", backoff)
	if track, err := s.queries.GetTrackWithMeta(d.TrackID); err == nil && track != nil {
		s.logActivity("retry_scheduled", "track", d.TrackID,
			fmt.Sprintf("Scheduled retry %d/4 in %s for %s - %s: %s", d.Attempts+1, backoff, track.ArtistName, track.Title, errMsg))
	}
	return s.queries.ScheduleRetry(d.ID, retryAt, errMsg)
}

func retryDelay(attempts int) time.Duration {
	delays := []time.Duration{
		5 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
		1 * time.Hour,
	}
	if attempts >= len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempts]
}

func (s *Service) logActivity(action, entityType string, entityID int64, details string) {
	if s.activityLog != nil {
		s.activityLog.Record(action, entityType, entityID, details)
	}
}

// ManualSearchResult is a scored slskd result returned to the frontend.
type ManualSearchResult struct {
	Username      string `json:"username"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	BitRate       int    `json:"bit_rate"`
	SampleRate    int    `json:"sample_rate"`
	BitDepth      int    `json:"bit_depth"`
	Duration      int    `json:"duration"`
	Format        string `json:"format"`
	Score         int    `json:"score"`
	FreeSlot      bool   `json:"free_slot"`
	QueueLength   int    `json:"queue_length"`
	Blacklisted   bool   `json:"blacklisted"`
	NegativeMatch    bool   `json:"negative_match"`
	Locked           bool   `json:"locked"`
	LanguageMismatch bool   `json:"language_mismatch"`
	DetectedLanguage string `json:"detected_language,omitempty"`
}

const arcodOnlySearchID = "arcod-only"

// ManualSearch runs a search across enabled download sources and returns scored results.
// StartManualSearch kicks off an slskd search (or returns a virtual search ID if only Arcod is enabled).
// If customQuery is non-empty, it is used instead of the default "artist title" query.
func (s *Service) StartManualSearch(ctx context.Context, trackID int64, customQuery string) (string, string, error) {
	track, err := s.queries.GetTrackWithMeta(trackID)
	if err != nil {
		return "", "", err
	}
	query := matching.CleanSearchQuery(track.ArtistName, track.Title)
	if customQuery != "" {
		query = customQuery
	}

	slskdOn := s.isSlskdEnabled()
	arcodOn := s.isArcodEnabled()
	if !slskdOn && !arcodOn {
		return "", "", fmt.Errorf("all download sources are disabled")
	}
	if !slskdOn {
		return arcodOnlySearchID, query, nil
	}

	search, err := s.slskd.StartSearch(ctx, query)
	if err != nil {
		if arcodOn {
			slog.Warn("downloader: slskd manual search failed, falling back to arcod-only", "error", err)
			return arcodOnlySearchID, query, nil
		}
		return "", "", fmt.Errorf("start search: %w", err)
	}
	return search.ID, query, nil
}

// ManualSearchResponse holds scored results plus completion status.
type ManualSearchResponse struct {
	Results    []ManualSearchResult `json:"results"`
	IsComplete bool                 `json:"is_complete"`
	FileCount  int                  `json:"file_count"`
}

// PollManualSearch returns the current scored results for an in-progress search,
// ordering source groups according to download_source_priority.
func (s *Service) PollManualSearch(ctx context.Context, trackID int64, searchID string) (*ManualSearchResponse, error) {
	track, err := s.queries.GetTrackWithMeta(trackID)
	if err != nil {
		return nil, err
	}

	var arcodRes []ManualSearchResult
	if s.isArcodEnabled() {
		arcodRes = s.getArcodManualResults(ctx, trackID, track)
	}

	if searchID == arcodOnlySearchID {
		if arcodRes == nil {
			arcodRes = []ManualSearchResult{}
		}
		return &ManualSearchResponse{
			Results:    arcodRes,
			IsComplete: true,
			FileCount:  len(arcodRes),
		}, nil
	}

	search, err := s.slskd.GetSearch(ctx, searchID)
	if err != nil {
		return nil, fmt.Errorf("get search: %w", err)
	}

	cfg := s.getScoringConfig()
	cfg.fallbackEnabled = true
	cfg.includeAll = true // manual search surfaces every slskd result; the user picks
	candidates := scoreCandidates(search.Responses, track, nil, cfg)

	slskdRes := formatCandidates(candidates, s.queries, cfg.negativeKeywords, track)
	var formatted []ManualSearchResult
	if s.sourceRank(SourceSlskd) < s.sourceRank(SourceArcod) {
		formatted = append(formatted, slskdRes...)
		formatted = append(formatted, arcodRes...)
	} else {
		formatted = append(formatted, arcodRes...)
		formatted = append(formatted, slskdRes...)
	}
	if formatted == nil {
		formatted = []ManualSearchResult{}
	}

	return &ManualSearchResponse{
		Results:    formatted,
		IsComplete: search.IsComplete,
		FileCount:  search.FileCount,
	}, nil
}

func (s *Service) getArcodManualResults(ctx context.Context, trackID int64, track *models.Track) []ManualSearchResult {
	if val, ok := s.arcodSearchCache.Load(trackID); ok {
		entry := val.(arcodCacheEntry)
		if time.Since(entry.cachedAt) < 2*time.Minute {
			return entry.results
		}
	}

	query := matching.CleanSearchQuery(track.ArtistName, track.Title)
	items, err := s.arcod.Search(ctx, query)
	if err != nil {
		slog.Warn("downloader: arcod manual search error", "error", err)
		return nil
	}

	var results []ManualSearchResult
	for _, item := range items {
		if !item.Streamable {
			continue
		}
		bitDepth := item.MaximumBitDepth
		if bitDepth == 0 {
			bitDepth = 16
		}
		sampleRate := int(item.MaximumSamplingRate * 1000)
		if sampleRate == 0 {
			sampleRate = 44100
		}
		spec := fmt.Sprintf("%d-bit/%.1fkHz", bitDepth, item.MaximumSamplingRate)
		if item.MaximumSamplingRate == float64(int(item.MaximumSamplingRate)) {
			spec = fmt.Sprintf("%d-bit/%dkHz", bitDepth, int(item.MaximumSamplingRate))
		}
		filename := fmt.Sprintf("arcod:%d/[FLAC %s] %s - %s.flac", item.ID, spec, item.Artist, item.Title)
		score := 120
		if bitDepth >= 24 && sampleRate >= 176400 {
			score = 135
		} else if bitDepth >= 24 && sampleRate >= 88200 {
			score = 130
		} else if bitDepth >= 24 {
			score = 126
		} else if sampleRate >= 48000 {
			score = 122
		}
		results = append(results, ManualSearchResult{
			Username:      "arcod (Direct FLAC)",
			Filename:      filename,
			Size:          int64(item.Duration * 1000 * 150),
			BitRate:       0,
			SampleRate:    sampleRate,
			BitDepth:      bitDepth,
			Duration:      item.Duration,
			Format:        "FLAC",
			Score:         score,
			FreeSlot:      true,
			QueueLength:   0,
			Blacklisted:   false,
			NegativeMatch: false,
			Locked:        false,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].BitDepth != results[j].BitDepth {
			return results[i].BitDepth > results[j].BitDepth
		}
		return results[i].SampleRate > results[j].SampleRate
	})

	s.arcodSearchCache.Store(trackID, arcodCacheEntry{
		results:  results,
		cachedAt: time.Now(),
	})
	return results
}

// CleanupSearch deletes an slskd search.
func (s *Service) CleanupSearch(ctx context.Context, searchID string) {
	if searchID == "" || searchID == arcodOnlySearchID || !s.isSlskdEnabled() {
		return
	}
	_ = s.slskd.DeleteSearch(ctx, searchID)
}

func formatCandidates(candidates []candidate, q *db.Queries, negativeKeywords []string, track *models.Track) []ManualSearchResult {
	results := make([]ManualSearchResult, 0, len(candidates))
	for _, c := range candidates {
		ext := strings.ToLower(filepath.Ext(c.file.Filename))
		if ext == "" {
			ext = inferExt(strings.ToLower(c.file.Filename))
		}
		format := strings.TrimPrefix(ext, ".")
		if format == "mp3" && c.file.BitRate > 0 {
			format = fmt.Sprintf("MP3 %dkbps", c.file.BitRate)
		} else {
			format = strings.ToUpper(format)
		}

		albumTitle := ""
		trackTitle := ""
		if track != nil {
			albumTitle = track.AlbumTitle
			trackTitle = track.Title
		}
		candLang := language.DetectCandidateFileLanguage(c.file.Filename, albumTitle, trackTitle)
		langMismatch := false
		if track != nil && !language.IsAllLanguages(track.ArtistLanguages) {
			if candLang != language.Unknown && !language.MatchesArtistLanguages(track.ArtistLanguages, candLang) {
				langMismatch = true
			}
		}

		var detectedLang string
		if candLang != language.Unknown {
			detectedLang = candLang
		}

		results = append(results, ManualSearchResult{
			Username:         c.username,
			Filename:         c.file.Filename,
			Size:             c.file.Size,
			BitRate:          c.file.BitRate,
			SampleRate:       c.file.SampleRate,
			BitDepth:         c.file.BitDepth,
			Duration:         c.file.Length,
			Format:           format,
			Score:            c.score,
			FreeSlot:         c.freeSlot,
			QueueLength:      c.queueLength,
			Blacklisted:      q != nil && q.IsBlacklisted(c.username, c.file.Filename),
			NegativeMatch:    matchesNegativeKeyword(strings.ToLower(c.file.Filename), negativeKeywords),
			Locked:           c.file.IsLocked,
			LanguageMismatch: langMismatch,
			DetectedLanguage: detectedLang,
		})
	}
	return results
}

// ManualDownload starts a download of a specific file from a specific user.
func (s *Service) ManualDownload(ctx context.Context, trackID int64, username, filename string, size int64, bitrate int) error {
	track, err := s.queries.GetTrackWithMeta(trackID)
	if err != nil {
		return err
	}

	if strings.HasPrefix(username, "arcod") || strings.HasPrefix(filename, "arcod:") {
		if !s.isArcodEnabled() {
			return fmt.Errorf("arcod is disabled in settings")
		}
		var arcodID int64
		if strings.HasPrefix(filename, "arcod:") {
			rest := strings.TrimPrefix(filename, "arcod:")
			if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
				arcodID, _ = strconv.ParseInt(rest[:slashIdx], 10, 64)
			}
		}
		if arcodID == 0 {
			best, err := s.arcod.FindBestTrackMatch(ctx, arcod.MatchQuery{
				Artist:     track.ArtistName,
				Title:      track.Title,
				Album:      track.AlbumTitle,
				DurationMs: track.DurationMs,
			})
			if err == nil && best != nil {
				arcodID = best.ID
			}
		}
		if arcodID == 0 {
			return fmt.Errorf("arcod track ID not found for %s - %s", track.ArtistName, track.Title)
		}

		destFileName := sanitizeFilename(fmt.Sprintf("%d - %s - %s.flac", track.ID, track.ArtistName, track.Title))
		_ = s.queries.UpdateTrackDownloadedFrom(trackID, "arcod")
		_ = s.queries.UpdateTrackDownloadedFilename(trackID, destFileName)
		_ = s.queries.UpdateTrackQuality(trackID, "flac", 0)

		dlID, err := s.queries.EnqueueDownloadReturningID(trackID)
		if err != nil {
			return err
		}

		transferKey := fmt.Sprintf("arcod|%d", arcodID)
		_ = s.queries.UpdateDownloadStatus(dlID, models.DownloadStatusDownloading, &transferKey, nil)
		_ = s.queries.UpdateTrackStatus(trackID, models.TrackStatusDownloading)

		destPath := filepath.Join(s.getDownloadsDir(), destFileName)
		_, err = s.arcod.DownloadTrack(ctx, arcodID, arcod.DefaultQuality, destPath)
		if err != nil {
			errStr := err.Error()
			_ = s.queries.UpdateTrackStatus(trackID, models.TrackStatusWanted)
			_ = s.queries.UpdateDownloadStatus(dlID, models.DownloadStatusFailed, nil, &errStr)
			return fmt.Errorf("arcod download: %w", err)
		}

		_ = s.queries.UpdateDownloadStatus(dlID, models.DownloadStatusOrganizing, &transferKey, nil)
		trackCopy := *track
		trackCopy.FilePath = &destFileName
		if err := s.organizer.Organize(&trackCopy); err != nil {
			return fmt.Errorf("organize: %w", err)
		}

		s.logActivity("download_complete", "track", trackID,
			fmt.Sprintf("Manual download from arcod: %s - %s", track.ArtistName, track.Title))
		for _, n := range s.notifiers {
			n.TriggerScan(ctx)
		}
		_ = s.queries.UpdateDownloadStatus(dlID, models.DownloadStatusComplete, &transferKey, nil)
		return nil
	}

	if !s.isSlskdEnabled() {
		return fmt.Errorf("slskd is disabled in settings")
	}

	_ = s.queries.UpdateTrackDownloadedFrom(trackID, username)
	_ = s.queries.UpdateTrackDownloadedFilename(trackID, filename)

	ext := strings.ToLower(filepath.Ext(filename))
	if format := strings.TrimPrefix(ext, "."); format != "" {
		_ = s.queries.UpdateTrackQuality(trackID, format, bitrate)
	}

	transfer, err := s.slskd.StartDownload(ctx, username, filename, size)
	if err != nil {
		return fmt.Errorf("start download: %w", err)
	}

	dlID, err := s.queries.EnqueueDownloadReturningID(trackID)
	if err != nil {
		return err
	}

	transferKey := username + "|" + transfer.ID
	_ = s.queries.UpdateDownloadStatus(dlID, models.DownloadStatusDownloading, &transferKey, nil)
	_ = s.queries.UpdateTrackStatus(trackID, models.TrackStatusDownloading)

	s.logActivity("download_started", "track", trackID,
		fmt.Sprintf("Manual download from %s: %s - %s", username, track.ArtistName, track.Title))

	return nil
}

func scoreCandidates(results []slskd.SearchResult, track *models.Track, ac availabilityChecker, cfg scoringConfig) []candidate {
	var title, artist, album string
	var durationMs int
	if track != nil {
		title = track.Title
		artist = track.ArtistName
		album = track.AlbumTitle
		durationMs = track.DurationMs
	}

	var candidates []candidate

	for _, result := range results {
		if ac != nil && ac.IsUserCooledDown(result.Username) {
			continue
		}
		for _, f := range result.Files {
			if f.IsLocked && !cfg.includeAll {
				continue
			}
			if ac != nil && ac.IsBlacklisted(result.Username, f.Filename) {
				continue
			}
			nameLower := strings.ToLower(f.Filename)
			// Manual search (includeAll) returns everything slskd found and lets the
			// user pick; the title/artist/negative filters below are auto-download
			// guards that must not silently drop results in the manual flow.
			if !cfg.includeAll {
				if !matching.MatchTitleInPath(f.Filename, title) {
					continue
				}
				if cfg.requireArtist && !matching.MatchArtistInPath(f.Filename, artist) {
					continue
				}
				if cfg.excludeNegative && matchesNegativeKeyword(nameLower, cfg.negativeKeywords) {
					continue
				}
				if track != nil && !language.IsAllLanguages(track.ArtistLanguages) {
					candLang := language.DetectCandidateFileLanguage(f.Filename, track.AlbumTitle, track.Title)
					if !language.MatchesArtistLanguagesWithPolicy(track.ArtistLanguages, candLang, cfg.allowUnknown) {
						continue
					}
				}
			}

			ext := strings.ToLower(filepath.Ext(f.Filename))
			if ext == "" {
				ext = inferExt(nameLower)
			}
			if ext == "" || !supportedExts[ext] {
				// Unknown/unsupported formats can't be quality-scored for an
				// unattended download, but manual search still surfaces them.
				if !cfg.includeAll {
					continue
				}
			}

			qualityScore, allowed := tierBasedScore(ext, f.BitRate, cfg)
			if !allowed && !cfg.includeAll {
				continue
			}
			score := qualityScore
			if losslessExts[ext] {
				score += losslessQualityBonus(nameLower, f)
			}
			// Keep artist bonus below the tier gap (25) so quality always dominates.
			if artist != "" && matching.MatchArtistInPath(f.Filename, artist) {
				score += 20
			}
			if !cfg.includeAll {
				if album != "" && matching.MatchAlbumInPath(f.Filename, album) {
					score += 8
				}
				if durationMs > 0 && f.Length > 0 {
					targetSec := durationMs / 1000
					diff := f.Length - targetSec
					if diff < 0 {
						diff = -diff
					}
					if diff <= 5 {
						score += 5
					} else if diff > 45 && float64(diff) > 0.35*float64(targetSec) {
						score -= 30
					}
				}
				if mismatch, hardReject := matching.HasUnrequestedVariant(f.Filename, title); hardReject {
					score -= 50
				} else if mismatch {
					score -= 35
				}
			}
			freeSlotBonus := 0
			if result.HasFreeUploadSlot {
				freeSlotBonus = 10
			}
			score += freeSlotBonus + queueScore(result.QueueLength)

			candidates = append(candidates, candidate{
				username:    result.Username,
				file:        f,
				score:       score,
				freeSlot:    result.HasFreeUploadSlot,
				queueLength: result.QueueLength,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].file.BitDepth != candidates[j].file.BitDepth {
			return candidates[i].file.BitDepth > candidates[j].file.BitDepth
		}
		if candidates[i].file.SampleRate != candidates[j].file.SampleRate {
			return candidates[i].file.SampleRate > candidates[j].file.SampleRate
		}
		return candidates[i].file.Size > candidates[j].file.Size
	})

	return candidates
}

func losslessQualityBonus(nameLower string, f slskd.SearchFile) int {
	switch {
	case f.BitDepth >= 24 && f.SampleRate >= 176400:
		return 45
	case f.BitDepth >= 24 && f.SampleRate >= 88200:
		return 35
	case f.BitDepth >= 24:
		return 25
	case f.SampleRate >= 48000:
		return 10
	}
	if f.BitDepth == 0 {
		if strings.Contains(nameLower, "24-192") || strings.Contains(nameLower, "24bit 192") || strings.Contains(nameLower, "24-bit 192") {
			return 40
		}
		if strings.Contains(nameLower, "24-96") || strings.Contains(nameLower, "24bit 96") || strings.Contains(nameLower, "24-bit 96") ||
			strings.Contains(nameLower, "24bit") || strings.Contains(nameLower, "24-bit") || strings.Contains(nameLower, "hires") || strings.Contains(nameLower, "hi-res") {
			return 25
		}
	}
	return 0
}

// QualityTierRank returns the rank of a format/bitrate in the given tiers.
// Lower rank = higher priority. Returns -1 if not matched.
func QualityTierRank(tiers []models.QualityTier, format string, bitrate int) int {
	format = strings.ToLower(format)
	for i, tier := range tiers {
		if strings.ToLower(tier.Format) != format {
			continue
		}
		if tier.MinBitrate > 0 && bitrate > 0 && bitrate < tier.MinBitrate {
			continue
		}
		return i
	}
	return -1
}

// IsUpgradeable returns true if the track's current quality can be improved
// according to the tier list (a lower-index tier exists that the track doesn't match).
func IsUpgradeable(tiers []models.QualityTier, track *models.Track) bool {
	if track.DownloadFormat == nil {
		return len(tiers) > 0
	}
	currentRank := QualityTierRank(tiers, *track.DownloadFormat, derefInt(track.DownloadBitrate))
	if currentRank < 0 {
		return len(tiers) > 0
	}
	return currentRank > 0
}

func staleTimeoutForState(state string) time.Duration {
	switch {
	case strings.Contains(state, "InProgress"), strings.Contains(state, "Initializing"):
		return 5 * time.Minute
	case strings.Contains(state, "Queued"):
		return 30 * time.Minute
	case strings.Contains(state, "Requested"):
		return 10 * time.Minute
	default:
		return 5 * time.Minute
	}
}

func mustParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

type candidate struct {
	username    string
	file        slskd.SearchFile
	score       int
	freeSlot    bool
	queueLength int
}

type availabilityChecker interface {
	IsBlacklisted(username, filename string) bool
	IsUserCooledDown(username string) bool
}

type scoringConfig struct {
	tiers            []models.QualityTier
	fallbackEnabled  bool
	negativeKeywords []string
	excludeNegative  bool
	requireArtist    bool
	allowUnknown     bool
	// includeAll disables the title/artist/format filters so manual search can
	// surface every file slskd returned (still scored and annotated, never dropped).
	includeAll bool
}

func pickBestFile(results []slskd.SearchResult, track *models.Track, ac availabilityChecker, cfg scoringConfig) *candidate {
	cfg.requireArtist = true
	candidates := scoreCandidates(results, track, ac, cfg)
	if len(candidates) == 0 {
		return nil
	}
	return &candidates[0]
}

func inferExt(nameLower string) string {
	basename := nameLower
	if idx := strings.LastIndexAny(nameLower, `/\`); idx >= 0 {
		basename = nameLower[idx+1:]
	}
	for ext := range supportedExts {
		if strings.HasSuffix(basename, ext) {
			return ext
		}
	}
	return ""
}

func tierBasedScore(ext string, bitRate int, cfg scoringConfig) (int, bool) {
	format := strings.TrimPrefix(ext, ".")
	if len(cfg.tiers) == 0 {
		return fallbackScore(ext, bitRate), true
	}
	rank := QualityTierRank(cfg.tiers, format, bitRate)
	if rank >= 0 {
		score := 100 - (rank * 25)
		if score < 25 {
			score = 25
		}
		return score, true
	}
	if !cfg.fallbackEnabled {
		return 0, false
	}
	lowestTier := 100 - ((len(cfg.tiers) - 1) * 25)
	if lowestTier < 25 {
		lowestTier = 25
	}
	score := fallbackScore(ext, bitRate)
	if score >= lowestTier {
		score = lowestTier - 5
	}
	return score, true
}

func fallbackScore(ext string, bitRate int) int {
	if losslessExts[ext] {
		return 20
	}
	switch ext {
	case ".ogg", ".opus", ".aac", ".m4a":
		if bitRate >= 256 {
			return 15
		}
		return 10
	default:
		if bitRate >= 320 {
			return 15
		}
		if bitRate >= 256 {
			return 12
		}
		return 5
	}
}

func queueScore(queueLength int) int {
	return int(15.0 / float64(1+queueLength))
}

func matchesNegativeKeyword(filenameLower string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(filenameLower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
