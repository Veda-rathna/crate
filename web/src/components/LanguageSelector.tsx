import { useState } from 'react';

const PRESET_LANGUAGES = [
  { id: 'tamil', label: 'Tamil' },
  { id: 'telugu', label: 'Telugu' },
  { id: 'malayalam', label: 'Malayalam' },
  { id: 'hindi', label: 'Hindi' },
  { id: 'kannada', label: 'Kannada' },
  { id: 'english', label: 'English' },
];

export interface LanguageSelectorProps {
  selected: string[];
  onChange: (languages: string[]) => void;
  disabled?: boolean;
}

export default function LanguageSelector({ selected, onChange, disabled }: LanguageSelectorProps) {
  const [showAddCustom, setShowAddCustom] = useState(false);
  const [customInput, setCustomInput] = useState('');

  const normalizedSelected = (selected || [])
    .map((l) => l.toLowerCase().trim())
    .filter(Boolean);

  const isAll = normalizedSelected.length === 0 || normalizedSelected.includes('all');

  const handleToggle = (langId: string) => {
    if (disabled) return;
    const lower = langId.toLowerCase().trim();

    if (lower === 'all') {
      onChange([]);
      return;
    }

    if (isAll) {
      onChange([lower]);
      return;
    }

    if (normalizedSelected.includes(lower)) {
      const remaining = normalizedSelected.filter((l) => l !== lower && l !== 'all');
      onChange(remaining);
    } else {
      const next = [...normalizedSelected.filter((l) => l !== 'all'), lower];
      onChange(next);
    }
  };

  const handleAddCustom = () => {
    if (!customInput.trim()) return;
    const lower = customInput.toLowerCase().trim();
    if (lower === 'all') {
      onChange([]);
    } else if (!normalizedSelected.includes(lower)) {
      const next = isAll ? [lower] : [...normalizedSelected, lower];
      onChange(next);
    }
    setCustomInput('');
    setShowAddCustom(false);
  };

  const presetIds = new Set(PRESET_LANGUAGES.map((p) => p.id));
  const customSelected = normalizedSelected.filter((l) => l !== 'all' && !presetIds.has(l));

  const formatLangName = (id: string) => {
    const preset = PRESET_LANGUAGES.find((p) => p.id === id);
    if (preset) return preset.label;
    return id.charAt(0).toUpperCase() + id.slice(1);
  };

  return (
    <div className="bg-zinc-900/70 border border-white/[0.06] rounded-xl p-3.5 space-y-2.5">
      <div>
        <p className="text-sm font-medium text-zinc-100">Language Filter</p>
        <p className="text-[11px] text-zinc-400 mt-0.5">
          {isAll
            ? 'Auto-downloads releases in all languages'
            : `Only auto-downloads releases in: ${normalizedSelected.map(formatLangName).join(', ')}`}
        </p>
      </div>

      <div className="flex flex-wrap gap-1.5">
        <button
          type="button"
          disabled={disabled}
          onClick={() => handleToggle('all')}
          className={`px-2.5 py-1 text-xs rounded-full font-medium transition-all ${
            isAll
              ? 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/35'
              : 'bg-zinc-800/80 text-zinc-400 border border-white/[0.06] hover:border-white/[0.14] active:scale-95'
          } ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
        >
          {isAll && <span className="mr-1">✓</span>}
          All Languages
        </button>

        {PRESET_LANGUAGES.map(({ id, label }) => {
          const active = !isAll && normalizedSelected.includes(id);
          return (
            <button
              key={id}
              type="button"
              disabled={disabled}
              onClick={() => handleToggle(id)}
              className={`px-2.5 py-1 text-xs rounded-full font-medium transition-all ${
                active
                  ? 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/35'
                  : 'bg-zinc-800/80 text-zinc-400 border border-white/[0.06] hover:border-white/[0.14] active:scale-95'
              } ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
            >
              {active && <span className="mr-1">✓</span>}
              {label}
            </button>
          );
        })}

        {customSelected.map((lang) => (
          <button
            key={lang}
            type="button"
            disabled={disabled}
            onClick={() => handleToggle(lang)}
            className={`px-2.5 py-1 text-xs rounded-full font-medium transition-all bg-emerald-500/15 text-emerald-300 border border-emerald-500/35 ${
              disabled ? 'opacity-50 cursor-not-allowed' : 'active:scale-95'
            }`}
          >
            <span className="mr-1">✓</span>
            {formatLangName(lang)}
            <span className="ml-1 text-[10px] opacity-75">✕</span>
          </button>
        ))}

        {!disabled &&
          (showAddCustom ? (
            <div className="inline-flex items-center gap-1.5">
              <input
                type="text"
                value={customInput}
                onChange={(e) => setCustomInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleAddCustom();
                  } else if (e.key === 'Escape') {
                    setShowAddCustom(false);
                    setCustomInput('');
                  }
                }}
                placeholder="e.g. Punjabi"
                className="bg-zinc-950 border border-white/[0.12] rounded-full px-3 py-1 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-emerald-500/50 w-28"
                autoFocus
              />
              <button
                type="button"
                onClick={handleAddCustom}
                className="text-xs font-medium text-emerald-400 hover:text-emerald-300 px-1.5"
              >
                Add
              </button>
              <button
                type="button"
                onClick={() => {
                  setShowAddCustom(false);
                  setCustomInput('');
                }}
                className="text-xs text-zinc-500 hover:text-zinc-300 px-1"
              >
                ✕
              </button>
            </div>
          ) : (
            <button
              type="button"
              onClick={() => setShowAddCustom(true)}
              className="px-2.5 py-1 text-xs rounded-full text-zinc-400 border border-dashed border-zinc-700 hover:text-zinc-200 hover:border-zinc-500 transition-colors"
            >
              + Other
            </button>
          ))}
      </div>
    </div>
  );
}
