package matching

import "testing"

func TestFoldLatinDiacriticsPreservesIndic(t *testing.T) {
	if got := FoldLatinDiacritics("Beyoncé - Café"); got != "Beyonce - Cafe" {
		t.Errorf("got %q, want %q", got, "Beyonce - Cafe")
	}
	if got := FoldLatinDiacritics("Sigur Rós"); got != "Sigur Ros" {
		t.Errorf("got %q, want %q", got, "Sigur Ros")
	}
	tamil := "தமிழ் பாடல்"
	if got := FoldLatinDiacritics(tamil); got != tamil {
		t.Errorf("Indic script altered: got %q, want %q", got, tamil)
	}
	hindi := "हिन्दी गाना"
	if got := FoldLatinDiacritics(hindi); got != hindi {
		t.Errorf("Devanagari script altered: got %q, want %q", got, hindi)
	}
}

func TestTitlesMatchRelaxed(t *testing.T) {
	tests := []struct {
		target string
		cand   string
		want   bool
	}{
		{"What's Going On?", "Whats Going On", true},
		{"Don’t Let Me Down", "Don't Let Me Down", true},
		{"Beyoncé", "Beyonce", true},
		{"Time (2023 Remaster)", "Time", true},
		{"Hukum - Thalaivar Alappara (From \"Jailer\")", "Hukum", true},
		{"Rocks & Trees", "Rocks and Trees", true},
		{"Song (feat. Guest Artist)", "Song", true},
		{"OK Computer [Deluxe Edition]", "OK Computer", true},
		{"Track - Part 1", "Track - Part 2", false},
		{"One", "Everyone", false},
		{"Bohemian Rhapsody", "Lose Yourself", false},
	}
	for _, tt := range tests {
		got := TitlesMatch(tt.target, tt.cand)
		if got != tt.want {
			t.Errorf("TitlesMatch(%q, %q) = %v, want %v", tt.target, tt.cand, got, tt.want)
		}
	}
}

func TestArtistsMatchRelaxed(t *testing.T) {
	tests := []struct {
		target string
		cand   string
		want   bool
	}{
		{"A.R. Rahman", "AR Rahman", true},
		{"A.R. Rahman", "A. R. Rahman", true},
		{"Beyoncé", "Beyonce", true},
		{"Guns N' Roses", "Guns N Roses", true},
		{"The Beatles", "Beatles", true},
		{"Anirudh Ravichander", "Anirudh Ravichander, Jonita Gandhi", true},
		{"Anirudh Ravichander, Jonita Gandhi", "Anirudh Ravichander", true},
		{"Air", "Chair", false},
		{"Pink Floyd", "Pink", false},
		{"", "Radiohead", false},
		{"Radiohead", "", false},
	}
	for _, tt := range tests {
		got := ArtistsMatch(tt.target, tt.cand)
		if got != tt.want {
			t.Errorf("ArtistsMatch(%q, %q) = %v, want %v", tt.target, tt.cand, got, tt.want)
		}
	}
}

func TestExtractVariantsAndUnrequested(t *testing.T) {
	if vars := ExtractVariants("Live Forever"); len(vars) != 0 {
		t.Errorf("expected no variants for 'Live Forever', got %v", vars)
	}
	if vars := ExtractVariants("Cover Me"); len(vars) != 0 {
		t.Errorf("expected no variants for 'Cover Me', got %v", vars)
	}
	if vars := ExtractVariants("Time (2023 Remaster)"); len(vars) != 0 {
		t.Errorf("expected no variants for remaster, got %v", vars)
	}

	mismatch, hard := HasUnrequestedVariant("Time (Live)", "Time")
	if !mismatch || hard {
		t.Errorf("Time (Live) vs Time: mismatch=%v hard=%v, want true/false", mismatch, hard)
	}

	mismatch, hard = HasUnrequestedVariant("Time (Live)", "Time (Live)")
	if mismatch || hard {
		t.Errorf("Time (Live) vs Time (Live): mismatch=%v hard=%v, want false/false", mismatch, hard)
	}

	mismatch, hard = HasUnrequestedVariant("Time (Karaoke Version)", "Time")
	if !mismatch || !hard {
		t.Errorf("Time (Karaoke) vs Time: mismatch=%v hard=%v, want true/true", mismatch, hard)
	}
}

func TestMatchTrackPath(t *testing.T) {
	tests := []struct {
		path   string
		artist string
		title  string
		want   bool
	}{
		{"music/Fleetwood Mac/Rumours/02 - Dreams.flac", "Fleetwood Mac", "Dreams", true},
		{"music/Beyonce/Lemonade/01 - Dont Hurt Yourself.flac", "Beyoncé", "Don't Hurt Yourself", true},
		{"music/AR Rahman/Roja/01 - Chinna Chinna Aasai.flac", "A.R. Rahman", "Chinna Chinna Aasai", true},
		{"music/Anirudh Ravichander/Jailer/01 - Hukum.flac", "Anirudh Ravichander", `Hukum - Thalaivar Alappara (From "Jailer")`, true},
		{"music/Radiohead/OK Computer/01 - Time.flac", "Radiohead", "Time (2023 Remaster)", true},
		{"music/SomeoneElse/01 - Everyone.flac", "SomeoneElse", "One", false},
		{"music/Chair/01 - Track.flac", "Air", "Track", false},
		{"music/downloads/Dreams.flac", "Fleetwood Mac", "Dreams", false},
	}
	for _, tt := range tests {
		got := MatchArtistInPath(tt.path, tt.artist) && MatchTitleInPath(tt.path, tt.title)
		if got != tt.want {
			t.Errorf("path match(%q, %q, %q) = %v, want %v", tt.path, tt.artist, tt.title, got, tt.want)
		}
	}
}

func TestCleanSearchQuery(t *testing.T) {
	if got := CleanSearchQuery("Test Artist", "Track A1"); got != "Test Artist Track A1" {
		t.Errorf("CleanSearchQuery simple = %q", got)
	}
	if got := CleanSearchQuery("Anirudh Ravichander", `Hukum (From "Jailer") [Deluxe Edition]`); got != "Anirudh Ravichander Hukum" {
		t.Errorf("CleanSearchQuery noisy = %q", got)
	}
	if got := CleanSearchQuery("Pink Floyd", "Time (2011 Remaster)"); got != "Pink Floyd Time" {
		t.Errorf("CleanSearchQuery remaster = %q", got)
	}
	if got := CleanSearchQuery("Artist", "What's Going On?"); got != "Artist What's Going On" {
		t.Errorf("CleanSearchQuery punct = %q", got)
	}
}

func TestNormalizeAlbumGroupKey(t *testing.T) {
	if a, b := NormalizeAlbumGroupKey("The Wall (Disc 1)"), NormalizeAlbumGroupKey("The Wall (Disc 2)"); a != b {
		t.Errorf("disc albums should group together: %q vs %q", a, b)
	}
	if a, b := NormalizeAlbumGroupKey("Café del Mar"), NormalizeAlbumGroupKey("Cafe del Mar"); a != b {
		t.Errorf("diacritic albums should group together: %q vs %q", a, b)
	}
}

