package releasedate

import (
	"testing"
	"time"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

func TestParseYear(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"2026-11-11", 2026},
		{"2026-11", 2026},
		{"2026", 2026},
		{"0000-00-00", 0},
		{"", 0},
		{"invalid", 0},
	}
	for _, tc := range cases {
		if got := ParseYear(tc.in); got != tc.want {
			t.Errorf("ParseYear(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestIsOnOrAfter(t *testing.T) {
	since := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)

	cases := []struct {
		date string
		want bool
	}{
		{"2026-06-16", true},
		{"2026-06-15", true}, // same-day release
		{"2026-06-14", false},
		{"2026-06", true}, // partial YYYY-MM same month
		{"2026-07", true},
		{"2026-05", false},
		{"2026", true}, // partial YYYY same year
		{"2027", true},
		{"2025", false},
		{"0000-00-00", true}, // unknown date not treated as year 0001
		{"", true},
	}
	for _, tc := range cases {
		if got := IsOnOrAfter(tc.date, since); got != tc.want {
			t.Errorf("IsOnOrAfter(%q, %s) = %v, want %v", tc.date, since.Format(time.RFC3339), got, tc.want)
		}
	}
}

func TestSortAlbumsDescending(t *testing.T) {
	albums := []*pb.AlbumSummary{
		{Id: "old", Title: "Old Album", Year: 1997, Metadata: map[string]string{"release_date": "1997-06-16"}},
		{Id: "none", Title: "Undated", Year: 0},
		{Id: "partial", Title: "Partial Year", Year: 2025, Metadata: map[string]string{"release_date": "2025"}},
		{Id: "latest", Title: "Latest Album", Year: 2026, Metadata: map[string]string{"release_date": "2026-11-01"}},
		{Id: "month", Title: "Partial Month", Year: 2026, Metadata: map[string]string{"release_date": "2026-03"}},
	}

	SortAlbumsDescending(albums)

	wantOrder := []string{"latest", "month", "partial", "old", "none"}
	for i, wantID := range wantOrder {
		if albums[i].Id != wantID {
			t.Errorf("position %d: got %q, want %q", i, albums[i].Id, wantID)
		}
		if albums[i].Rank != int32(len(albums)-i) {
			t.Errorf("position %d rank = %d, want %d", i, albums[i].Rank, len(albums)-i)
		}
	}
}

