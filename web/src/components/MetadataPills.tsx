import { formatFans } from '../lib/format';

const knownKeys: Record<string, (v: string) => string> = {
  type: (v) => v,
  country: (v) => v,
  disambiguation: (v) => v,
  active_since: (v) => `Since ${v}`,
  fans: (v) => `${formatFans(Number(v))} fans`,
  release_date: (v) => v,
  record_type: (v) => v,
};

const keyOrder = ['type', 'country', 'disambiguation', 'active_since', 'fans'];

export default function MetadataPills({ metadata }: { metadata?: Record<string, string> }) {
  if (!metadata) return null;

  const entries = keyOrder
    .filter((k) => metadata[k])
    .map((k) => ({ key: k, value: (knownKeys[k] || ((v: string) => v))(metadata[k]) }));

  Object.keys(metadata).forEach((k) => {
    if (!keyOrder.includes(k) && knownKeys[k]) {
      entries.push({ key: k, value: knownKeys[k](metadata[k]) });
    }
  });

  Object.keys(metadata).forEach((k) => {
    if (!entries.some((e) => e.key === k)) {
      entries.push({ key: k, value: `${k}: ${metadata[k]}` });
    }
  });

  if (entries.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-1.5">
      {entries.map(({ key, value }) => (
        <span
          key={key}
          className="text-[11px] bg-zinc-800/80 border border-white/[0.06] text-zinc-300 px-2 py-0.5 rounded-md"
        >
          {value}
        </span>
      ))}
    </div>
  );
}
