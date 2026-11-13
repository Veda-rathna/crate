package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/TheOutdoorProgrammer/crate/internal/matching"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
)

// Manual link — the escape hatch when the automatic reconcile can't match an
// imported album/track to the provider (title drift, deluxe editions, tagging
// quirks). See docs/adr/0007-reconcile-local-import.md.
//
// After reconcile the provider's whole discography is already present, so
// "linking" a leftover local entity means *merging* it into the existing
// provider row — not re-stamping a provider id, which would collide on the
// unique (provider, provider_id) index. Owned files are carried over; the empty
// local shell is removed. Nothing else is touched.

// handleLinkAlbum merges a local album into another album by the same artist
// that's already linked to a provider: each owned local track claims the
// matching wanted track (by title, disc/track number as tiebreak); a track the
// provider doesn't list is moved over as-is (owned + local extra); the emptied
// local album is deleted.
func (s *Server) handleLinkAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		TargetAlbumID int64 `json:"target_album_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	src, err := s.queries.GetAlbum(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "album not found")
		return
	}
	if src.Provider != provider.LocalProvider {
		writeError(w, http.StatusBadRequest, "only a local album can be linked")
		return
	}
	dst, err := s.queries.GetAlbum(req.TargetAlbumID)
	if err != nil {
		writeError(w, http.StatusNotFound, "target album not found")
		return
	}
	if dst.ID == src.ID || dst.ArtistID != src.ArtistID {
		writeError(w, http.StatusBadRequest, "target must be another album by the same artist")
		return
	}
	if dst.Provider == provider.LocalProvider {
		writeError(w, http.StatusBadRequest, "target album is not linked to a provider")
		return
	}

	if err := s.mergeAlbum(src, dst); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link album")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "linked", "merged_into": dst.ID})
}

func (s *Server) mergeAlbum(src, dst *models.Album) error {
	srcTracks, err := s.queries.ListTracksByAlbum(src.ID)
	if err != nil {
		return err
	}
	dstTracks, err := s.queries.ListTracksByAlbum(dst.ID)
	if err != nil {
		return err
	}

	used := make(map[int64]bool)
	for i := range srcTracks {
		t := &srcTracks[i]
		if match := findWantedMatch(dstTracks, used, t, len(srcTracks), len(dstTracks)); match != nil && t.FilePath != nil {
			used[match.ID] = true
			if err := s.queries.ClaimTrackFile(match.ID, *t.FilePath, strVal(t.DownloadFormat), intVal(t.DownloadBitrate), t.DurationMs, t.MBRecordingID); err != nil {
				return err
			}
			_ = s.queries.DeletePendingDownloadByTrack(match.ID)
			if err := s.queries.DeleteTrack(t.ID); err != nil {
				return err
			}
			continue
		}
		// No provider match (or no file) — keep the track, move it under dst.
		if err := s.queries.UpdateTrackAlbum(t.ID, dst.ID); err != nil {
			return err
		}
	}
	return s.queries.DeleteAlbum(src.ID)
}

// handleLinkTrack claims a wanted provider track using an owned local track's
// file — "this local file IS that track" — then removes the local duplicate.
func (s *Server) handleLinkTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		TargetTrackID int64 `json:"target_track_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	src, err := s.queries.GetTrack(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "track not found")
		return
	}
	if src.Provider != provider.LocalProvider || src.FilePath == nil {
		writeError(w, http.StatusBadRequest, "only an owned local track can be linked")
		return
	}
	dst, err := s.queries.GetTrack(req.TargetTrackID)
	if err != nil {
		writeError(w, http.StatusNotFound, "target track not found")
		return
	}
	if dst.AlbumID != src.AlbumID {
		writeError(w, http.StatusBadRequest, "target track must be in the same album")
		return
	}
	if dst.Status != models.TrackStatusWanted {
		writeError(w, http.StatusBadRequest, "target track already has a file")
		return
	}

	if err := s.queries.ClaimTrackFile(dst.ID, *src.FilePath, strVal(src.DownloadFormat), intVal(src.DownloadBitrate), src.DurationMs, src.MBRecordingID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link track")
		return
	}
	_ = s.queries.DeletePendingDownloadByTrack(dst.ID)
	if err := s.queries.DeleteTrack(src.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove local duplicate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "linked", "claimed": dst.ID})
}

// handleMatchUnmatchedAlbums automatically pairs and merges all unmatched local albums
// under an artist with their corresponding provider albums based on title similarity,
// track count alignment, and track numbers.
func (s *Server) handleMatchUnmatchedAlbums(w http.ResponseWriter, r *http.Request) {
	artistID, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return
	}

	artist, err := s.queries.GetArtist(artistID)
	if err != nil || artist == nil {
		writeError(w, http.StatusNotFound, "artist not found")
		return
	}
	if artist.Provider == provider.LocalProvider {
		writeError(w, http.StatusBadRequest, "cannot auto-match: artist is not linked to a provider")
		return
	}

	albums, err := s.queries.ListAlbumsByArtist(artistID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list albums")
		return
	}

	var locals []*models.Album
	var targets []*models.Album
	for i := range albums {
		a := &albums[i]
		if a.Provider == provider.LocalProvider {
			locals = append(locals, a)
		} else {
			targets = append(targets, a)
		}
	}

	if len(locals) == 0 || len(targets) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"matched":   0,
			"remaining": len(locals),
		})
		return
	}

	// Pre-load tracks for all candidates to avoid repeated queries
	albumTracks := make(map[int64][]models.Track)
	for _, a := range append(append([]*models.Album(nil), locals...), targets...) {
		if tr, err := s.queries.ListTracksByAlbum(a.ID); err == nil {
			albumTracks[a.ID] = tr
		}
	}

	type matchCandidate struct {
		local  *models.Album
		target *models.Album
		score  int
	}

	var candidates []matchCandidate
	for _, loc := range locals {
		for _, tgt := range targets {
			score := scoreAlbumMatch(loc, albumTracks[loc.ID], tgt, albumTracks[tgt.ID])
			if score >= 70 {
				candidates = append(candidates, matchCandidate{
					local:  loc,
					target: tgt,
					score:  score,
				})
			}
		}
	}

	// Sort candidate matches highest score first
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	usedLocals := make(map[int64]bool)
	usedTargets := make(map[int64]bool)
	matchedCount := 0

	for _, cand := range candidates {
		if usedLocals[cand.local.ID] || usedTargets[cand.target.ID] {
			continue
		}
		if err := s.mergeAlbum(cand.local, cand.target); err != nil {
			slog.Error("auto-match: merge album failed", "local_id", cand.local.ID, "target_id", cand.target.ID, "error", err)
			continue
		}
		usedLocals[cand.local.ID] = true
		usedTargets[cand.target.ID] = true
		matchedCount++
	}

	if matchedCount > 0 {
		s.recordActivity("albums_auto_matched", "artist", artistID,
			fmt.Sprintf("Auto-matched %d album(s) for %s", matchedCount, artist.Name))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"matched":   matchedCount,
		"remaining": len(locals) - matchedCount,
	})
}

var (
	albumQualifierRE   = regexp.MustCompile(`(?i)\s*[\(\[](?:[^\]\)]*(?:original motion picture soundtrack|motion picture soundtrack|soundtrack|original score|score|ost|deluxe|edition|single|ep|from\s+"[^"]*")[^\]\)]*)[\)\]]`)
	albumTrailingRE    = regexp.MustCompile(`(?i)\s*[\-\x{2013}]\s*(?:original motion picture soundtrack|motion picture soundtrack|soundtrack|original score|score|ost|single|ep)\s*$`)
	albumPunctRE       = regexp.MustCompile(`[^a-z0-9\p{L}]+`)
)

func cleanAlbumTitle(title string) string {
	s := albumQualifierRE.ReplaceAllString(title, " ")
	s = albumTrailingRE.ReplaceAllString(s, " ")
	s = albumPunctRE.ReplaceAllString(strings.ToLower(s), " ")
	return strings.TrimSpace(s)
}

func scoreAlbumMatch(loc *models.Album, locTracks []models.Track, tgt *models.Album, tgtTracks []models.Track) int {
	score := 0

	cleanLoc := cleanAlbumTitle(loc.Title)
	cleanTgt := cleanAlbumTitle(tgt.Title)

	if cleanLoc != "" && cleanLoc == cleanTgt {
		score += 100
	} else if foldEqual(loc.Title, tgt.Title) {
		score += 100
	} else if matching.TitlesMatch(loc.Title, tgt.Title) {
		score += 70
	} else if cleanLoc != "" && cleanTgt != "" && (strings.Contains(cleanLoc, cleanTgt) || strings.Contains(cleanTgt, cleanLoc)) {
		score += 50
	} else {
		return 0 // Title has no similarity
	}

	// Track count alignment
	lenLoc := len(locTracks)
	lenTgt := len(tgtTracks)
	if lenLoc > 0 && lenTgt > 0 {
		if lenLoc == lenTgt {
			score += 50
		} else if abs(lenLoc-lenTgt) <= 2 {
			score += 10
		} else {
			score -= 30
		}
	}

	// Year compatibility
	if loc.Year != nil && tgt.Year != nil && *loc.Year > 0 && *tgt.Year > 0 {
		if *loc.Year == *tgt.Year {
			score += 10
		} else if abs(*loc.Year-*tgt.Year) > 1 {
			score -= 20
		}
	}

	return score
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// findWantedMatch returns the wanted dst track that best matches src using tiered matching:
// 1. exact title fold + track/disc number
// 2. exact title fold
// 3. relaxed title match (matching.TitlesMatch) + track/disc number
// 4. relaxed title match
// 5. exact track & disc number when track counts align and duration is compatible
// 6. single-track release match
func findWantedMatch(dst []models.Track, used map[int64]bool, src *models.Track, totalSrc, totalDst int) *models.Track {
	// Tier 1: exact title fold + disc/track number
	for i := range dst {
		d := &dst[i]
		if used[d.ID] || d.Status != models.TrackStatusWanted {
			continue
		}
		if foldEqual(d.Title, src.Title) && d.TrackNumber == src.TrackNumber && d.DiscNumber == src.DiscNumber {
			return d
		}
	}

	// Tier 2: exact title fold
	for i := range dst {
		d := &dst[i]
		if used[d.ID] || d.Status != models.TrackStatusWanted {
			continue
		}
		if foldEqual(d.Title, src.Title) {
			return d
		}
	}

	// Tier 3: relaxed title match + disc/track number
	for i := range dst {
		d := &dst[i]
		if used[d.ID] || d.Status != models.TrackStatusWanted {
			continue
		}
		if matching.TitlesMatch(d.Title, src.Title) && d.TrackNumber == src.TrackNumber && d.DiscNumber == src.DiscNumber {
			return d
		}
	}

	// Tier 4: relaxed title match
	for i := range dst {
		d := &dst[i]
		if used[d.ID] || d.Status != models.TrackStatusWanted {
			continue
		}
		if matching.TitlesMatch(d.Title, src.Title) {
			return d
		}
	}

	// Tier 5: track number and disc number alignment when total track count matches
	if totalSrc > 0 && totalSrc == totalDst {
		for i := range dst {
			d := &dst[i]
			if used[d.ID] || d.Status != models.TrackStatusWanted {
				continue
			}
			if d.TrackNumber == src.TrackNumber && d.DiscNumber == src.DiscNumber {
				if d.DurationMs <= 0 || src.DurationMs <= 0 || abs(d.DurationMs-src.DurationMs) <= 15000 {
					return d
				}
			}
		}
	}

	// Tier 6: Single-track release match
	if totalSrc == 1 && totalDst == 1 {
		for i := range dst {
			d := &dst[i]
			if !used[d.ID] && d.Status == models.TrackStatusWanted {
				return d
			}
		}
	}

	return nil
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intVal(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
