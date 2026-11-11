import { useState, useCallback, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { formatFans } from '../lib/format';
import { useSystemProcess } from '../context/SystemContext';
import { IconCheck, IconChevronRight, IconClose, IconLibrary, IconSearch } from '../components/Icons';
import type { ArtistSearchResult, ProviderInfo } from '../types/index';

const PAGE_SIZE = 25;
const RECENT_KEY = 'crate-recent-searches';

function loadRecent(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch {
    return [];
  }
}

export default function Search() {
  const [query, setQuery] = useState('');
  const [submitted, setSubmitted] = useState('');
  const [provider, setProvider] = useState<string>('');
  const [extraArtists, setExtraArtists] = useState<ArtistSearchResult[]>([]);
  const [offset, setOffset] = useState(PAGE_SIZE);
  const [loadingMore, setLoadingMore] = useState(false);
  const [recentSearches, setRecentSearches] = useState<string[]>(loadRecent);

  const { providers } = useSystemProcess();

  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: api.getSettings,
  });

  const { data: libraryArtists } = useQuery({
    queryKey: ['artists'],
    queryFn: api.listArtists,
  });

  const defaultProvider = settings?.provider_primary || 'musicbrainz';
  const activeProvider = provider || defaultProvider;
  const activeProviderInfo = providers.find((p: ProviderInfo) => p.name === activeProvider);
  const providerLabel = activeProviderInfo?.display_name || activeProvider;

  const libraryLookup = useMemo(() => {
    const map = new Map<string, number>();
    for (const a of libraryArtists ?? []) {
      map.set(`${a.provider}:${a.provider_id}`, a.id);
    }
    return map;
  }, [libraryArtists]);

  const { data: searchData, isLoading, isFetching, error } = useQuery({
    queryKey: ['search', submitted, activeProvider],
    queryFn: () => api.search(submitted, activeProvider, PAGE_SIZE, 0),
    enabled: !!submitted,
  });

  const artists = [...(searchData?.artists || []), ...extraArtists];
  const total = searchData?.total ?? 0;

  const saveRecent = useCallback((term: string) => {
    setRecentSearches((prev) => {
      const next = [term, ...prev.filter((x) => x.toLowerCase() !== term.toLowerCase())].slice(0, 6);
      localStorage.setItem(RECENT_KEY, JSON.stringify(next));
      return next;
    });
  }, []);

  const runSearch = useCallback(
    (term: string) => {
      const trimmed = term.trim();
      if (!trimmed) return;
      setQuery(trimmed);
      setExtraArtists([]);
      setOffset(PAGE_SIZE);
      setSubmitted(trimmed);
      saveRecent(trimmed);
    },
    [saveRecent]
  );

  const loadMore = useCallback(async () => {
    if (loadingMore || offset >= total) return;
    setLoadingMore(true);
    try {
      const data = await api.search(submitted, activeProvider, PAGE_SIZE, offset);
      setExtraArtists((prev) => [...prev, ...(data.artists || [])]);
      setOffset((prev) => prev + PAGE_SIZE);
    } finally {
      setLoadingMore(false);
    }
  }, [submitted, activeProvider, offset, total, loadingMore]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    runSearch(query);
  };

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100">Discover</h2>
        <p className="text-xs text-zinc-400 mt-0.5">
          Search metadata providers to browse discographies and start watching music.
        </p>
      </div>

      {/* Search Form + Provider Pill Switcher */}
      <form onSubmit={handleSubmit} className="space-y-3">
        <div className="flex gap-2">
          <div className="relative flex-1 min-w-0">
            <IconSearch className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-zinc-500 pointer-events-none" />
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search for an artist..."
              className="w-full bg-zinc-900/90 border border-white/[0.08] rounded-xl pl-10 pr-9 py-3 text-base md:text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-500/50 focus:ring-2 focus:ring-amber-500/20 transition-all"
              autoFocus
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery('')}
                className="absolute right-3 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-zinc-200"
                aria-label="Clear search"
              >
                <IconClose className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
          <button
            type="submit"
            disabled={!query.trim() || isLoading}
            className="px-5 py-3 bg-amber-400 hover:bg-amber-300 text-zinc-950 rounded-xl font-semibold text-sm active:scale-[0.98] transition-all disabled:opacity-40 shrink-0"
          >
            Search
          </button>
        </div>

        {/* Provider Pills */}
        {providers.length > 0 && (
          <div className="flex items-center gap-2 overflow-x-auto no-scrollbar pb-0.5">
            <span className="text-[11px] text-zinc-500 font-medium shrink-0">Provider:</span>
            {providers.map((p: ProviderInfo) => {
              const isSelected = activeProvider === p.name;
              const isDefault = p.name === defaultProvider;
              return (
                <button
                  key={p.name}
                  type="button"
                  disabled={!p.healthy}
                  onClick={() => {
                    setProvider(p.name === defaultProvider ? '' : p.name);
                    setExtraArtists([]);
                    setOffset(PAGE_SIZE);
                  }}
                  className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium border whitespace-nowrap transition-all shrink-0 ${
                    !p.healthy
                      ? 'bg-zinc-900/40 border-white/[0.04] text-zinc-600 cursor-not-allowed'
                      : isSelected
                        ? 'bg-zinc-100 text-zinc-950 border-zinc-100'
                        : 'bg-zinc-900/70 border-white/[0.08] text-zinc-300 hover:border-white/[0.18]'
                  }`}
                >
                  <span
                    className={`w-1.5 h-1.5 rounded-full ${
                      p.healthy ? 'bg-emerald-500' : 'bg-rose-500'
                    }`}
                  />
                  <span>{p.display_name}</span>
                  {isDefault && (
                    <span
                      className={`text-[10px] ${isSelected ? 'text-zinc-600' : 'text-zinc-500'}`}
                    >
                      default
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        )}
      </form>

      {/* Searching Status */}
      {isLoading && (
        <div className="space-y-3">
          <div className="flex items-center gap-2.5 text-xs text-sky-300 bg-sky-950/30 border border-sky-500/20 rounded-xl px-3.5 py-2.5">
            <div className="w-3.5 h-3.5 border-2 border-sky-500/30 border-t-sky-300 rounded-full animate-spin shrink-0" />
            <span>
              Querying <strong className="font-semibold">{providerLabel}</strong> for &ldquo;{submitted}&rdquo;…
            </span>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-2.5">
            {[...Array(4)].map((_, i) => (
              <div
                key={i}
                className="flex items-center gap-3 bg-zinc-900/60 border border-white/[0.05] rounded-2xl p-3 animate-pulse"
              >
                <div className="w-12 h-12 rounded-full bg-zinc-800 shrink-0" />
                <div className="flex-1 space-y-2">
                  <div className="h-3.5 bg-zinc-800 rounded w-36" />
                  <div className="h-2.5 bg-zinc-800/70 rounded w-24" />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {error && (
        <div className="rounded-xl bg-rose-950/30 border border-rose-500/25 p-4 text-sm text-rose-300">
          Search failed via {providerLabel}: {(error as Error).message}
        </div>
      )}

      {/* Results */}
      {!isLoading && artists.length > 0 && (
        <div className="space-y-3">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <span>
              Showing {artists.length} of {total.toLocaleString()} results via{' '}
              <span className="text-zinc-200 font-medium">{providerLabel}</span>
            </span>
            {isFetching && <span className="text-sky-400">Refreshing…</span>}
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-2.5">
            {artists.map((artist, i) => {
              const libraryId = libraryLookup.get(`${activeProvider}:${artist.id}`);
              const browseUrl = `/browse/artist/${artist.id}${provider ? `?provider=${provider}` : ''}`;

              return (
                <div
                  key={`${artist.id}-${i}`}
                  className="flex items-center gap-3 bg-zinc-900/65 hover:bg-zinc-900 border border-white/[0.06] hover:border-white/[0.14] rounded-2xl p-3 transition-all"
                >
                  <Link to={browseUrl} className="flex items-center gap-3 flex-1 min-w-0">
                    <div className="w-12 h-12 rounded-xl bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.06]">
                      {artist.image_url ? (
                        <img src={artist.image_url} alt="" className="w-full h-full object-cover" />
                      ) : (
                        <div className="w-full h-full flex items-center justify-center text-zinc-600">
                          <IconLibrary className="w-5 h-5" />
                        </div>
                      )}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-1.5">
                        <p className="font-semibold text-sm text-zinc-100 truncate">{artist.name}</p>
                        {libraryId && (
                          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-300 border border-emerald-500/25 text-[10px] font-medium shrink-0">
                            <IconCheck className="w-2.5 h-2.5" />
                            In Library
                          </span>
                        )}
                      </div>
                      <p className="text-xs text-zinc-400 truncate mt-0.5">
                        {artist.album_count > 0 && `${artist.album_count} releases`}
                        {artist.metadata?.fans &&
                          `${artist.album_count > 0 ? ' · ' : ''}${formatFans(Number(artist.metadata.fans))} fans`}
                        {artist.metadata?.country && ` · ${artist.metadata.country}`}
                        {artist.metadata?.disambiguation && ` · ${artist.metadata.disambiguation}`}
                      </p>
                    </div>
                  </Link>

                  <div className="flex items-center gap-1.5 shrink-0">
                    {libraryId && (
                      <Link
                        to={`/artist/${libraryId}`}
                        className="px-2.5 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-xs font-medium text-zinc-200 transition-colors"
                      >
                        Open
                      </Link>
                    )}
                    <Link
                      to={browseUrl}
                      className="p-1.5 rounded-lg text-zinc-500 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
                      aria-label={`Browse ${artist.name}`}
                    >
                      <IconChevronRight className="w-4 h-4" />
                    </Link>
                  </div>
                </div>
              );
            })}
          </div>

          {offset < total && (
            <button
              type="button"
              onClick={loadMore}
              disabled={loadingMore}
              className="w-full py-3 text-sm font-medium text-zinc-300 bg-zinc-900/70 hover:bg-zinc-900 border border-white/[0.07] rounded-xl transition-colors disabled:opacity-50"
            >
              {loadingMore
                ? 'Loading more results…'
                : `Load More (${artists.length} of ${total.toLocaleString()})`}
            </button>
          )}
        </div>
      )}

      {submitted && !isLoading && !error && artists.length === 0 && (
        <div className="rounded-2xl bg-zinc-900/50 border border-white/[0.06] text-center py-12 px-4">
          <p className="text-zinc-300 font-medium text-sm">
            No artists found for &ldquo;{submitted}&rdquo; on {providerLabel}
          </p>
          <p className="text-zinc-500 text-xs mt-1.5">
            Try switching metadata providers above or checking the spelling.
          </p>
        </div>
      )}

      {/* Initial / Empty State */}
      {!submitted && !isLoading && (
        <div className="space-y-6 pt-2">
          {recentSearches.length > 0 && (
            <div>
              <div className="flex items-center justify-between mb-2">
                <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider">
                  Recent Searches
                </p>
                <button
                  type="button"
                  onClick={() => {
                    localStorage.removeItem(RECENT_KEY);
                    setRecentSearches([]);
                  }}
                  className="text-[11px] text-zinc-500 hover:text-zinc-300"
                >
                  Clear
                </button>
              </div>
              <div className="flex flex-wrap gap-2">
                {recentSearches.map((term) => (
                  <button
                    key={term}
                    type="button"
                    onClick={() => runSearch(term)}
                    className="px-3 py-1.5 rounded-full bg-zinc-900/80 border border-white/[0.07] hover:border-amber-500/40 text-xs text-zinc-300 hover:text-amber-200 transition-colors"
                  >
                    {term}
                  </button>
                ))}
              </div>
            </div>
          )}

          <div className="rounded-2xl bg-zinc-900/40 border border-white/[0.05] text-center py-12 px-4">
            <div className="w-12 h-12 mx-auto mb-3 rounded-2xl bg-zinc-900 border border-white/[0.07] flex items-center justify-center text-zinc-500">
              <IconSearch className="w-6 h-6" />
            </div>
            <p className="text-zinc-200 font-medium text-sm">Find artists to add to your crate</p>
            <p className="text-zinc-500 text-xs mt-1.5 max-w-md mx-auto leading-relaxed">
              Type an artist name above. You can watch an entire discography, pick individual albums or tracks, and filter auto-downloads by language.
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
