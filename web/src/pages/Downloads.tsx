import { useState, useCallback, useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { api } from '../api/client';
import {
  formatCountdown,
  formatFileSize,
  formatRelativeTime,
  formatSpeed,
  getActivityMeta,
  type ActivityCategory,
} from '../lib/format';
import { useSystemProcess } from '../context/SystemContext';
import { useToast } from '../components/Toast';
import DetailSheet, { DetailRow } from '../components/DetailSheet';
import StatusBadge from '../components/StatusBadge';
import {
  IconActivity,
  IconChevronRight,
  IconClose,
  IconDownload,
  IconPause,
  IconPlay,
  IconRefresh,
} from '../components/Icons';
import type { DownloadProgress, DownloadQueueItem, ActivityLog } from '../types/index';

const ACTIVITY_PAGE_SIZE = 50;

export default function Downloads() {
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [searchParams, setSearchParams] = useSearchParams();
  const initialTab = searchParams.get('tab') === 'activity' ? 'activity' : 'queue';
  const [tab, setTab] = useState<'queue' | 'activity'>(initialTab);
  const [activityFilter, setActivityFilter] = useState<'all' | ActivityCategory>('all');
  const [stageFilter, setStageFilter] = useState<string>('all');
  const [selected, setSelected] = useState<DownloadQueueItem | null>(null);

  const { activeTasks, importStatus, status } = useSystemProcess();

  const handleTabChange = (next: 'queue' | 'activity') => {
    setTab(next);
    setSearchParams(next === 'activity' ? { tab: 'activity' } : {}, { replace: true });
  };

  const { data: downloads, isLoading } = useQuery({
    queryKey: ['downloads'],
    queryFn: () => api.listDownloads(),
    refetchInterval: 4000,
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

  const [extraActivity, setExtraActivity] = useState<ActivityLog[]>([]);
  const [activityOffset, setActivityOffset] = useState(ACTIVITY_PAGE_SIZE);
  const [loadingMoreActivity, setLoadingMoreActivity] = useState(false);

  const { data: activityData } = useQuery({
    queryKey: ['activity'],
    queryFn: () => api.listActivity(ACTIVITY_PAGE_SIZE, 0),
    enabled: tab === 'activity',
    refetchInterval: 6_000,
  });

  const activityItems = useMemo(
    () => [...(activityData?.items || []), ...extraActivity],
    [activityData?.items, extraActivity]
  );
  const activityTotal = activityData?.total ?? 0;

  const filteredActivity = useMemo(() => {
    if (activityFilter === 'all') return activityItems;
    return activityItems.filter((a) => getActivityMeta(a.action).category === activityFilter);
  }, [activityItems, activityFilter]);

  const loadMoreActivity = useCallback(async () => {
    if (loadingMoreActivity || activityOffset >= activityTotal) return;
    setLoadingMoreActivity(true);
    try {
      const data = await api.listActivity(ACTIVITY_PAGE_SIZE, activityOffset);
      setExtraActivity((prev) => [...prev, ...(data.items || [])]);
      setActivityOffset((prev) => prev + ACTIVITY_PAGE_SIZE);
    } finally {
      setLoadingMoreActivity(false);
    }
  }, [activityOffset, activityTotal, loadingMoreActivity]);

  const queue = useMutation({
    mutationFn: api.queueDownloads,
    onSuccess: (res) => {
      toast(
        res.queued > 0
          ? `Queued ${res.queued} wanted track${res.queued > 1 ? 's' : ''}`
          : 'No unqueued wanted tracks found',
        res.queued > 0 ? 'success' : 'info'
      );
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const retry = useMutation({
    mutationFn: (id: number) => api.retryDownload(id),
    onSuccess: () => {
      toast('Re-queued for immediate search', 'success');
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const dismiss = useMutation({
    mutationFn: (id: number) => api.deleteDownload(id),
    onMutate: async (id) => {
      await queryClient.cancelQueries({ queryKey: ['downloads'] });
      queryClient.setQueryData(['downloads'], (old: DownloadQueueItem[] | undefined) =>
        old?.filter((d) => d.id !== id)
      );
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
  });

  const clearByStatus = useMutation({
    mutationFn: (status: string) => api.clearDownloadsByStatus(status),
    onMutate: async (status) => {
      await queryClient.cancelQueries({ queryKey: ['downloads'] });
      queryClient.setQueryData(['downloads'], (old: DownloadQueueItem[] | undefined) =>
        old?.filter((d) => d.status !== status)
      );
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
  });

  const pauseSingle = useMutation({
    mutationFn: (id: number) => api.pauseDownload(id),
    onSuccess: () => {
      toast('Download paused', 'info');
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const resumeSingle = useMutation({
    mutationFn: (id: number) => api.resumeDownload(id),
    onSuccess: () => {
      toast('Download resumed', 'success');
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const pauseAll = useMutation({
    mutationFn: () => api.pauseAllDownloads(),
    onSuccess: (res) => {
      toast(`Paused all downloads (${res.affected} items paused)`, 'info');
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const resumeAll = useMutation({
    mutationFn: () => api.resumeAllDownloads(),
    onSuccess: (res) => {
      toast(`Resumed all downloads (${res.affected} items resumed)`, 'success');
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  // Map live transfer progress by download_id and track_id for direct inline display
  const progressByDownloadId = useMemo(() => {
    const map = new Map<number, DownloadProgress>();
    for (const p of progress ?? []) {
      if (p.download_id) map.set(p.download_id, p);
    }
    return map;
  }, [progress]);

  const progressByTrackId = useMemo(() => {
    const map = new Map<number, DownloadProgress>();
    for (const p of progress ?? []) {
      if (p.track_id) map.set(p.track_id, p);
    }
    return map;
  }, [progress]);

  const searching = downloads?.filter((d) => d.status === 'searching') ?? [];
  const downloading = downloads?.filter((d) => d.status === 'downloading') ?? [];
  const organizing = downloads?.filter((d) => d.status === 'organizing') ?? [];
  const active = [...downloading, ...searching, ...organizing];
  const pending = downloads?.filter((d) => d.status === 'pending') ?? [];
  const paused = downloads?.filter((d) => d.status === 'paused') ?? [];
  const failed = downloads?.filter((d) => d.status === 'failed') ?? [];
  const complete = downloads?.filter((d) => d.status === 'complete') ?? [];

  const matchedProgressKeys = new Set<string>();
  for (const dl of active) {
    const p = progressByDownloadId.get(dl.id) ?? progressByTrackId.get(dl.track_id);
    if (p) matchedProgressKeys.add(`${p.username}|${p.filename}`);
  }
  const unmatchedProgress = (progress ?? []).filter(
    (p) => !matchedProgressKeys.has(`${p.username}|${p.filename}`)
  );

  const showSection = (name: string) => stageFilter === 'all' || stageFilter === name;

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100">
            Downloads & Activity
          </h2>
          <p className="text-xs text-zinc-400 mt-0.5">
            Track live searches, file transfers, library organization, and background events.
          </p>
        </div>

        <div className="flex items-center gap-2">
          <div className="flex bg-zinc-900 border border-white/[0.08] rounded-xl p-1">
            <button
              type="button"
              onClick={() => handleTabChange('queue')}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-colors ${
                tab === 'queue' ? 'bg-zinc-800 text-white' : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Queue
              {active.length + pending.length > 0 && (
                <span className="ml-1.5 px-1.5 py-px rounded bg-sky-500/20 text-sky-300 text-[10px] tabular-nums">
                  {active.length + pending.length}
                </span>
              )}
            </button>
            <button
              type="button"
              onClick={() => handleTabChange('activity')}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-colors ${
                tab === 'activity'
                  ? 'bg-zinc-800 text-white'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Activity Log
            </button>
          </div>

          {tab === 'queue' && (
            <div className="flex items-center gap-2 shrink-0">
              {status?.downloads_paused ? (
                <button
                  type="button"
                  onClick={() => resumeAll.mutate()}
                  disabled={resumeAll.isPending}
                  className="inline-flex items-center gap-1.5 px-3.5 py-2 bg-emerald-500 hover:bg-emerald-400 text-zinc-950 rounded-xl text-xs font-semibold active:scale-[0.98] transition-all disabled:opacity-50 shrink-0 shadow-sm"
                  title="Resume all downloads"
                >
                  <IconPlay className="w-3.5 h-3.5 fill-current" />
                  <span>{resumeAll.isPending ? 'Resuming…' : 'Resume Downloads'}</span>
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => pauseAll.mutate()}
                  disabled={pauseAll.isPending || (active.length === 0 && pending.length === 0)}
                  className="inline-flex items-center gap-1.5 px-3.5 py-2 bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 rounded-xl text-xs font-medium transition-colors disabled:opacity-50 shrink-0"
                  title="Pause all active and pending downloads"
                >
                  <IconPause className="w-3.5 h-3.5" />
                  <span>{pauseAll.isPending ? 'Pausing…' : 'Pause All'}</span>
                </button>
              )}

              <button
                type="button"
                onClick={() => queue.mutate()}
                disabled={queue.isPending || status?.downloads_paused}
                className="px-3.5 py-2 bg-amber-400 hover:bg-amber-300 text-zinc-950 rounded-xl text-xs font-semibold active:scale-[0.98] transition-all disabled:opacity-50 shrink-0"
              >
                {queue.isPending ? 'Queuing…' : 'Queue All Wanted'}
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Global Downloads Paused Banner */}
      {tab === 'queue' && status?.downloads_paused && (
        <div className="flex items-center justify-between gap-3 p-3.5 rounded-xl bg-amber-500/15 border border-amber-500/30 text-amber-200 animate-fade-in">
          <div className="flex items-center gap-2.5">
            <IconPause className="w-4 h-4 text-amber-400 shrink-0" />
            <p className="text-xs font-medium">
              Downloads are paused — background searches and file transfers will not run until resumed.
            </p>
          </div>
          <button
            type="button"
            onClick={() => resumeAll.mutate()}
            disabled={resumeAll.isPending}
            className="px-3 py-1 rounded-lg bg-amber-400 hover:bg-amber-300 text-zinc-950 text-xs font-semibold shrink-0 transition-colors"
          >
            Resume Downloads
          </button>
        </div>
      )}

      {/* Running System Jobs (Syncs / Reconciliations / Imports) */}
      {(activeTasks.length > 0 || importStatus?.status === 'running') && (
        <div className="space-y-2">
          <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
            Background System Tasks
          </p>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-2.5">
            {importStatus?.status === 'running' && (
              <div className="rounded-2xl bg-sky-950/30 border border-sky-500/25 p-3.5">
                <div className="flex items-center justify-between gap-2 mb-1">
                  <span className="text-xs font-semibold text-sky-200">
                    {importStatus.dry_run ? 'Scanning Library (Dry Run)' : 'Importing Library'}
                  </span>
                  <span className="text-[11px] font-mono text-sky-300 tabular-nums">
                    {importStatus.processed}
                    {importStatus.total > 0 ? ` / ${importStatus.total}` : ''} files
                  </span>
                </div>
                <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden mt-2">
                  <div
                    className={`h-full bg-sky-400 rounded-full transition-all duration-300 ${
                      importStatus.total === 0 ? 'w-1/2 animate-pulse' : ''
                    }`}
                    style={
                      importStatus.total > 0
                        ? {
                            width: `${Math.min(100, Math.round((importStatus.processed / importStatus.total) * 100))}%`,
                          }
                        : undefined
                    }
                  />
                </div>
              </div>
            )}

            {activeTasks.map((task) => {
              const pct =
                task.total > 0 ? Math.min(100, Math.round((task.current / task.total) * 100)) : 0;
              return (
                <Link
                  key={task.id}
                  to={task.entity_id > 0 ? `/artist/${task.entity_id}` : '/'}
                  className="block rounded-2xl bg-sky-950/30 border border-sky-500/25 hover:border-sky-500/45 p-3.5 transition-colors"
                >
                  <div className="flex items-center justify-between gap-2 mb-1">
                    <span className="text-xs font-semibold text-sky-200 truncate">
                      {task.title}
                    </span>
                    {task.total > 0 && (
                      <span className="text-[11px] font-mono text-sky-300 tabular-nums shrink-0">
                        {task.current}/{task.total}
                      </span>
                    )}
                  </div>
                  <p className="text-[11px] text-zinc-400 truncate">{task.detail}</p>
                  <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden mt-2">
                    <div
                      className={`h-full bg-sky-400 rounded-full transition-all duration-300 ${
                        task.total === 0 ? 'w-1/2 animate-pulse' : ''
                      }`}
                      style={task.total > 0 ? { width: `${pct}%` } : undefined}
                    />
                  </div>
                </Link>
              );
            })}
          </div>
        </div>
      )}

      {tab === 'queue' && (
        <>
          {/* Pipeline Stage Summary */}
          <div className="rounded-2xl bg-zinc-900/60 border border-white/[0.06] p-3.5 space-y-2.5">
            <div className="flex items-center justify-between gap-2">
              <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
                Download Pipeline
              </p>
              {stageFilter !== 'all' && (
                <button
                  type="button"
                  onClick={() => setStageFilter('all')}
                  className="text-[11px] text-amber-400 hover:text-amber-300 font-medium"
                >
                  Show all stages
                </button>
              )}
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-7 gap-2">
              <PipelineCard
                label="Queued"
                count={pending.length}
                active={stageFilter === 'pending'}
                onClick={() => setStageFilter(stageFilter === 'pending' ? 'all' : 'pending')}
                tone="amber"
              />
              <PipelineCard
                label="Searching"
                count={searching.length}
                active={stageFilter === 'active'}
                onClick={() => setStageFilter(stageFilter === 'active' ? 'all' : 'active')}
                tone="sky"
              />
              <PipelineCard
                label="Downloading"
                count={downloading.length}
                active={stageFilter === 'active'}
                onClick={() => setStageFilter(stageFilter === 'active' ? 'all' : 'active')}
                tone="sky"
              />
              <PipelineCard
                label="Organizing"
                count={organizing.length}
                active={stageFilter === 'active'}
                onClick={() => setStageFilter(stageFilter === 'active' ? 'all' : 'active')}
                tone="indigo"
              />
              <PipelineCard
                label="Paused"
                count={paused.length}
                active={stageFilter === 'paused'}
                onClick={() => setStageFilter(stageFilter === 'paused' ? 'all' : 'paused')}
                tone="amber"
              />
              <PipelineCard
                label="Failed"
                count={failed.length}
                active={stageFilter === 'failed'}
                onClick={() => setStageFilter(stageFilter === 'failed' ? 'all' : 'failed')}
                tone="rose"
              />
              <PipelineCard
                label="Complete"
                count={complete.length}
                active={stageFilter === 'complete'}
                onClick={() => setStageFilter(stageFilter === 'complete' ? 'all' : 'complete')}
                tone="emerald"
              />
            </div>

            <p className="text-[11px] text-zinc-500 leading-relaxed">
              Pipeline runs every 10s: <span className="text-zinc-400">Arcod lossless check</span> →{' '}
              <span className="text-zinc-400">Soulseek peer search</span> →{' '}
              <span className="text-zinc-400">Transfer</span> →{' '}
              <span className="text-zinc-400">Non-destructive tagging & library move</span> →{' '}
              <span className="text-zinc-400">Player rescan</span>.
            </p>
          </div>

          {isLoading && (
            <div className="space-y-2">
              {[...Array(3)].map((_, i) => (
                <div
                  key={i}
                  className="bg-zinc-900/60 border border-white/[0.05] rounded-xl h-16 animate-pulse"
                />
              ))}
            </div>
          )}

          {!isLoading && !downloads?.length && !unmatchedProgress.length && (
            <div className="rounded-2xl bg-zinc-900/40 border border-white/[0.05] text-center py-14 px-4">
              <div className="w-12 h-12 mx-auto mb-3 rounded-2xl bg-zinc-900 border border-white/[0.07] flex items-center justify-center text-zinc-500">
                <IconDownload className="w-6 h-6" />
              </div>
              <p className="text-zinc-200 font-medium text-sm">Download queue is clear</p>
              <p className="text-zinc-500 text-xs mt-1.5 max-w-md mx-auto">
                Click &ldquo;Queue All Wanted&rdquo; above to search for any wanted tracks in your watchlist, or check the Activity Log tab to see past operations.
              </p>
            </div>
          )}

          {unmatchedProgress.length > 0 && showSection('active') && (
            <Section title={`Live Transfers (${unmatchedProgress.length})`}>
              {unmatchedProgress.map((p, i) => (
                <div
                  key={`${p.username}-${i}`}
                  className="bg-zinc-900/70 border border-sky-500/20 rounded-xl p-3 space-y-2"
                >
                  <div className="flex items-center justify-between gap-2">
                    <p className="text-sm font-medium text-zinc-100 truncate">{p.filename}</p>
                    <span className="text-xs font-mono text-sky-300 shrink-0 tabular-nums">
                      {p.percent_complete.toFixed(0)}%
                    </span>
                  </div>
                  <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden">
                    <div
                      className="h-full bg-sky-400 rounded-full transition-all duration-500"
                      style={{ width: `${p.percent_complete}%` }}
                    />
                  </div>
                  <div className="flex items-center justify-between text-[11px] text-zinc-400">
                    <span>
                      {p.username} · {p.state}
                    </span>
                    <span className="tabular-nums">
                      {p.average_speed_bps > 0 ? formatSpeed(p.average_speed_bps) : ''}
                      {p.size > 0
                        ? ` · ${formatFileSize(p.bytes_transferred)} / ${formatFileSize(p.size)}`
                        : ''}
                    </span>
                  </div>
                </div>
              ))}
            </Section>
          )}

          {active.length > 0 && showSection('active') && (
            <Section title={`Active (${active.length})`}>
              {active.map((dl) => (
                <DownloadRow
                  key={dl.id}
                  dl={dl}
                  liveProgress={
                    progressByDownloadId.get(dl.id) ?? progressByTrackId.get(dl.track_id)
                  }
                  onRetry={retry.mutate}
                  onDismiss={dismiss.mutate}
                  onPause={pauseSingle.mutate}
                  onResume={resumeSingle.mutate}
                  onSelect={setSelected}
                />
              ))}
            </Section>
          )}

          {paused.length > 0 && showSection('paused') && (
            <Section
              title={`Paused (${paused.length})`}
              onClear={() => clearByStatus.mutate('paused')}
              clearLabel="Clear All"
            >
              {paused.map((dl) => (
                <DownloadRow
                  key={dl.id}
                  dl={dl}
                  onRetry={retry.mutate}
                  onDismiss={dismiss.mutate}
                  onPause={pauseSingle.mutate}
                  onResume={resumeSingle.mutate}
                  onSelect={setSelected}
                />
              ))}
            </Section>
          )}

          {pending.length > 0 && showSection('pending') && (
            <Section
              title={`Queued & Waiting (${pending.length})`}
              onClear={() => clearByStatus.mutate('pending')}
              clearLabel="Clear All"
            >
              {pending.map((dl) => (
                <DownloadRow
                  key={dl.id}
                  dl={dl}
                  onRetry={retry.mutate}
                  onDismiss={dismiss.mutate}
                  onPause={pauseSingle.mutate}
                  onResume={resumeSingle.mutate}
                  onSelect={setSelected}
                />
              ))}
            </Section>
          )}

          {failed.length > 0 && showSection('failed') && (
            <Section
              title={`Failed (${failed.length})`}
              onClear={() => clearByStatus.mutate('failed')}
              clearLabel="Clear All"
            >
              {failed.map((dl) => (
                <DownloadRow
                  key={dl.id}
                  dl={dl}
                  onRetry={retry.mutate}
                  onDismiss={dismiss.mutate}
                  onPause={pauseSingle.mutate}
                  onResume={resumeSingle.mutate}
                  onSelect={setSelected}
                />
              ))}
            </Section>
          )}

          {complete.length > 0 && showSection('complete') && (
            <Section
              title={`Recently Completed (${complete.length})`}
              onClear={() => clearByStatus.mutate('complete')}
              clearLabel="Clear All"
            >
              {complete.map((dl) => (
                <DownloadRow
                  key={dl.id}
                  dl={dl}
                  onRetry={retry.mutate}
                  onDismiss={dismiss.mutate}
                  onPause={pauseSingle.mutate}
                  onResume={resumeSingle.mutate}
                  onSelect={setSelected}
                />
              ))}
            </Section>
          )}
        </>
      )}

      {tab === 'activity' && (
        <div className="space-y-3">
          <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar pb-0.5">
            {(
              [
                { id: 'all', label: 'All Events' },
                { id: 'downloads', label: 'Downloads' },
                { id: 'errors', label: 'Errors & Retries' },
                { id: 'library', label: 'Library & Sync' },
              ] as const
            ).map((f) => (
              <button
                key={f.id}
                type="button"
                onClick={() => setActivityFilter(f.id)}
                className={`px-3 py-1.5 rounded-full text-xs font-medium border whitespace-nowrap transition-colors shrink-0 ${
                  activityFilter === f.id
                    ? 'bg-zinc-100 text-zinc-950 border-zinc-100'
                    : 'bg-zinc-900/70 text-zinc-400 border-white/[0.07] hover:text-zinc-200'
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>

          <ActivityList
            activity={filteredActivity}
            total={activityTotal}
            offset={activityOffset}
            loadingMore={loadingMoreActivity}
            onLoadMore={loadMoreActivity}
          />
        </div>
      )}

      <DetailSheet
        open={!!selected}
        onClose={() => setSelected(null)}
        title={selected?.track?.title ?? `Download #${selected?.id}`}
        subtitle={
          selected?.track?.artist_name
            ? `${selected.track.artist_name}${selected.track.album_title ? ` · ${selected.track.album_title}` : ''}`
            : undefined
        }
      >
        {selected && (
          <div className="space-y-1">
            <DetailRow label="Stage">
              <StatusBadge status={selected.status} />
            </DetailRow>
            <DetailRow label="Process">
              {selected.status === 'pending' && selected.next_retry_at
                ? `Waiting for automatic retry (${formatCountdown(selected.next_retry_at)})`
                : selected.status === 'pending'
                  ? 'Waiting in queue for next available download slot'
                  : selected.status === 'searching'
                    ? 'Checking Arcod & querying Soulseek peers'
                    : selected.status === 'downloading'
                      ? 'Transferring audio file'
                      : selected.status === 'organizing'
                        ? 'Tagging metadata & organizing into library folder'
                        : selected.status === 'complete'
                          ? 'Saved to library and player scan triggered'
                          : 'Stopped after error'}
            </DetailRow>
            <DetailRow label="Attempts">
              {selected.attempts} / 4
              {selected.attempts < 4 && selected.status !== 'complete'
                ? ` (${4 - selected.attempts} left)`
                : ''}
            </DetailRow>
            <DetailRow label="Source">{selected.source || 'manual'}</DetailRow>
            <DetailRow label="Queued">{new Date(selected.created_at).toLocaleString()}</DetailRow>
            {selected.last_attempt && (
              <DetailRow label="Last Attempt">
                {new Date(selected.last_attempt).toLocaleString()}
              </DetailRow>
            )}
            {selected.next_retry_at && (
              <DetailRow label="Next Retry">
                {new Date(selected.next_retry_at).toLocaleString()} (
                {formatCountdown(selected.next_retry_at)})
              </DetailRow>
            )}
            {selected.error && (
              <div className="mt-3 p-3 bg-rose-950/35 border border-rose-500/25 rounded-xl">
                <p className="text-[11px] font-semibold text-rose-400 uppercase tracking-wider mb-1">
                  Error Details
                </p>
                <p className="text-sm text-rose-200 break-words leading-relaxed">
                  {selected.error}
                </p>
              </div>
            )}

            <div className="flex flex-wrap gap-2 pt-4 mt-2 border-t border-white/[0.06]">
              {selected.track?.album_id && (
                <Link
                  to={`/album/${selected.track.album_id}`}
                  onClick={() => setSelected(null)}
                  className="px-3.5 py-2 rounded-xl bg-zinc-800 hover:bg-zinc-700 text-xs font-medium text-zinc-200 transition-colors"
                >
                  Open Album (Manual Search)
                </Link>
              )}
              {selected.status === 'failed' && (
                <button
                  type="button"
                  onClick={() => {
                    retry.mutate(selected.id);
                    setSelected(null);
                  }}
                  className="px-3.5 py-2 rounded-xl bg-amber-400 hover:bg-amber-300 text-zinc-950 text-xs font-semibold transition-colors"
                >
                  Retry Now
                </button>
              )}
              {(selected.status === 'pending' || selected.status === 'searching' || selected.status === 'downloading') && (
                <button
                  type="button"
                  onClick={() => {
                    pauseSingle.mutate(selected.id);
                    setSelected(null);
                  }}
                  className="px-3.5 py-2 rounded-xl bg-zinc-800 hover:bg-zinc-700 text-amber-300 text-xs font-semibold transition-colors inline-flex items-center gap-1.5"
                >
                  <IconPause className="w-3.5 h-3.5" />
                  <span>Pause Download</span>
                </button>
              )}
              {selected.status === 'paused' && (
                <button
                  type="button"
                  onClick={() => {
                    resumeSingle.mutate(selected.id);
                    setSelected(null);
                  }}
                  className="px-3.5 py-2 rounded-xl bg-emerald-500 hover:bg-emerald-400 text-zinc-950 text-xs font-semibold transition-colors inline-flex items-center gap-1.5"
                >
                  <IconPlay className="w-3.5 h-3.5 fill-current" />
                  <span>Resume Download</span>
                </button>
              )}
            </div>
          </div>
        )}
      </DetailSheet>
    </div>
  );
}

function PipelineCard({
  label,
  count,
  active,
  onClick,
  tone,
}: {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
  tone: 'amber' | 'sky' | 'indigo' | 'rose' | 'emerald';
}) {
  const ringMap = {
    amber: active ? 'border-amber-400 bg-amber-500/15' : 'border-white/[0.06] bg-zinc-950/50',
    sky: active ? 'border-sky-400 bg-sky-500/15' : 'border-white/[0.06] bg-zinc-950/50',
    indigo: active ? 'border-indigo-400 bg-indigo-500/15' : 'border-white/[0.06] bg-zinc-950/50',
    rose: active ? 'border-rose-400 bg-rose-500/15' : 'border-white/[0.06] bg-zinc-950/50',
    emerald: active ? 'border-emerald-400 bg-emerald-500/15' : 'border-white/[0.06] bg-zinc-950/50',
  };
  const countColor = {
    amber: count > 0 ? 'text-amber-300' : 'text-zinc-500',
    sky: count > 0 ? 'text-sky-300' : 'text-zinc-500',
    indigo: count > 0 ? 'text-indigo-300' : 'text-zinc-500',
    rose: count > 0 ? 'text-rose-300' : 'text-zinc-500',
    emerald: count > 0 ? 'text-emerald-300' : 'text-zinc-500',
  };

  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-xl border px-2.5 py-2 text-left transition-all hover:border-white/[0.16] ${ringMap[tone]}`}
    >
      <p className={`text-lg font-bold tabular-nums leading-none ${countColor[tone]}`}>{count}</p>
      <p className="text-[11px] text-zinc-400 mt-1 truncate">{label}</p>
    </button>
  );
}

function ActivityList({
  activity,
  total,
  offset,
  loadingMore,
  onLoadMore,
}: {
  activity: ActivityLog[];
  total: number;
  offset: number;
  loadingMore: boolean;
  onLoadMore: () => void;
}) {
  if (!activity.length) {
    return (
      <div className="rounded-2xl bg-zinc-900/40 border border-white/[0.05] text-center py-14 px-4">
        <div className="w-12 h-12 mx-auto mb-3 rounded-2xl bg-zinc-900 border border-white/[0.07] flex items-center justify-center text-zinc-500">
          <IconActivity className="w-6 h-6" />
        </div>
        <p className="text-zinc-300 font-medium text-sm">No matching events yet</p>
        <p className="text-zinc-500 text-xs mt-1">
          Crate logs searches, downloads, retries, new releases, library imports, and rejections here.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      {activity.map((a) => {
        const meta = getActivityMeta(a.action);
        const target =
          a.entity_type === 'artist' && a.entity_id > 0
            ? `/artist/${a.entity_id}`
            : a.entity_type === 'album' && a.entity_id > 0
              ? `/album/${a.entity_id}`
              : null;

        const row = (
          <div className="flex items-start gap-3 bg-zinc-900/60 hover:bg-zinc-900 border border-white/[0.05] rounded-xl px-3.5 py-3 transition-colors">
            <div className={`w-2 h-2 rounded-full mt-1.5 shrink-0 ${meta.dotClass}`} />
            <div className="flex-1 min-w-0">
              <div className="flex flex-wrap items-center justify-between gap-2 mb-1">
                <span
                  className={`text-[10px] font-semibold uppercase tracking-wider px-1.5 py-px rounded border ${meta.badgeClass}`}
                >
                  {meta.label}
                </span>
                <span
                  className="text-[11px] text-zinc-500 tabular-nums"
                  title={new Date(a.created_at).toLocaleString()}
                >
                  {formatRelativeTime(a.created_at)} · {new Date(a.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                </span>
              </div>
              <p className="text-sm text-zinc-200 break-words leading-snug">{a.details}</p>
            </div>
            {target && <IconChevronRight className="w-4 h-4 text-zinc-600 shrink-0 mt-1" />}
          </div>
        );

        return target ? (
          <Link key={a.id} to={target} className="block">
            {row}
          </Link>
        ) : (
          <div key={a.id}>{row}</div>
        );
      })}

      {offset < total && (
        <button
          type="button"
          onClick={onLoadMore}
          disabled={loadingMore}
          className="w-full py-3 text-sm font-medium text-zinc-300 bg-zinc-900/70 hover:bg-zinc-900 border border-white/[0.07] rounded-xl transition-colors disabled:opacity-50 mt-2"
        >
          {loadingMore
            ? 'Loading…'
            : `Load More (${activity.length} of ${total.toLocaleString()})`}
        </button>
      )}
    </div>
  );
}

function Section({
  title,
  children,
  onClear,
  clearLabel,
}: {
  title: string;
  children: React.ReactNode;
  onClear?: () => void;
  clearLabel?: string;
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">{title}</p>
        {onClear && (
          <button
            type="button"
            onClick={onClear}
            className="text-xs text-zinc-500 hover:text-rose-400 transition-colors font-medium"
          >
            {clearLabel || 'Clear'}
          </button>
        )}
      </div>
      <div className="space-y-1.5">{children}</div>
    </div>
  );
}

function DownloadRow({
  dl,
  liveProgress,
  onRetry,
  onDismiss,
  onPause,
  onResume,
  onSelect,
}: {
  dl: DownloadQueueItem;
  liveProgress?: DownloadProgress;
  onRetry: (id: number) => void;
  onDismiss: (id: number) => void;
  onPause?: (id: number) => void;
  onResume?: (id: number) => void;
  onSelect: (dl: DownloadQueueItem) => void;
}) {
  const trackTitle = dl.track?.title ?? `Track #${dl.track_id}`;
  const artistName = dl.track?.artist_name;
  const albumTitle = dl.track?.album_title;
  const retryText = dl.next_retry_at ? formatCountdown(dl.next_retry_at) : '';

  return (
    <div
      className="bg-zinc-900/65 hover:bg-zinc-900 border border-white/[0.06] rounded-xl px-3.5 py-3 cursor-pointer transition-colors"
      onClick={() => onSelect(dl)}
    >
      <div className="flex items-center gap-3">
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium text-zinc-100 truncate">{trackTitle}</p>
          <p className="text-xs text-zinc-400 truncate mt-0.5">
            {artistName}
            {albumTitle && ` · ${albumTitle}`}
          </p>
          {dl.status === 'searching' && (
            <p className="text-[11px] text-sky-300/90 mt-1">
              Searching Arcod & Soulseek peers…
            </p>
          )}
          {dl.status === 'organizing' && (
            <p className="text-[11px] text-indigo-300/90 mt-1">
              Tagging audio & moving into library folder…
            </p>
          )}
          {dl.status === 'pending' && dl.next_retry_at && (
            <p className="text-[11px] text-amber-300/90 mt-1 truncate">
              Retry {dl.attempts}/4 {retryText}
              {dl.error ? ` · ${dl.error}` : ''}
            </p>
          )}
          {dl.status === 'paused' && (
            <p className="text-[11px] text-amber-300/90 mt-1">
              Download paused
            </p>
          )}
          {dl.status === 'failed' && dl.error && (
            <p className="text-[11px] text-rose-300/90 mt-1 truncate">{dl.error}</p>
          )}
        </div>

        <StatusBadge status={dl.status} />

        {(dl.status === 'pending' || dl.status === 'searching' || dl.status === 'downloading') && onPause && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onPause(dl.id);
            }}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] rounded-lg text-xs font-medium text-amber-300 transition-colors shrink-0"
            title="Pause download"
          >
            <IconPause className="w-3.5 h-3.5" />
            <span className="hidden sm:inline">Pause</span>
          </button>
        )}

        {dl.status === 'paused' && onResume && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onResume(dl.id);
            }}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 bg-emerald-500/15 hover:bg-emerald-500/25 border border-emerald-500/30 rounded-lg text-xs font-medium text-emerald-300 transition-colors shrink-0"
            title="Resume download"
          >
            <IconPlay className="w-3.5 h-3.5 fill-current" />
            <span>Resume</span>
          </button>
        )}

        {dl.status === 'failed' && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onRetry(dl.id);
            }}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] rounded-lg text-xs font-medium text-zinc-200 transition-colors shrink-0"
          >
            <IconRefresh className="w-3.5 h-3.5" />
            Retry
          </button>
        )}

        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onDismiss(dl.id);
          }}
          className="p-1.5 rounded-lg text-zinc-500 hover:text-rose-400 hover:bg-zinc-800 transition-colors shrink-0"
          title={
            dl.status === 'downloading' || dl.status === 'searching' ? 'Cancel' : 'Dismiss'
          }
          aria-label="Remove download"
        >
          <IconClose className="w-3.5 h-3.5" />
        </button>
      </div>

      {/* Inline Live Transfer Progress */}
      {liveProgress && (
        <div className="mt-2.5 pt-2.5 border-t border-white/[0.05] space-y-1.5">
          <div className="flex items-center justify-between text-[11px] text-zinc-400 gap-2">
            <span className="truncate">
              {liveProgress.username} · {liveProgress.state}
            </span>
            <span className="font-mono text-sky-300 shrink-0 tabular-nums">
              {liveProgress.percent_complete.toFixed(0)}%
              {liveProgress.average_speed_bps > 0
                ? ` · ${formatSpeed(liveProgress.average_speed_bps)}`
                : ''}
            </span>
          </div>
          <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden">
            <div
              className="h-full bg-sky-400 rounded-full transition-all duration-500"
              style={{ width: `${liveProgress.percent_complete}%` }}
            />
          </div>
        </div>
      )}
    </div>
  );
}
