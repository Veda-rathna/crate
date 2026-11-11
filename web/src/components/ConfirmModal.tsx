import { useEffect } from 'react';
import { IconAlert, IconClose } from './Icons';

interface ConfirmModalProps {
  open: boolean;
  title: string;
  description: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: 'danger' | 'primary';
  loading?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmModal({
  open,
  title,
  description,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  tone = 'danger',
  loading = false,
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !loading) onCancel();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, loading, onCancel]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-[95] flex items-end md:items-center justify-center md:p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-modal-title"
      onClick={() => !loading && onCancel()}
    >
      <div className="absolute inset-0 bg-black/70 backdrop-blur-xs" />
      <div
        className="relative w-full md:max-w-md bg-zinc-900 border border-white/[0.08] rounded-t-2xl md:rounded-2xl shadow-2xl animate-slide-up md:animate-modal-in p-5 pb-[calc(1.25rem+env(safe-area-inset-bottom,0px))] md:pb-5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start gap-3">
          <div
            className={`w-10 h-10 rounded-xl flex items-center justify-center shrink-0 ${
              tone === 'danger'
                ? 'bg-rose-500/15 text-rose-400 border border-rose-500/20'
                : 'bg-amber-500/15 text-amber-400 border border-amber-500/20'
            }`}
          >
            <IconAlert className="w-5 h-5" />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-start justify-between gap-2">
              <h3 id="confirm-modal-title" className="text-base font-semibold text-zinc-100">
                {title}
              </h3>
              <button
                type="button"
                onClick={onCancel}
                disabled={loading}
                className="text-zinc-500 hover:text-zinc-200 p-1 -mr-1 -mt-1 rounded-lg transition-colors"
              >
                <IconClose className="w-4 h-4" />
              </button>
            </div>
            <p className="text-sm text-zinc-400 mt-1.5 leading-relaxed">{description}</p>
          </div>
        </div>

        <div className="flex flex-col-reverse sm:flex-row sm:justify-end gap-2 mt-5">
          <button
            type="button"
            onClick={onCancel}
            disabled={loading}
            className="w-full sm:w-auto px-4 py-2.5 rounded-xl text-sm font-medium bg-zinc-800 hover:bg-zinc-700 text-zinc-200 transition-colors disabled:opacity-50"
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={loading}
            className={`w-full sm:w-auto px-4 py-2.5 rounded-xl text-sm font-semibold transition-all disabled:opacity-50 ${
              tone === 'danger'
                ? 'bg-rose-600 hover:bg-rose-500 text-white active:scale-[0.98]'
                : 'bg-amber-500 hover:bg-amber-400 text-zinc-950 active:scale-[0.98]'
            }`}
          >
            {loading ? 'Working…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

