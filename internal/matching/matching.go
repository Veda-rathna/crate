// Package matching provides relaxed, diacritic- and punctuation-tolerant
// metadata comparison helpers shared across Arcod, Soulseek candidate scoring,
// library import, and local-import reconciliation.
package matching

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	// Bracketed/parenthesized edition, soundtrack, language, and feature noise.
	// Deliberately preserves version tags (live, remix, acoustic, instrumental,
	// acapella, karaoke, cover, tribute, demo, sped up, slowed, nightcore) so
	// ExtractVariants and variant-mismatch guards can still see them.
	noiseParenRegex = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*\b(?:remaster(?:ed)?|deluxe|expanded|special|anniversary|collector(?:'?s)?|super\s+deluxe|edition|version|edit|mono|stereo|bonus(?:\s+track)?|explicit|clean|single|ep|ost|soundtrack|motion\s+picture|original\s+score|from\b|feat\.?|ft\.?|featuring|with\b|prod\.?|(?:tamil|telugu|hindi|malayalam|kannada|english)(?:\s+(?:version|ost|audio|original))?|disc\s*\d+|cd\s*\d+|\d{4})(?:\b|[^\w\)\]])[^\)\]]*[\)\]]`)

	// Trailing dash qualifiers, e.g. "Song - 2011 Remaster" or "Song - From \"Leo\"".
	noiseDashRegex = regexp.MustCompile(`(?i)\s+[-–—]\s+(?:\d{4}\s+)?(?:remaster(?:ed)?(?:\s+\d{4})?|deluxe(?:\s+edition)?|expanded(?:\s+edition)?|anniversary(?:\s+edition)?|single|ep|from\b.*|radio\s+edit|album\s+version|bonus\s+track|explicit|clean|soundtrack|ost|original\s+motion\s+picture\s+soundtrack|tamil|telugu|hindi|malayalam|kannada).*$`)

	// Trailing or parenthesized featuring credits.
	featParenRegex = regexp.MustCompile(`(?i)\s*[\(\[]\s*(?:feat\.?|ft\.?|featuring|with)\b[^\)\]]*[\)\]]`)
	featTailRegex  = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring)\b.*$`)

	// Disc suffix in album tags (e.g. "The Wall (Disc 1)", "The Wall - CD 2").
	discSuffixRegex = regexp.MustCompile(`(?i)(?:\s*[\(\[]\s*(?:disc|disk|cd)\s*\d+(?:\s*of\s*\d+)?\s*[\)\]]|\s*[-–—]\s*(?:disc|disk|cd)\s*\d+)\s*$`)

	// Edition/year/format tags stripped for CleanSearchQuery.
	searchQueryNoiseRegex = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*\b(?:remaster(?:ed)?|deluxe|expanded|special|anniversary|collector(?:'?s)?|edition|mono|stereo|bonus|explicit|clean|ost|soundtrack|motion\s+picture|from\b|feat\.?|ft\.?|featuring|disc\s*\d+|cd\s*\d+|\d{4})(?:\b|[^\w\)\]])[^\)\]]*[\)\]]`)
	searchPunctRegex      = regexp.MustCompile(`["?!()\[\]{}*<>|:/\\]+`)
	multiSpaceRegex       = regexp.MustCompile(`\s+`)

	// Qualifier segment extraction for version-mismatch detection.
	parenSegmentRegex = regexp.MustCompile(`[\(\[]([^\)\]]+)[\)\]]`)
)

// variantChecks defines version keywords that denote a distinct musical
// version (live, instrumental, remix, etc.) rather than a remaster/edition.
var variantChecks = []struct {
	key     string
	pattern *regexp.Regexp
}{
	{"live", regexp.MustCompile(`(?i)\b(live(?:\s+(?:at|in|from|on|version|session|recording))?)\b`)},
	{"instrumental", regexp.MustCompile(`(?i)\binstrumental\b`)},
	{"acapella", regexp.MustCompile(`(?i)\ba\s*capella\b|\bacappella\b`)},
	{"karaoke", regexp.MustCompile(`(?i)\bkaraoke\b`)},
	{"remix", regexp.MustCompile(`(?i)\bremix(?:ed)?\b`)},
	{"acoustic", regexp.MustCompile(`(?i)\bacoustic\b`)},
	{"demo", regexp.MustCompile(`(?i)\bdemo\b`)},
	{"cover", regexp.MustCompile(`(?i)\bcover\b`)},
	{"tribute", regexp.MustCompile(`(?i)\btribute\b`)},
	{"sped_up", regexp.MustCompile(`(?i)\bsped\s*up\b`)},
	{"slowed", regexp.MustCompile(`(?i)\bslowed\b`)},
	{"nightcore", regexp.MustCompile(`(?i)\bnightcore\b`)},
	{"8d", regexp.MustCompile(`(?i)\b8d(?:\s+audio)?\b`)},
}

// hardRejectVariants are versions that are virtually never an acceptable substitute
// for a standard track unless explicitly requested in the target title.
var hardRejectVariants = map[string]bool{
	"karaoke":      true,
	"tribute":      true,
	"instrumental": true,
	"acapella":     true,
	"cover":        true,
	"sped_up":      true,
	"slowed":       true,
	"nightcore":    true,
	"8d":           true,
}

// FoldLatinDiacritics decomposes Latin accents and drops combining marks in the
// U+0300..U+036F block while leaving non-Latin combining marks (such as Indic
// vowel signs and virama) untouched.
func FoldLatinDiacritics(s string) string {
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if r >= 0x0300 && r <= 0x036F && unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}

// tokenize converts s into normalized lowercase word tokens.
// When stripApostrophes is true, apostrophes inside words ("Don't") collapse
// to "dont"; when false, apostrophes split into spaces ("don t").
func tokenize(s string, stripApostrophes bool) []string {
	s = FoldLatinDiacritics(s)
	s = strings.ToLower(s)

	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\'', '’', '‘', 'ʼ', '`':
			if !stripApostrophes {
				b.WriteByte(' ')
			}
		case '&', '+':
			b.WriteString(" and ")
		default:
			if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		}
	}
	return strings.Fields(b.String())
}

// Normalize returns a canonical space-joined normalized representation of s
// with diacritics folded, punctuation removed, '&' normalized to 'and', and
// apostrophes collapsed.
func Normalize(s string) string {
	return strings.Join(tokenize(s, true), " ")
}

// collapseInitials joins runs of single ASCII letters (e.g. ["a", "r", "rahman"] -> ["ar", "rahman"]).
func collapseInitials(tokens []string) []string {
	if len(tokens) < 2 {
		return tokens
	}
	var out []string
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			out = append(out, run.String())
			run.Reset()
		}
	}
	for _, tok := range tokens {
		if len(tok) == 1 && tok[0] >= 'a' && tok[0] <= 'z' {
			run.WriteString(tok)
		} else {
			flush()
			out = append(out, tok)
		}
	}
	flush()
	return out
}

// StripQualifiers removes edition, remaster, soundtrack, language, and feature
// metadata noise while keeping the core title intact.
func StripQualifiers(s string) string {
	prev := ""
	cur := strings.TrimSpace(s)
	for cur != prev {
		prev = cur
		cur = featParenRegex.ReplaceAllString(cur, "")
		cur = noiseParenRegex.ReplaceAllString(cur, "")
		cur = featTailRegex.ReplaceAllString(cur, "")
		cur = noiseDashRegex.ReplaceAllString(cur, "")
		cur = strings.TrimSpace(cur)
	}
	return cur
}

// StripDiscSuffix removes trailing disc indicators like "(Disc 1)" or "- CD 2".
func StripDiscSuffix(s string) string {
	return strings.TrimSpace(discSuffixRegex.ReplaceAllString(strings.TrimSpace(s), ""))
}

// NormalizeAlbumGroupKey produces a stable grouping key for an album title
// during library import so minor punctuation/diacritic/disc-tag differences
// collapse into one album.
func NormalizeAlbumGroupKey(title string) string {
	stripped := StripDiscSuffix(title)
	if norm := Normalize(stripped); norm != "" {
		return norm
	}
	return strings.ToLower(strings.TrimSpace(title))
}

// PrimaryArtist strips trailing or parenthesized featuring credits when no
// explicit album-artist tag is present.
func PrimaryArtist(artist string) string {
	s := featParenRegex.ReplaceAllString(artist, "")
	s = featTailRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" {
		return strings.TrimSpace(artist)
	}
	return s
}

// ExtractVariants detects distinct musical version markers (live, remix,
// instrumental, karaoke, etc.) in qualifier positions without misclassifying
// regular words in song titles like "Live Forever" or "Cover Me".
func ExtractVariants(s string) map[string]bool {
	out := make(map[string]bool)
	if strings.TrimSpace(s) == "" {
		return out
	}

	var segments []string
	for _, m := range parenSegmentRegex.FindAllStringSubmatch(s, -1) {
		if len(m) > 1 {
			segments = append(segments, m[1])
		}
	}
	// Check segments after " - " or " – " / " — "
	for _, sep := range []string{" - ", " – ", " — "} {
		if _, after, ok := strings.Cut(s, sep); ok {
			segments = append(segments, after)
		}
	}

	for _, seg := range segments {
		for _, vc := range variantChecks {
			if vc.pattern.MatchString(seg) {
				out[vc.key] = true
			}
		}
	}

	// Unambiguous terms anywhere in the string when outside regular single-word titles
	lower := strings.ToLower(FoldLatinDiacritics(s))
	unambiguous := []struct {
		key string
		re  *regexp.Regexp
	}{
		{"karaoke", regexp.MustCompile(`\bkaraoke\b`)},
		{"acapella", regexp.MustCompile(`\ba\s*capella\b|\bacappella\b`)},
		{"instrumental", regexp.MustCompile(`\binstrumental\b`)},
		{"sped_up", regexp.MustCompile(`\bsped\s*up\b`)},
		{"slowed", regexp.MustCompile(`\bslowed\b`)},
		{"nightcore", regexp.MustCompile(`\bnightcore\b`)},
		{"8d", regexp.MustCompile(`\b8d\s+audio\b`)},
		{"tribute", regexp.MustCompile(`\btribute\s+to\b`)},
	}
	for _, u := range unambiguous {
		if u.re.MatchString(lower) {
			out[u.key] = true
		}
	}

	// Detect trailing "live" / "remix" / "acoustic" in multi-word filenames without parens
	// (e.g. "Artist - Track Remix.flac" or "Track Live at Wembley.flac").
	if len(strings.Fields(s)) >= 2 {
		for _, kw := range []struct {
			key string
			re  *regexp.Regexp
		}{
			{"live", regexp.MustCompile(`(?i)\blive\s+(?:at|in|from|on)\b|\blive\s+version\b`)},
			{"remix", regexp.MustCompile(`(?i)\bremix\b`)},
			{"acoustic", regexp.MustCompile(`(?i)\bacoustic\s+version\b`)},
		} {
			if kw.re.MatchString(lower) {
				out[kw.key] = true
			}
		}
	}

	return out
}

// HasUnrequestedVariant returns true if candidate carries a version marker that
// was not requested in target. The second return value is true for hard-reject
// variants (karaoke, tribute, instrumental, acapella, cover, sped_up, etc.).
func HasUnrequestedVariant(candidate, target string) (hasMismatch bool, hardReject bool) {
	candVars := ExtractVariants(candidate)
	if len(candVars) == 0 {
		return false, false
	}
	targVars := ExtractVariants(target)
	for k := range candVars {
		if !targVars[k] {
			hasMismatch = true
			if hardRejectVariants[k] {
				hardReject = true
			}
		}
	}
	return hasMismatch, hardReject
}

// CleanSearchQuery builds a clean search query from artist and title, stripping
// edition/remaster/from-movie/featuring noise and search-breaking punctuation
// while keeping meaningful words and preserving simple inputs cleanly.
func CleanSearchQuery(artist, title string) string {
	cleanTitle := strings.TrimSpace(title)
	cleanTitle = featParenRegex.ReplaceAllString(cleanTitle, "")
	cleanTitle = searchQueryNoiseRegex.ReplaceAllString(cleanTitle, "")
	cleanTitle = featTailRegex.ReplaceAllString(cleanTitle, "")
	cleanTitle = noiseDashRegex.ReplaceAllString(cleanTitle, "")
	cleanTitle = searchPunctRegex.ReplaceAllString(cleanTitle, " ")
	cleanTitle = strings.TrimSpace(multiSpaceRegex.ReplaceAllString(cleanTitle, " "))
	if cleanTitle == "" {
		cleanTitle = strings.TrimSpace(multiSpaceRegex.ReplaceAllString(searchPunctRegex.ReplaceAllString(title, " "), " "))
	}

	cleanArtist := strings.TrimSpace(multiSpaceRegex.ReplaceAllString(searchPunctRegex.ReplaceAllString(artist, " "), " "))
	if cleanArtist == "" {
		return cleanTitle
	}
	if cleanTitle == "" {
		return cleanArtist
	}
	return cleanArtist + " " + cleanTitle
}

var numberedSubtitleRegex = regexp.MustCompile(`(?i)^(?:part|pt\.?|vol\.?|volume|chapter|ch\.?|act|movement|mov\.?|intro|outro|interlude|reprise|disc|cd|\d+|[ivx]+)\b`)

// titleTokenVariants generates candidate token lists for a title (full, stripped,
// apostrophe-spaced, initial-collapsed, and optional subtitle lead before " - ").
func titleTokenVariants(s string) [][]string {
	var out [][]string
	seen := make(map[string]bool)
	add := func(toks []string) {
		if len(toks) == 0 {
			return
		}
		key := strings.Join(toks, " ")
		if !seen[key] {
			seen[key] = true
			out = append(out, toks)
		}
	}

	stripped := StripQualifiers(s)
	for _, form := range []string{s, stripped} {
		t1 := tokenize(form, true)
		add(t1)
		add(collapseInitials(t1))
		t2 := tokenize(form, false)
		add(t2)
		add(collapseInitials(t2))
	}

	// Handle subtitle-separated titles like "Hukum - Thalaivar Alappara",
	// except when the subtitle distinguishes parts/movements (e.g. "Part 1").
	for _, sep := range []string{" - ", " – ", " — ", ": "} {
		if lead, rest, ok := strings.Cut(stripped, sep); ok {
			if numberedSubtitleRegex.MatchString(strings.TrimSpace(rest)) {
				continue
			}
			leadToks := tokenize(lead, true)
			if len(leadToks) >= 2 || (len(leadToks) == 1 && len([]rune(leadToks[0])) >= 4) {
				add(leadToks)
			}
		}
	}

	return out
}

// containsTokenSeq returns true if needle appears as a contiguous sub-slice of haystack.
func containsTokenSeq(haystack, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func tokenListsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func tokenOverlapRatio(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setB := make(map[string]bool, len(b))
	for _, t := range b {
		setB[t] = true
	}
	hits := 0
	for _, t := range a {
		if setB[t] {
			hits++
		}
	}
	denom := len(a)
	if len(b) > denom {
		denom = len(b)
	}
	return float64(hits) / float64(denom)
}

func allTokensPresent(haystack, needle []string) bool {
	if len(needle) == 0 {
		return false
	}
	setH := make(map[string]bool, len(haystack))
	for _, t := range haystack {
		setH[t] = true
	}
	for _, t := range needle {
		if !setH[t] {
			return false
		}
	}
	return true
}

// TitlesMatch checks whether two track or album titles match under relaxed rules
// (case-insensitive, diacritics folded, punctuation/apostrophe normalized,
// '&'/'and' unified, edition/remaster/featuring noise stripped).
// Rejects mismatches if one has a distinct unrequested musical variant like
// Karaoke/Tribute/Instrumental/Remix/Live when the other doesn't? No — for
// pure title matching (e.g. album/track reconciliation) we allow stripped match,
// while Arcod/downloader layer checks ExtractVariants explicitly.
func TitlesMatch(target, candidate string) bool {
	return TitleMatchQuality(target, candidate) > 0
}

// TitleMatchQuality returns a confidence score (0 = no match, 100 = exact normalized,
// 85 = stripped core match, 70 = prefix/lead match, 55 = high token overlap).
func TitleMatchQuality(target, candidate string) int {
	tNorm := tokenize(target, true)
	cNorm := tokenize(candidate, true)
	if len(tNorm) == 0 || len(cNorm) == 0 {
		return 0
	}

	// 1. Exact normalized match
	if tokenListsEqual(tNorm, cNorm) ||
		tokenListsEqual(collapseInitials(tNorm), collapseInitials(cNorm)) ||
		tokenListsEqual(tokenize(target, false), tokenize(candidate, false)) {
		return 100
	}

	// 2. Stripped core normalized match
	tCore := tokenize(StripQualifiers(target), true)
	cCore := tokenize(StripQualifiers(candidate), true)
	if len(tCore) > 0 && len(cCore) > 0 {
		if tokenListsEqual(tCore, cCore) ||
			tokenListsEqual(collapseInitials(tCore), collapseInitials(cCore)) ||
			tokenListsEqual(tokenize(StripQualifiers(target), false), tokenize(StripQualifiers(candidate), false)) {
			return 85
		}
	}

	// 3. Lead/contiguous token match across variants
	tVars := titleTokenVariants(target)
	cVars := titleTokenVariants(candidate)
	for _, tv := range tVars {
		for _, cv := range cVars {
			if tokenListsEqual(tv, cv) {
				return 75
			}
			// Allow longer core to match shorter core when one has a subtitle (>= 2 tokens or >= 4 runes)
			if len(tv) >= 2 && containsTokenSeq(cv, tv) {
				return 65
			}
			if len(cv) >= 2 && containsTokenSeq(tv, cv) {
				return 65
			}
		}
	}

	// 4. High token overlap for multi-word titles (e.g. minor word order/filler differences)
	if len(tCore) >= 3 && len(cCore) >= 3 {
		if tokenOverlapRatio(tCore, cCore) >= 0.75 {
			return 55
		}
	}

	return 0
}

// splitArtists splits collaboration strings like "A, B & C" or "A feat. B"
// into individual artist names alongside the full string.
var artistSplitRegex = regexp.MustCompile(`(?i)\s*(?:,|;|/|\s&\s|\s+and\s+|\s+x\s+|\s+feat\.?\s+|\s+ft\.?\s+|\s+featuring\s+|\s+with\s+)\s*`)

func artistVariants(s string) [][]string {
	var out [][]string
	seen := make(map[string]bool)
	add := func(toks []string) {
		if len(toks) == 0 {
			return
		}
		key := strings.Join(toks, " ")
		if !seen[key] {
			seen[key] = true
			out = append(out, toks)
		}
	}

	addForms := func(part string) {
		t1 := tokenize(part, true)
		add(t1)
		add(collapseInitials(t1))
		t2 := tokenize(part, false)
		add(t2)
		add(collapseInitials(t2))
		// Optional leading "the " tolerance (only if remaining >= 1 token of >= 3 runes)
		if len(t1) >= 2 && t1[0] == "the" {
			rest := t1[1:]
			if len(rest) >= 2 || len([]rune(rest[0])) >= 3 {
				add(rest)
			}
		}
	}

	addForms(s)
	addForms(PrimaryArtist(s))
	for _, p := range artistSplitRegex.Split(s, -1) {
		p = strings.TrimSpace(p)
		if p != "" {
			addForms(p)
		}
	}
	return out
}

// ArtistsMatch checks whether two artist names match under relaxed rules
// (diacritics, apostrophes, initialisms like "A.R." vs "AR", optional "The",
// and split collaboration lists).
func ArtistsMatch(target, candidate string) bool {
	if strings.TrimSpace(target) == "" || strings.TrimSpace(candidate) == "" {
		return false
	}
	tVars := artistVariants(target)
	cVars := artistVariants(candidate)
	for _, tv := range tVars {
		for _, cv := range cVars {
			if tokenListsEqual(tv, cv) {
				return true
			}
		}
	}
	return false
}

func pathTokens(filePath string) (full []string, base []string) {
	// Normalize both '/' and '\' separators so Windows/Unix Soulseek paths split cleanly.
	p := strings.ReplaceAll(filePath, `\`, "/")
	baseName := filepath.Base(p)
	if ext := filepath.Ext(baseName); ext != "" {
		baseName = strings.TrimSuffix(baseName, ext)
	}
	return tokenize(p, true), tokenize(baseName, true)
}

func pathTokensSpaced(filePath string) (full []string, base []string) {
	p := strings.ReplaceAll(filePath, `\`, "/")
	baseName := filepath.Base(p)
	if ext := filepath.Ext(baseName); ext != "" {
		baseName = strings.TrimSuffix(baseName, ext)
	}
	return tokenize(p, false), tokenize(baseName, false)
}

// MatchTitleInPath checks whether a Soulseek file path matches trackTitle
// using whole-token matching (avoiding substring-inside-word false positives
// like "one" matching "everyone") while relaxing punctuation, diacritics,
// apostrophes, and edition/featuring noise.
func MatchTitleInPath(filePath, trackTitle string) bool {
	if strings.TrimSpace(trackTitle) == "" {
		return false
	}
	full1, base1 := pathTokens(filePath)
	full2, base2 := pathTokensSpaced(filePath)
	full1Col := collapseInitials(full1)
	base1Col := collapseInitials(base1)

	variants := titleTokenVariants(trackTitle)
	for _, needle := range variants {
		if containsTokenSeq(base1, needle) || containsTokenSeq(base2, needle) || containsTokenSeq(base1Col, needle) {
			return true
		}
		if containsTokenSeq(full1, needle) || containsTokenSeq(full2, needle) || containsTokenSeq(full1Col, needle) {
			return true
		}
	}

	// For multi-word titles (>= 3 tokens), also accept all core tokens present in basename
	// (handles reordered or slightly decorated filenames).
	core := tokenize(StripQualifiers(trackTitle), true)
	if len(core) >= 3 && allTokensPresent(base1, core) {
		return true
	}

	return false
}

// MatchArtistInPath checks whether artistName matches any segment/token sequence
// in a Soulseek file path.
func MatchArtistInPath(filePath, artistName string) bool {
	if strings.TrimSpace(artistName) == "" {
		return false
	}
	full1, _ := pathTokens(filePath)
	full2, _ := pathTokensSpaced(filePath)
	full1Col := collapseInitials(full1)
	full2Col := collapseInitials(full2)

	for _, needle := range artistVariants(artistName) {
		if containsTokenSeq(full1, needle) || containsTokenSeq(full2, needle) ||
			containsTokenSeq(full1Col, needle) || containsTokenSeq(full2Col, needle) {
			return true
		}
	}
	return false
}

// MatchAlbumInPath checks whether albumTitle matches in the file's directory/path.
func MatchAlbumInPath(filePath, albumTitle string) bool {
	if strings.TrimSpace(albumTitle) == "" {
		return false
	}
	full1, _ := pathTokens(filePath)
	full2, _ := pathTokensSpaced(filePath)
	for _, needle := range titleTokenVariants(albumTitle) {
		if containsTokenSeq(full1, needle) || containsTokenSeq(full2, needle) {
			return true
		}
	}
	return false
}
