import { useState, useEffect, useRef } from 'react';
import { IconClose, IconSearch } from './Icons';

export default function FilterBar({
  value,
  onChange,
  placeholder = 'Filter...',
  debounceMs = 0,
  className = 'mb-3',
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  debounceMs?: number;
  className?: string;
}) {
  const [local, setLocal] = useState(value);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => {
    setLocal(value);
  }, [value]);

  const handleChange = (v: string) => {
    setLocal(v);
    if (debounceMs > 0) {
      clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => onChange(v), debounceMs);
    } else {
      onChange(v);
    }
  };

  useEffect(() => () => clearTimeout(timerRef.current), []);

  return (
    <div className={`relative ${className}`}>
      <IconSearch className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-zinc-500 pointer-events-none" />
      <input
        type="text"
        value={local}
        onChange={(e) => handleChange(e.target.value)}
        placeholder={placeholder}
        className="w-full bg-zinc-900/80 border border-white/[0.08] rounded-xl pl-10 pr-9 py-2.5 text-base md:text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-500/50 focus:ring-2 focus:ring-amber-500/20 transition-all"
      />
      {local && (
        <button
          type="button"
          onClick={() => handleChange('')}
          aria-label="Clear filter"
          className="absolute right-2.5 top-1/2 -translate-y-1/2 p-1 rounded-md text-zinc-500 hover:text-zinc-200 transition-colors"
        >
          <IconClose className="w-3.5 h-3.5" />
        </button>
      )}
    </div>
  );
}
