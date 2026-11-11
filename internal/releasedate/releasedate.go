package releasedate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

// ParseYear extracts a valid positive 4-digit year from a release date string
// (YYYY-MM-DD, YYYY-MM, YYYY, or RFC3339). Invalid or zero years like "0000-00-00" return 0.
func ParseYear(dateStr string) int {
	s := strings.TrimSpace(dateStr)
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil || y <= 0 {
		return 0
	}
	return y
}

// NormalizeSortKey returns a fixed-width YYYY-MM-DD key suitable for chronological
// lexicographic sorting, padding missing month/day precision with "00".
// Returns "" for empty or invalid dates.
func NormalizeSortKey(dateStr string) string {
	s := strings.TrimSpace(dateStr)
	if s == "" {
		return ""
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil && t.Year() > 0 {
			return s[:10]
		}
	}
	if len(s) >= 7 {
		if t, err := time.Parse("2006-01", s[:7]); err == nil && t.Year() > 0 {
			return s[:7] + "-00"
		}
	}
	if y := ParseYear(s); y > 0 {
		return fmt.Sprintf("%04d-00-00", y)
	}
	return ""
}

func albumSortKey(a *pb.AlbumSummary) string {
	if a == nil {
		return ""
	}
	if a.Metadata != nil {
		if key := NormalizeSortKey(a.Metadata["release_date"]); key != "" {
			return key
		}
	}
	if a.Year > 0 {
		return fmt.Sprintf("%04d-00-00", a.Year)
	}
	return ""
}

// SortAlbumsDescending orders albums newest-first by release_date (falling back to Year),
// placing undated releases last, and updates Rank monotonically descending.
func SortAlbumsDescending(albums []*pb.AlbumSummary) {
	sort.SliceStable(albums, func(i, j int) bool {
		ki, kj := albumSortKey(albums[i]), albumSortKey(albums[j])
		if ki != kj {
			if ki == "" {
				return false
			}
			if kj == "" {
				return true
			}
			return ki > kj
		}
		return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
	})
	for i, a := range albums {
		if a != nil {
			a.Rank = int32(len(albums) - i)
		}
	}
}

// IsOnOrAfter reports whether releaseDate falls on or after since at the finest
// precision present in releaseDate (day for YYYY-MM-DD, month for YYYY-MM, year for YYYY).
// Same-day releases return true. Unknown, empty, or "0000-00-00" dates return true so
// releases without full dates are never dropped as year-0001.
func IsOnOrAfter(releaseDate string, since time.Time) bool {
	if since.IsZero() {
		return true
	}
	s := strings.TrimSpace(releaseDate)
	if s == "" {
		return true
	}
	sinceUTC := since.UTC()

	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			if t.Year() <= 0 {
				return true
			}
			sinceDay := time.Date(sinceUTC.Year(), sinceUTC.Month(), sinceUTC.Day(), 0, 0, 0, 0, time.UTC)
			return !t.Before(sinceDay)
		}
	}
	if len(s) >= 7 {
		if t, err := time.Parse("2006-01", s[:7]); err == nil {
			if t.Year() <= 0 {
				return true
			}
			sinceMonth := time.Date(sinceUTC.Year(), sinceUTC.Month(), 1, 0, 0, 0, 0, time.UTC)
			return !t.Before(sinceMonth)
		}
	}
	if y := ParseYear(s); y > 0 {
		return y >= sinceUTC.Year()
	}
	return true
}

