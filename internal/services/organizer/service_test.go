package organizer

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/naming"
)

type orgEnv struct {
	svc       *Service
	queries   *db.Queries
	downloads string
	library   string
	track     *models.Track
}

// newOrgEnv wires a real in-memory DB with one artist/album/track and a fake
// downloaded file waiting in the downloads dir.
func newOrgEnv(t *testing.T, year *int) *orgEnv {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	queries := db.NewQueries(database)

	artist := &models.Artist{Name: "Radiohead", Provider: "test", ProviderID: "a1", Status: models.ArtistStatusWatched}
	if err := queries.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	album := &models.Album{ArtistID: artist.ID, Title: "OK Computer", Year: year, Provider: "test", ProviderID: "al1", RecordType: "album", Status: models.AlbumStatusWatched}
	if err := queries.CreateAlbum(album); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{AlbumID: album.ID, Title: "Karma Police", TrackNumber: 6, DiscNumber: 1, Provider: "test", ProviderID: "t1", Status: models.TrackStatusWanted}
	if err := queries.CreateTrack(track); err != nil {
		t.Fatal(err)
	}

	downloads := t.TempDir()
	srcName := "raw download.flac"
	if err := os.WriteFile(filepath.Join(downloads, srcName), []byte("audio"), 0644); err != nil {
		t.Fatal(err)
	}
	track.FilePath = &srcName

	library := t.TempDir()
	return &orgEnv{
		svc:       NewService(queries, downloads, library),
		queries:   queries,
		downloads: downloads,
		library:   library,
		track:     track,
	}
}

func (e *orgEnv) mustOrganize(t *testing.T) {
	t.Helper()
	if err := e.svc.Organize(e.track); err != nil {
		t.Fatalf("Organize: %v", err)
	}
}

func (e *orgEnv) assertLibraryFile(t *testing.T, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(e.library, rel)); err != nil {
		t.Errorf("expected library file %q: %v", rel, err)
	}
	stored, err := e.queries.GetTrackWithMeta(e.track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FilePath == nil || *stored.FilePath != rel {
		got := "<nil>"
		if stored.FilePath != nil {
			got = *stored.FilePath
		}
		t.Errorf("stored file_path = %q, want %q", got, rel)
	}
}

func intPtr(n int) *int { return &n }

func TestOrganizeDefaultTemplate(t *testing.T) {
	env := newOrgEnv(t, intPtr(1997))
	env.mustOrganize(t)
	env.assertLibraryFile(t, "Radiohead/OK Computer (1997)/06 - Karma Police.flac")
}

func TestOrganizeDefaultTemplateNoYear(t *testing.T) {
	env := newOrgEnv(t, nil)
	env.mustOrganize(t)
	env.assertLibraryFile(t, "Radiohead/OK Computer/06 - Karma Police.flac")
}

func TestOrganizeCustomTemplate(t *testing.T) {
	env := newOrgEnv(t, intPtr(1997))
	env.queries.SetSetting(naming.SettingKey, "{artist}/{artist} - {year} - {album}/{track:2} {title}")
	env.mustOrganize(t)
	env.assertLibraryFile(t, "Radiohead/Radiohead - 1997 - OK Computer/06 Karma Police.flac")
}

func TestOrganizeInvalidStoredTemplateFallsBack(t *testing.T) {
	env := newOrgEnv(t, intPtr(1997))
	env.queries.SetSetting(naming.SettingKey, "{bogus}/{title}")
	env.mustOrganize(t)
	env.assertLibraryFile(t, "Radiohead/OK Computer (1997)/06 - Karma Police.flac")
}

func TestOrganizeUpgradeRemovesReplacedFile(t *testing.T) {
	env := newOrgEnv(t, intPtr(1997))

	// Simulate a previous download organized under a different template.
	oldRel := filepath.Join("Radiohead", "Radiohead - 1997 - OK Computer", "06 Karma Police.mp3")
	oldAbs := filepath.Join(env.library, oldRel)
	if err := os.MkdirAll(filepath.Dir(oldAbs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldAbs, []byte("old audio"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := env.queries.UpdateTrackFilePath(env.track.ID, oldRel); err != nil {
		t.Fatal(err)
	}

	env.mustOrganize(t)
	env.assertLibraryFile(t, "Radiohead/OK Computer (1997)/06 - Karma Police.flac")

	if _, err := os.Stat(oldAbs); !os.IsNotExist(err) {
		t.Errorf("replaced file should be removed, stat err = %v", err)
	}
	// The now-empty album dir is pruned; the shared artist dir stays.
	if _, err := os.Stat(filepath.Dir(oldAbs)); !os.IsNotExist(err) {
		t.Errorf("empty album dir should be pruned, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.library, "Radiohead")); err != nil {
		t.Errorf("artist dir still in use, should not be pruned: %v", err)
	}
}

func TestOrganizeSamePathOverwrites(t *testing.T) {
	env := newOrgEnv(t, intPtr(1997))

	rel := "Radiohead/OK Computer (1997)/06 - Karma Police.flac"
	abs := filepath.Join(env.library, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("old audio"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := env.queries.UpdateTrackFilePath(env.track.ID, rel); err != nil {
		t.Fatal(err)
	}

	env.mustOrganize(t)
	env.assertLibraryFile(t, rel)

	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "audio" {
		t.Errorf("file should be the new download, got %q", data)
	}
}

func TestOrganizeLeavesFilesOutsideLibrary(t *testing.T) {
	env := newOrgEnv(t, intPtr(1997))

	// Imported tracks can point at absolute paths outside the library.
	outside := t.TempDir()
	oldAbs := filepath.Join(outside, "karma.flac")
	if err := os.WriteFile(oldAbs, []byte("imported"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := env.queries.UpdateTrackFilePath(env.track.ID, oldAbs); err != nil {
		t.Fatal(err)
	}

	env.mustOrganize(t)
	env.assertLibraryFile(t, "Radiohead/OK Computer (1997)/06 - Karma Police.flac")

	if _, err := os.Stat(oldAbs); err != nil {
		t.Errorf("file outside library must never be deleted: %v", err)
	}
}

func TestFindFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "complete", "user")
	os.MkdirAll(sub, 0755)
	target := filepath.Join(sub, "song.flac")
	os.WriteFile(target, []byte("data"), 0644)

	found, err := findFile(dir, "song.flac")
	if err != nil {
		t.Fatal(err)
	}
	if found != target {
		t.Errorf("findFile = %q, want %q", found, target)
	}
}

func TestFindFileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := findFile(dir, "nonexistent.flac")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")

	data := make([]byte, 2<<20)
	for i := range data {
		data[i] = byte(i % 251)
	}
	os.WriteFile(src, data, 0644)

	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(data) {
		t.Fatalf("copied %d bytes, want %d", len(got), len(data))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("mismatch at byte %d", i)
			break
		}
	}
}

func TestCopyFileSrcMissing(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "dst"))
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func writeValidMinimalFLAC(t *testing.T, path string, comments map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("fLaC")
	buf.WriteByte(0x80)
	buf.Write([]byte{0x00, 0x00, 0x22})
	si := make([]byte, 34)
	binary.BigEndian.PutUint16(si[0:2], 4096)
	binary.BigEndian.PutUint16(si[2:4], 4096)
	si[10] = 0xAC
	si[11] = 0x44
	si[12] = byte((44100&0xF)<<4) | byte(1<<1) | byte(15>>4)
	si[13] = byte(15&0xF) << 4
	buf.Write(si)
	buf.Write([]byte{0xFF, 0xF8, 0x00, 0x00})

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	if len(comments) > 0 {
		f, err := flac.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cmt := flacvorbis.New()
		for k, v := range comments {
			cmt.Add(k, v)
		}
		block := cmt.Marshal()
		f.Meta = append(f.Meta, &block)
		if err := f.Save(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrganizeMultipleSourcesStayOneAlbum(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	queries := db.NewQueries(database)

	artist := &models.Artist{Name: "Anirudh Ravichander", Provider: "spotify", ProviderID: "ar1", Status: models.ArtistStatusWatched}
	if err := queries.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	yr := 2023
	album := &models.Album{ArtistID: artist.ID, Title: "Jailer", Year: &yr, Provider: "spotify", ProviderID: "al1", RecordType: "album", Status: models.AlbumStatusWatched}
	if err := queries.CreateAlbum(album); err != nil {
		t.Fatal(err)
	}
	tr1 := &models.Track{AlbumID: album.ID, Title: "Hukum", TrackNumber: 1, DiscNumber: 1, Provider: "spotify", ProviderID: "t1", Status: models.TrackStatusWanted}
	tr2 := &models.Track{AlbumID: album.ID, Title: "Kaavaalaa", TrackNumber: 2, DiscNumber: 1, Provider: "spotify", ProviderID: "t2", Status: models.TrackStatusWanted}
	if err := queries.CreateTrack(tr1); err != nil {
		t.Fatal(err)
	}
	if err := queries.CreateTrack(tr2); err != nil {
		t.Fatal(err)
	}

	downloads := t.TempDir()
	lib := t.TempDir()
	svc := NewService(queries, downloads, lib)

	f1 := "1 - Anirudh - Hukum.flac"
	f2 := "02 - Kaavaalaa.flac"
	writeValidMinimalFLAC(t, filepath.Join(downloads, f1), map[string]string{
		"ALBUMARTIST":         "Anirudh Ravichander",
		"ALBUM":               "Jailer (Original Motion Picture Soundtrack)",
		"MUSICBRAINZ_ALBUMID": "mb-1",
	})
	writeValidMinimalFLAC(t, filepath.Join(downloads, f2), map[string]string{
		"ALBUMARTIST":         "Shilpa Rao, Anirudh Ravichander",
		"ALBUM ARTIST":        "Various Artists",
		"ALBUM":               "Jailer - Single",
		"COMPILATION":         "1",
		"MUSICBRAINZ_ALBUMID": "mb-2",
	})

	tr1.FilePath = &f1
	tr2.FilePath = &f2
	if err := svc.Organize(tr1); err != nil {
		t.Fatal(err)
	}
	if err := svc.Organize(tr2); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		filepath.Join("Anirudh Ravichander", "Jailer (2023)", "01 - Hukum.flac"),
		filepath.Join("Anirudh Ravichander", "Jailer (2023)", "02 - Kaavaalaa.flac"),
	} {
		parsed, err := flac.ParseFile(filepath.Join(lib, rel))
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		var cmt *flacvorbis.MetaDataBlockVorbisComment
		for _, b := range parsed.Meta {
			if b.Type == flac.VorbisComment {
				cmt, _ = flacvorbis.ParseFromMetaDataBlock(*b)
			}
		}
		if cmt == nil {
			t.Fatalf("no vorbis comments in %s", rel)
		}
		if v, _ := cmt.Get("ALBUMARTIST"); len(v) != 1 || v[0] != "Anirudh Ravichander" {
			t.Errorf("%s ALBUMARTIST = %v, want [Anirudh Ravichander]", rel, v)
		}
		if v, _ := cmt.Get("ALBUM"); len(v) != 1 || v[0] != "Jailer" {
			t.Errorf("%s ALBUM = %v, want [Jailer]", rel, v)
		}
		if v, _ := cmt.Get("COMPILATION"); len(v) != 0 {
			t.Errorf("%s COMPILATION should be stripped, got %v", rel, v)
		}
		if v, _ := cmt.Get("MUSICBRAINZ_ALBUMID"); len(v) != 0 {
			t.Errorf("%s MUSICBRAINZ_ALBUMID should be stripped, got %v", rel, v)
		}
	}
}

func TestFindFileWithHintAvoidsCollision(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "user1", "Album A", "01 - Intro.flac")
	pathB := filepath.Join(dir, "user2", "Album B", "01 - Intro.flac")
	os.MkdirAll(filepath.Dir(pathA), 0755)
	os.MkdirAll(filepath.Dir(pathB), 0755)
	os.WriteFile(pathA, []byte("A"), 0644)
	os.WriteFile(pathB, []byte("B"), 0644)

	got, err := findFileWithHint(dir, "01 - Intro.flac", `@@user2\Music\Album B\01 - Intro.flac`)
	if err != nil {
		t.Fatal(err)
	}
	if got != pathB {
		t.Errorf("findFileWithHint = %q, want %q", got, pathB)
	}
}
