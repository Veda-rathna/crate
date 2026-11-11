import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import {
  formatCountdown,
  formatDuration,
  formatFileSize,
  formatSpeed,
  formatTotalDuration,
} from '../lib/format';
import FilterBar from '../components/FilterBar';
import DetailSheet, { DetailRow } from '../components/DetailSheet';
import ProviderBadge from '../components/ProviderBadge';
import ProgressBar from '../components/ProgressBar';
import StatusBadge from '../components/StatusBadge';
import ConfirmModal from '../components/ConfirmModal';
import {
  IconDisc,
  IconLink,
  IconRefresh,
  IconSearch,
  IconTrash,
} from '../components/Icons';
import type { DownloadProgress, DownloadQueueItem, ManualSearchResult, Track, Album } from '../types/index';

export default function AlbumDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { toast } = useToast();

  const { data: album, isLoading } = useQuery({
    queryKey: ['album', id],
    queryFn: () => api.getAlbum(Number(id)),
    enabled: !!id,
    refetchInterval: 8_000,
  });

  const { data: downloads } = useQuery({
    queryKey: ['downloads'],
    queryFn: () => api.listDownloads(),
    refetchInterval: 4_000,
  });

  const hasActiveDownloads =
    downloads?.some(
      (d) =>
        d.status === 'downloading' ||
        d.status === 'searching' ||
        d.status === 'pending' ||
        d.status === 'organizing'
    ) ?? false;

  const { data: progress } = useQuery({
    queryKey: ['download-progress'],
    queryFn: api.getDownloadProgress,
    refetchInterval: hasActiveDownloads ? 2000 : false,
    enabled: hasActiveDownloads,
  });

  const { data: artistForLink } = useQuery({
    queryKey: ['artist', album?.artist_id],
    queryFn: () => api.getArtist(album!.artist_id),
    enabled: !!album && album.provider === 'local',
  });

  const linkAlbum = useMutation({
    mutationFn: (targetAlbumID: number) => api.linkAlbum(Number(id), targetAlbumID),
    onSuccess: (res) => {
      toast('Album linked and merged', 'success');
      queryClient.invalidateQueries({ queryKey: ['artist'] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      navigate(`/album/${res.merged_into}`);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const linkTrack = useMutation({
    mutationFn: (vars: { localId: number; targetId: number }) =>
      api.linkTrack(vars.localId, vars.targetId),
    onSuccess: () => {
      toast('Track linked', 'success');
      setLinkTrackId(null);
      queryClient.invalidateQueries({ queryKey: ['album', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const [showAlbumLink, setShowAlbumLink] = useState(false);
  const [linkTrackId, setLinkTrackId] = useState<number | null>(null);
  const [confirmDeleteAlbum, setConfirmDeleteAlbum] = useState(false);
  const [confirmDeleteTrack, setConfirmDeleteTrack] = useState<Track | null>(null);
  const [confirmRejectTrack, setConfirmRejectTrack] = useState<Track | null>(null);

  const linkSiblings = useMemo(
    () =>
      (artistForLink?.albums ?? []).filter(
        (a: Album) => a.id !== Number(id) && a.provider !== 'local'
      ),
    [artistForLink, id]
  );

  const downloadByTrackId = useMemo(() => {
    const map = new Map<number, DownloadQueueItem>();
    for (const d of downloads ?? []) {
      if (d.status !== 'complete') {
        map.set(d.track_id, d);
      }
    }
    return map;
  }, [downloads]);

  const progressByTrackId = useMemo(() => {
    const map = new Map<number, DownloadProgress>();
    for (const p of progress ?? []) {
      if (p.track_id) map.set(p.track_id, p);
    }
    return map;
  }, [progress]);

  const unwatchAlbum = useMutation({
    mutationFn: () => api.unwatchAlbum(Number(id)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      toast('Album removed from library', 'info');
      if (window.history.length > 1) {
        navigate(-1);
      } else {
        navigate('/');
      }
    },
  });

  const unwatchTrack = useMutation({
    mutationFn: (trackId: number) => api.unwatchTrack(trackId),
    onSuccess: () => {
      setConfirmDeleteTrack(null);
      setSelectedTrack(null);
      queryClient.invalidateQueries({ queryKey: ['album', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      toast('Track removed', 'info');
    },
  });

  const rejectTrack = useMutation({
    mutationFn: (trackId: number) => api.rejectTrack(trackId),
    onSuccess: () => {
      setConfirmRejectTrack(null);
      setSelectedTrack(null);
      queryClient.invalidateQueries({ queryKey: ['album', id] });
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      toast('Track rejected — file deleted, source blacklisted & re-queued', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const ignoreAlbum = useMutation({
    mutationFn: () => api.ignoreAlbum(Number(id)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['album', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      toast('Album marked ignored', 'info');
    },
  });

  const unignoreAlbum = useMutation({
    mutationFn: () => api.unignoreAlbum(Number(id)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['album', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      toast('Album unignored', 'success');
    },
  });

  const ignoreTrack = useMutation({
    mutationFn: (trackId: number) => api.ignoreTrack(trackId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['album', id] }),
  });

  const unignoreTrack = useMutation({
    mutationFn: (trackId: number) => api.unignoreTrack(trackId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['album', id] }),
  });

  const queueTrack = useMutation({
    mutationFn: (trackId: number) => api.queueTrack(trackId),
    onSuccess: () => {
      toast('Track queued for download', 'success');
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const queueAll = useMutation({
    mutationFn: () => api.queueAlbumTracks(Number(id)),
    onSuccess: (data) => {
      toast(
        data.queued > 0
          ? `Queued ${data.queued} track${data.queued > 1 ? 's' : ''}`
          : 'Wanted tracks are already queued',
        data.queued > 0 ? 'success' : 'info'
      );
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const [trackFilter, setTrackFilter] = useState('');
  const [selectedTrack, setSelectedTrack] = useState<Track | null>(null);
  const [manualSearchTrackId, setManualSearchTrackId] = useState<number | null>(null);
  const [manualResults, setManualResults] = useState<ManualSearchResult[]>([]);
  const [manualSearching, setManualSearching] = useState(false);
  const [manualSearchComplete, setManualSearchComplete] = useState(false);
  const [manualFileCount, setManualFileCount] = useState(0);
  const [manualQuery, setManualQuery] = useState('');
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const activeSearchRef = useRef<{ trackId: number; searchId: string } | null>(null);

  const cleanupSearch = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    if (activeSearchRef.current) {
      api
        .deleteManualSearch(activeSearchRef.current.trackId, activeSearchRef.current.searchId)
        .catch(() => {});
      activeSearchRef.current = null;
    }
  }, []);

  useEffect(() => cleanupSearch, [cleanupSearch]);

  const runSearch = useCallback(
    async (trackId: number, customQuery?: string) => {
      cleanupSearch();
      setManualResults([]);
      setManualSearching(true);
      setManualSearchComplete(false);
      setManualFileCount(0);
      try {
        const { search_id, query } = await api.startManualSearch(trackId, customQuery);
        setManualQuery(query);
        activeSearchRef.current = { trackId, searchId: search_id };

        let done = false;
        const poll = async () => {
          try {
            const resp = await api.pollManualSearch(trackId, search_id);
            setManualResults(resp.results);
            setManualFileCount(resp.file_count);
            if (resp.is_complete) {
              done = true;
              if (pollRef.current) {
                clearInterval(pollRef.current);
                pollRef.current = null;
              }
              setManualSearching(false);
              setManualSearchComplete(true);
            }
          } catch {
            done = true;
            if (pollRef.current) {
              clearInterval(pollRef.current);
              pollRef.current = null;
            }
            setManualSearching(false);
          }
        };

        await poll();
        if (!done) {
          pollRef.current = setInterval(poll, 2000);
        }
      } catch (err) {
        toast(err instanceof Error ? err.message : 'Search failed', 'error');
        setManualSearching(false);
      }
    },
    [cleanupSearch, toast]
  );

  const startManualSearch = async (trackId: number) => {
    if (manualSearchTrackId === trackId) {
      cleanupSearch();
      setManualSearchTrackId(null);
      setManualResults([]);
      setManualSearchComplete(false);
      setManualFileCount(0);
      setManualQuery('');
      return;
    }
    setManualSearchTrackId(trackId);
    await runSearch(trackId);
  };

  const manualDownload = useMutation({
    mutationFn: (r: ManualSearchResult) =>
      api.manualDownloadTrack(manualSearchTrackId!, r.username, r.filename, r.size, r.bit_rate),
    onSuccess: () => {
      toast('Download started', 'success');
      cleanupSearch();
      setManualSearchTrackId(null);
      setManualResults([]);
      setManualSearchComplete(false);
      setManualFileCount(0);
      setManualQuery('');
      queryClient.invalidateQueries({ queryKey: ['album', id] });
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const stats = useMemo(() => {
    if (!album?.tracks) return null;
    let totalDuration = 0;
    let ownedTracks = 0;
    let wantedTracks = 0;
    for (const track of album.tracks) {
      totalDuration += track.duration_ms;
      if (track.status === 'owned') ownedTracks++;
      if (track.status === 'wanted') wantedTracks++;
    }
    return { totalDuration, ownedTracks, wantedTracks, totalTracks: album.tracks.length };
  }, [album]);

  const filteredTracks = useMemo((): Track[] => {
    if (!album?.tracks) return [];
    if (!trackFilter) return album.tracks;
    const q = trackFilter.toLowerCase();
    return album.tracks.filter((t) => t.title.toLowerCase().includes(q));
  }, [album, trackFilter]);

  const multiDisc = useMemo(() => {
    const discs = new Set((album?.tracks ?? []).map((t) => t.disc_number || 1));
    return discs.size > 1;
  }, [album?.tracks]);

  const wantedTracks = useMemo(
    () => album?.tracks?.filter((t) => t.status === 'wanted') ?? [],
    [album]
  );

  if (isLoading) {
    return (
      <div className="space-y-4 animate-pulse">
        <div className="flex items-center gap-4">
          <div className="w-20 h-20 md:w-24 md:h-24 rounded-xl bg-zinc-800 shrink-0" />
          <div className="flex-1 space-y-2">
            <div className="h-6 bg-zinc-800 rounded w-44" />
            <div className="h-3.5 bg-zinc-800/70 rounded w-32" />
          </div>
        </div>
        <div className="space-y-1">
          {[...Array(8)].map((_, i) => (
            <div key={i} className="h-14 bg-zinc-900/60 rounded-xl" />
          ))}
        </div>
      </div>
    );
  }

  if (!album) {
    return (
      <div className="rounded-2xl bg-zinc-900/50 border border-white/[0.06] py-12 text-center text-zinc-400 text-sm">
        Album not found
      </div>
    );
  }

  const selectedQueueItem = selectedTrack ? downloadByTrackId.get(selectedTrack.id) : undefined;
  const selectedProgress = selectedTrack ? progressByTrackId.get(selectedTrack.id) : undefined;

  return (
    <div className="space-y-5">
      {/* Album Hero */}
      <div className="rounded-2xl bg-zinc-900/70 border border-white/[0.07] p-4 md:p-5 space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div className="flex items-center gap-4 min-w-0">
            <div className="w-20 h-20 md:w-24 md:h-24 rounded-xl bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.08]">
              {album.cover_url ? (
                <img
                  src={album.cover_url}
                  alt={album.title}
                  className="w-full h-full object-cover"
                  onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
                />
              ) : (
                <div className="w-full h-full flex items-center justify-center text-zinc-600">
                  <IconDisc className="w-8 h-8" />
                </div>
              )}
            </div>

            <div className="flex-1 min-w-0">
              <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100 truncate">
                {album.title}
              </h2>
              <p className="text-xs text-zinc-400 mt-1">
                {album.artist_name && (
                  <Link
                    to={`/artist/${album.artist_id}`}
                    className="text-zinc-200 hover:text-amber-300 font-medium transition-colors"
                  >
                    {album.artist_name}
                  </Link>
                )}
                {album.year ? ` · ${album.year}` : ''}
                {stats ? ` · ${stats.totalTracks} tracks · ${formatTotalDuration(stats.totalDuration)}` : ''}
              </p>
              <div className="flex flex-wrap items-center gap-1.5 mt-2">
                <ProviderBadge provider={album.provider} />
                {album.record_type && album.record_type !== 'album' && (
                  <span className="text-[10px] font-medium uppercase text-zinc-400 bg-zinc-800 border border-white/[0.06] px-2 py-0.5 rounded-md">
                    {album.record_type}
                  </span>
                )}
                {album.status === 'ignored' && <StatusBadge status="ignored" />}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <button
              type="button"
              onClick={() =>
                album.status === 'ignored' ? unignoreAlbum.mutate() : ignoreAlbum.mutate()
              }
              disabled={ignoreAlbum.isPending || unignoreAlbum.isPending}
              className={`px-3.5 py-2 rounded-xl text-xs font-medium border transition-colors ${
                album.status === 'ignored'
                  ? 'bg-zinc-700 border-white/[0.15] text-zinc-100'
                  : 'bg-zinc-800 hover:bg-zinc-700 border-white/[0.08] text-zinc-300'
              }`}
            >
              {album.status === 'ignored' ? 'Unignore' : 'Ignore'}
            </button>
            <button
              type="button"
              onClick={() => setConfirmDeleteAlbum(true)}
              disabled={unwatchAlbum.isPending}
              className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl text-xs font-medium bg-zinc-800 hover:bg-rose-500/15 border border-white/[0.08] hover:border-rose-500/30 text-zinc-300 hover:text-rose-300 transition-colors"
            >
              <IconTrash className="w-3.5 h-3.5" />
              <span>Delete</span>
            </button>
          </div>
        </div>

        {stats && stats.totalTracks > 0 && (
          <div className="pt-3 border-t border-white/[0.06] flex flex-col sm:flex-row sm:items-center gap-3">
            <div className="flex-1">
              <ProgressBar owned={stats.ownedTracks} total={stats.totalTracks} showPct />
            </div>
            {stats.wantedTracks > 0 && (
              <button
                type="button"
                onClick={() => queueAll.mutate()}
                disabled={queueAll.isPending || queueAll.isSuccess}
                className="px-4 py-2 bg-amber-400 hover:bg-amber-300 text-zinc-950 rounded-xl font-semibold text-xs active:scale-[0.98] transition-all disabled:opacity-60 flex items-center justify-center gap-1.5 shrink-0"
              >
                <IconSearch className="w-3.5 h-3.5" />
                {queueAll.isSuccess
                  ? 'Queued!'
                  : queueAll.isPending
                    ? 'Queuing…'
                    : `Queue ${stats.wantedTracks} Wanted`}
              </button>
            )}
          </div>
        )}
      </div>

      {/* Local Unlinked Album Banner */}
      {album.provider === 'local' && (
        <div className="bg-amber-950/25 border border-amber-500/30 rounded-2xl p-4 space-y-3">
          <div className="flex flex-col sm:flex-row sm:items-center gap-3">
            <div className="flex items-start gap-3 flex-1 min-w-0">
              <IconLink className="w-5 h-5 text-amber-400 shrink-0 mt-0.5" />
              <div>
                <p className="text-sm text-amber-200 font-semibold">Unmatched local album</p>
                <p className="text-xs text-amber-200/70 mt-0.5">
                  Link this imported folder to a provider release under the artist to merge owned files and reveal missing tracks.
                </p>
              </div>
            </div>
            <button
              type="button"
              onClick={() => setShowAlbumLink((v) => !v)}
              className="px-4 py-2 rounded-xl text-xs font-semibold bg-amber-400 hover:bg-amber-300 text-zinc-950 transition-colors shrink-0"
            >
              {showAlbumLink ? 'Close Picker' : 'Link Release'}
            </button>
          </div>

          {showAlbumLink && (
            <div className="space-y-1.5 max-h-60 overflow-y-auto pt-2 border-t border-amber-500/20">
              {linkSiblings.length === 0 && (
                <p className="text-xs text-amber-200/70 py-1">
                  No linked releases on this artist yet — link the artist to a provider first.
                </p>
              )}
              {linkSiblings.map((sib: Album) => (
                <button
                  key={sib.id}
                  type="button"
                  onClick={() => linkAlbum.mutate(sib.id)}
                  disabled={linkAlbum.isPending}
                  className="w-full flex items-center gap-3 bg-zinc-950/75 hover:bg-zinc-800 border border-white/[0.06] rounded-xl p-2.5 text-left transition-colors disabled:opacity-50"
                >
                  <div className="w-9 h-9 rounded-lg bg-zinc-800 overflow-hidden shrink-0">
                    {sib.cover_url && (
                      <img
                        src={sib.cover_url}
                        alt=""
                        className="w-full h-full object-cover"
                        onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
                      />
                    )}
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-zinc-100 truncate">{sib.title}</p>
                    <p className="text-[11px] text-zinc-400">
                      {sib.year ? `${sib.year} · ` : ''}
                      {sib.tracks?.length ?? 0} tracks
                    </p>
                  </div>
                  <span className="text-xs font-medium text-amber-300 shrink-0">Merge Here</span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Tracklist */}
      {album.tracks && album.tracks.length > 0 && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
              Tracks ({filteredTracks.length})
            </p>
          </div>

          {album.tracks.length > 4 && (
            <FilterBar
              value={trackFilter}
              onChange={setTrackFilter}
              placeholder="Filter tracks..."
              className="mb-0"
            />
          )}

          {trackFilter && filteredTracks.length === 0 && (
            <p className="text-zinc-500 text-sm text-center py-6">No matching tracks</p>
          )}

          <div className="rounded-2xl bg-zinc-900/60 border border-white/[0.06] overflow-hidden divide-y divide-white/[0.05]">
            {filteredTracks.map((track, idx) => {
              const prevDisc = idx > 0 ? filteredTracks[idx - 1].disc_number || 1 : null;
              const currDisc = track.disc_number || 1;
              const showDiscHeader = multiDisc && currDisc !== prevDisc;

              const queueItem = downloadByTrackId.get(track.id);
              const liveProg = progressByTrackId.get(track.id);
              const isQueued = !!queueItem && queueItem.status !== 'failed';
              const isSearching = queueTrack.isPending && queueTrack.variables === track.id;
              const isManualOpen = manualSearchTrackId === track.id;
              const isLocalUnmatched = track.provider === 'local' && album.provider !== 'local';
              const isLinkOpen = linkTrackId === track.id;

              return (
                <div key={track.id}>
                  {showDiscHeader && (
                    <div className="px-4 py-1.5 bg-zinc-950/60 border-b border-white/[0.05] text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
                      Disc {currDisc}
                    </div>
                  )}

                  <div
                    className="px-3.5 py-3 hover:bg-white/[0.02] cursor-pointer transition-colors"
                    onClick={() => setSelectedTrack(track)}
                  >
                    <div className="flex items-center gap-3">
                      <span className="text-xs text-zinc-500 w-6 text-right shrink-0 tabular-nums">
                        {track.track_number}
                      </span>

                      <div className="flex-1 min-w-0">
                        <p className="text-sm font-medium text-zinc-100 truncate">{track.title}</p>
                        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-zinc-500 mt-0.5">
                          <span className="tabular-nums">{formatDuration(track.duration_ms)}</span>
                          {track.status === 'owned' && track.download_format && (
                            <span className="text-zinc-300 uppercase font-mono text-[10px]">
                              {track.download_format}
                              {track.download_bitrate ? ` ${track.download_bitrate}k` : ''}
                            </span>
                          )}
                          {track.status === 'owned' && track.downloaded_from && (
                            <span className="truncate">from {track.downloaded_from}</span>
                          )}
                          {queueItem?.status === 'pending' && queueItem.next_retry_at && (
                            <span className="text-amber-300">
                              Retry {queueItem.attempts}/4 {formatCountdown(queueItem.next_retry_at)}
                            </span>
                          )}
                          {queueItem?.status === 'failed' && queueItem.error && (
                            <span className="text-rose-300 truncate">{queueItem.error}</span>
                          )}
                        </div>
                      </div>

                      {/* Status + Quick Actions */}
                      <div
                        className="flex items-center gap-1.5 shrink-0"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {track.status === 'owned' && isLocalUnmatched && (
                          <>
                            <button
                              type="button"
                              onClick={() => setLinkTrackId(isLinkOpen ? null : track.id)}
                              className={`p-1.5 rounded-lg border transition-colors ${
                                isLinkOpen
                                  ? 'bg-amber-500/20 border-amber-500/40 text-amber-300'
                                  : 'bg-zinc-800 border-white/[0.06] text-amber-400 hover:bg-zinc-700'
                              }`}
                              title="Link to a wanted track"
                            >
                              <IconLink className="w-3.5 h-3.5" />
                            </button>
                            <StatusBadge status="local" />
                          </>
                        )}

                        {track.status === 'owned' && !isLocalUnmatched && (
                          <>
                            <button
                              type="button"
                              onClick={() => startManualSearch(track.id)}
                              disabled={manualSearching && isManualOpen}
                              className={`p-1.5 rounded-lg border transition-colors ${
                                isManualOpen
                                  ? 'bg-sky-500/20 border-sky-500/40 text-sky-300'
                                  : 'bg-zinc-800/80 border-white/[0.06] text-zinc-400 hover:text-zinc-100'
                              }`}
                              title="Manual search (replace quality)"
                            >
                              <IconSearch className="w-3.5 h-3.5" />
                            </button>
                            <StatusBadge status="owned" />
                          </>
                        )}

                        {track.status !== 'owned' && queueItem && (
                          <StatusBadge status={queueItem.status} />
                        )}

                        {track.status !== 'owned' && !queueItem && (
                          <StatusBadge status={track.status} />
                        )}

                        {(track.status === 'wanted' || track.status === 'ignored') && !isQueued && (
                          <>
                            <button
                              type="button"
                              onClick={() => queueTrack.mutate(track.id)}
                              disabled={isSearching || track.status === 'ignored'}
                              className="p-1.5 rounded-lg bg-zinc-800/80 hover:bg-zinc-700 border border-white/[0.06] text-zinc-300 disabled:opacity-25 transition-colors"
                              title={
                                track.status === 'ignored'
                                  ? 'Ignored'
                                  : 'Queue auto search'
                              }
                            >
                              <IconRefresh className="w-3.5 h-3.5" />
                            </button>
                            <button
                              type="button"
                              onClick={() => startManualSearch(track.id)}
                              disabled={
                                (manualSearching && isManualOpen) || track.status === 'ignored'
                              }
                              className={`p-1.5 rounded-lg border transition-colors disabled:opacity-25 ${
                                isManualOpen
                                  ? 'bg-sky-500/20 border-sky-500/40 text-sky-300'
                                  : 'bg-zinc-800/80 hover:bg-zinc-700 border-white/[0.06] text-zinc-300'
                              }`}
                              title="Manual Soulseek / Arcod search"
                            >
                              <IconSearch className="w-3.5 h-3.5" />
                            </button>
                          </>
                        )}
                      </div>
                    </div>

                    {/* Live Progress Bar for Downloading Track */}
                    {liveProg && (
                      <div className="mt-2 ml-9 space-y-1">
                        <div className="flex items-center justify-between text-[11px] text-sky-300">
                          <span className="truncate">
                            {liveProg.username} · {liveProg.state}
                          </span>
                          <span className="font-mono tabular-nums shrink-0">
                            {liveProg.percent_complete.toFixed(0)}%
                            {liveProg.average_speed_bps > 0
                              ? ` · ${formatSpeed(liveProg.average_speed_bps)}`
                              : ''}
                          </span>
                        </div>
                        <div className="h-1 bg-zinc-800 rounded-full overflow-hidden">
                          <div
                            className="h-full bg-sky-400 rounded-full transition-all duration-500"
                            style={{ width: `${liveProg.percent_complete}%` }}
                          />
                        </div>
                      </div>
                    )}
                  </div>

                  {/* Manual Search Panel */}
                  {isManualOpen && (
                    <div className="bg-zinc-950/80 border-t border-white/[0.06] px-4 py-3.5 space-y-3 animate-fade-in">
                      <form
                        onSubmit={(e) => {
                          e.preventDefault();
                          if (manualSearchTrackId && manualQuery.trim()) {
                            runSearch(manualSearchTrackId, manualQuery.trim());
                          }
                        }}
                        className="flex gap-2"
                      >
                        <input
                          type="text"
                          value={manualQuery}
                          onChange={(e) => {
                            const v = e.target.value;
                            setManualQuery(v);
                            if (v.trim() === '') {
                              setManualResults([]);
                              setManualSearchComplete(false);
                              setManualFileCount(0);
                            }
                          }}
                          placeholder="Custom Soulseek query..."
                          className="flex-1 bg-zinc-900 border border-white/[0.08] rounded-xl px-3 py-2 text-sm md:text-xs text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-sky-500/50"
                          disabled={manualSearching}
                        />
                        <button
                          type="submit"
                          disabled={manualSearching || !manualQuery.trim()}
                          className="px-3.5 py-2 bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] disabled:opacity-40 rounded-xl text-xs font-medium text-zinc-200 transition-colors shrink-0"
                        >
                          Re-search
                        </button>
                      </form>

                      {manualSearching && manualResults.length === 0 && (
                        <div className="flex items-center gap-2.5 py-4 justify-center text-xs text-sky-300">
                          <div className="w-4 h-4 border-2 border-sky-500/30 border-t-sky-300 rounded-full animate-spin" />
                          <span>Searching Arcod & Soulseek peers…</span>
                        </div>
                      )}

                      {manualSearchComplete && manualResults.length === 0 && (
                        <p className="text-xs text-zinc-500 text-center py-4">
                          No results found. Try editing the search query above.
                        </p>
                      )}

                      {manualResults.length > 0 && (
                        <div className="space-y-1.5 max-h-72 overflow-y-auto pr-0.5">
                          <div className="flex items-center justify-between text-[11px] text-zinc-400 pb-1">
                            <span>
                              {manualResults.length} results
                              {manualFileCount > 0 ? ` (${manualFileCount} files scanned)` : ''}
                            </span>
                            {manualSearching && (
                              <span className="inline-flex items-center gap-1.5 text-sky-300">
                                <span className="w-2.5 h-2.5 border border-sky-500/40 border-t-sky-300 rounded-full animate-spin" />
                                Receiving peer results…
                              </span>
                            )}
                          </div>

                          {manualResults.map((r, i) => (
                            <button
                              key={`${r.username}-${i}`}
                              type="button"
                              onClick={() =>
                                !r.blacklisted && !r.locked && manualDownload.mutate(r)
                              }
                              disabled={r.blacklisted || r.locked || manualDownload.isPending}
                              className={`w-full text-left rounded-xl px-3 py-2.5 border transition-colors ${
                                r.blacklisted || r.locked
                                  ? 'opacity-40 cursor-not-allowed bg-zinc-900/40 border-white/[0.04]'
                                  : r.negative_match
                                    ? 'opacity-65 bg-zinc-900/50 border-white/[0.05] hover:border-white/[0.12]'
                                    : r.language_mismatch
                                      ? 'bg-amber-950/25 border-amber-500/30 hover:bg-amber-950/40'
                                      : 'bg-zinc-900/80 border-white/[0.07] hover:border-amber-500/40 hover:bg-zinc-800/80'
                              }`}
                            >
                              <div className="flex items-center gap-3">
                                <div className="flex-1 min-w-0">
                                  <div className="flex items-center gap-1.5">
                                    <p className="text-xs font-medium text-zinc-100 truncate">
                                      {r.filename.split(/[/\\]/).pop()}
                                    </p>
                                    {r.language_mismatch && (
                                      <span className="shrink-0 text-[9px] font-medium bg-amber-500/20 text-amber-300 border border-amber-500/30 px-1.5 py-0.5 rounded">
                                        {r.detected_language
                                          ? `Lang: ${r.detected_language}`
                                          : 'Lang mismatch'}
                                      </span>
                                    )}
                                  </div>
                                  <p className="text-[11px] text-zinc-400 truncate mt-0.5">
                                    {r.username}
                                    {' · '}
                                    <span className="uppercase font-mono text-zinc-300">
                                      {r.format}
                                      {r.bit_depth ? ` ${r.bit_depth}b` : ''}
                                      {r.sample_rate ? `/${Math.round(r.sample_rate / 1000)}k` : ''}
                                      {r.bit_rate && !r.bit_depth ? ` ${r.bit_rate}kbps` : ''}
                                    </span>
                                    {' · '}
                                    {formatFileSize(r.size)}
                                    {r.free_slot && (
                                      <span className="text-emerald-400"> · free slot</span>
                                    )}
                                    {r.queue_length > 0 && ` · ${r.queue_length} queued`}
                                    {r.blacklisted && ' · blacklisted'}
                                    {r.locked && ' · locked'}
                                    {r.negative_match && ' · negative keyword'}
                                  </p>
                                </div>

                                <div className="flex items-center gap-2 shrink-0">
                                  <span
                                    className={`text-xs font-mono font-semibold tabular-nums px-2 py-0.5 rounded-md ${
                                      r.score >= 100
                                        ? 'bg-emerald-500/15 text-emerald-300'
                                        : r.score >= 50
                                          ? 'bg-sky-500/15 text-sky-300'
                                          : 'bg-zinc-800 text-zinc-400'
                                    }`}
                                  >
                                    {r.score}
                                  </span>
                                </div>
                              </div>
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Local Track Link Picker */}
                  {isLinkOpen && (
                    <div className="bg-amber-950/20 border-t border-amber-500/25 px-4 py-3 space-y-2 animate-fade-in">
                      <p className="text-xs text-amber-200 font-medium">
                        Which release track does this local file match?
                      </p>
                      {wantedTracks.length === 0 && (
                        <p className="text-xs text-zinc-500 py-1">
                          No wanted tracks on this album to claim.
                        </p>
                      )}
                      <div className="space-y-1 max-h-56 overflow-y-auto">
                        {wantedTracks.map((wt) => (
                          <button
                            key={wt.id}
                            type="button"
                            onClick={() => linkTrack.mutate({ localId: track.id, targetId: wt.id })}
                            disabled={linkTrack.isPending}
                            className="w-full text-left rounded-xl px-3 py-2 bg-zinc-900/80 hover:bg-zinc-800 border border-white/[0.06] transition-colors disabled:opacity-50"
                          >
                            <p className="text-xs text-zinc-100 truncate">
                              <span className="text-zinc-500 tabular-nums mr-2">
                                {wt.track_number}
                              </span>
                              {wt.title}
                            </p>
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Track Detail Modal / Sheet */}
      <DetailSheet
        open={!!selectedTrack}
        onClose={() => setSelectedTrack(null)}
        title={selectedTrack?.title ?? ''}
        subtitle={album.title}
      >
        {selectedTrack && (
          <div className="space-y-1">
            <DetailRow label="Track">
              {selectedTrack.disc_number > 1 ? `Disc ${selectedTrack.disc_number}, ` : ''}#
              {selectedTrack.track_number}
            </DetailRow>
            <DetailRow label="Duration">{formatDuration(selectedTrack.duration_ms)}</DetailRow>
            <DetailRow label="Status">
              <StatusBadge status={selectedQueueItem?.status ?? selectedTrack.status} />
            </DetailRow>
            {selectedProgress && (
              <DetailRow label="Transfer">
                {selectedProgress.percent_complete.toFixed(0)}%
                {selectedProgress.average_speed_bps > 0
                  ? ` · ${formatSpeed(selectedProgress.average_speed_bps)}`
                  : ''}{' '}
                ({selectedProgress.username})
              </DetailRow>
            )}
            {selectedQueueItem?.error && (
              <DetailRow label="Last Error">
                <span className="text-rose-300">{selectedQueueItem.error}</span>
              </DetailRow>
            )}
            {selectedTrack.download_format && (
              <DetailRow label="Quality">
                {selectedTrack.download_format.toUpperCase()}
                {selectedTrack.download_bitrate ? ` ${selectedTrack.download_bitrate} kbps` : ''}
              </DetailRow>
            )}
            {selectedTrack.downloaded_from && (
              <DetailRow label="Source">{selectedTrack.downloaded_from}</DetailRow>
            )}
            {selectedTrack.file_path && (
              <DetailRow label="File Path">
                <span className="font-mono text-xs">{selectedTrack.file_path}</span>
              </DetailRow>
            )}

            {/* Track Actions */}
            <div className="flex flex-wrap gap-2 pt-4 mt-2 border-t border-white/[0.06]">
              {(selectedTrack.status === 'wanted' || selectedTrack.status === 'ignored') && (
                <>
                  <button
                    type="button"
                    disabled={selectedTrack.status === 'ignored' || queueTrack.isPending}
                    onClick={() => {
                      queueTrack.mutate(selectedTrack.id);
                      setSelectedTrack(null);
                    }}
                    className="px-3.5 py-2 rounded-xl bg-amber-400 hover:bg-amber-300 text-zinc-950 text-xs font-semibold disabled:opacity-40 transition-colors"
                  >
                    Queue Auto Search
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      const tid = selectedTrack.id;
                      setSelectedTrack(null);
                      startManualSearch(tid);
                    }}
                    disabled={selectedTrack.status === 'ignored'}
                    className="px-3.5 py-2 rounded-xl bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-xs font-medium disabled:opacity-40 transition-colors"
                  >
                    Manual Search
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      if (selectedTrack.status === 'ignored') {
                        unignoreTrack.mutate(selectedTrack.id);
                      } else {
                        ignoreTrack.mutate(selectedTrack.id);
                      }
                      setSelectedTrack(null);
                    }}
                    className="px-3.5 py-2 rounded-xl bg-zinc-800 hover:bg-zinc-700 text-zinc-300 text-xs font-medium transition-colors"
                  >
                    {selectedTrack.status === 'ignored' ? 'Unignore' : 'Ignore'}
                  </button>
                </>
              )}

              {selectedTrack.status === 'owned' && (
                <>
                  <button
                    type="button"
                    onClick={() => {
                      const tid = selectedTrack.id;
                      setSelectedTrack(null);
                      startManualSearch(tid);
                    }}
                    className="px-3.5 py-2 rounded-xl bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-xs font-medium transition-colors"
                  >
                    Manual Replace
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmRejectTrack(selectedTrack)}
                    className="px-3.5 py-2 rounded-xl bg-orange-500/15 hover:bg-orange-500/25 border border-orange-500/30 text-orange-300 text-xs font-semibold transition-colors"
                  >
                    Reject & Re-download
                  </button>
                </>
              )}

              <button
                type="button"
                onClick={() => setConfirmDeleteTrack(selectedTrack)}
                className="px-3.5 py-2 rounded-xl bg-rose-500/15 hover:bg-rose-500/25 border border-rose-500/25 text-rose-300 text-xs font-medium transition-colors ml-auto"
              >
                Remove Track
              </button>
            </div>
          </div>
        )}
      </DetailSheet>

      <ConfirmModal
        open={confirmDeleteAlbum}
        title={`Remove "${album.title}"?`}
        description="This removes the album and all its tracks from Crate's library watchlist. Audio files on disk are preserved."
        confirmLabel="Remove Album"
        loading={unwatchAlbum.isPending}
        onConfirm={() => unwatchAlbum.mutate()}
        onCancel={() => setConfirmDeleteAlbum(false)}
      />

      <ConfirmModal
        open={!!confirmDeleteTrack}
        title={`Remove "${confirmDeleteTrack?.title}"?`}
        description="This removes the track entry from Crate's watchlist."
        confirmLabel="Remove Track"
        loading={unwatchTrack.isPending}
        onConfirm={() => confirmDeleteTrack && unwatchTrack.mutate(confirmDeleteTrack.id)}
        onCancel={() => setConfirmDeleteTrack(null)}
      />

      <ConfirmModal
        open={!!confirmRejectTrack}
        title={`Reject & Re-download "${confirmRejectTrack?.title}"?`}
        description="This deletes the current audio file from your library, blacklists its source so Crate won't pick the bad file again, and immediately re-queues the track for a fresh download."
        confirmLabel="Reject & Re-download"
        loading={rejectTrack.isPending}
        onConfirm={() => confirmRejectTrack && rejectTrack.mutate(confirmRejectTrack.id)}
        onCancel={() => setConfirmRejectTrack(null)}
      />
    </div>
  );
}
