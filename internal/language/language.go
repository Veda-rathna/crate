package language

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

// Supported canonical language identifiers
const (
	All       = "all"
	Unknown   = "unknown"
	Tamil     = "tamil"
	Telugu    = "telugu"
	Malayalam = "malayalam"
	Hindi     = "hindi"
	Kannada   = "kannada"
	English   = "english"
)

// SupportedLanguages lists the predefined selectable languages.
var SupportedLanguages = []string{
	Tamil,
	Telugu,
	Malayalam,
	Hindi,
	Kannada,
	English,
}

// Normalize maps aliases, ISO 639-1/2/3 codes, and common variants to canonical names.
func Normalize(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch l {
	case "ta", "tam", "tamil":
		return Tamil
	case "te", "tel", "telugu":
		return Telugu
	case "ml", "mal", "malayalam":
		return Malayalam
	case "hi", "hin", "hindi":
		return Hindi
	case "kn", "kan", "kannada":
		return Kannada
	case "en", "eng", "english":
		return English
	case "all", "*", "":
		return All
	default:
		return l
	}
}

// IsAllLanguages returns true if languages slice is empty, contains "all", or has no specific filters.
func IsAllLanguages(languages []string) bool {
	if len(languages) == 0 {
		return true
	}
	for _, l := range languages {
		if Normalize(l) == All {
			return true
		}
	}
	return false
}

// MatchesArtistLanguages checks if detectedLang is allowed for an artist configured with artistLangs.
// - If artistLangs is empty or includes "all", everything matches (preserves default behavior).
// - If artist has specific languages configured, an Unknown detected language does NOT match.
// - If detectedLang matches any configured language, returns true.
func MatchesArtistLanguages(artistLangs []string, detectedLang string) bool {
	return MatchesArtistLanguagesWithPolicy(artistLangs, detectedLang, false)
}

// MatchesArtistLanguagesWithPolicy checks if detectedLang is allowed for an artist configured with artistLangs.
// When allowUnknown is true, releases with Unknown language are accepted (only confirmed foreign
// languages are blocked), which avoids dropping unlabelled original-language releases.
func MatchesArtistLanguagesWithPolicy(artistLangs []string, detectedLang string, allowUnknown bool) bool {
	if IsAllLanguages(artistLangs) {
		return true
	}
	if detectedLang == Unknown || detectedLang == "" {
		return allowUnknown
	}
	normDetected := Normalize(detectedLang)
	if normDetected == Unknown {
		return allowUnknown
	}
	for _, l := range artistLangs {
		if Normalize(l) == normDetected {
			return true
		}
	}
	return false
}

// DetectScript checks for Indic unicode scripts in the text.
// Requires at least 2 characters in the script to avoid stray symbols.
func DetectScript(text string) string {
	counts := make(map[string]int)
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Tamil, r):
			counts[Tamil]++
		case unicode.Is(unicode.Telugu, r):
			counts[Telugu]++
		case unicode.Is(unicode.Malayalam, r):
			counts[Malayalam]++
		case unicode.Is(unicode.Kannada, r):
			counts[Kannada]++
		case unicode.Is(unicode.Devanagari, r):
			counts[Hindi]++
		}
	}
	maxCount := 0
	best := Unknown
	for lang, count := range counts {
		if count > maxCount {
			maxCount = count
			best = lang
		}
	}
	if maxCount >= 2 {
		return best
	}
	return Unknown
}

var (
	genreKeywordPatterns = map[string]*regexp.Regexp{
		Tamil:     regexp.MustCompile(`(?i)\b(tamil|kollywood)\b`),
		Telugu:    regexp.MustCompile(`(?i)\b(telugu|tollywood)\b`),
		Malayalam: regexp.MustCompile(`(?i)\b(malayalam|mollywood)\b`),
		Hindi:     regexp.MustCompile(`(?i)\b(hindi|bollywood)\b`),
		Kannada:   regexp.MustCompile(`(?i)\b(kannada|sandalwood)\b`),
		English:   regexp.MustCompile(`(?i)\benglish\b`),
	}

	labelLanguagePatterns = map[string]*regexp.Regexp{
		Tamil:     regexp.MustCompile(`(?i)\btamil\b`),
		Telugu:    regexp.MustCompile(`(?i)\btelugu\b`),
		Malayalam: regexp.MustCompile(`(?i)\bmalayalam\b`),
		Hindi:     regexp.MustCompile(`(?i)\bhindi\b`),
		Kannada:   regexp.MustCompile(`(?i)\bkannada\b`),
	}
)

func matchUniquePattern(text string, patterns map[string]*regexp.Regexp) string {
	if strings.TrimSpace(text) == "" {
		return Unknown
	}
	var matched string
	for lang, re := range patterns {
		if re.MatchString(text) {
			if matched != "" && matched != lang {
				return Unknown
			}
			matched = lang
		}
	}
	if matched == "" {
		return Unknown
	}
	return matched
}

func detectWorkLanguages(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return Unknown
	}
	var matched string
	for _, token := range strings.Split(raw, ",") {
		t := strings.ToLower(strings.TrimSpace(token))
		switch t {
		case "", "zxx", "mul", "und", "qaa":
			continue
		}
		norm := Normalize(t)
		if norm == Unknown || norm == All {
			continue
		}
		if matched != "" && matched != norm {
			return Unknown
		}
		matched = norm
	}
	if matched == "" {
		return Unknown
	}
	return matched
}

func detectAuthoritativeMetadata(metadata map[string]string) string {
	if metadata == nil {
		return Unknown
	}
	// 1. MusicBrainz work languages (lyrics/composition language) — highest reliability
	if lang := detectWorkLanguages(metadata["work_languages"]); lang != Unknown {
		return lang
	}
	// 2. Curated genres / tags from MusicBrainz, Deezer, Spotify, or Apple Music
	for _, key := range []string{"genres", "tags"} {
		if lang := matchUniquePattern(metadata[key], genreKeywordPatterns); lang != Unknown {
			return lang
		}
	}
	// 3. Explicit Indic language keywords in record label / copyright lines (e.g. "T-Series Tamil")
	for _, key := range []string{"label", "copyrights"} {
		if lang := matchUniquePattern(metadata[key], labelLanguagePatterns); lang != Unknown {
			return lang
		}
	}
	// 4. Indic ISO 15924 script codes (skip Latn because Indic tracks are routinely romanized)
	for _, key := range []string{"script", "text_script"} {
		if script := strings.ToLower(strings.TrimSpace(metadata[key])); script != "" {
			switch script {
			case "taml":
				return Tamil
			case "telu":
				return Telugu
			case "mlym":
				return Malayalam
			case "deva":
				return Hindi
			case "knda":
				return Kannada
			}
		}
	}
	// 5. Explicit language / lang unless it's potentially ambiguous MusicBrainz eng+Latn text-representation
	for _, key := range []string{"language", "lang"} {
		if v := strings.TrimSpace(metadata[key]); v != "" {
			norm := Normalize(v)
			if norm != Unknown && norm != All {
				if norm == English && strings.EqualFold(metadata["text_script"], "Latn") {
					return Unknown
				}
				return norm
			}
		}
	}
	return Unknown
}

// DetectFromMetadata inspects provider metadata map (work languages, genres, tags, labels, scripts, and language tags).
func DetectFromMetadata(metadata map[string]string) string {
	if metadata == nil {
		return Unknown
	}
	if lang := detectAuthoritativeMetadata(metadata); lang != Unknown {
		return lang
	}
	for _, key := range []string{"language", "lang", "text_language"} {
		if v := strings.TrimSpace(metadata[key]); v != "" {
			norm := Normalize(v)
			if norm != Unknown && norm != All {
				return norm
			}
		}
	}
	return Unknown
}

// IsAmbiguousEnglishMetadata reports whether detected English language relies solely on MusicBrainz
// text-representation ("eng" + "Latn") without any work language, genre, tag, or title confirmation.
func IsAmbiguousEnglishMetadata(album *pb.AlbumDetail) bool {
	if album == nil || album.Metadata == nil {
		return false
	}
	if Normalize(album.Metadata["text_language"]) != English {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(album.Metadata["text_script"]), "Latn") {
		return false
	}
	if detectAuthoritativeMetadata(album.Metadata) != Unknown {
		return false
	}
	if DetectStructuredTags(album.Title) != Unknown {
		return false
	}
	for _, t := range album.Tracks {
		if t == nil {
			continue
		}
		if detectAuthoritativeMetadata(t.Metadata) != Unknown || DetectStructuredTags(t.Title) != Unknown {
			return false
		}
	}
	return true
}

// Regex patterns for structured language tags.
// Note: We deliberately require bracketed delimiters, word boundaries with version/ost/audio keywords,
// or delimited path/tag separators to prevent false positives on titles like "English Vinglish" or "Hindi Medium".
var (
	// Brackets / parentheses: e.g. [Tamil], (Tamil), [Telugu Version], (Hindi OST)
	bracketPatterns = map[string]*regexp.Regexp{
		Tamil:     regexp.MustCompile(`(?i)[\[\(\{][^\]\)\}]*\b(tamil|tam)\b[^\]\)\}]*[\]\)\}]`),
		Telugu:    regexp.MustCompile(`(?i)[\[\(\{][^\]\)\}]*\b(telugu|tel)\b[^\]\)\}]*[\]\)\}]`),
		Malayalam: regexp.MustCompile(`(?i)[\[\(\{][^\]\)\}]*\b(malayalam|mal)\b[^\]\)\}]*[\]\)\}]`),
		Hindi:     regexp.MustCompile(`(?i)[\[\(\{][^\]\)\}]*\b(hindi|hin)\b[^\]\)\}]*[\]\)\}]`),
		Kannada:   regexp.MustCompile(`(?i)[\[\(\{][^\]\)\}]*\b(kannada|kan)\b[^\]\)\}]*[\]\)\}]`),
		English:   regexp.MustCompile(`(?i)[\[\(\{][^\]\)\}]*\b(english|eng)\b[^\]\)\}]*[\]\)\}]`),
	}

	// Delimited tags e.g. " - Tamil - ", " - Telugu.mp3", "| Tamil |", "_Tamil_"
	delimitedTagPatterns = map[string]*regexp.Regexp{
		Tamil:     regexp.MustCompile(`(?i)(?:[\s\-_\.\|]|^)tamil(?:[\s\-_\.\|]|$)`),
		Telugu:    regexp.MustCompile(`(?i)(?:[\s\-_\.\|]|^)telugu(?:[\s\-_\.\|]|$)`),
		Malayalam: regexp.MustCompile(`(?i)(?:[\s\-_\.\|]|^)malayalam(?:[\s\-_\.\|]|$)`),
		Hindi:     regexp.MustCompile(`(?i)(?:[\s\-_\.\|]|^)hindi(?:[\s\-_\.\|]|$)`),
		Kannada:   regexp.MustCompile(`(?i)(?:[\s\-_\.\|]|^)kannada(?:[\s\-_\.\|]|$)`),
		English:   regexp.MustCompile(`(?i)(?:[\s\-_\.\|]|^)english(?:[\s\-_\.\|]|$)`),
	}

	// Keywords like "Tamil Version", "Telugu OST", "Hindi Dub", "Malayalam Original"
	versionKeywordPatterns = map[string]*regexp.Regexp{
		Tamil:     regexp.MustCompile(`(?i)\btamil\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix)\b`),
		Telugu:    regexp.MustCompile(`(?i)\btelugu\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix)\b`),
		Malayalam: regexp.MustCompile(`(?i)\bmalayalam\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix)\b`),
		Hindi:     regexp.MustCompile(`(?i)\bhindi\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix)\b`),
		Kannada:   regexp.MustCompile(`(?i)\bkannada\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix)\b`),
		English:   regexp.MustCompile(`(?i)\benglish\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix)\b`),
	}

	// Trailing or delimited release/track qualifiers like "Album - Telugu", "Song | Hindi", "Track - Tamil Version"
	suffixSeparatorPatterns = map[string]*regexp.Regexp{
		Tamil:     regexp.MustCompile(`(?i)(?:^|\s)[\-\x{2013}\x{2014}\|/]\s*tamil(?:\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix))?(?:\s*$|\s*[\(\[\{\-\x{2013}\x{2014}\|/])`),
		Telugu:    regexp.MustCompile(`(?i)(?:^|\s)[\-\x{2013}\x{2014}\|/]\s*telugu(?:\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix))?(?:\s*$|\s*[\(\[\{\-\x{2013}\x{2014}\|/])`),
		Malayalam: regexp.MustCompile(`(?i)(?:^|\s)[\-\x{2013}\x{2014}\|/]\s*malayalam(?:\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix))?(?:\s*$|\s*[\(\[\{\-\x{2013}\x{2014}\|/])`),
		Hindi:     regexp.MustCompile(`(?i)(?:^|\s)[\-\x{2013}\x{2014}\|/]\s*hindi(?:\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix))?(?:\s*$|\s*[\(\[\{\-\x{2013}\x{2014}\|/])`),
		Kannada:   regexp.MustCompile(`(?i)(?:^|\s)[\-\x{2013}\x{2014}\|/]\s*kannada(?:\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix))?(?:\s*$|\s*[\(\[\{\-\x{2013}\x{2014}\|/])`),
		English:   regexp.MustCompile(`(?i)(?:^|\s)[\-\x{2013}\x{2014}\|/]\s*english(?:\s+(?:version|ost|soundtrack|audio|songs?|album|original|dub(?:bed)?|edition|mix))?(?:\s*$|\s*[\(\[\{\-\x{2013}\x{2014}\|/])`),
	}
)

// DetectStructuredTags finds explicit language qualifiers in bracketed/parenthesized tags, suffix qualifiers, or version descriptors.
func DetectStructuredTags(text string) string {
	if text == "" {
		return Unknown
	}
	seen := make(map[string]struct{})
	var matched []string
	add := func(lang string) {
		if _, ok := seen[lang]; ok {
			return
		}
		seen[lang] = struct{}{}
		matched = append(matched, lang)
	}
	for _, patSet := range []map[string]*regexp.Regexp{bracketPatterns, versionKeywordPatterns, suffixSeparatorPatterns} {
		for lang, re := range patSet {
			if re.MatchString(text) {
				add(lang)
			}
		}
	}
	// If multiple contradictory languages are detected, do not guess
	if len(matched) == 1 {
		return matched[0]
	}
	return Unknown
}

// DetectFileLanguage determines language from a file path or filename.
// Checks folder segments (e.g. "/Tamil Songs/", "\Telugu FLAC\"), filename tags, and script.
func DetectFileLanguage(pathStr string) string {
	if pathStr == "" {
		return Unknown
	}
	// 1. Check unicode script in path
	if s := DetectScript(pathStr); s != Unknown {
		return s
	}

	// 2. Check structured bracket tags in filename
	base := filepath.Base(pathStr)
	if lang := DetectStructuredTags(base); lang != Unknown {
		return lang
	}

	// 3. Check directory segments in path
	cleanPath := filepath.ToSlash(pathStr)
	segments := strings.Split(cleanPath, "/")
	for _, seg := range segments {
		segTrimmed := strings.TrimSpace(seg)
		if segTrimmed == "" {
			continue
		}
		if s := DetectScript(segTrimmed); s != Unknown {
			return s
		}
		if lang := DetectStructuredTags(segTrimmed); lang != Unknown {
			return lang
		}
		// Check delimited folder names like "Tamil", "Tamil Songs", "Telugu", etc.
		for lang, re := range delimitedTagPatterns {
			if re.MatchString(segTrimmed) {
				return lang
			}
		}
	}

	return Unknown
}

// DetectTrackLanguage determines language using album title and track title.
func DetectTrackLanguage(albumTitle, trackTitle string) string {
	// 1. Script in track or album title
	if s := DetectScript(trackTitle); s != Unknown {
		return s
	}
	if s := DetectScript(albumTitle); s != Unknown {
		return s
	}

	// 2. Structured tags in track title (e.g. "Badass (Telugu Version)")
	if lang := DetectStructuredTags(trackTitle); lang != Unknown {
		return lang
	}

	// 3. Structured tags in album title (e.g. "Leo (Tamil)", "Pushpa [Hindi]")
	if lang := DetectStructuredTags(albumTitle); lang != Unknown {
		return lang
	}

	return Unknown
}

// DetectReleaseSummaryLanguage detects language for an album summary returned by a provider.
func DetectReleaseSummaryLanguage(title string, metadata map[string]string) string {
	// 1. Provider metadata
	if lang := DetectFromMetadata(metadata); lang != Unknown {
		return lang
	}
	// 2. Unicode script in title
	if s := DetectScript(title); s != Unknown {
		return s
	}
	// 3. Structured tags in title
	if lang := DetectStructuredTags(title); lang != Unknown {
		return lang
	}
	return Unknown
}

// DetectAlbumDetailLanguage detects language for a full AlbumDetail (from provider or browse).
func DetectAlbumDetailLanguage(album *pb.AlbumDetail) string {
	if album == nil {
		return Unknown
	}
	// 1. Authoritative metadata on album detail (work_languages, genres, tags, explicit label/script, non-ambiguous language)
	if lang := detectAuthoritativeMetadata(album.Metadata); lang != Unknown {
		return lang
	}
	// 2. Script or tags on album title
	if s := DetectScript(album.Title); s != Unknown {
		return s
	}
	if lang := DetectStructuredTags(album.Title); lang != Unknown {
		return lang
	}
	// 3. Check tracks metadata, scripts, or tags
	trackLangs := make(map[string]int)
	for _, t := range album.Tracks {
		if t == nil {
			continue
		}
		if metaLang := detectAuthoritativeMetadata(t.Metadata); metaLang != Unknown {
			trackLangs[metaLang]++
		}
		if s := DetectScript(t.Title); s != Unknown {
			trackLangs[s]++
		}
		if tagLang := DetectStructuredTags(t.Title); tagLang != Unknown {
			trackLangs[tagLang]++
		}
	}
	maxCount := 0
	bestTrackLang := Unknown
	for lang, count := range trackLangs {
		if count > maxCount {
			maxCount = count
			bestTrackLang = lang
		}
	}
	if bestTrackLang != Unknown {
		return bestTrackLang
	}
	// 4. Fallback to text-representation language (e.g. MusicBrainz "eng" + "Latn")
	return DetectFromMetadata(album.Metadata)
}

// DetectCandidateFileLanguage determines the language of a download candidate (slskd file).
// It combines file path info with the track/album context.
func DetectCandidateFileLanguage(filename, albumTitle, trackTitle string) string {
	// 1. Check file-specific language (e.g. Soulseek file/folder tags)
	fileLang := DetectFileLanguage(filename)
	if fileLang != Unknown {
		return fileLang
	}

	// 2. Fall back to track/album language
	trackLang := DetectTrackLanguage(albumTitle, trackTitle)
	if trackLang != Unknown {
		return trackLang
	}

	return Unknown
}

