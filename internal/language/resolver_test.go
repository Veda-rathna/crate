package language

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
	"golang.org/x/time/rate"
)

type memCache struct {
	data map[string][]byte
}

func newMemCache() *memCache {
	return &memCache{data: make(map[string][]byte)}
}

func (m *memCache) Get(key string) ([]byte, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *memCache) Set(key string, value []byte, ttl time.Duration) {
	m.data[key] = append([]byte(nil), value...)
}

type memSettings map[string]string

func (m memSettings) AllSettings() (map[string]string, error) {
	return m, nil
}

func TestResolverLocalFastPathSkipsNetwork(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := NewResolver(newMemCache(), memSettings{}, ResolverConfig{
		ITunesBaseURL: srv.URL,
		ITunesLimiter: rate.NewLimiter(rate.Inf, 10),
	})

	album := &pb.AlbumDetail{
		Title:    "Jailer",
		Metadata: map[string]string{"work_languages": "tam", "text_language": "eng", "text_script": "Latn"},
	}
	allowed, lang := res.CheckAlbum(context.Background(), "Anirudh Ravichander", []string{Tamil}, album)
	if !allowed || lang != Tamil {
		t.Fatalf("expected allowed Tamil, got allowed=%v lang=%q", allowed, lang)
	}
	if calls.Load() != 0 {
		t.Errorf("expected 0 network calls for local fast path, got %d", calls.Load())
	}
}

func TestResolverITunesUPCLookupAndCache(t *testing.T) {
	var lookupCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lookup" {
			lookupCalls.Add(1)
			if r.URL.Query().Get("upc") != "8901234567890" || r.URL.Query().Get("country") != "in" {
				t.Errorf("unexpected lookup query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resultCount": 1,
				"results": []map[string]any{
					{
						"wrapperType":      "collection",
						"artistName":       "Anirudh Ravichander",
						"collectionName":   "Coolie (Original Motion Picture Soundtrack)",
						"primaryGenreName": "Tamil",
					},
				},
			})
			return
		}
		t.Errorf("unexpected endpoint hit: %s", r.URL.Path)
	}))
	defer srv.Close()

	cache := newMemCache()
	res := NewResolver(cache, memSettings{}, ResolverConfig{
		ITunesBaseURL: srv.URL,
		ITunesLimiter: rate.NewLimiter(rate.Inf, 10),
	})

	album := &pb.AlbumDetail{
		Title:    "Coolie",
		Metadata: map[string]string{"upc": "890-1234567890", "text_language": "eng", "text_script": "Latn"},
		Tracks:   []*pb.TrackInfo{{Title: "Chikitu"}},
	}

	allowed, lang := res.CheckAlbum(context.Background(), "Anirudh Ravichander", []string{Tamil}, album)
	if !allowed || lang != Tamil {
		t.Fatalf("expected iTunes UPC Tamil resolution, got allowed=%v lang=%q", allowed, lang)
	}

	// Second call must hit cache without making another HTTP call
	allowed, lang = res.CheckAlbum(context.Background(), "Anirudh Ravichander", []string{Tamil}, album)
	if !allowed || lang != Tamil {
		t.Fatalf("expected cached Tamil resolution, got allowed=%v lang=%q", allowed, lang)
	}
	if lookupCalls.Load() != 1 {
		t.Errorf("expected exactly 1 iTunes HTTP call across cached lookups, got %d", lookupCalls.Load())
	}
}

func TestResolverITunesSearchSkipsDubsAndMatchesSingleSuffix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("expected /search fallback, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resultCount": 2,
			"results": []map[string]any{
				{
					"wrapperType":      "collection",
					"artistName":       "Anirudh Ravichander",
					"collectionName":   `Aaja Raja (From "KH x RK Reunion") (Telugu) - Single`,
					"primaryGenreName": "Telugu",
				},
				{
					"wrapperType":      "collection",
					"artistName":       "Anirudh Ravichander",
					"collectionName":   `Aaja Raja (From "KH x RK Reunion") - Single`,
					"primaryGenreName": "Tamil",
				},
			},
		})
	}))
	defer srv.Close()

	res := NewResolver(newMemCache(), memSettings{}, ResolverConfig{
		ITunesBaseURL: srv.URL,
		ITunesLimiter: rate.NewLimiter(rate.Inf, 10),
	})

	album := &pb.AlbumDetail{
		Title:  `Aaja Raja (From "KH x RK Reunion")`,
		Tracks: []*pb.TrackInfo{{Title: "Aaja Raja"}},
	}
	allowed, lang := res.CheckAlbum(context.Background(), "Anirudh Ravichander", []string{Tamil}, album)
	if !allowed || lang != Tamil {
		t.Fatalf("expected iTunes search to skip Telugu dub and resolve Tamil, got allowed=%v lang=%q", allowed, lang)
	}
}

func TestResolverLLMTieBreakerAndCaching(t *testing.T) {
	var llmCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resultCount": 1,
				"results": []map[string]any{
					{
						"wrapperType":      "collection",
						"artistName":       "Anirudh Ravichander",
						"collectionName":   "Jailer",
						"primaryGenreName": "Indian",
					},
				},
			})
		case "/v1/chat/completions":
			llmCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer secret-key" {
				t.Errorf("expected Bearer auth header, got %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": "tamil"}},
				},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	settings := memSettings{
		"lang_llm_enabled": "true",
		"lang_llm_url":     srv.URL + "/v1",
		"lang_llm_api_key": "secret-key",
		"lang_llm_model":   "gpt-4o-mini",
	}
	res := NewResolver(newMemCache(), settings, ResolverConfig{
		ITunesBaseURL: srv.URL,
		ITunesLimiter: rate.NewLimiter(rate.Inf, 10),
		LLMLimiter:    rate.NewLimiter(rate.Inf, 10),
	})

	album := &pb.AlbumDetail{
		Title:    "Jailer",
		Metadata: map[string]string{"label": "Sun Pictures", "text_language": "eng", "text_script": "Latn"},
		Tracks:   []*pb.TrackInfo{{Title: "Hukum"}, {Title: "Kaavaalaa"}},
	}

	allowed, lang := res.CheckAlbum(context.Background(), "Anirudh Ravichander", []string{Tamil}, album)
	if !allowed || lang != Tamil {
		t.Fatalf("expected LLM tie-breaker to resolve Tamil, got allowed=%v lang=%q", allowed, lang)
	}

	// Second call hits LLM cache
	allowed, lang = res.CheckAlbum(context.Background(), "Anirudh Ravichander", []string{Tamil}, album)
	if !allowed || lang != Tamil {
		t.Fatalf("expected cached LLM verdict Tamil, got allowed=%v lang=%q", allowed, lang)
	}
	if llmCalls.Load() != 1 {
		t.Errorf("expected exactly 1 LLM call due to caching, got %d", llmCalls.Load())
	}
}

func TestResolverAllowUnknownAndEnglishArtistFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"resultCount": 0, "results": []any{}})
	}))
	defer srv.Close()

	albumMBEngLatn := &pb.AlbumDetail{
		Title:    "Unlabelled Track",
		Metadata: map[string]string{"text_language": "eng", "text_script": "Latn", "language": "eng", "script": "Latn"},
		Tracks:   []*pb.TrackInfo{{Title: "Track 1"}},
	}

	// Under Tamil filter with allowUnknown=true, unconfirmed MB eng+Latn resolves to Unknown -> Allowed
	resPermissive := NewResolver(newMemCache(), memSettings{"lang_allow_unknown": "true"}, ResolverConfig{
		ITunesBaseURL: srv.URL,
		ITunesLimiter: rate.NewLimiter(rate.Inf, 10),
	})
	allowed, lang := resPermissive.CheckAlbum(context.Background(), "Some Artist", []string{Tamil}, albumMBEngLatn)
	if !allowed || lang != Unknown {
		t.Errorf("expected permissive policy to allow unverified eng+Latn as Unknown, got allowed=%v lang=%q", allowed, lang)
	}

	// Under English artist filter, uncontradicted MB eng+Latn still counts as English
	resStrict := NewResolver(newMemCache(), memSettings{"lang_allow_unknown": "false"}, ResolverConfig{
		ITunesBaseURL: srv.URL,
		ITunesLimiter: rate.NewLimiter(rate.Inf, 10),
	})
	allowed, lang = resStrict.CheckAlbum(context.Background(), "English Band", []string{English}, albumMBEngLatn)
	if !allowed || lang != English {
		t.Errorf("expected English artist to keep eng+Latn as English, got allowed=%v lang=%q", allowed, lang)
	}
}

