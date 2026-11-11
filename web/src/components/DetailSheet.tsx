import { useEffect } from 'react';
import { IconClose } from './Icons';

interface DetailSheetProps {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
  children: React.ReactNode;
}

export default function DetailSheet({
  open,
  onClose,
  title,
  subtitle,
  children,
}: DetailSheetProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-[90] flex items-end md:items-center justify-center md:p-4"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={onClose}
    >
      <div className="absolute inset-0 bg-black/65 backdrop-blur-xs" />
      <div
        className="relative w-full md:max-w-lg bg-zinc-900 border border-white/[0.08] rounded-t-2xl md:rounded-2xl shadow-2xl animate-slide-up md:animate-modal-in max-h-[82vh] flex flex-col pb-[env(safe-area-inset-bottom,0px)] md:pb-0"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="md:hidden flex justify-center pt-2.5 pb-0.5">
          <div className="w-10 h-1 rounded-full bg-zinc-700" />
        </div>
        <div className="flex items-start justify-between gap-3 px-5 pt-3.5 pb-3 border-b border-white/[0.07]">
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-zinc-100 truncate">{title}</h3>
            {subtitle && <p className="text-xs text-zinc-400 truncate mt-0.5">{subtitle}</p>}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="text-zinc-500 hover:text-white p-1.5 -mr-1.5 -mt-1 rounded-lg hover:bg-zinc-800 transition-colors shrink-0"
            aria-label="Close"
          >
            <IconClose className="w-4 h-4" />
          </button>
        </div>
        <div className="overflow-y-auto px-5 py-4">{children}</div>
      </div>
    </div>
  );
}

export function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex justify-between items-start gap-4 py-2 border-b border-white/[0.04] last:border-0">
      <span className="text-[11px] text-zinc-500 uppercase tracking-wider shrink-0 pt-0.5">
        {label}
      </span>
      <span className="text-sm text-zinc-200 text-right min-w-0 break-words">{children}</span>
    </div>
  );
}
