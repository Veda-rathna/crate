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

  // Normalize selected list (lowercase, trimmed)
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

  // Find any selected languages not present in presets
  const presetIds = new Set(PRESET_LANGUAGES.map((p) => p.id));
  const customSelected = normalizedSelected.filter((l) => l !== 'all' && !presetIds.has(l));

  const formatLangName = (id: string) => {
    const preset = PRESET_LANGUAGES.find((p) => p.id === id);
    if (preset) return preset.label;
    return id.charAt(0).toUpperCase() + id.slice(1);
  };

  return (
    <div className="bg-zinc-800/50 rounded-lg p-3 space-y-2">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium">Language Preference</p>
          <p className="text-[11px] text-zinc-500">
            {isAll
              ? 'Auto-downloads releases in all languages'
              : `Only auto-downloads in: ${normalizedSelected.map(formatLangName).join(', ')}`}
          </p>
        </div>
      </div>

      <div className="flex flex-wrap gap-1.5 pt-1">
        {/* "All Languages" button */}
        <button
          type="button"
          disabled={disabled}
          onClick={() => handleToggle('all')}
          className={`px-2.5 py-1 text-xs rounded-full font-medium transition-all ${
            isAll
              ? 'bg-green-900/60 text-green-300 border border-green-700/70 shadow-sm'
              : 'bg-zinc-800 text-zinc-400 border border-zinc-700/40 hover:border-zinc-600 active:scale-95'
          } ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
        >
          {isAll && <span className="mr-1">✓</span>}
          All Languages
        </button>

        {/* Preset languages */}
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
                  ? 'bg-green-900/60 text-green-300 border border-green-700/70 shadow-sm'
                  : 'bg-zinc-800 text-zinc-400 border border-zinc-700/40 hover:border-zinc-600 active:scale-95'
              } ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
            >
              {active && <span className="mr-1">✓</span>}
              {label}
            </button>
          );
        })}

        {/* Custom languages */}
        {customSelected.map((lang) => (
          <button
            key={lang}
            type="button"
            disabled={disabled}
            onClick={() => handleToggle(lang)}
            className={`px-2.5 py-1 text-xs rounded-full font-medium transition-all bg-green-900/60 text-green-300 border border-green-700/70 ${
              disabled ? 'opacity-50 cursor-not-allowed' : 'active:scale-95'
            }`}
          >
            <span className="mr-1">✓</span>
            {formatLangName(lang)}
            <span className="ml-1 text-[10px] text-green-400/80 hover:text-white">✕</span>
          </button>
        ))}

        {/* Add custom language button/input */}
        {!disabled && (
          showAddCustom ? (
            <div className="inline-flex items-center gap-1">
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
                className="bg-zinc-900 border border-zinc-700 rounded-full px-2.5 py-0.5 text-xs text-zinc-200 placeholder-zinc-500 outline-none focus:border-green-600 w-24"
                autoFocus
              />
              <button
                type="button"
                onClick={handleAddCustom}
                className="text-xs text-green-400 hover:text-green-300 px-1"
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
              className="px-2 py-1 text-xs rounded-full text-zinc-500 border border-dashed border-zinc-700 hover:text-zinc-300 hover:border-zinc-500"
            >
              + Other
            </button>
          )
        )}
      </div>
    </div>
  );
}

