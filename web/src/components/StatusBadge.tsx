const STATUS_STYLES: Record<string, { classes: string; label?: string; dot?: string }> = {
  owned: {
    classes: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/25',
  },
  complete: {
    classes: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/25',
  },
  wanted: {
    classes: 'bg-amber-500/15 text-amber-300 border-amber-500/25',
  },
  pending: {
    classes: 'bg-zinc-700/50 text-zinc-300 border-zinc-600/40',
    label: 'Queued',
  },
  searching: {
    classes: 'bg-sky-500/15 text-sky-300 border-sky-500/25',
    dot: 'bg-sky-400 animate-pulse',
  },
  downloading: {
    classes: 'bg-sky-500/15 text-sky-300 border-sky-500/25',
    dot: 'bg-sky-400 animate-pulse',
  },
  organizing: {
    classes: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/25',
    dot: 'bg-indigo-400 animate-pulse',
  },
  failed: {
    classes: 'bg-rose-500/15 text-rose-300 border-rose-500/25',
  },
  ignored: {
    classes: 'bg-zinc-800/80 text-zinc-400 border-zinc-700/60',
  },
  local: {
    classes: 'bg-amber-500/15 text-amber-300 border-amber-500/25',
  },
  'not linked': {
    classes: 'bg-amber-500/15 text-amber-300 border-amber-500/25',
  },
  unmatched: {
    classes: 'bg-amber-500/15 text-amber-300 border-amber-500/25',
  },
  orphaned: {
    classes: 'bg-rose-500/15 text-rose-300 border-rose-500/25',
  },
  watched: {
    classes: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/25',
  },
  partial: {
    classes: 'bg-zinc-800 text-zinc-300 border-zinc-700',
  },
};

export default function StatusBadge({
  status,
  label,
}: {
  status: string;
  label?: string;
}) {
  const key = status.toLowerCase();
  const cfg = STATUS_STYLES[key] ?? {
    classes: 'bg-zinc-800 text-zinc-300 border-zinc-700',
  };
  const displayLabel = label ?? cfg.label ?? status;

  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md border text-[10px] font-semibold uppercase tracking-wider shrink-0 ${cfg.classes}`}
    >
      {cfg.dot && <span className={`w-1.5 h-1.5 rounded-full ${cfg.dot}`} />}
      {displayLabel}
    </span>
  );
}

