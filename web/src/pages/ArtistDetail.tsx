import { useEffect, useMemo, useRef, useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useSystemProcess } from '../context/SystemContext';
import { useToast } from '../components/Toast';
import { formatTotalDuration } from '../lib/format';
import FilterBar from '../components/FilterBar';
import ProviderBadge from '../components/ProviderBadge';
import ProgressBar from '../components/ProgressBar';
import LanguageSelector from '../components/LanguageSelector';
import StatusBadge from '../components/StatusBadge';
import ConfirmModal from '../components/ConfirmModal';
import {
  IconAlert,
  IconChevronRight,
  IconClose,
  IconDisc,
  IconGrip,
  IconLibrary,
  IconLink,
  IconPause,
  IconPlay,
  IconRefresh,
  IconSearch,
  IconTrash,
} from '../components/Icons';
import type { Track, Album, ProviderInfo } from '../types/index';

export default function ArtistDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const { activeTasks, providers } = useSystemProcess();

  const [trackFilter, setTrackFilter] = useState('');
  const [albumFilter, setAlbumFilter] = useState<'all' | 'missing' | 'owned'>('all');
  const [showRelinkSearch, setShowRelinkSearch] = useState(false);
  const [relinkQuery, setRelinkQuery] = useState('');
  const [relinkProvider, setRelinkProvider] = useState('');
  const [reconciling, setReconciling] = useState(false);
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);
  const [draggingAlbumId, setDraggingAlbumId] = useState<number | null>(null);
  const [dropTargetId, setDropTargetId] = useState<number | null>(null);
  const [pendingMerge, setPendingMerge] = useState<{ src: Album; dst: Album } | null>(null);
  const [manualLinkSource, setManualLinkSource] = useState<Album | null>(null);

  const lastAlbumCount = useRef(-1);
  const reconcileTicks = useRef(0);

  const artistIdNum = Number(id);
  const activeArtistTask = activeTasks.find(
    (t) => t.entity_id === artistIdNum && (t.kind === 'sync_artist' || t.kind === 'reconcile_artist')
  );

  const { data: artist, isLoading } = useQuery({
    queryKey: ['artist', id],
    queryFn: () => api.getArtist(artistIdNum),
    enabled: !!id,
    refetchInterval: (query) => {
      if (reconciling || activeArtistTask) return 2000;
      const a = query.state.data;
      if (!a?.albums) return false;
      const hasActive = a.albums.some((al: Album) =>
        al.tracks?.some((t: Track) => t.status === 'downloading')
      );
      return hasActive ? 6_000 : false;
    },
  });

  const { data: relinkResults } = useQuery({
    queryKey: ['relink-search', relinkQuery, relinkProvider],
    queryFn: () => api.search(relinkQuery, relinkProvider || undefined),
    enabled: !!relinkQuery && showRelinkSearch,
  });

  const toggleNewReleases = useMutation({
    mutationFn: (enabled: boolean) => api.toggleNewReleases(artistIdNum, enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: ['artist', id] });
      const prev = queryClient.getQueryData(['artist', id]);
      queryClient.setQueryData(['artist', id], (old: Record<string, unknown> | undefined) =>
        old ? { ...old, watch_new_releases: enabled } : old
      );
      return { prev };
    },
    onError: (_err, _enabled, context) => {
      if (context?.prev) queryClient.setQueryData(['artist', id], context.prev);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    },
  });

  const updateLanguages = useMutation({
    mutationFn: (languages: string[]) => api.updateArtistLanguages(artistIdNum, languages),
    onMutate: async (languages) => {
      await queryClient.cancelQueries({ queryKey: ['artist', id] });
      const prev = queryClient.getQueryData(['artist', id]);
      queryClient.setQueryData(['artist', id], (old: Record<string, unknown> | undefined) =>
        old ? { ...old, languages } : old
      );
      return { prev };
    },
    onSuccess: () => toast('Language preferences updated', 'success'),
    onError: (err: Error, _languages, context) => {
      if (context?.prev) queryClient.setQueryData(['artist', id], context.prev);
      toast(err.message, 'error');
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    },
  });

  const unwatch = useMutation({
    mutationFn: () => api.unwatchArtist(artistIdNum),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      toast('Artist removed from library', 'info');
      navigate('/');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const queueAll = useMutation({
    mutationFn: () => api.queueArtistTracks(artistIdNum),
    onSuccess: (data) => {
      toast(
        data.queued > 0
          ? `Queued ${data.queued} track${data.queued > 1 ? 's' : ''} for download`
          : 'All wanted tracks are already queued',
        data.queued > 0 ? 'success' : 'info'
      );
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const syncMetadata = useMutation({
    mutationFn: () => api.syncArtist(artistIdNum),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['status'] });
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      toast(
        res.status === 'already_syncing'
          ? 'Sync is already in progress'
          : 'Syncing artist metadata & latest releases…',
        res.status === 'already_syncing' ? 'info' : 'success'
      );
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const pauseSync = useMutation({
    mutationFn: () => api.pauseArtistSync(artistIdNum),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      toast('Artist discography sync paused', 'info');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const resumeSync = useMutation({
    mutationFn: () => api.resumeArtistSync(artistIdNum),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      toast('Artist discography sync resumed', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const relink = useMutation({
    mutationFn: (providerID: string) =>
      api.relinkEntity('artist', artistIdNum, providerID, {
        provider: relinkProvider || undefined,
        sync: true,
      }),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      setShowRelinkSearch(false);
      if (res?.reconciling) {
        lastAlbumCount.current = -1;
        reconcileTicks.current = 0;
        setReconciling(true);
        toast('Relinked — syncing provider discography…', 'success');
      } else {
        toast('Artist relinked', 'success');
      }
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const autoMatch = useMutation({
    mutationFn: () => api.matchUnmatchedAlbums(artistIdNum),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      if (data.matched > 0) {
        toast(
          `Auto-matched ${data.matched} album${data.matched > 1 ? 's' : ''}!`,
          'success'
        );
      } else {
        toast('No high-confidence matches found. Drag & drop or use manual link.', 'info');
      }
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const linkAlbum = useMutation({
    mutationFn: ({ srcId, targetAlbumId }: { srcId: number; targetAlbumId: number }) =>
      api.linkAlbum(srcId, targetAlbumId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      toast('Album successfully linked and metadata mapped!', 'success');
      setPendingMerge(null);
      setManualLinkSource(null);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  useEffect(() => {
    if (!reconciling || !artist) return;
    if (activeArtistTask) return; // Wait while server task is explicitly running
    const count = artist.albums?.length ?? 0;
    reconcileTicks.current += 1;
    if ((count > 0 && count === lastAlbumCount.current) || reconcileTicks.current > 20) {
      setReconciling(false);
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    }
    lastAlbumCount.current = count;
  }, [artist, reconciling, activeArtistTask, queryClient]);

  const stats = useMemo(() => {
    if (!artist?.albums) return null;
    let totalTracks = 0;
    let ownedTracks = 0;
    let totalDuration = 0;
    let wantedTracks = 0;
    let downloadingTracks = 0;
    const recordTypes: Record<string, number> = {};

    for (const album of artist.albums) {
      const type = album.record_type || 'album';
      recordTypes[type] = (recordTypes[type] || 0) + 1;
      if (album.tracks) {
        for (const track of album.tracks) {
          totalTracks++;
          totalDuration += track.duration_ms;
          if (track.status === 'owned') ownedTracks++;
          if (track.status === 'wanted') wantedTracks++;
          if (track.status === 'downloading') downloadingTracks++;
        }
      }
    }

    return {
      totalTracks,
      ownedTracks,
      totalDuration,
      wantedTracks,
      downloadingTracks,
      recordTypes,
    };
  }, [artist]);

  const unmatchedAlbums = useMemo(() => {
    if (!artist?.albums || artist.provider === 'local') return [];
    return artist.albums.filter((a) => a.provider === 'local');
  }, [artist]);

  const providerAlbums = useMemo(() => {
    if (!artist?.albums || artist.provider === 'local') return [];
    return artist.albums.filter((a) => a.provider !== 'local');
  }, [artist]);

  const filteredAlbums = useMemo((): Album[] => {
    if (!artist?.albums) return [];
    const q = trackFilter.toLowerCase();
    return artist.albums.filter((album) => {
      if (albumFilter === 'missing') {
        const hasWanted = album.tracks?.some((t) => t.status === 'wanted' || t.status === 'downloading');
        if (!hasWanted) return false;
      } else if (albumFilter === 'owned') {
        const allOwned =
          album.tracks && album.tracks.length > 0 && album.tracks.every((t) => t.status === 'owned');
        if (!allOwned) return false;
      }

      if (!q) return true;
      if (album.title.toLowerCase().includes(q)) return true;
      return album.tracks?.some((t) => t.title.toLowerCase().includes(q)) ?? false;
    });
  }, [artist, trackFilter, albumFilter]);

  if (isLoading) {
    return (
      <div className="space-y-4 animate-pulse">
        <div className="flex items-center gap-4">
          <div className="w-16 h-16 md:w-20 md:h-20 rounded-2xl bg-zinc-800 shrink-0" />
          <div className="flex-1 space-y-2">
            <div className="h-6 bg-zinc-800 rounded w-44" />
            <div className="h-3.5 bg-zinc-800/70 rounded w-28" />
          </div>
        </div>
        <div className="h-24 bg-zinc-900/70 rounded-2xl" />
        <div className="grid grid-cols-1 md:grid-cols-2 gap-2.5">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="h-16 bg-zinc-900/60 rounded-xl" />
          ))}
        </div>
      </div>
    );
  }

  if (!artist) {
    return (
      <div className="rounded-2xl bg-zinc-900/50 border border-white/[0.06] py-12 text-center text-zinc-400 text-sm">
        Artist not found
      </div>
    );
  }

  const isSyncingOrReconciling = !!activeArtistTask || reconciling;

  return (
    <div className="space-y-5">
      {/* Orphaned Provider Alert */}
      {artist.orphaned && (
        <div className="flex items-start gap-3 bg-rose-950/35 border border-rose-500/30 rounded-2xl px-4 py-3">
          <IconAlert className="w-5 h-5 text-rose-400 shrink-0 mt-0.5" />
          <div className="flex-1 min-w-0">
            <p className="text-sm text-rose-200 font-semibold">Provider offline</p>
            <p className="text-xs text-rose-300/80 mt-0.5">
              This artist&apos;s metadata provider ({artist.provider}) is unreachable. Relink to another provider to continue syncing new releases.
            </p>
          </div>
          <button
            type="button"
            onClick={() => setShowRelinkSearch(true)}
            className="px-3 py-1.5 rounded-lg bg-rose-500/20 hover:bg-rose-500/30 text-rose-200 text-xs font-medium shrink-0 transition-colors"
          >
            Relink
          </button>
        </div>
      )}

      {/* Hero Header */}
      <div className="rounded-2xl bg-zinc-900/70 border border-white/[0.07] p-4 md:p-5">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div className="flex items-center gap-4 min-w-0">
            <div className="w-16 h-16 md:w-20 md:h-20 rounded-2xl bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.08]">
              {artist.image_url ? (
                <img
                  src={artist.image_url}
                  alt={artist.name}
                  className="w-full h-full object-cover"
                />
              ) : (
                <div className="w-full h-full flex items-center justify-center text-zinc-600">
                  <IconLibrary className="w-7 h-7" />
                </div>
              )}
            </div>
            <div className="flex-1 min-w-0">
              <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100 truncate">
                {artist.name}
              </h2>
              <div className="flex flex-wrap items-center gap-2 mt-1.5">
                <ProviderBadge provider={artist.provider} />
                <StatusBadge
                  status={artist.status}
                  label={
                    artist.status === 'watched'
                      ? 'Full Discography'
                      : artist.status === 'partial'
                        ? 'Partial Watch'
                        : 'Owned'
                  }
                />
                {artist.sync_paused && <StatusBadge status="sync paused" />}
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2 shrink-0">
            {artist.provider !== 'local' && (
              artist.sync_paused ? (
                <button
                  type="button"
                  onClick={() => resumeSync.mutate()}
                  disabled={resumeSync.isPending}
                  className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl text-xs font-semibold bg-emerald-500 hover:bg-emerald-400 text-zinc-950 transition-colors shadow-sm disabled:opacity-50"
                  title="Resume discography syncing for this artist"
                >
                  <IconPlay className="w-3.5 h-3.5 fill-current" />
                  <span>{resumeSync.isPending ? 'Resuming…' : 'Resume Sync'}</span>
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => pauseSync.mutate()}
                  disabled={pauseSync.isPending}
                  className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl text-xs font-medium bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 transition-colors disabled:opacity-50"
                  title="Pause discography syncing for this artist"
                >
                  <IconPause className="w-3.5 h-3.5" />
                  <span>{pauseSync.isPending ? 'Pausing…' : 'Pause Sync'}</span>
                </button>
              )
            )}
            {artist.provider !== 'local' && (
              <button
                type="button"
                onClick={() => syncMetadata.mutate()}
                disabled={isSyncingOrReconciling || syncMetadata.isPending || artist.orphaned}
                className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl text-xs font-medium bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 transition-colors disabled:opacity-50"
              >
                <IconRefresh
                  className={`w-3.5 h-3.5 ${isSyncingOrReconciling || syncMetadata.isPending ? 'animate-spin' : ''}`}
                />
                <span>{isSyncingOrReconciling ? 'Syncing…' : 'Sync Metadata'}</span>
              </button>
            )}
            <button
              type="button"
              onClick={() => {
                if (!showRelinkSearch && !relinkQuery) {
                  setRelinkQuery(artist.name);
                }
                setShowRelinkSearch(!showRelinkSearch);
              }}
              className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl text-xs font-medium bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 transition-colors"
            >
              <IconLink className="w-3.5 h-3.5" />
              <span>{artist.provider === 'local' ? 'Link Provider' : 'Relink'}</span>
            </button>
            <button
              type="button"
              onClick={() => setConfirmDeleteOpen(true)}
              disabled={unwatch.isPending}
              className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl text-xs font-medium bg-zinc-800 hover:bg-rose-500/15 border border-white/[0.08] hover:border-rose-500/30 text-zinc-300 hover:text-rose-300 transition-colors"
            >
              <IconTrash className="w-3.5 h-3.5" />
              <span>Delete</span>
            </button>
          </div>
        </div>
      </div>

      {/* Live Background Sync / Reconcile Progress */}
      {isSyncingOrReconciling && (
        <div className="rounded-2xl bg-sky-950/35 border border-sky-500/30 p-4 space-y-2 animate-fade-in">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="w-4 h-4 border-2 border-sky-500/40 border-t-sky-300 rounded-full animate-spin shrink-0" />
              <p className="text-sm font-semibold text-sky-200 truncate">
                {activeArtistTask?.title || 'Linking & reconciling discography…'}
              </p>
            </div>
            {activeArtistTask && activeArtistTask.total > 0 && (
              <span className="text-xs font-mono text-sky-300 tabular-nums shrink-0">
                {activeArtistTask.current} / {activeArtistTask.total}
              </span>
            )}
          </div>
          <p className="text-xs text-sky-200/80">
            {activeArtistTask?.detail ||
              'Pulling releases from provider and matching your existing tracks…'}
          </p>
          <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden">
            <div
              className={`h-full bg-sky-400 rounded-full transition-all duration-300 ${
                !activeArtistTask || activeArtistTask.total === 0 ? 'w-1/2 animate-pulse' : ''
              }`}
              style={
                activeArtistTask && activeArtistTask.total > 0
                  ? {
                      width: `${Math.min(100, Math.round((activeArtistTask.current / activeArtistTask.total) * 100))}%`,
                    }
                  : undefined
              }
            />
          </div>
        </div>
      )}

      {/* Local Unlinked Banner */}
      {artist.provider === 'local' && !showRelinkSearch && (
        <div className="bg-amber-950/25 border border-amber-500/30 rounded-2xl p-4">
          <div className="flex flex-col sm:flex-row sm:items-center gap-3">
            <div className="flex items-start gap-3 flex-1 min-w-0">
              <IconLink className="w-5 h-5 text-amber-400 shrink-0 mt-0.5" />
              <div className="min-w-0">
                <p className="text-sm text-amber-200 font-semibold">Not linked to a provider</p>
                <p className="text-xs text-amber-200/70 mt-0.5 leading-relaxed">
                  This imported artist isn&apos;t tracked yet. Link it to MusicBrainz or Deezer to reveal its full discography and detect missing tracks while keeping your owned files.
                </p>
              </div>
            </div>
            <button
              type="button"
              onClick={() => {
                setRelinkQuery(artist.name);
                setShowRelinkSearch(true);
              }}
              className="px-4 py-2 rounded-xl text-xs font-semibold bg-amber-400 hover:bg-amber-300 text-zinc-950 transition-colors shrink-0"
            >
              Link Artist
            </button>
          </div>
        </div>
      )}

      {/* Relink Search Panel */}
      {showRelinkSearch && (
        <div className="bg-zinc-900/80 border border-white/[0.08] rounded-2xl p-4 space-y-3 animate-fade-in">
          <div className="flex items-center justify-between">
            <p className="text-xs font-semibold text-zinc-300 uppercase tracking-wider">
              {artist.provider === 'local'
                ? 'Link to Metadata Provider'
                : 'Relink to Different Provider'}
            </p>
            <button
              type="button"
              onClick={() => setShowRelinkSearch(false)}
              className="text-xs text-zinc-500 hover:text-zinc-300"
            >
              Close
            </button>
          </div>

          <div className="flex flex-col sm:flex-row gap-2">
            <input
              type="text"
              value={relinkQuery}
              onChange={(e) => setRelinkQuery(e.target.value)}
              placeholder={`Search for "${artist.name}"...`}
              className="flex-1 bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2.5 text-base md:text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-500/50"
              autoFocus
            />
            {providers.length > 1 && (
              <select
                value={relinkProvider}
                onChange={(e) => setRelinkProvider(e.target.value)}
                className="bg-zinc-950 border border-white/[0.08] rounded-xl px-3 py-2.5 text-sm outline-none text-zinc-200"
              >
                <option value="">Default Provider</option>
                {providers
                  .filter((p: ProviderInfo) => p.healthy)
                  .map((p: ProviderInfo) => (
                    <option key={p.name} value={p.name}>
                      {p.display_name}
                    </option>
                  ))}
              </select>
            )}
          </div>

          {relinkResults && relinkResults.artists.length > 0 && (
            <div className="space-y-1.5 max-h-60 overflow-y-auto">
              {relinkResults.artists.map((r) => (
                <button
                  key={r.id}
                  type="button"
                  onClick={() => relink.mutate(r.id)}
                  disabled={relink.isPending}
                  className="w-full flex items-center gap-3 bg-zinc-950/70 hover:bg-zinc-800 border border-white/[0.05] rounded-xl p-2.5 text-left transition-colors disabled:opacity-50"
                >
                  <div className="w-9 h-9 rounded-lg bg-zinc-800 overflow-hidden shrink-0">
                    {r.image_url && <img src={r.image_url} alt="" className="w-full h-full object-cover" />}
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-zinc-100 truncate">{r.name}</p>
                    <p className="text-[11px] text-zinc-400">
                      {r.album_count} releases
                      {r.metadata?.country ? ` · ${r.metadata.country}` : ''}
                      {r.metadata?.disambiguation ? ` · ${r.metadata.disambiguation}` : ''}
                    </p>
                  </div>
                  <span className="text-xs font-medium text-amber-300 shrink-0">Select</span>
                </button>
              ))}
            </div>
          )}

          {relinkQuery && relinkResults && relinkResults.artists.length === 0 && (
            <p className="text-xs text-zinc-500 text-center py-2">No matching artists found</p>
          )}
        </div>
      )}

      {/* Collection & Controls */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-3">
        {stats && (
          <div className="lg:col-span-5 space-y-2.5">
            <div className="rounded-xl bg-zinc-900/70 border border-white/[0.06] p-3.5 space-y-3">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium text-zinc-100">Collection Progress</p>
                {stats.downloadingTracks > 0 && (
                  <StatusBadge
                    status="downloading"
                    label={`${stats.downloadingTracks} downloading`}
                  />
                )}
              </div>
              <ProgressBar owned={stats.ownedTracks} total={stats.totalTracks} showPct />
              <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-zinc-400 pt-1 border-t border-white/[0.05]">
                <span>{artist.albums?.length || 0} releases</span>
                <span>{formatTotalDuration(stats.totalDuration)}</span>
                {Object.entries(stats.recordTypes).length > 1 && (
                  <span className="text-zinc-500">
                    {Object.entries(stats.recordTypes)
                      .map(([type, count]) => `${count} ${type}${count > 1 ? 's' : ''}`)
                      .join(', ')}
                  </span>
                )}
              </div>
            </div>

            {stats.wantedTracks > 0 && (
              <button
                type="button"
                onClick={() => queueAll.mutate()}
                disabled={queueAll.isPending || queueAll.isSuccess}
                className="w-full py-3 bg-amber-400 hover:bg-amber-300 text-zinc-950 rounded-xl font-semibold text-sm active:scale-[0.98] transition-all disabled:opacity-60 flex items-center justify-center gap-2"
              >
                <IconSearch className="w-4 h-4" />
                {queueAll.isSuccess
                  ? 'Queued for Download!'
                  : queueAll.isPending
                    ? 'Queuing…'
                    : `Queue ${stats.wantedTracks} Wanted Track${stats.wantedTracks > 1 ? 's' : ''}`}
              </button>
            )}
          </div>
        )}

        <div className={`${stats ? 'lg:col-span-7' : 'lg:col-span-12'} space-y-2.5`}>
          <LanguageSelector
            selected={artist.languages || []}
            onChange={(newLanguages) => updateLanguages.mutate(newLanguages)}
            disabled={updateLanguages.isPending}
          />

          <label className="flex items-center justify-between gap-3 bg-zinc-900/70 border border-white/[0.06] rounded-xl px-3.5 py-3 cursor-pointer">
            <div>
              <p className="text-sm font-medium text-zinc-100">Watch for new releases</p>
              <p className="text-[11px] text-zinc-400 mt-0.5">
                Scheduler automatically adds and queues future releases
              </p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={artist.watch_new_releases}
              onClick={() => toggleNewReleases.mutate(!artist.watch_new_releases)}
              className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                artist.watch_new_releases ? 'bg-emerald-500' : 'bg-zinc-700'
              }`}
            >
              <span
                className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
                  artist.watch_new_releases ? 'translate-x-[22px]' : 'translate-x-0.5'
                }`}
              />
            </button>
          </label>
        </div>
      </div>

      {/* Albums */}
      {artist.albums && artist.albums.length > 0 && (
        <div className="space-y-3">
          {unmatchedAlbums.length > 0 && providerAlbums.length > 0 && (
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-amber-500/10 border border-amber-500/30 rounded-2xl p-4 animate-fade-in">
              <div className="flex items-start sm:items-center gap-3">
                <div className="p-2 rounded-xl bg-amber-500/20 text-amber-300 shrink-0">
                  <IconLink className="w-5 h-5" />
                </div>
                <div>
                  <p className="text-sm font-semibold text-amber-200">
                    {unmatchedAlbums.length} Unmatched Local Album{unmatchedAlbums.length > 1 ? 's' : ''} Detected
                  </p>
                  <p className="text-xs text-amber-300/80 mt-0.5">
                    Automatically match them to provider releases, or drag &amp; drop an unmatched album onto a wanted release below.
                  </p>
                </div>
              </div>
              <button
                type="button"
                onClick={() => autoMatch.mutate()}
                disabled={autoMatch.isPending}
                className="inline-flex items-center justify-center gap-1.5 px-4 py-2 rounded-xl text-xs font-semibold bg-amber-400 hover:bg-amber-300 text-zinc-950 transition-colors disabled:opacity-50 shrink-0"
              >
                <IconRefresh className={`w-3.5 h-3.5 ${autoMatch.isPending ? 'animate-spin' : ''}`} />
                <span>{autoMatch.isPending ? 'Auto-Matching…' : 'Auto-Match All'}</span>
              </button>
            </div>
          )}

          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
              Albums & Releases ({filteredAlbums.length})
            </p>
            <div className="flex items-center gap-1.5">
              {(
                [
                  { id: 'all', label: 'All' },
                  { id: 'missing', label: 'Missing Tracks' },
                  { id: 'owned', label: 'Complete' },
                ] as const
              ).map((f) => (
                <button
                  key={f.id}
                  type="button"
                  onClick={() => setAlbumFilter(f.id)}
                  className={`px-2.5 py-1 rounded-full text-[11px] font-medium border transition-colors ${
                    albumFilter === f.id
                      ? 'bg-zinc-100 text-zinc-950 border-zinc-100'
                      : 'bg-zinc-900/70 text-zinc-400 border-white/[0.07] hover:text-zinc-200'
                  }`}
                >
                  {f.label}
                </button>
              ))}
            </div>
          </div>

          <FilterBar
            value={trackFilter}
            onChange={setTrackFilter}
            placeholder="Filter by song or album title..."
            className="mb-0"
          />

          {filteredAlbums.length === 0 && (
            <p className="text-zinc-500 text-sm text-center py-8">
              No matching albums or tracks
            </p>
          )}

          <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
            {filteredAlbums.map((album) => {
              const isUnmatched = album.provider === 'local' && artist.provider !== 'local';
              const canDropHere = draggingAlbumId !== null && album.provider !== 'local' && draggingAlbumId !== album.id;
              const isDropTarget = dropTargetId === album.id;
              const isDraggingThis = draggingAlbumId === album.id;

              return (
                <div
                  key={album.id}
                  draggable={isUnmatched}
                  onDragStart={(e) => {
                    if (!isUnmatched) return;
                    e.dataTransfer.setData('text/plain', String(album.id));
                    e.dataTransfer.effectAllowed = 'move';
                    setDraggingAlbumId(album.id);
                  }}
                  onDragEnd={() => {
                    setDraggingAlbumId(null);
                    setDropTargetId(null);
                  }}
                  onDragOver={(e) => {
                    if (!canDropHere) return;
                    e.preventDefault();
                    e.dataTransfer.dropEffect = 'move';
                    if (dropTargetId !== album.id) {
                      setDropTargetId(album.id);
                    }
                  }}
                  onDragLeave={(e) => {
                    if (e.currentTarget.contains(e.relatedTarget as Node)) return;
                    if (dropTargetId === album.id) {
                      setDropTargetId(null);
                    }
                  }}
                  onDrop={(e) => {
                    if (!canDropHere) return;
                    e.preventDefault();
                    e.stopPropagation();
                    const srcId = Number(e.dataTransfer.getData('text/plain')) || draggingAlbumId;
                    setDraggingAlbumId(null);
                    setDropTargetId(null);
                    if (srcId && srcId !== album.id) {
                      const srcAlbum = artist.albums?.find((a) => a.id === srcId);
                      if (srcAlbum) {
                        setPendingMerge({ src: srcAlbum, dst: album });
                      }
                    }
                  }}
                  className={`group relative flex items-center gap-2.5 rounded-xl p-2.5 transition-all ${
                    isDraggingThis
                      ? 'opacity-40 border border-dashed border-amber-400 bg-amber-500/10'
                      : isDropTarget
                        ? 'border-2 border-amber-400 bg-amber-500/20 ring-4 ring-amber-400/30 scale-[1.01]'
                        : canDropHere
                          ? 'border border-dashed border-amber-500/40 bg-amber-500/5'
                          : isUnmatched
                            ? 'bg-zinc-900/80 hover:bg-zinc-900 border border-amber-500/25 hover:border-amber-500/45 cursor-grab active:cursor-grabbing'
                            : 'bg-zinc-900/65 hover:bg-zinc-900 border border-white/[0.06] hover:border-white/[0.14]'
                  }`}
                >
                  {isUnmatched && (
                    <div
                      className="text-amber-400/60 group-hover:text-amber-400 shrink-0 cursor-grab active:cursor-grabbing p-0.5"
                      title="Drag onto a wanted release to link"
                    >
                      <IconGrip className="w-4 h-4" />
                    </div>
                  )}

                  <Link
                    to={`/album/${album.id}`}
                    className="flex items-center gap-3 flex-1 min-w-0"
                    onClick={(e) => {
                      if (draggingAlbumId !== null) e.preventDefault();
                    }}
                  >
                    <div className="w-12 h-12 rounded-lg bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.06]">
                      {album.cover_url ? (
                        <img
                          src={album.cover_url}
                          alt=""
                          className="w-full h-full object-cover"
                          onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
                        />
                      ) : (
                        <div className="w-full h-full flex items-center justify-center text-zinc-600">
                          <IconDisc className="w-5 h-5" />
                        </div>
                      )}
                    </div>

                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-1.5">
                        <p className="font-medium text-sm text-zinc-100 group-hover:text-amber-200 transition-colors truncate">
                          {album.title}
                        </p>
                        {album.record_type && album.record_type !== 'album' && (
                          <span className="text-[9px] font-medium uppercase text-zinc-400 bg-zinc-800 border border-white/[0.05] px-1.5 py-px rounded shrink-0">
                            {album.record_type}
                          </span>
                        )}
                      </div>
                      <p className="text-xs text-zinc-400 mt-0.5 truncate">
                        {album.year ? `${album.year} · ` : ''}
                        {album.tracks?.length || 0} tracks
                        {trackFilter &&
                          album.tracks &&
                          (() => {
                            const q = trackFilter.toLowerCase();
                            const count = album.tracks.filter((t) =>
                              t.title.toLowerCase().includes(q)
                            ).length;
                            return count > 0 ? ` · ${count} match${count > 1 ? 'es' : ''}` : '';
                          })()}
                      </p>
                    </div>
                  </Link>

                  <div className="flex items-center gap-2 shrink-0">
                    {isDropTarget ? (
                      <span className="text-xs font-semibold text-amber-300 bg-amber-400/20 px-2.5 py-1 rounded-lg animate-pulse">
                        Drop to Link
                      </span>
                    ) : (
                      <>
                        <AlbumStatusSummary album={album} artistProvider={artist.provider} />
                        {isUnmatched && (
                          <button
                            type="button"
                            title="Manually link to a release"
                            onClick={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              setManualLinkSource(album);
                            }}
                            className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg text-xs font-semibold bg-amber-400/20 hover:bg-amber-400 text-amber-300 hover:text-zinc-950 border border-amber-400/30 transition-all shrink-0"
                          >
                            <IconLink className="w-3 h-3" />
                            <span>Link</span>
                          </button>
                        )}
                        <Link
                          to={`/album/${album.id}`}
                          className="text-zinc-600 group-hover:text-zinc-400 p-0.5"
                        >
                          <IconChevronRight className="w-4 h-4" />
                        </Link>
                      </>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {pendingMerge && (
        <ConfirmModal
          open={!!pendingMerge}
          title="Link Local Album to Release?"
          description={`Merge "${pendingMerge.src.title}" (${pendingMerge.src.tracks?.length || 0} tracks) into "${pendingMerge.dst.title}"? Local files will claim matching wanted tracks, and the duplicate local entry will be removed.`}
          confirmLabel={linkAlbum.isPending ? 'Linking…' : 'Link & Merge'}
          loading={linkAlbum.isPending}
          onConfirm={() =>
            linkAlbum.mutate({
              srcId: pendingMerge.src.id,
              targetAlbumId: pendingMerge.dst.id,
            })
          }
          onCancel={() => setPendingMerge(null)}
        />
      )}

      <ManualLinkModal
        open={!!manualLinkSource}
        sourceAlbum={manualLinkSource}
        providerAlbums={providerAlbums}
        loading={linkAlbum.isPending}
        onSelect={(targetAlbumId) => {
          if (manualLinkSource) {
            linkAlbum.mutate({
              srcId: manualLinkSource.id,
              targetAlbumId,
            });
          }
        }}
        onClose={() => setManualLinkSource(null)}
      />

      <ConfirmModal
        open={confirmDeleteOpen}
        title={`Remove ${artist.name}?`}
        description="This removes the artist, along with all of its tracked albums and tracks, from Crate's library database. Existing audio files on disk are not deleted."
        confirmLabel="Remove Artist"
        loading={unwatch.isPending}
        onConfirm={() => unwatch.mutate()}
        onCancel={() => setConfirmDeleteOpen(false)}
      />
    </div>
  );
}

function AlbumStatusSummary({
  album,
  artistProvider,
}: {
  album: Album;
  artistProvider: string;
}) {
  if (album.provider === 'local' && artistProvider !== 'local') {
    return <StatusBadge status="unmatched" />;
  }
  const tracks = album.tracks;
  if (!tracks?.length) return null;
  const owned = tracks.filter((t) => t.status === 'owned').length;
  const downloading = tracks.filter((t) => t.status === 'downloading').length;
  const ignored = tracks.filter((t) => t.status === 'ignored').length;
  const total = tracks.length;

  if (ignored === total) {
    return <StatusBadge status="ignored" />;
  }
  if (downloading > 0) {
    return <StatusBadge status="downloading" label={`${downloading} active`} />;
  }
  if (owned === total) {
    return <StatusBadge status="owned" />;
  }
  if (owned > 0) {
    return (
      <span className="text-xs font-medium text-zinc-300 bg-zinc-800 border border-white/[0.06] px-2 py-0.5 rounded-md tabular-nums shrink-0">
        {owned}/{total}
      </span>
    );
  }
  return <StatusBadge status="wanted" />;
}

function ManualLinkModal({
  open,
  sourceAlbum,
  providerAlbums,
  loading,
  onSelect,
  onClose,
}: {
  open: boolean;
  sourceAlbum: Album | null;
  providerAlbums: Album[];
  loading: boolean;
  onSelect: (targetAlbumId: number) => void;
  onClose: () => void;
}) {
  const [search, setSearch] = useState('');

  useEffect(() => {
    if (open) setSearch('');
  }, [open]);

  if (!open || !sourceAlbum) return null;

  const filtered = providerAlbums.filter((a) => {
    if (!search) return true;
    return a.title.toLowerCase().includes(search.toLowerCase());
  });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-sm animate-fade-in">
      <div className="w-full max-w-lg bg-zinc-900 border border-white/[0.08] rounded-2xl p-5 shadow-2xl space-y-4 max-h-[85vh] flex flex-col">
        <div className="flex items-center justify-between shrink-0">
          <div>
            <h3 className="text-base font-semibold text-zinc-100">Link Local Album</h3>
            <p className="text-xs text-zinc-400 mt-0.5 truncate max-w-sm">
              Link <span className="text-amber-300 font-medium">{sourceAlbum.title}</span> to a release
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
          >
            <IconClose className="w-4 h-4" />
          </button>
        </div>

        <div className="shrink-0">
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search provider releases..."
            className="w-full bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-500/50"
            autoFocus
          />
        </div>

        <div className="flex-1 overflow-y-auto space-y-2 pr-1 min-h-[160px]">
          {filtered.length === 0 ? (
            <div className="text-center py-8 text-xs text-zinc-500">
              No matching releases found
            </div>
          ) : (
            filtered.map((a) => (
              <div
                key={a.id}
                className="flex items-center gap-3 bg-zinc-950/70 hover:bg-zinc-800/80 border border-white/[0.05] rounded-xl p-2.5 transition-colors"
              >
                <div className="w-10 h-10 rounded-lg bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.06]">
                  {a.cover_url ? (
                    <img src={a.cover_url} alt="" className="w-full h-full object-cover" />
                  ) : (
                    <div className="w-full h-full flex items-center justify-center text-zinc-600">
                      <IconDisc className="w-4 h-4" />
                    </div>
                  )}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-1.5">
                    <p className="text-xs font-semibold text-zinc-200 truncate">{a.title}</p>
                    {a.record_type && a.record_type !== 'album' && (
                      <span className="text-[9px] font-medium uppercase text-zinc-400 bg-zinc-800 border border-white/[0.05] px-1 py-px rounded shrink-0">
                        {a.record_type}
                      </span>
                    )}
                  </div>
                  <p className="text-[11px] text-zinc-400 mt-0.5">
                    {a.year ? `${a.year} · ` : ''}
                    {a.tracks?.length || 0} tracks
                  </p>
                </div>
                <button
                  type="button"
                  disabled={loading}
                  onClick={() => onSelect(a.id)}
                  className="px-3 py-1.5 rounded-lg text-xs font-medium bg-amber-400 hover:bg-amber-300 text-zinc-950 transition-colors shrink-0 disabled:opacity-50"
                >
                  {loading ? 'Linking…' : 'Link'}
                </button>
              </div>
            ))
          )}
        </div>

        <div className="flex justify-end pt-2 border-t border-white/[0.06] shrink-0">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 rounded-xl text-xs font-medium text-zinc-300 hover:bg-zinc-800 transition-colors"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}
