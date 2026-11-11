import { useMemo, useState, useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { useSystemProcess } from '../context/SystemContext';
import AlphabetRail from '../components/AlphabetRail';
import FilterBar from '../components/FilterBar';
import ProgressBar from '../components/ProgressBar';
import StatusBadge from '../components/StatusBadge';
import {
  IconActivity,
  IconChevronRight,
  IconGrid,
  IconLibrary,
  IconList,
  IconSearch,
} from '../components/Icons';
import type { Artist } from '../types/index';

type StatusFilter = 'all' | 'missing' | 'complete' | 'unlinked' | 'orphaned';
type ViewMode = 'grid' | 'list';

export default function Library() {
  const [filter, setFilter] = useState('');
  const [debouncedFilter, setDebouncedFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [viewMode, setViewMode] = useState<ViewMode>(() => {
    const saved = localStorage.getItem('crate-library-view');
    return saved === 'list' || saved === 'grid' ? saved : 'grid';
  });

  const {
    status,
    activeTasks,
    importStatus,
    hasRunningWork,
    activeDownloadsCount,
    pendingDownloadsCount,
    headline,
    openProcessDrawer,
  } = useSystemProcess();

  const { data: artists, isLoading } = useQuery({
    queryKey: ['artists'],
    queryFn: api.listArtists,
  });

  const { data: searchResults } = useQuery({
    queryKey: ['library-search', debouncedFilter],
    queryFn: () => api.searchLibrary(debouncedFilter),
    enabled: debouncedFilter.length >= 2,
  });

  const handleViewChange = (mode: ViewMode) => {
    setViewMode(mode);
    localStorage.setItem('crate-library-view', mode);
  };

  const matchedArtistIds = useMemo(() => {
    if (!debouncedFilter || !searchResults) return null;
    const ids = new Set<number>();
    for (const r of searchResults) ids.add(r.artist_id);
    return ids;
  }, [debouncedFilter, searchResults]);

  const counts = useMemo(() => {
    if (!artists) {
      return { all: 0, missing: 0, complete: 0, unlinked: 0, orphaned: 0, totalTracks: 0, ownedTracks: 0 };
    }
    let missing = 0;
    let complete = 0;
    let unlinked = 0;
    let orphaned = 0;
    let totalTracks = 0;
    let ownedTracks = 0;
    for (const a of artists) {
      const total = a.total_tracks ?? 0;
      const owned = a.owned_tracks ?? 0;
      totalTracks += total;
      ownedTracks += owned;
      if (total > 0 && owned < total) missing++;
      if (total > 0 && owned >= total) complete++;
      if (a.provider === 'local') unlinked++;
      if (a.orphaned) orphaned++;
    }
    return { all: artists.length, missing, complete, unlinked, orphaned, totalTracks, ownedTracks };
  }, [artists]);

  const filteredArtists = useMemo(() => {
    if (!artists) return [];
    const q = debouncedFilter.toLowerCase();
    return artists.filter((a) => {
      if (statusFilter === 'missing') {
        const total = a.total_tracks ?? 0;
        const owned = a.owned_tracks ?? 0;
        if (!(total > 0 && owned < total)) return false;
      } else if (statusFilter === 'complete') {
        const total = a.total_tracks ?? 0;
        const owned = a.owned_tracks ?? 0;
        if (!(total > 0 && owned >= total)) return false;
      } else if (statusFilter === 'unlinked' && a.provider !== 'local') {
        return false;
      } else if (statusFilter === 'orphaned' && !a.orphaned) {
        return false;
      }

      if (!q) return true;
      if (a.name.toLowerCase().includes(q)) return true;
      return matchedArtistIds?.has(a.id) ?? false;
    });
  }, [artists, debouncedFilter, matchedArtistIds, statusFilter]);

  const matchCountByArtist = useMemo(() => {
    if (!searchResults || !debouncedFilter) return null;
    const map = new Map<number, number>();
    for (const r of searchResults) {
      map.set(r.artist_id, (map.get(r.artist_id) || 0) + 1);
    }
    return map;
  }, [searchResults, debouncedFilter]);

  const grouped = useMemo(() => groupByLetter(filteredArtists), [filteredArtists]);
  const activeLetters = useMemo(() => new Set(grouped.map((g) => g.letter)), [grouped]);
  const scrollRestored = useRef(false);

  useEffect(() => {
    if (!artists?.length || scrollRestored.current) return;
    const saved = sessionStorage.getItem('library-scroll');
    if (saved) {
      requestAnimationFrame(() => window.scrollTo(0, parseInt(saved, 10)));
    }
    scrollRestored.current = true;
  }, [artists]);

  useEffect(() => {
    const onScroll = () => sessionStorage.setItem('library-scroll', String(window.scrollY));
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  if (isLoading) {
    return (
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="h-7 w-32 bg-zinc-800 rounded-lg animate-pulse" />
          <div className="h-9 w-28 bg-zinc-800 rounded-xl animate-pulse" />
        </div>
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-2.5">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="h-20 bg-zinc-900/70 border border-white/[0.06] rounded-xl animate-pulse" />
          ))}
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
          {[...Array(6)].map((_, i) => (
            <div key={i} className="h-20 bg-zinc-900/60 border border-white/[0.05] rounded-xl animate-pulse" />
          ))}
        </div>
      </div>
    );
  }

  if (!artists?.length) {
    return (
      <div className="max-w-lg mx-auto text-center py-14 md:py-20 px-4">
        <div className="w-16 h-16 mx-auto mb-4 rounded-2xl bg-zinc-900 border border-white/[0.08] flex items-center justify-center text-amber-400">
          <IconLibrary className="w-8 h-8" />
        </div>
        <h2 className="text-xl font-bold text-zinc-100">Your crate is empty</h2>
        <p className="text-zinc-400 text-sm mt-2 leading-relaxed">
          Search MusicBrainz, Deezer, or Spotify to watch artists and automatically collect their discographies, or import an existing local library in Settings.
        </p>
        <div className="flex flex-col sm:flex-row items-center justify-center gap-2.5 mt-6">
          <Link
            to="/search"
            className="w-full sm:w-auto inline-flex items-center justify-center gap-2 px-5 py-2.5 bg-amber-400 hover:bg-amber-300 text-zinc-950 rounded-xl font-semibold text-sm active:scale-[0.98] transition-all"
          >
            <IconSearch className="w-4 h-4" />
            Search Artists
          </Link>
          <Link
            to="/settings"
            className="w-full sm:w-auto inline-flex items-center justify-center px-5 py-2.5 bg-zinc-900 hover:bg-zinc-800 border border-white/[0.08] text-zinc-200 rounded-xl font-medium text-sm transition-colors"
          >
            Import Local Library
          </Link>
        </div>
      </div>
    );
  }

  const wantedTracks = Math.max(0, counts.totalTracks - counts.ownedTracks);
  const ownershipPct =
    counts.totalTracks > 0 ? Math.round((counts.ownedTracks / counts.totalTracks) * 100) : 0;

  return (
    <div className="relative space-y-4">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100">Library</h2>
          <p className="text-xs text-zinc-400 mt-0.5">
            {counts.all} artist{counts.all !== 1 ? 's' : ''} · {counts.ownedTracks.toLocaleString()} of{' '}
            {counts.totalTracks.toLocaleString()} tracks owned ({ownershipPct}%)
          </p>
        </div>
        <Link
          to="/search"
          className="inline-flex items-center gap-1.5 px-3.5 py-2 bg-amber-400 hover:bg-amber-300 text-zinc-950 rounded-xl text-xs font-semibold active:scale-[0.98] transition-all shadow-sm"
        >
          <IconSearch className="w-3.5 h-3.5" />
          Add Artist
        </Link>
      </div>

      {/* Live Background Work Banner */}
      {(hasRunningWork || activeTasks.length > 0 || importStatus?.status === 'running') && (
        <button
          type="button"
          onClick={openProcessDrawer}
          className="w-full text-left flex items-center gap-3 rounded-xl bg-sky-950/35 border border-sky-500/25 hover:border-sky-500/45 px-3.5 py-2.5 transition-colors"
        >
          <div className="w-8 h-8 rounded-lg bg-sky-500/15 border border-sky-500/25 flex items-center justify-center text-sky-300 shrink-0">
            <IconActivity className="w-4 h-4 animate-pulse" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-xs font-semibold text-sky-200 truncate">{headline}</p>
            <p className="text-[11px] text-sky-300/70 truncate">
              {activeTasks[0]?.detail || 'Tap to inspect live pipeline & recent events'}
            </p>
          </div>
          <IconChevronRight className="w-4 h-4 text-sky-400 shrink-0" />
        </button>
      )}

      {/* Overview Stats */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-2.5">
        <div className="rounded-xl bg-zinc-900/70 border border-white/[0.06] p-3.5">
          <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-500">Artists</p>
          <p className="text-xl font-bold tabular-nums text-zinc-100 mt-1">{counts.all}</p>
          <p className="text-[11px] text-zinc-500 mt-0.5">{counts.complete} complete</p>
        </div>

        <div className="rounded-xl bg-zinc-900/70 border border-white/[0.06] p-3.5">
          <div className="flex items-center justify-between">
            <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-500">Owned</p>
            <span className="text-[11px] font-semibold text-emerald-400 tabular-nums">
              {ownershipPct}%
            </span>
          </div>
          <p className="text-xl font-bold tabular-nums text-zinc-100 mt-1">
            {counts.ownedTracks.toLocaleString()}
          </p>
          <div className="mt-1.5 h-1 bg-zinc-800 rounded-full overflow-hidden">
            <div
              className="h-full bg-emerald-500 rounded-full transition-all duration-300"
              style={{ width: `${ownershipPct}%` }}
            />
          </div>
        </div>

        <Link
          to="/downloads"
          className="rounded-xl bg-zinc-900/70 border border-white/[0.06] hover:border-white/[0.14] p-3.5 transition-colors"
        >
          <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-500">
            Missing / Wanted
          </p>
          <p className="text-xl font-bold tabular-nums text-amber-300 mt-1">
            {(status?.wanted_tracks ?? wantedTracks).toLocaleString()}
          </p>
          <p className="text-[11px] text-zinc-500 mt-0.5">
            {activeDownloadsCount + pendingDownloadsCount > 0
              ? `${activeDownloadsCount + pendingDownloadsCount} in queue`
              : 'Tap to manage queue'}
          </p>
        </Link>

        {counts.unlinked > 0 || counts.orphaned > 0 ? (
          <button
            type="button"
            onClick={() => setStatusFilter(counts.unlinked > 0 ? 'unlinked' : 'orphaned')}
            className="text-left rounded-xl bg-amber-950/25 border border-amber-500/25 hover:border-amber-500/45 p-3.5 transition-colors"
          >
            <p className="text-[11px] font-medium uppercase tracking-wider text-amber-300/80">
              Needs Attention
            </p>
            <p className="text-xl font-bold tabular-nums text-amber-300 mt-1">
              {counts.unlinked + counts.orphaned}
            </p>
            <p className="text-[11px] text-amber-200/70 mt-0.5 truncate">
              {counts.unlinked > 0 ? `${counts.unlinked} unlinked` : ''}
              {counts.unlinked > 0 && counts.orphaned > 0 ? ' · ' : ''}
              {counts.orphaned > 0 ? `${counts.orphaned} orphaned` : ''}
            </p>
          </button>
        ) : (
          <Link
            to="/downloads"
            className="rounded-xl bg-zinc-900/70 border border-white/[0.06] hover:border-white/[0.14] p-3.5 transition-colors"
          >
            <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-500">
              Queue Status
            </p>
            <p className="text-xl font-bold tabular-nums text-zinc-100 mt-1">
              {(status?.active_downloads ?? 0) + (status?.pending_downloads ?? 0)}
            </p>
            <p className="text-[11px] text-zinc-500 mt-0.5">
              {(status?.failed_downloads ?? 0) > 0
                ? `${status?.failed_downloads} failed`
                : 'Active & pending'}
            </p>
          </Link>
        )}
      </div>

      {/* Filter + Chips + View Mode */}
      <div className="space-y-2.5">
        <div className="flex items-center gap-2">
          <div className="flex-1 min-w-0">
            <FilterBar
              value={filter}
              onChange={(v) => {
                setFilter(v);
                setDebouncedFilter(v);
              }}
              debounceMs={300}
              placeholder="Filter by artist or song title..."
              className="mb-0"
            />
          </div>

          <div className="flex items-center bg-zinc-900/80 border border-white/[0.08] rounded-xl p-1 shrink-0">
            <button
              type="button"
              onClick={() => handleViewChange('grid')}
              aria-label="Grid view"
              className={`p-1.5 rounded-lg transition-colors ${
                viewMode === 'grid'
                  ? 'bg-zinc-800 text-amber-300'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              <IconGrid className="w-4 h-4" />
            </button>
            <button
              type="button"
              onClick={() => handleViewChange('list')}
              aria-label="List view"
              className={`p-1.5 rounded-lg transition-colors ${
                viewMode === 'list'
                  ? 'bg-zinc-800 text-amber-300'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              <IconList className="w-4 h-4" />
            </button>
          </div>
        </div>

        <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar pb-0.5">
          <FilterChip
            active={statusFilter === 'all'}
            onClick={() => setStatusFilter('all')}
            label={`All (${counts.all})`}
          />
          <FilterChip
            active={statusFilter === 'missing'}
            onClick={() => setStatusFilter('missing')}
            label={`Missing Tracks (${counts.missing})`}
          />
          <FilterChip
            active={statusFilter === 'complete'}
            onClick={() => setStatusFilter('complete')}
            label={`Complete (${counts.complete})`}
          />
          {counts.unlinked > 0 && (
            <FilterChip
              active={statusFilter === 'unlinked'}
              onClick={() => setStatusFilter('unlinked')}
              label={`Not Linked (${counts.unlinked})`}
              tone="amber"
            />
          )}
          {counts.orphaned > 0 && (
            <FilterChip
              active={statusFilter === 'orphaned'}
              onClick={() => setStatusFilter('orphaned')}
              label={`Orphaned (${counts.orphaned})`}
              tone="rose"
            />
          )}
        </div>
      </div>

      {viewMode === 'list' && filteredArtists.length >= 12 && (
        <AlphabetRail activeLetters={activeLetters} />
      )}

      <div className={viewMode === 'list' && filteredArtists.length >= 12 ? 'pr-6 md:pr-8' : ''}>
        {filteredArtists.length === 0 ? (
          <div className="rounded-2xl bg-zinc-900/50 border border-white/[0.06] py-12 text-center">
            <p className="text-zinc-400 text-sm">No artists match this filter</p>
            <button
              type="button"
              onClick={() => {
                setFilter('');
                setDebouncedFilter('');
                setStatusFilter('all');
              }}
              className="mt-3 text-xs text-amber-400 hover:text-amber-300 font-medium"
            >
              Reset filters
            </button>
          </div>
        ) : viewMode === 'grid' ? (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
            {filteredArtists
              .slice()
              .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }))
              .map((artist) => (
                <ArtistCard
                  key={artist.id}
                  artist={artist}
                  matchCount={matchCountByArtist?.get(artist.id)}
                  isTrackMatch={
                    !!matchedArtistIds?.has(artist.id) &&
                    !artist.name.toLowerCase().includes(debouncedFilter.toLowerCase())
                  }
                />
              ))}
          </div>
        ) : (
          <div className="space-y-4">
            {grouped.map(({ letter, artists: group }) => (
              <div key={letter} id={`section-${letter}`}>
                {grouped.length > 1 && (
                  <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5 scroll-mt-16">
                    {letter}
                  </p>
                )}
                <div className="space-y-1.5">
                  {group.map((artist) => (
                    <ArtistRow
                      key={artist.id}
                      artist={artist}
                      matchCount={matchCountByArtist?.get(artist.id)}
                      isTrackMatch={
                        !!matchedArtistIds?.has(artist.id) &&
                        !artist.name.toLowerCase().includes(debouncedFilter.toLowerCase())
                      }
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function FilterChip({
  active,
  onClick,
  label,
  tone = 'default',
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  tone?: 'default' | 'amber' | 'rose';
}) {
  const activeStyle =
    tone === 'amber'
      ? 'bg-amber-500/20 text-amber-200 border-amber-500/40'
      : tone === 'rose'
        ? 'bg-rose-500/20 text-rose-200 border-rose-500/40'
        : 'bg-zinc-100 text-zinc-950 border-zinc-100';

  return (
    <button
      type="button"
      onClick={onClick}
      className={`px-3 py-1.5 rounded-full text-xs font-medium border whitespace-nowrap transition-all shrink-0 ${
        active
          ? activeStyle
          : 'bg-zinc-900/70 text-zinc-400 border-white/[0.07] hover:text-zinc-200 hover:border-white/[0.14]'
      }`}
    >
      {label}
    </button>
  );
}

function ArtistCard({
  artist,
  matchCount,
  isTrackMatch,
}: {
  artist: Artist;
  matchCount?: number;
  isTrackMatch?: boolean;
}) {
  const total = artist.total_tracks ?? 0;
  const owned = artist.owned_tracks ?? 0;

  return (
    <Link
      to={`/artist/${artist.id}`}
      className="group flex flex-col justify-between gap-3 bg-zinc-900/65 hover:bg-zinc-900 border border-white/[0.06] hover:border-white/[0.14] rounded-2xl p-3.5 transition-all active:scale-[0.99]"
    >
      <div className="flex items-center gap-3 min-w-0">
        <div className="w-12 h-12 rounded-xl bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.06]">
          {artist.image_url ? (
            <img src={artist.image_url} alt={artist.name} className="w-full h-full object-cover" />
          ) : (
            <div className="w-full h-full flex items-center justify-center text-zinc-600">
              <IconLibrary className="w-5 h-5" />
            </div>
          )}
        </div>

        <div className="flex-1 min-w-0">
          <p className="font-semibold text-sm text-zinc-100 group-hover:text-amber-200 transition-colors truncate">
            {artist.name}
          </p>
          <div className="flex flex-wrap items-center gap-1.5 mt-1">
            {artist.provider === 'local' && <StatusBadge status="not linked" />}
            {artist.orphaned && <StatusBadge status="orphaned" />}
            {artist.status === 'owned' && <StatusBadge status="owned" />}
            {artist.languages && artist.languages.length > 0 && !artist.languages.includes('all') && (
              <span className="text-[10px] text-zinc-400 bg-zinc-800/90 border border-white/[0.06] px-1.5 py-0.5 rounded uppercase">
                {artist.languages.join(', ')}
              </span>
            )}
          </div>
        </div>

        <IconChevronRight className="w-4 h-4 text-zinc-600 group-hover:text-zinc-400 shrink-0 transition-colors" />
      </div>

      {isTrackMatch && matchCount ? (
        <p className="text-xs text-amber-300/90 font-medium">
          {matchCount} matching track{matchCount > 1 ? 's' : ''}
        </p>
      ) : total > 0 ? (
        <ProgressBar owned={owned} total={total} showPct />
      ) : (
        <p className="text-[11px] text-zinc-500">Syncing discography or no tracks yet</p>
      )}
    </Link>
  );
}

function ArtistRow({
  artist,
  matchCount,
  isTrackMatch,
}: {
  artist: Artist;
  matchCount?: number;
  isTrackMatch?: boolean;
}) {
  return (
    <Link
      to={`/artist/${artist.id}`}
      className="flex items-center gap-3 bg-zinc-900/60 hover:bg-zinc-900 border border-white/[0.05] hover:border-white/[0.12] rounded-xl p-2.5 transition-colors"
    >
      <div className="w-11 h-11 rounded-full bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.06]">
        {artist.image_url ? (
          <img src={artist.image_url} alt={artist.name} className="w-full h-full object-cover" />
        ) : (
          <div className="w-full h-full flex items-center justify-center text-zinc-600">
            <IconLibrary className="w-5 h-5" />
          </div>
        )}
      </div>
      <div className="flex-1 min-w-0">
        <p className="font-medium text-sm text-zinc-100 truncate">{artist.name}</p>
        {isTrackMatch && matchCount ? (
          <p className="text-[11px] text-amber-300 mt-0.5">
            {matchCount} matching track{matchCount > 1 ? 's' : ''}
          </p>
        ) : (
          <div className="mt-1">
            <ProgressBar owned={artist.owned_tracks ?? 0} total={artist.total_tracks ?? 0} />
          </div>
        )}
      </div>
      {artist.provider === 'local' && <StatusBadge status="not linked" />}
      {artist.orphaned && <StatusBadge status="orphaned" />}
      {artist.status === 'owned' && <StatusBadge status="owned" />}
      <IconChevronRight className="w-4 h-4 text-zinc-600 shrink-0" />
    </Link>
  );
}

function groupByLetter(artists: Artist[]): { letter: string; artists: Artist[] }[] {
  const sorted = [...artists].sort((a, b) =>
    a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
  );

  const groups: { letter: string; artists: Artist[] }[] = [];
  for (const artist of sorted) {
    const first = artist.name[0]?.toUpperCase() ?? '#';
    const letter = /[A-Z]/.test(first) ? first : '#';
    const last = groups[groups.length - 1];
    if (last?.letter === letter) {
      last.artists.push(artist);
    } else {
      groups.push({ letter, artists: [artist] });
    }
  }
  return groups;
}
