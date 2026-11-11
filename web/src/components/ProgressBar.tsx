export default function ProgressBar({
  owned,
  total,
  showPct = false,
}: {
  owned: number;
  total: number;
  showPct?: boolean;
}) {
  if (total === 0) return null;
  const pct = Math.min(100, Math.round((owned / total) * 100));

  return (
    <div className="flex items-center gap-2.5">
      <div className="flex-1 h-1.5 bg-zinc-800 rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all duration-300 ${
            pct === 100 ? 'bg-emerald-500' : 'bg-amber-400'
          }`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="text-[11px] text-zinc-400 tabular-nums shrink-0">
        {owned}/{total}
        {showPct && <span className="text-zinc-500 ml-1">({pct}%)</span>}
      </span>
    </div>
  );
}
