package language

import (
	"testing"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Tamil", Tamil},
		{"ta", Tamil},
		{"tam", Tamil},
		{"TELUGU", Telugu},
		{"te", Telugu},
		{"tel", Telugu},
		{"malayalam", Malayalam},
		{"ml", Malayalam},
		{"mal", Malayalam},
		{"HINDI", Hindi},
		{"hi", Hindi},
		{"hin", Hindi},
		{"Kannada", Kannada},
		{"kn", Kannada},
		{"kan", Kannada},
		{"English", English},
		{"en", English},
		{"eng", English},
		{"all", All},
		{"*", All},
		{"", All},
	}
	for _, tt := range tests {
		got := Normalize(tt.input)
		if got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsAllLanguages(t *testing.T) {
	if !IsAllLanguages(nil) {
		t.Error("expected nil slice to be AllLanguages")
	}
	if !IsAllLanguages([]string{}) {
		t.Error("expected empty slice to be AllLanguages")
	}
	if !IsAllLanguages([]string{"all"}) {
		t.Error("expected ['all'] to be AllLanguages")
	}
	if !IsAllLanguages([]string{"ALL"}) {
		t.Error("expected ['ALL'] to be AllLanguages")
	}
	if IsAllLanguages([]string{"tamil"}) {
		t.Error("expected ['tamil'] NOT to be AllLanguages")
	}
	if IsAllLanguages([]string{"tamil", "telugu"}) {
		t.Error("expected ['tamil', 'telugu'] NOT to be AllLanguages")
	}
}

func TestMatchesArtistLanguages(t *testing.T) {
	// 1. All Languages preserves old behavior (accepts anything)
	if !MatchesArtistLanguages(nil, "tamil") {
		t.Error("AllLanguages should match tamil")
	}
	if !MatchesArtistLanguages([]string{}, "telugu") {
		t.Error("AllLanguages should match telugu")
	}
	if !MatchesArtistLanguages([]string{"all"}, Unknown) {
		t.Error("AllLanguages should match unknown")
	}

	// 2. Tamil-only artist rejects non-Tamil and unknown candidates
	tamilOnly := []string{"tamil"}
	if !MatchesArtistLanguages(tamilOnly, "tamil") {
		t.Error("Tamil-only should match tamil")
	}
	if !MatchesArtistLanguages(tamilOnly, "tam") {
		t.Error("Tamil-only should match tam alias")
	}
	if MatchesArtistLanguages(tamilOnly, "telugu") {
		t.Error("Tamil-only must reject telugu")
	}
	if MatchesArtistLanguages(tamilOnly, "hindi") {
		t.Error("Tamil-only must reject hindi")
	}
	if MatchesArtistLanguages(tamilOnly, "malayalam") {
		t.Error("Tamil-only must reject malayalam")
	}
	if MatchesArtistLanguages(tamilOnly, Unknown) {
		t.Error("Tamil-only must reject unknown")
	}
	if MatchesArtistLanguages(tamilOnly, "") {
		t.Error("Tamil-only must reject empty language")
	}

	// 3. Multi-language artist accepts any of the configured languages
	tamilAndTelugu := []string{"tamil", "telugu"}
	if !MatchesArtistLanguages(tamilAndTelugu, "tamil") {
		t.Error("Multi-lang should match tamil")
	}
	if !MatchesArtistLanguages(tamilAndTelugu, "telugu") {
		t.Error("Multi-lang should match telugu")
	}
	if MatchesArtistLanguages(tamilAndTelugu, "hindi") {
		t.Error("Multi-lang must reject hindi")
	}
	if MatchesArtistLanguages(tamilAndTelugu, "malayalam") {
		t.Error("Multi-lang must reject malayalam")
	}
	if MatchesArtistLanguages(tamilAndTelugu, Unknown) {
		t.Error("Multi-lang must reject unknown")
	}
}

func TestDetectScript(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"பொன்னியின் செல்வன்", Tamil},
		{"நான் ரெடி தான் வரவா", Tamil},
		{"బాహుబలి ది బిగినింగ్", Telugu},
		{"മഞ്ഞുമ്മൽ ബോയ്സ്", Malayalam},
		{"ಕಾಂತಾರ ಚಾಪ್ಟರ್", Kannada},
		{"दंगल फिल्म", Hindi},
		{"Radiohead - OK Computer", Unknown},
		{"English Vinglish", Unknown},
	}
	for _, tt := range tests {
		got := DetectScript(tt.text)
		if got != tt.want {
			t.Errorf("DetectScript(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestDetectFromMetadata(t *testing.T) {
	if got := DetectFromMetadata(map[string]string{"language": "tam"}); got != Tamil {
		t.Errorf("expected Tamil, got %q", got)
	}
	if got := DetectFromMetadata(map[string]string{"lang": "Telugu"}); got != Telugu {
		t.Errorf("expected Telugu, got %q", got)
	}
	if got := DetectFromMetadata(map[string]string{"script": "Taml"}); got != Tamil {
		t.Errorf("expected Tamil, got %q", got)
	}
	if got := DetectFromMetadata(map[string]string{"script": "Deva"}); got != Hindi {
		t.Errorf("expected Hindi, got %q", got)
	}
	if got := DetectFromMetadata(nil); got != Unknown {
		t.Errorf("expected Unknown, got %q", got)
	}
}

func TestStructuredTagDetection(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Leo (Tamil)", Tamil},
		{"Leo [Tamil]", Tamil},
		{"Pushpa 2 [Telugu]", Telugu},
		{"Jawan (Hindi OST)", Hindi},
		{"Manjummel Boys (Malayalam Version)", Malayalam},
		{"KGF Chapter 2 [Kannada Original]", Kannada},
		{"Coldplay - Yellow [English]", English},
		// Title with words like "English" or "Hindi" without tags must NOT match falsely
		{"English Vinglish", Unknown},
		{"Hindi Medium", Unknown},
		{"Learn English Fast", Unknown},
		{"Tamil Anthem (Original)", Unknown},
	}
	for _, tt := range tests {
		got := DetectStructuredTags(tt.input)
		if got != tt.want {
			t.Errorf("DetectStructuredTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectFileLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{`C:\Music\Tamil\Anirudh\Leo\01 - Badass.flac`, Tamil},
		{`/media/music/Tamil Songs/2023 - Jailer/Hukum.mp3`, Tamil},
		{`D:\Downloads\Telugu\Pushpa\Oo Antava.flac`, Telugu},
		{`Anirudh - Badass [Tamil].flac`, Tamil},
		{`Anirudh - Badass (Telugu Version).mp3`, Telugu},
		{`D:\Music\Anirudh\Leo\பொன்னியின் செல்வன்.flac`, Tamil},
		{`D:\Music\UnknownFolder\01 - Song.flac`, Unknown},
	}
	for _, tt := range tests {
		got := DetectFileLanguage(tt.path)
		if got != tt.want {
			t.Errorf("DetectFileLanguage(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestDetectCandidateFileLanguage(t *testing.T) {
	// Candidate file tag takes priority
	got := DetectCandidateFileLanguage("Anirudh - Badass [Telugu].flac", "Leo (Tamil)", "Badass")
	if got != Telugu {
		t.Errorf("file tag should override album tag, got %q, want Telugu", got)
	}

	// Falls back to album tag if filename has none
	got = DetectCandidateFileLanguage("01 - Badass.flac", "Leo (Tamil)", "Badass")
	if got != Tamil {
		t.Errorf("should fall back to album title tag, got %q, want Tamil", got)
	}

	// Unknown when neither has tag or script
	got = DetectCandidateFileLanguage("01 - Track.flac", "Album Title", "Track Title")
	if got != Unknown {
		t.Errorf("expected Unknown, got %q", got)
	}
}

func TestDetectAlbumDetailLanguage(t *testing.T) {
	album := &pb.AlbumDetail{
		Title: "Leo",
		Metadata: map[string]string{
			"language": "tam",
		},
		Tracks: []*pb.TrackInfo{
			{Title: "Badass"},
		},
	}
	if got := DetectAlbumDetailLanguage(album); got != Tamil {
		t.Errorf("expected Tamil from album detail, got %q", got)
	}
}

func TestDetectFromRichMetadataPrecedence(t *testing.T) {
	// MusicBrainz Romanized Tamil soundtrack: text-representation is eng+Latn, but work_languages is tam
	mbTamilRomanized := &pb.AlbumDetail{
		Title: "Jailer",
		Metadata: map[string]string{
			"language":       "eng",
			"script":         "Latn",
			"text_language":  "eng",
			"text_script":    "Latn",
			"work_languages": "tam",
			"genres":         "filmi",
		},
		Tracks: []*pb.TrackInfo{{Title: "Hukum"}},
	}
	if got := DetectAlbumDetailLanguage(mbTamilRomanized); got != Tamil {
		t.Errorf("expected work_languages tam to override eng+Latn, got %q", got)
	}
	if IsAmbiguousEnglishMetadata(mbTamilRomanized) {
		t.Error("did not expect album with work_languages tam to be treated as ambiguous English")
	}

	// Deezer/MB genres & tags beat eng+Latn
	genreTagged := &pb.AlbumDetail{
		Title: "Vikram",
		Metadata: map[string]string{
			"language":      "eng",
			"script":        "Latn",
			"text_language": "eng",
			"text_script":   "Latn",
			"genres":        "Filmi, Tamil Soundtrack",
		},
	}
	if got := DetectAlbumDetailLanguage(genreTagged); got != Tamil {
		t.Errorf("expected Tamil from genres, got %q", got)
	}

	// Conflicting genres return Unknown instead of guessing
	if got := DetectFromMetadata(map[string]string{"genres": "Tamil, Telugu"}); got != Unknown {
		t.Errorf("expected Unknown for conflicting genres, got %q", got)
	}

	// Explicit label language cue (T-Series Tamil, etc.)
	if got := DetectFromMetadata(map[string]string{"label": "T-Series Tamil"}); got != Tamil {
		t.Errorf("expected Tamil from label keyword, got %q", got)
	}

	// Ambiguous eng+Latn without work/genre/tag confirmation is flagged
	ambiguous := &pb.AlbumDetail{
		Title: "Aaja Raja",
		Metadata: map[string]string{
			"language":      "eng",
			"script":        "Latn",
			"text_language": "eng",
			"text_script":   "Latn",
			"label":         "Sony Music South",
		},
		Tracks: []*pb.TrackInfo{{Title: "Aaja Raja (From \"KH x RK Reunion\")"}},
	}
	if got := DetectAlbumDetailLanguage(ambiguous); got != English {
		t.Errorf("expected fallback English, got %q", got)
	}
	if !IsAmbiguousEnglishMetadata(ambiguous) {
		t.Error("expected eng+Latn without work/genre/tag to be flagged as ambiguous English")
	}
}

func TestStructuredSuffixAndDubTags(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Badass - Telugu", Telugu},
		{`Aaja Raja (From "KH x RK Reunion") - Hindi`, Hindi},
		{"Hukum - Kannada Dub", Kannada},
		{"Leo | Malayalam", Malayalam},
		{"Kaavaalaa - Tamil Version", Tamil},
		{"Spider-Man - Across the Spider-Verse", Unknown},
		{"Non-English Track", Unknown},
		{"English Vinglish", Unknown},
		{"Hindi Medium", Unknown},
		{"Tamil Anthem (Original)", Unknown},
	}
	for _, tt := range tests {
		got := DetectStructuredTags(tt.input)
		if got != tt.want {
			t.Errorf("DetectStructuredTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchesArtistLanguagesWithPolicy(t *testing.T) {
	if MatchesArtistLanguagesWithPolicy([]string{"tamil"}, Unknown, false) {
		t.Error("strict policy must reject Unknown")
	}
	if !MatchesArtistLanguagesWithPolicy([]string{"tamil"}, Unknown, true) {
		t.Error("permissive policy must allow Unknown")
	}
	if MatchesArtistLanguagesWithPolicy([]string{"tamil"}, Telugu, true) {
		t.Error("permissive policy must still block confirmed Telugu")
	}
}

