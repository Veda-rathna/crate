export function formatDuration(ms: number): string {
  const mins = Math.floor(ms / 60000);
  const secs = Math.floor((ms % 60000) / 1000);
  return `${mins}:${secs.toString().padStart(2, '0')}`;
}

export function formatFans(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return n.toString();
}

export function formatTotalDuration(ms: number): string {
  const hours = Math.floor(ms / 3600000);
  const mins = Math.floor((ms % 3600000) / 60000);
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

export function formatFileSize(bytes: number): string {
  if (bytes >= 1_000_000_000) return `${(bytes / 1_000_000_000).toFixed(1)} GB`;
  if (bytes >= 1_000_000) return `${(bytes / 1_000_000).toFixed(1)} MB`;
  if (bytes >= 1_000) return `${(bytes / 1_000).toFixed(0)} KB`;
  return `${bytes} B`;
}

export function formatSpeed(bps: number): string {
  if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} MB/s`;
  if (bps >= 1_000) return `${(bps / 1_000).toFixed(0)} KB/s`;
  return `${bps.toFixed(0)} B/s`;
}

export function formatRelativeTime(iso: string): string {
  const ts = new Date(iso).getTime();
  if (Number.isNaN(ts)) return iso;
  const diffSec = Math.max(0, Math.floor((Date.now() - ts) / 1000));
  if (diffSec < 10) return 'just now';
  if (diffSec < 60) return `${diffSec}s ago`;
  const mins = Math.floor(diffSec / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  const days = Math.floor(hrs / 24);
  if (days < 7) return `${days}d ago`;
  return new Date(ts).toLocaleDateString();
}

export function formatCountdown(iso: string): string {
  const ts = new Date(iso).getTime();
  if (Number.isNaN(ts)) return '';
  const diffSec = Math.floor((ts - Date.now()) / 1000);
  if (diffSec <= 0) return 'due now';
  if (diffSec < 60) return `in ${diffSec}s`;
  const mins = Math.floor(diffSec / 60);
  if (mins < 60) return `in ${mins}m`;
  const hrs = Math.floor(mins / 60);
  const remMins = mins % 60;
  return remMins > 0 ? `in ${hrs}h ${remMins}m` : `in ${hrs}h`;
}

export type ActivityCategory = 'downloads' | 'errors' | 'library';

export interface ActivityActionMeta {
  label: string;
  category: ActivityCategory;
  dotClass: string;
  badgeClass: string;
}

const ACTION_META_MAP: Record<string, ActivityActionMeta> = {
  search_started: {
    label: 'Searching',
    category: 'downloads',
    dotClass: 'bg-sky-400',
    badgeClass: 'bg-sky-500/15 text-sky-300 border-sky-500/20',
  },
  download_started: {
    label: 'Downloading',
    category: 'downloads',
    dotClass: 'bg-sky-400',
    badgeClass: 'bg-sky-500/15 text-sky-300 border-sky-500/20',
  },
  download_complete: {
    label: 'Completed',
    category: 'downloads',
    dotClass: 'bg-emerald-400',
    badgeClass: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/20',
  },
  download_failed: {
    label: 'Failed',
    category: 'errors',
    dotClass: 'bg-rose-400',
    badgeClass: 'bg-rose-500/15 text-rose-300 border-rose-500/20',
  },
  retry_scheduled: {
    label: 'Retry Scheduled',
    category: 'errors',
    dotClass: 'bg-amber-400',
    badgeClass: 'bg-amber-500/15 text-amber-300 border-amber-500/20',
  },
  user_cooldown: {
    label: 'Peer Cooldown',
    category: 'errors',
    dotClass: 'bg-amber-400',
    badgeClass: 'bg-amber-500/15 text-amber-300 border-amber-500/20',
  },
  track_rejected: {
    label: 'Rejected',
    category: 'errors',
    dotClass: 'bg-orange-400',
    badgeClass: 'bg-orange-500/15 text-orange-300 border-orange-500/20',
  },
  file_missing: {
    label: 'File Missing',
    category: 'errors',
    dotClass: 'bg-amber-400',
    badgeClass: 'bg-amber-500/15 text-amber-300 border-amber-500/20',
  },
  artist_watched: {
    label: 'Artist Watched',
    category: 'library',
    dotClass: 'bg-indigo-400',
    badgeClass: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/20',
  },
  album_watched: {
    label: 'Album Watched',
    category: 'library',
    dotClass: 'bg-indigo-400',
    badgeClass: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/20',
  },
  discography_synced: {
    label: 'Synced',
    category: 'library',
    dotClass: 'bg-emerald-400',
    badgeClass: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/20',
  },
  artist_reconciled: {
    label: 'Library Linked',
    category: 'library',
    dotClass: 'bg-emerald-400',
    badgeClass: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/20',
  },
  new_releases: {
    label: 'New Releases',
    category: 'library',
    dotClass: 'bg-amber-400',
    badgeClass: 'bg-amber-500/15 text-amber-300 border-amber-500/20',
  },
  auto_queued: {
    label: 'Auto-Queued',
    category: 'downloads',
    dotClass: 'bg-sky-400',
    badgeClass: 'bg-sky-500/15 text-sky-300 border-sky-500/20',
  },
  upgrade_queued: {
    label: 'Upgrade Queued',
    category: 'downloads',
    dotClass: 'bg-indigo-400',
    badgeClass: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/20',
  },
  library_import: {
    label: 'Library Import',
    category: 'library',
    dotClass: 'bg-emerald-400',
    badgeClass: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/20',
  },
};

export function getActivityMeta(action: string): ActivityActionMeta {
  return (
    ACTION_META_MAP[action] ?? {
      label: action.replace(/_/g, ' '),
      category: 'library',
      dotClass: 'bg-zinc-400',
      badgeClass: 'bg-zinc-800 text-zinc-300 border-zinc-700',
    }
  );
}
