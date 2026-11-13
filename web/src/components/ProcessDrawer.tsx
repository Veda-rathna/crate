import { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useSystemProcess } from '../context/SystemContext';
import { formatRelativeTime, getActivityMeta } from '../lib/format';
import { IconActivity, IconChevronRight, IconClose } from './Icons';

export default function ProcessDrawer() {
  const {
    processDrawerOpen,
    closeProcessDrawer,
    status,
    providers,
    recentActivity,
    activeTasks,
    importStatus,
    hasRunningWork,
  } = useSystemProcess();

  useEffect(() => {
    if (!processDrawerOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeProcessDrawer();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [processDrawerOpen, closeProcessDrawer]);

  if (!processDrawerOpen) return null;

  const isImporting = importStatus?.status === 'running';
  const importPct =
    importStatus && importStatus.total > 0
      ? Math.min(100, Math.round((importStatus.processed / importStatus.total) * 100))
      : 0;

  return (
    <div
      className="fixed inset-0 z-[90] flex items-end md:items-stretch md:justify-end"
      role="dialog"
      aria-modal="true"
      aria-label="Live System Activity"
      onClick={closeProcessDrawer}
    >
      <div className="absolute inset-0 bg-black/65 backdrop-blur-xs" />

      <div
        className="relative w-full md:w-[420px] max-h-[85vh] md:max-h-full bg-zinc-950 border-t md:border-t-0 md:border-l border-white/[0.08] rounded-t-2xl md:rounded-none shadow-2xl flex flex-col animate-slide-up md:animate-slide-in-right pb-[env(safe-area-inset-bottom,0px)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="md:hidden flex justify-center pt-2.5 pb-1">
          <div className="w-10 h-1 rounded-full bg-zinc-700" />
        </div>

        <div className="flex items-center justify-between px-5 py-4 border-b border-white/[0.07]">
          <div className="flex items-center gap-2.5">
            <div
              className={`w-8 h-8 rounded-lg flex items-center justify-center ${
                hasRunningWork
                  ? 'bg-sky-500/15 text-sky-400 border border-sky-500/25'
                  : 'bg-zinc-800/80 text-zinc-400 border border-white/[0.07]'
              }`}
            >
              <IconActivity className={`w-4 h-4 ${hasRunningWork ? 'animate-pulse' : ''}`} />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-zinc-100">System Processes</h2>
              <p className="text-[11px] text-zinc-500">
                {hasRunningWork ? 'Active background work in progress' : 'Live monitor & recent events'}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={closeProcessDrawer}
            className="p-1.5 rounded-lg text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors"
            aria-label="Close process monitor"
          >
            <IconClose className="w-4 h-4" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">
          {/* Active background tasks & imports */}
          {(activeTasks.length > 0 || isImporting) && (
            <section>
              <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider mb-2">
                Running Now
              </p>
              <div className="space-y-2">
                {isImporting && importStatus && (
                  <Link
                    to="/settings"
                    onClick={closeProcessDrawer}
                    className="block rounded-xl bg-sky-950/30 border border-sky-500/25 p-3 hover:border-sky-500/40 transition-colors"
                  >
                    <div className="flex items-center justify-between gap-2 mb-1.5">
                      <span className="text-xs font-semibold text-sky-300">
                        {importStatus.dry_run ? 'Scanning Library (Dry Run)' : 'Importing Library'}
                      </span>
                      <span className="text-[11px] font-mono text-sky-300/80 tabular-nums">
                        {importStatus.processed}
                        {importStatus.total > 0 ? ` / ${importStatus.total}` : ''}
                      </span>
                    </div>
                    <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden">
                      <div
                        className={`h-full bg-sky-400 rounded-full transition-all duration-300 ${
                          importStatus.total === 0 ? 'w-1/2 animate-pulse' : ''
                        }`}
                        style={importStatus.total > 0 ? { width: `${importPct}%` } : undefined}
                      />
                    </div>
                  </Link>
                )}

                {activeTasks.map((task) => {
                  const pct =
                    task.total > 0 ? Math.min(100, Math.round((task.current / task.total) * 100)) : 0;
                  return (
                    <Link
                      key={task.id}
                      to={task.entity_id > 0 ? `/artist/${task.entity_id}` : '/'}
                      onClick={closeProcessDrawer}
                      className="block rounded-xl bg-sky-950/30 border border-sky-500/25 p-3 hover:border-sky-500/40 transition-colors"
                    >
                      <div className="flex items-center justify-between gap-2 mb-1">
                        <span className="text-xs font-semibold text-sky-300 truncate">
                          {task.title}
                        </span>
                        {task.total > 0 && (
                          <span className="text-[11px] font-mono text-sky-300/80 tabular-nums shrink-0">
                            {task.current}/{task.total}
                          </span>
                        )}
                      </div>
                      <p className="text-[11px] text-zinc-400 truncate mb-2">{task.detail}</p>
                      <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden">
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
            </section>
          )}

          {/* Download pipeline summary */}
          <section>
            <div className="flex items-center justify-between mb-2">
              <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
                Download Pipeline
              </p>
              <Link
                to="/downloads"
                onClick={closeProcessDrawer}
                className="text-xs text-amber-400 hover:text-amber-300 inline-flex items-center gap-0.5 font-medium"
              >
                Open Queue <IconChevronRight className="w-3.5 h-3.5" />
              </Link>
            </div>
            {status?.downloads_paused && (
              <div className="mb-2.5 px-3 py-1.5 rounded-lg bg-amber-500/10 border border-amber-500/25 text-xs text-amber-300 flex items-center gap-2">
                <span className="w-2 h-2 rounded-full bg-amber-400 shrink-0" />
                <span className="font-medium">Downloads are paused globally</span>
              </div>
            )}
            <div className="grid grid-cols-5 gap-1.5">
              <StagePill label="Queued" count={status?.pending_downloads ?? 0} tone="zinc" />
              <StagePill label="Paused" count={status?.paused_downloads ?? 0} tone="amber" />
              <StagePill
                label="Searching"
                count={status?.searching_downloads ?? 0}
                tone="sky"
              />
              <StagePill
                label="Downloading"
                count={status?.downloading_downloads ?? 0}
                tone="sky"
              />
              <StagePill
                label="Organizing"
                count={status?.organizing_downloads ?? 0}
                tone="indigo"
              />
            </div>
            {(status?.failed_downloads ?? 0) > 0 && (
              <Link
                to="/downloads"
                onClick={closeProcessDrawer}
                className="mt-2 flex items-center justify-between px-3 py-2 rounded-lg bg-rose-500/10 border border-rose-500/20 text-xs text-rose-300 hover:bg-rose-500/15 transition-colors"
              >
                <span>{status?.failed_downloads} failed download(s) need attention</span>
                <IconChevronRight className="w-3.5 h-3.5" />
              </Link>
            )}
          </section>

          {/* Provider health */}
          {providers.length > 0 && (
            <section>
              <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider mb-2">
                Metadata Providers
              </p>
              <div className="flex flex-wrap gap-2">
                {providers.map((p) => (
                  <div
                    key={p.name}
                    className={`inline-flex items-center gap-2 px-2.5 py-1.5 rounded-lg border text-xs ${
                      p.healthy
                        ? 'bg-zinc-900 border-white/[0.07] text-zinc-200'
                        : 'bg-rose-950/30 border-rose-500/25 text-rose-300'
                    }`}
                  >
                    <span
                      className={`w-2 h-2 rounded-full ${
                        p.healthy ? 'bg-emerald-400' : 'bg-rose-400 animate-pulse'
                      }`}
                    />
                    <span className="font-medium">{p.display_name}</span>
                    <span className="text-[10px] text-zinc-500">
                      {p.healthy ? 'Online' : 'Offline'}
                    </span>
                  </div>
                ))}
              </div>
            </section>
          )}

          {/* Recent activity timeline */}
          <section>
            <div className="flex items-center justify-between mb-2">
              <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
                What Happened Recently
              </p>
              <Link
                to="/downloads?tab=activity"
                onClick={closeProcessDrawer}
                className="text-xs text-zinc-400 hover:text-zinc-200 inline-flex items-center gap-0.5"
              >
                Full Log <IconChevronRight className="w-3.5 h-3.5" />
              </Link>
            </div>
            {recentActivity.length === 0 ? (
              <div className="rounded-xl bg-zinc-900/60 border border-white/[0.05] p-4 text-center">
                <p className="text-xs text-zinc-500">No background events recorded yet.</p>
              </div>
            ) : (
              <div className="space-y-1.5">
                {recentActivity.map((item) => {
                  const meta = getActivityMeta(item.action);
                  const target =
                    item.entity_type === 'artist' && item.entity_id > 0
                      ? `/artist/${item.entity_id}`
                      : item.entity_type === 'album' && item.entity_id > 0
                        ? `/album/${item.entity_id}`
                        : null;
                  const inner = (
                    <div className="flex items-start gap-2.5 rounded-xl bg-zinc-900/60 border border-white/[0.05] hover:border-white/[0.1] px-3 py-2.5 transition-colors">
                      <span className={`w-2 h-2 rounded-full mt-1.5 shrink-0 ${meta.dotClass}`} />
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center justify-between gap-2 mb-0.5">
                          <span
                            className={`text-[10px] font-semibold uppercase tracking-wider px-1.5 py-px rounded border ${meta.badgeClass}`}
                          >
                            {meta.label}
                          </span>
                          <span className="text-[10px] text-zinc-500 shrink-0 tabular-nums">
                            {formatRelativeTime(item.created_at)}
                          </span>
                        </div>
                        <p className="text-xs text-zinc-200 leading-snug break-words">
                          {item.details}
                        </p>
                      </div>
                    </div>
                  );
                  return target ? (
                    <Link key={item.id} to={target} onClick={closeProcessDrawer} className="block">
                      {inner}
                    </Link>
                  ) : (
                    <div key={item.id}>{inner}</div>
                  );
                })}
              </div>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}

function StagePill({
  label,
  count,
  tone,
}: {
  label: string;
  count: number;
  tone: 'zinc' | 'sky' | 'indigo' | 'amber';
}) {
  const active = count > 0;
  const toneStyles = {
    zinc: active
      ? 'bg-amber-500/10 border-amber-500/25 text-amber-200'
      : 'bg-zinc-900/70 border-white/[0.05] text-zinc-500',
    amber: active
      ? 'bg-amber-500/15 border-amber-500/30 text-amber-300'
      : 'bg-zinc-900/70 border-white/[0.05] text-zinc-500',
    sky: active
      ? 'bg-sky-500/10 border-sky-500/25 text-sky-200'
      : 'bg-zinc-900/70 border-white/[0.05] text-zinc-500',
    indigo: active
      ? 'bg-indigo-500/10 border-indigo-500/25 text-indigo-200'
      : 'bg-zinc-900/70 border-white/[0.05] text-zinc-500',
  };

  return (
    <div className={`rounded-xl border px-2 py-2.5 text-center ${toneStyles[tone]}`}>
      <p className="text-base font-bold tabular-nums leading-none">{count}</p>
      <p className="text-[10px] mt-1 opacity-80 truncate">{label}</p>
    </div>
  );
}

