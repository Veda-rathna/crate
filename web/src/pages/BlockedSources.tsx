import { useMemo, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { formatCountdown, formatRelativeTime } from '../lib/format';
import { useToast } from '../components/Toast';
import FilterBar from '../components/FilterBar';
import ConfirmModal from '../components/ConfirmModal';
import { IconClose } from '../components/Icons';
import type { BlacklistEntry, UserCooldown } from '../types/index';

export default function BlockedSources() {
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [filter, setFilter] = useState('');
  const [confirmClear, setConfirmClear] = useState<'cooldowns' | 'blacklist' | null>(null);

  const { data: blacklist, isLoading: loadingBlacklist } = useQuery({
    queryKey: ['blacklist'],
    queryFn: api.listBlacklist,
  });

  const { data: cooldowns, isLoading: loadingCooldowns } = useQuery({
    queryKey: ['cooldowns'],
    queryFn: api.listCooldowns,
    refetchInterval: 30_000,
  });

  const removeBlacklist = useMutation({
    mutationFn: (id: number) => api.deleteBlacklistEntry(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['blacklist'] });
      toast('Removed file from blacklist', 'info');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const removeCooldown = useMutation({
    mutationFn: (id: number) => api.deleteCooldown(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cooldowns'] });
      toast('Removed user cooldown', 'info');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const clearAllBlacklist = useMutation({
    mutationFn: () => api.clearBlacklist(),
    onSuccess: () => {
      setConfirmClear(null);
      queryClient.invalidateQueries({ queryKey: ['blacklist'] });
      toast('Blacklist cleared', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const clearAllCooldowns = useMutation({
    mutationFn: () => api.clearCooldowns(),
    onSuccess: () => {
      setConfirmClear(null);
      queryClient.invalidateQueries({ queryKey: ['cooldowns'] });
      toast('Cooldowns cleared', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const filteredCooldowns = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return cooldowns ?? [];
    return (cooldowns ?? []).filter(
      (c) => c.username.toLowerCase().includes(q) || c.reason.toLowerCase().includes(q)
    );
  }, [cooldowns, filter]);

  const filteredBlacklist = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return blacklist ?? [];
    return (blacklist ?? []).filter(
      (b) =>
        b.username.toLowerCase().includes(q) ||
        b.filename.toLowerCase().includes(q) ||
        b.reason.toLowerCase().includes(q)
    );
  }, [blacklist, filter]);

  const hasAny = (cooldowns?.length ?? 0) > 0 || (blacklist?.length ?? 0) > 0;
  const isLoading = loadingBlacklist || loadingCooldowns;

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100">
          Blocked Sources
        </h2>
        <p className="text-xs text-zinc-400 mt-0.5">
          Temporarily shadow-banned peers and permanently blacklisted files skipped during automatic search.
        </p>
      </div>

      {hasAny && (
        <FilterBar
          value={filter}
          onChange={setFilter}
          placeholder="Filter by username, filename, or reason..."
          className="mb-0"
        />
      )}

      {isLoading && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 animate-pulse">
          <div className="h-40 bg-zinc-900/60 rounded-2xl" />
          <div className="h-40 bg-zinc-900/60 rounded-2xl" />
        </div>
      )}

      {!isLoading && !hasAny && (
        <div className="rounded-2xl bg-zinc-900/40 border border-white/[0.06] text-center py-14 px-4">
          <p className="text-zinc-200 font-medium text-sm">No blocked sources</p>
          <p className="text-zinc-500 text-xs mt-1.5 max-w-md mx-auto">
            Failed transfers and stalled peers are automatically tracked here so Crate stops retrying broken sources.
          </p>
        </div>
      )}

      {!isLoading && hasAny && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5 items-start">
          {/* Active Cooldowns */}
          <section className="rounded-2xl bg-zinc-900/60 border border-white/[0.06] p-4 space-y-3">
            <div className="flex items-center justify-between gap-2">
              <div>
                <h3 className="text-sm font-semibold text-zinc-100">
                  Active Cooldowns ({filteredCooldowns.length})
                </h3>
                <p className="text-[11px] text-zinc-500">
                  Temporary per-peer shadow bans from stalled or rejected starts
                </p>
              </div>
              {(cooldowns?.length ?? 0) > 0 && (
                <button
                  type="button"
                  onClick={() => setConfirmClear('cooldowns')}
                  disabled={clearAllCooldowns.isPending}
                  className="text-xs font-medium text-zinc-400 hover:text-rose-400 transition-colors shrink-0"
                >
                  Clear All
                </button>
              )}
            </div>

            {filteredCooldowns.length === 0 ? (
              <p className="text-xs text-zinc-500 py-4 text-center">No active cooldowns</p>
            ) : (
              <div className="space-y-2">
                {filteredCooldowns.map((c: UserCooldown) => (
                  <div
                    key={c.id}
                    className="flex items-start gap-3 bg-zinc-950/60 border border-white/[0.05] rounded-xl px-3.5 py-3"
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="text-sm font-medium text-zinc-100 truncate">{c.username}</p>
                        <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-300 border border-amber-500/25 shrink-0 tabular-nums">
                          expires {formatCountdown(c.expires_at)}
                        </span>
                      </div>
                      <p className="text-xs text-zinc-400 truncate mt-1">{c.reason}</p>
                      <p className="text-[11px] text-zinc-500 mt-0.5">
                        Blocked {formatRelativeTime(c.created_at)}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => removeCooldown.mutate(c.id)}
                      className="p-1.5 rounded-lg text-zinc-500 hover:text-rose-400 hover:bg-zinc-800 transition-colors shrink-0"
                      title="Unblock peer"
                      aria-label={`Remove cooldown for ${c.username}`}
                    >
                      <IconClose className="w-4 h-4" />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </section>

          {/* Blacklisted Files */}
          <section className="rounded-2xl bg-zinc-900/60 border border-white/[0.06] p-4 space-y-3">
            <div className="flex items-center justify-between gap-2">
              <div>
                <h3 className="text-sm font-semibold text-zinc-100">
                  Blacklisted Files ({filteredBlacklist.length})
                </h3>
                <p className="text-[11px] text-zinc-500">
                  Specific peer files permanently skipped after errors or rejections
                </p>
              </div>
              {(blacklist?.length ?? 0) > 0 && (
                <button
                  type="button"
                  onClick={() => setConfirmClear('blacklist')}
                  disabled={clearAllBlacklist.isPending}
                  className="text-xs font-medium text-zinc-400 hover:text-rose-400 transition-colors shrink-0"
                >
                  Clear All
                </button>
              )}
            </div>

            {filteredBlacklist.length === 0 ? (
              <p className="text-xs text-zinc-500 py-4 text-center">No blacklisted files</p>
            ) : (
              <div className="space-y-2 max-h-[600px] overflow-y-auto pr-0.5">
                {filteredBlacklist.map((b: BlacklistEntry) => (
                  <div
                    key={b.id}
                    className="flex items-start gap-3 bg-zinc-950/60 border border-white/[0.05] rounded-xl px-3.5 py-3"
                  >
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-zinc-100 truncate">{b.username}</p>
                      <p className="text-xs font-mono text-zinc-300 truncate mt-0.5">
                        {b.filename.split(/[/\\]/).pop()}
                      </p>
                      <p className="text-[11px] text-zinc-500 truncate mt-0.5">
                        {b.reason} · {formatRelativeTime(b.created_at)}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => removeBlacklist.mutate(b.id)}
                      className="p-1.5 rounded-lg text-zinc-500 hover:text-rose-400 hover:bg-zinc-800 transition-colors shrink-0"
                      title="Allow file again"
                      aria-label={`Unblacklist ${b.filename}`}
                    >
                      <IconClose className="w-4 h-4" />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>
      )}

      <ConfirmModal
        open={confirmClear !== null}
        title={
          confirmClear === 'cooldowns' ? 'Clear all peer cooldowns?' : 'Clear all blacklisted files?'
        }
        description={
          confirmClear === 'cooldowns'
            ? 'All temporarily shadow-banned users will be eligible for Soulseek downloads immediately.'
            : 'Previously failed or rejected files will become eligible for automatic download selection again.'
        }
        confirmLabel="Clear All"
        loading={clearAllCooldowns.isPending || clearAllBlacklist.isPending}
        onConfirm={() => {
          if (confirmClear === 'cooldowns') clearAllCooldowns.mutate();
          if (confirmClear === 'blacklist') clearAllBlacklist.mutate();
        }}
        onCancel={() => setConfirmClear(null)}
      />
    </div>
  );
}
