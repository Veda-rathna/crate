import { createContext, useContext, useState, useCallback, useRef, type ReactNode } from 'react';
import { IconAlert, IconCheck, IconClose } from './Icons';

interface Toast {
  id: number;
  message: string;
  type: 'success' | 'error' | 'info';
}

interface ToastContextValue {
  toast: (message: string, type?: Toast['type']) => void;
}

const ToastContext = createContext<ToastContextValue>({ toast: () => {} });

export function useToast() {
  return useContext(ToastContext);
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(0);

  const toast = useCallback((message: string, type: Toast['type'] = 'info') => {
    const id = nextId.current++;
    setToasts((prev) => [...prev.slice(-3), { id, message, type }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 4500);
  }, []);

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((x) => x.id !== id));
  }, []);

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      <div className="fixed bottom-20 left-4 right-4 md:bottom-auto md:top-5 md:right-5 md:left-auto z-[100] flex flex-col gap-2 md:w-full md:max-w-sm pointer-events-none">
        {toasts.map((t) => (
          <div
            key={t.id}
            role="status"
            className={`pointer-events-auto flex items-center gap-2.5 px-3.5 py-3 rounded-xl text-sm font-medium shadow-xl backdrop-blur-md border animate-fade-in ${
              t.type === 'error'
                ? 'bg-rose-950/90 text-rose-200 border-rose-500/35'
                : t.type === 'success'
                  ? 'bg-emerald-950/90 text-emerald-200 border-emerald-500/35'
                  : 'bg-zinc-900/95 text-zinc-100 border-white/[0.12]'
            }`}
          >
            {t.type === 'success' && <IconCheck className="w-4 h-4 text-emerald-400 shrink-0" />}
            {t.type === 'error' && <IconAlert className="w-4 h-4 text-rose-400 shrink-0" />}
            <span className="flex-1 leading-snug">{t.message}</span>
            <button
              type="button"
              onClick={() => dismiss(t.id)}
              className="text-current opacity-60 hover:opacity-100 p-0.5 rounded transition-opacity shrink-0"
              aria-label="Dismiss notification"
            >
              <IconClose className="w-3.5 h-3.5" />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
