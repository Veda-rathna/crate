export default function ProviderBadge({ provider }: { provider: string }) {
  const isLocal = provider === 'local';
  return (
    <span
      className={`inline-flex items-center gap-1 text-[10px] font-medium px-2 py-0.5 rounded-md border ${
        isLocal
          ? 'text-amber-300 bg-amber-500/10 border-amber-500/20'
          : 'text-zinc-400 bg-zinc-800/80 border-white/[0.06]'
      }`}
    >
      <svg
        className="w-2.5 h-2.5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
      </svg>
      {provider}
    </span>
  );
}
