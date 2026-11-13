import { useState, useEffect, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useSystemProcess } from '../context/SystemContext';
import { useToast } from '../components/Toast';
import ConfirmModal from '../components/ConfirmModal';
import { IconChevronRight, IconClose, IconRefresh, IconTrash } from '../components/Icons';
import type { ArcodAccount } from '../types/index';

interface QualityTier {
  format: string;
  min_bitrate?: number;
  label: string;
}

type SettingsTab = 'all' | 'arcod' | 'providers' | 'library' | 'downloads' | 'integrations' | 'maintenance';
type DownloadSource = 'arcod' | 'slskd';

const DEFAULT_SOURCE_PRIORITY: DownloadSource[] = ['arcod', 'slskd'];

const SOURCE_META: Record<DownloadSource, { label: string; description: string }> = {
  arcod: {
    label: 'Arcod (arcod.xyz)',
    description: 'Direct lossless FLAC streaming & CDN downloads',
  },
  slskd: {
    label: 'slskd (Soulseek)',
    description: 'Peer-to-peer Soulseek search & transfers',
  },
};

function parseSourcePriority(raw?: string): DownloadSource[] {
  if (!raw) return [...DEFAULT_SOURCE_PRIORITY];
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [...DEFAULT_SOURCE_PRIORITY];
    const out: DownloadSource[] = [];
    for (const item of parsed) {
      if ((item === 'arcod' || item === 'slskd') && !out.includes(item)) {
        out.push(item);
      }
    }
    for (const def of DEFAULT_SOURCE_PRIORITY) {
      if (!out.includes(def)) out.push(def);
    }
    return out;
  } catch {
    return [...DEFAULT_SOURCE_PRIORITY];
  }
}

function tierLabel(format: string, bitrate?: number): string {
  const name = format.toUpperCase();
  if (format === 'flac' || format === 'wav') return name;
  return bitrate ? `${name} ${bitrate}kbps` : name;
}

const NAMING_TOKENS = [
  '{artist}',
  '{albumartist}',
  '{album}',
  '{year}',
  '{disc}',
  '{track:2}',
  '{title}',
];

interface FieldDef {
  key: string;
  label: string;
  placeholder: string;
  type?: string;
  description?: string;
}

const ARCOD_FIELDS: FieldDef[] = [
  {
    key: 'arcod_url',
    label: 'Arcod URL',
    placeholder: 'https://arcod.xyz',
    description: 'Base URL for arcod lossless streaming service',
  },
];

const SLSKD_FIELDS: FieldDef[] = [
  {
    key: 'max_concurrent_slskd',
    label: 'Max Concurrent Downloads',
    placeholder: '10',
    type: 'number',
    description: 'How many files to download from Soulseek simultaneously',
  },
  {
    key: 'shadow_ban_duration_minutes',
    label: 'Shadow Ban Duration (minutes)',
    placeholder: '60',
    type: 'number',
    description: 'How long to temporarily block a peer after failed or stale queued transfers',
  },
];

const LANGUAGE_LLM_FIELDS: FieldDef[] = [
  {
    key: 'lang_llm_url',
    label: 'OpenAI-Compatible API Base URL',
    placeholder: 'https://api.openai.com/v1',
    description:
      'Works with OpenAI, Groq (https://api.groq.com/openai/v1), Gemini OpenAI endpoint, or OpenRouter',
  },
  {
    key: 'lang_llm_api_key',
    label: 'API Key',
    placeholder: 'sk-...',
    type: 'password',
    description: 'Bearer token stored locally and redacted when read',
  },
  {
    key: 'lang_llm_model',
    label: 'Model Name',
    placeholder: 'gpt-4o-mini',
    description: 'Lightweight text model used only when metadata + Apple Music are inconclusive',
  },
];

const INTEGRATION_SECTIONS: { title: string; fields: FieldDef[] }[] = [
  {
    title: 'Spotify Provider (Optional)',
    fields: [
      {
        key: 'spotify_client_id',
        label: 'Client ID',
        placeholder: 'Spotify Client ID',
        description: 'From developer.spotify.com/dashboard',
      },
      {
        key: 'spotify_client_secret',
        label: 'Client Secret',
        placeholder: 'Spotify Client Secret',
        type: 'password',
      },
    ],
  },
  {
    title: 'Navidrome (Optional)',
    fields: [
      {
        key: 'navidrome_url',
        label: 'Server URL',
        placeholder: 'http://localhost:4533',
        description: 'Triggers a Subsonic library rescan after each completed download',
      },
      { key: 'navidrome_user', label: 'Username', placeholder: 'admin' },
      {
        key: 'navidrome_password',
        label: 'Password',
        placeholder: 'Navidrome password',
        type: 'password',
      },
    ],
  },
  {
    title: 'Music Assistant (Optional)',
    fields: [
      {
        key: 'music_assistant_url',
        label: 'Server URL',
        placeholder: 'http://localhost:8095',
        description:
          'Syncs library after downloads and watches the reject playlist (restart required to connect)',
      },
      {
        key: 'music_assistant_token',
        label: 'API Token',
        placeholder: 'Long-lived MA token',
        type: 'password',
      },
      {
        key: 'music_assistant_reject_playlist',
        label: 'Reject Playlist Name',
        placeholder: 'Crate Reject',
        description: 'Drop a track into this MA playlist to mark it bad — auto-created if missing',
      },
    ],
  },
];

const SCHEDULING_FIELDS: FieldDef[] = [
  {
    key: 'max_auto_queue',
    label: 'Max Auto-Queue Per Cycle',
    placeholder: '50',
    type: 'number',
    description: 'Limit how many wanted tracks the scheduler queues per cycle',
  },
  {
    key: 'retry_delay_hours',
    label: 'Failed Download Retry Delay (hours)',
    placeholder: '1',
    type: 'number',
    description: 'Wait this many hours before retrying a failed download attempt',
  },
  {
    key: 'requeue_cooldown_hours',
    label: 'Re-queue Cooldown (hours)',
    placeholder: '1',
    type: 'number',
    description: 'Wait this many hours before retrying a previously failed track in library',
  },
  {
    key: 'cache_ttl_hours',
    label: 'Metadata Cache TTL (hours)',
    placeholder: '24',
    type: 'number',
    description: 'How long provider search and browse results stay cached',
  },
  {
    key: 'activity_retention_days',
    label: 'Activity Log Retention (days)',
    placeholder: '30',
    type: 'number',
    description: 'Delete activity log entries older than this',
  },
];

export default function Settings() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const { status, providers } = useSystemProcess();

  const [activeTab, setActiveTab] = useState<SettingsTab>('all');
  const [form, setForm] = useState<Record<string, string>>({});
  const [tiers, setTiers] = useState<QualityTier[]>([]);
  const [sourcePriority, setSourcePriority] = useState<DownloadSource[]>(DEFAULT_SOURCE_PRIORITY);
  const [negativeKeywords, setNegativeKeywords] = useState<string[]>([]);
  const [baselinePayload, setBaselinePayload] = useState<string>('');
  const [saved, setSaved] = useState(false);
  const [newKeyword, setNewKeyword] = useState('');
  const [namingPreview, setNamingPreview] = useState<{ path?: string; error?: string }>({});

  const [accountModalOpen, setAccountModalOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState<ArcodAccount | null>(null);
  const [accountToDelete, setAccountToDelete] = useState<ArcodAccount | null>(null);
  const [testingAccountId, setTestingAccountId] = useState<string | null>(null);

  const { data: arcodAccountsData, refetch: refetchArcodAccounts } = useQuery({
    queryKey: ['arcod-accounts'],
    queryFn: () => api.listArcodAccounts(),
  });
  const arcodAccounts = arcodAccountsData?.accounts || [];

  const autoCreateAccount = useMutation({
    mutationFn: () => api.autoCreateArcodAccount(),
    onSuccess: (acc) => {
      refetchArcodAccounts();
      toast(`Created account ${acc.email} on arcod.xyz!`, 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const addAccount = useMutation({
    mutationFn: (data: { email: string; password: string; token?: string }) =>
      api.addArcodAccount(data),
    onSuccess: () => {
      refetchArcodAccounts();
      toast('Arcod account added', 'success');
      setAccountModalOpen(false);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const updateAccount = useMutation({
    mutationFn: ({ id, data }: { id: string; data: { email?: string; password?: string; token?: string } }) =>
      api.updateArcodAccount(id, data),
    onSuccess: () => {
      refetchArcodAccounts();
      toast('Arcod account updated', 'success');
      setEditingAccount(null);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const deleteAccount = useMutation({
    mutationFn: (id: string) => api.deleteArcodAccount(id),
    onSuccess: () => {
      refetchArcodAccounts();
      toast('Account removed from pool', 'info');
      setAccountToDelete(null);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const testAccount = useMutation({
    mutationFn: (id: string) => api.testArcodAccount(id),
    onMutate: (id) => setTestingAccountId(id),
    onSuccess: (res) => {
      refetchArcodAccounts();
      toast(res.message, 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
    onSettled: () => setTestingAccountId(null),
  });

  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: api.getSettings,
  });

  useEffect(() => {
    if (settings) {
      setForm(settings);
      let parsedTiers: QualityTier[] = [];
      let parsedKw: string[] = [];
      if (settings.quality_tiers) {
        try {
          parsedTiers = JSON.parse(settings.quality_tiers);
        } catch {
          /* ignore */
        }
      }
      if (settings.negative_keywords) {
        try {
          parsedKw = JSON.parse(settings.negative_keywords);
        } catch {
          /* ignore */
        }
      }
      const parsedSources = parseSourcePriority(settings.download_source_priority);
      setTiers(parsedTiers);
      setSourcePriority(parsedSources);
      setNegativeKeywords(parsedKw);
      setBaselinePayload(
        JSON.stringify({
          ...settings,
          arcod_enabled: settings.arcod_enabled ?? 'true',
          slskd_enabled: settings.slskd_enabled ?? 'true',
          lang_allow_unknown: settings.lang_allow_unknown ?? 'false',
          lang_llm_enabled: settings.lang_llm_enabled ?? 'false',
          download_source_priority: JSON.stringify(parsedSources),
          quality_tiers: JSON.stringify(parsedTiers),
          quality_fallback_enabled: settings.quality_fallback_enabled ?? 'true',
          negative_keywords: JSON.stringify(parsedKw),
        })
      );
    }
  }, [settings]);

  const currentPayload = useMemo(
    () =>
      JSON.stringify({
        ...form,
        arcod_enabled: form.arcod_enabled ?? 'true',
        slskd_enabled: form.slskd_enabled ?? 'true',
        lang_allow_unknown: form.lang_allow_unknown ?? 'false',
        lang_llm_enabled: form.lang_llm_enabled ?? 'false',
        download_source_priority: JSON.stringify(sourcePriority),
        quality_tiers: JSON.stringify(tiers),
        quality_fallback_enabled: form.quality_fallback_enabled ?? 'true',
        negative_keywords: JSON.stringify(negativeKeywords),
      }),
    [form, sourcePriority, tiers, negativeKeywords]
  );

  const isDirty = !!baselinePayload && currentPayload !== baselinePayload;

  useEffect(() => {
    const handle = setTimeout(() => {
      api
        .namingPreview(form.naming_template ?? '')
        .then((res) => setNamingPreview({ path: res.path }))
        .catch((err: Error) => setNamingPreview({ error: err.message }));
    }, 350);
    return () => clearTimeout(handle);
  }, [form.naming_template]);

  const importStatus = useQuery({
    queryKey: ['library-import'],
    queryFn: api.libraryImportStatus,
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 1000 : false),
  });

  const runImport = useMutation({
    mutationFn: (dryRun: boolean) => api.startLibraryImport(dryRun),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['library-import'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const importDone = importStatus.data?.status === 'done' && !importStatus.data.dry_run;
  useEffect(() => {
    if (importDone) {
      queryClient.invalidateQueries({ queryKey: ['status'] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    }
  }, [importDone, queryClient]);

  const save = useMutation({
    mutationFn: () =>
      api.updateSettings({
        ...form,
        arcod_enabled: form.arcod_enabled ?? 'true',
        slskd_enabled: form.slskd_enabled ?? 'true',
        lang_allow_unknown: form.lang_allow_unknown ?? 'false',
        lang_llm_enabled: form.lang_llm_enabled ?? 'false',
        download_source_priority: JSON.stringify(sourcePriority),
        quality_tiers: JSON.stringify(tiers),
        quality_fallback_enabled: form.quality_fallback_enabled ?? 'true',
        negative_keywords: JSON.stringify(negativeKeywords),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] });
      setBaselinePayload(currentPayload);
      setSaved(true);
      toast('Settings saved', 'success');
      setTimeout(() => setSaved(false), 2000);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const discardChanges = useCallback(() => {
    if (!settings) return;
    setForm(settings);
    setSourcePriority(parseSourcePriority(settings.download_source_priority));
    try {
      setTiers(settings.quality_tiers ? JSON.parse(settings.quality_tiers) : []);
    } catch {
      setTiers([]);
    }
    try {
      setNegativeKeywords(
        settings.negative_keywords ? JSON.parse(settings.negative_keywords) : []
      );
    } catch {
      setNegativeKeywords([]);
    }
  }, [settings]);

  const moveSource = useCallback(
    (index: number, direction: -1 | 1) => {
      const next = index + direction;
      if (next < 0 || next >= sourcePriority.length) return;
      const updated = [...sourcePriority];
      [updated[index], updated[next]] = [updated[next], updated[index]];
      setSourcePriority(updated);
    },
    [sourcePriority]
  );

  const moveTier = useCallback(
    (index: number, direction: -1 | 1) => {
      const next = index + direction;
      if (next < 0 || next >= tiers.length) return;
      const updated = [...tiers];
      [updated[index], updated[next]] = [updated[next], updated[index]];
      setTiers(updated);
    },
    [tiers]
  );

  const removeTier = useCallback(
    (index: number) => {
      setTiers(tiers.filter((_, i) => i !== index));
    },
    [tiers]
  );

  const addTier = useCallback(() => {
    setTiers([...tiers, { format: 'mp3', min_bitrate: 256, label: 'MP3 256kbps' }]);
  }, [tiers]);

  const clearCache = useMutation({
    mutationFn: () => api.clearCache(),
    onSuccess: () => toast('Provider metadata cache cleared', 'success'),
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const showSection = (tab: SettingsTab) => activeTab === 'all' || activeTab === tab;
  const arcodEnabled = form.arcod_enabled !== 'false';
  const slskdEnabled = form.slskd_enabled !== 'false';

  return (
    <div className="space-y-5 pb-16">
      <div>
        <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100">Settings</h2>
        <p className="text-xs text-zinc-400 mt-0.5">
          Configure metadata providers, library layout, download quality, and integrations.
        </p>
      </div>

      {/* System Summary Stats */}
      {status && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
          <Stat label="Artists" value={status.artists_count} />
          <Stat label="Owned Tracks" value={status.owned_tracks ?? 0} sub={`of ${status.total_tracks ?? 0}`} />
          <Stat label="Queued" value={status.pending_downloads} />
          <Stat
            label="Active"
            value={status.active_downloads + (status.organizing_downloads ?? 0)}
          />
        </div>
      )}

      {/* Section Jump Tabs */}
      <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar pb-0.5">
        {(
          [
            { id: 'all', label: 'All Settings' },
            { id: 'arcod', label: `Arcod Accounts (${arcodAccounts.length})` },
            { id: 'downloads', label: 'Downloaders & Quality' },
            { id: 'providers', label: 'Providers' },
            { id: 'library', label: 'Library & Import' },
            { id: 'integrations', label: 'Integrations' },
            { id: 'maintenance', label: 'Scheduling & Blocks' },
          ] as const
        ).map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setActiveTab(t.id)}
            className={`px-3 py-1.5 rounded-full text-xs font-medium border whitespace-nowrap transition-colors shrink-0 ${
              activeTab === t.id
                ? 'bg-zinc-100 text-zinc-950 border-zinc-100'
                : 'bg-zinc-900/70 text-zinc-400 border-white/[0.07] hover:text-zinc-200'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Providers */}
      {showSection('providers') && providers.length > 0 && (
        <SettingsSection
          title="Metadata Providers"
          description="Crate communicates with standalone gRPC provider services for search and discographies."
        >
          <div className="grid grid-cols-1 md:grid-cols-2 gap-2.5 mb-4">
            {providers.map((p) => (
              <div
                key={p.name}
                className="flex items-center gap-3 bg-zinc-950/60 border border-white/[0.06] rounded-xl px-3.5 py-3"
              >
                <span
                  className={`w-2.5 h-2.5 rounded-full shrink-0 ${
                    p.healthy ? 'bg-emerald-400' : 'bg-rose-500 animate-pulse'
                  }`}
                />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-zinc-100 truncate">{p.display_name}</p>
                  <p className="text-[11px] text-zinc-500 truncate font-mono">
                    v{p.version} · {p.address}
                  </p>
                </div>
                <span
                  className={`text-[10px] font-semibold uppercase px-2 py-0.5 rounded border ${
                    p.healthy
                      ? 'bg-emerald-500/15 text-emerald-300 border-emerald-500/25'
                      : 'bg-rose-500/15 text-rose-300 border-rose-500/25'
                  }`}
                >
                  {p.healthy ? 'Healthy' : 'Offline'}
                </span>
              </div>
            ))}
          </div>

          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">
              Default Search Provider
            </label>
            <select
              value={form.provider_primary || ''}
              onChange={(e) => setForm({ ...form, provider_primary: e.target.value })}
              className="w-full md:max-w-sm bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2.5 text-base md:text-sm text-zinc-100 outline-none focus:border-amber-500/50"
            >
              <option value="">Default (musicbrainz)</option>
              {providers.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.display_name}
                </option>
              ))}
            </select>
          </div>
        </SettingsSection>
      )}

      {/* Language Detection Accuracy */}
      {showSection('providers') && (
        <SettingsSection
          title="Language Detection & Filtering"
          description="Combines provider work languages & genres, Apple Music India lookups, and an optional lightweight AI tie-breaker — cached so your server uses near-zero CPU."
        >
          <div className="space-y-3">
            <label className="flex items-center justify-between gap-3 bg-zinc-950/60 border border-white/[0.06] rounded-xl px-3.5 py-3 cursor-pointer">
              <div>
                <p className="text-sm font-medium text-zinc-100">
                  Allow unlabelled releases &amp; tracks
                </p>
                <p className="text-[11px] text-zinc-400">
                  Recommended if originals lack language tags: still blocks confirmed dubs (e.g. Telugu, Hindi), while allowing releases &amp; Soulseek files whose language cannot be confirmed.
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={form.lang_allow_unknown === 'true'}
                onClick={() =>
                  setForm({
                    ...form,
                    lang_allow_unknown: form.lang_allow_unknown === 'true' ? 'false' : 'true',
                  })
                }
                className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                  form.lang_allow_unknown === 'true' ? 'bg-emerald-500' : 'bg-zinc-700'
                }`}
              >
                <span
                  className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
                    form.lang_allow_unknown === 'true' ? 'translate-x-[22px]' : 'translate-x-0.5'
                  }`}
                />
              </button>
            </label>

            <label className="flex items-center justify-between gap-3 bg-zinc-950/60 border border-white/[0.06] rounded-xl px-3.5 py-3 cursor-pointer">
              <div>
                <p className="text-sm font-medium text-zinc-100">
                  Enable Cloud AI Tie-Breaker (Optional)
                </p>
                <p className="text-[11px] text-zinc-400">
                  Only called over HTTPS when MusicBrainz/Deezer/Spotify and Apple Music India return unknown; verdicts are cached for 30 days.
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={form.lang_llm_enabled === 'true'}
                onClick={() =>
                  setForm({
                    ...form,
                    lang_llm_enabled: form.lang_llm_enabled === 'true' ? 'false' : 'true',
                  })
                }
                className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                  form.lang_llm_enabled === 'true' ? 'bg-emerald-500' : 'bg-zinc-700'
                }`}
              >
                <span
                  className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
                    form.lang_llm_enabled === 'true' ? 'translate-x-[22px]' : 'translate-x-0.5'
                  }`}
                />
              </button>
            </label>

            {form.lang_llm_enabled === 'true' && (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-1">
                {LANGUAGE_LLM_FIELDS.map((field) => (
                  <SettingInput
                    key={field.key}
                    field={field}
                    value={form[field.key] || ''}
                    onChange={(v) => setForm({ ...form, [field.key]: v })}
                  />
                ))}
              </div>
            )}
          </div>
        </SettingsSection>
      )}

      {/* Library & Import */}
      {showSection('library') && (
        <SettingsSection
          title="Library Layout & Importer"
          description="Control how downloaded tracks are named on disk and adopt existing audio files."
        >
          <div className="space-y-3">
            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-1.5">
                Naming Template
              </label>
              <input
                type="text"
                value={form.naming_template || ''}
                onChange={(e) => setForm({ ...form, naming_template: e.target.value })}
                placeholder="{artist}/{album} ({year})/{track:2} - {title}"
                spellCheck={false}
                className="w-full bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2.5 text-base md:text-sm font-mono text-zinc-100 placeholder-zinc-600 outline-none focus:border-amber-500/50"
              />
              <div className="flex flex-wrap items-center gap-1.5 mt-2">
                <span className="text-[11px] text-zinc-500 mr-1">Insert token:</span>
                {NAMING_TOKENS.map((tok) => (
                  <button
                    key={tok}
                    type="button"
                    onClick={() =>
                      setForm({
                        ...form,
                        naming_template: `${form.naming_template || ''}${tok}`,
                      })
                    }
                    className="px-2 py-0.5 rounded-md bg-zinc-800 hover:bg-zinc-700 border border-white/[0.06] text-[11px] font-mono text-amber-300 transition-colors"
                  >
                    {tok}
                  </button>
                ))}
              </div>
              <p className="text-[11px] text-zinc-500 mt-2 leading-relaxed">
                Applies to new downloads only — existing files are never renamed. Extension is appended automatically.
              </p>
              <div
                className={`mt-2.5 rounded-xl px-3.5 py-2.5 text-xs font-mono break-all border ${
                  namingPreview.error
                    ? 'bg-rose-950/35 border-rose-500/25 text-rose-300'
                    : 'bg-zinc-950/70 border-white/[0.06] text-zinc-300'
                }`}
              >
                <span className="text-zinc-500 mr-2 select-none">Preview:</span>
                {namingPreview.error ?? namingPreview.path ?? '…'}
              </div>
            </div>

            <div className="pt-4 mt-4 border-t border-white/[0.06] space-y-3">
              <div>
                <p className="text-sm font-semibold text-zinc-100">Import Existing Library</p>
                <p className="text-xs text-zinc-400 mt-1 leading-relaxed">
                  Scans embedded tags of MP3 and FLAC files inside your library folder without modifying or moving files. MusicBrainz-tagged files link automatically; others import as local and can be linked later.
                </p>
              </div>

              {importStatus.data?.status === 'running' && (
                <div className="rounded-xl bg-sky-950/30 border border-sky-500/25 p-3.5 space-y-2">
                  <div className="flex items-center justify-between text-xs text-sky-200">
                    <span className="font-semibold">
                      {importStatus.data.dry_run
                        ? 'Scanning library (dry run)…'
                        : 'Importing library…'}
                    </span>
                    <span className="font-mono tabular-nums">
                      {importStatus.data.processed}
                      {importStatus.data.total > 0 ? ` / ${importStatus.data.total}` : ''} files
                    </span>
                  </div>
                  <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden">
                    <div
                      className={`h-full bg-sky-400 rounded-full transition-all duration-300 ${
                        importStatus.data.total === 0 ? 'w-1/2 animate-pulse' : ''
                      }`}
                      style={
                        importStatus.data.total > 0
                          ? {
                              width: `${Math.min(100, Math.round((importStatus.data.processed / importStatus.data.total) * 100))}%`,
                            }
                          : undefined
                      }
                    />
                  </div>
                </div>
              )}

              {importStatus.data?.status === 'failed' && (
                <div className="rounded-xl bg-rose-950/35 border border-rose-500/25 px-3.5 py-2.5 text-xs text-rose-300">
                  Import failed: {importStatus.data.error}
                </div>
              )}

              {importStatus.data?.status === 'done' && importStatus.data.report && (
                <div className="rounded-xl bg-zinc-950/70 border border-white/[0.07] p-3.5 space-y-2.5">
                  <p className="text-xs font-semibold text-zinc-200">
                    {importStatus.data.dry_run
                      ? 'Dry Run Summary (No Changes Written)'
                      : 'Import Complete'}
                  </p>
                  <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs">
                    <MiniReportStat label="Artists" value={importStatus.data.report.artists_added} />
                    <MiniReportStat label="Albums" value={importStatus.data.report.albums_added} />
                    <MiniReportStat label="Tracks" value={importStatus.data.report.tracks_added} />
                    <MiniReportStat
                      label="Claimed Wanted"
                      value={importStatus.data.report.tracks_claimed}
                    />
                  </div>
                  <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-zinc-400">
                    {importStatus.data.report.musicbrainz_linked > 0 && (
                      <span className="text-emerald-300">
                        {importStatus.data.report.musicbrainz_linked} linked via MusicBrainz tags
                      </span>
                    )}
                    {importStatus.data.report.tracks_known > 0 && (
                      <span>{importStatus.data.report.tracks_known} already known</span>
                    )}
                    {importStatus.data.report.duplicate_files > 0 && (
                      <span>{importStatus.data.report.duplicate_files} duplicates skipped</span>
                    )}
                  </div>
                  {importStatus.data.report.files_skipped > 0 && (
                    <div className="pt-2 border-t border-white/[0.06] space-y-1">
                      <p className="text-xs text-amber-300 font-medium">
                        {importStatus.data.report.files_skipped} files skipped
                      </p>
                      {importStatus.data.report.skipped_samples?.slice(0, 5).map((sf, i) => (
                        <p key={i} className="font-mono text-[10px] text-zinc-500 truncate">
                          {sf.path} — {sf.reason}
                        </p>
                      ))}
                    </div>
                  )}
                </div>
              )}

              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  onClick={() => runImport.mutate(true)}
                  disabled={runImport.isPending || importStatus.data?.status === 'running'}
                  className="px-4 py-2 bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 rounded-xl text-xs font-medium transition-colors disabled:opacity-50"
                >
                  Scan (Dry Run)
                </button>
                <button
                  type="button"
                  onClick={() => runImport.mutate(false)}
                  disabled={runImport.isPending || importStatus.data?.status === 'running'}
                  className="px-4 py-2 bg-amber-400 hover:bg-amber-300 text-zinc-950 rounded-xl text-xs font-semibold transition-colors disabled:opacity-50"
                >
                  Import Library
                </button>
              </div>
            </div>
          </div>
        </SettingsSection>
      )}

      {/* Downloaders & Quality */}
      {(showSection('downloads') || activeTab === 'arcod') && (
        <div className="space-y-5">
          {showSection('downloads') && (
            <SettingsSection
              title="Download Source Priority"
            description="Choose which download source Crate tries first and enable or disable individual services. If the primary source finds no match or fails, Crate falls back to the next enabled source."
          >
            <div className="space-y-2">
              {sourcePriority.map((src, i) => {
                const enabled = src === 'arcod' ? arcodEnabled : slskdEnabled;
                const toggleKey = src === 'arcod' ? 'arcod_enabled' : 'slskd_enabled';
                const meta = SOURCE_META[src];
                return (
                  <div
                    key={src}
                    className={`flex items-center gap-3 bg-zinc-950/60 border rounded-xl px-3.5 py-2.5 transition-opacity ${
                      enabled ? 'border-white/[0.06]' : 'border-white/[0.04] opacity-60'
                    }`}
                  >
                    <span className="text-xs font-mono text-zinc-500 w-5 text-right shrink-0 tabular-nums">
                      #{i + 1}
                    </span>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="text-sm font-medium text-zinc-100 truncate">{meta.label}</p>
                        {!enabled && (
                          <span className="text-[10px] font-semibold uppercase px-1.5 py-px rounded bg-zinc-800 text-zinc-400 border border-white/[0.06]">
                            Disabled
                          </span>
                        )}
                      </div>
                      <p className="text-[11px] text-zinc-500 truncate">{meta.description}</p>
                    </div>
                    <div className="flex flex-col gap-1 shrink-0">
                      <button
                        type="button"
                        onClick={() => moveSource(i, -1)}
                        disabled={i === 0}
                        className="p-1 text-zinc-400 hover:text-white disabled:opacity-20 transition-colors"
                        aria-label={`Move ${meta.label} up`}
                      >
                        <svg
                          className="w-3.5 h-3.5"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <path d="m18 15-6-6-6 6" />
                        </svg>
                      </button>
                      <button
                        type="button"
                        onClick={() => moveSource(i, 1)}
                        disabled={i === sourcePriority.length - 1}
                        className="p-1 text-zinc-400 hover:text-white disabled:opacity-20 transition-colors"
                        aria-label={`Move ${meta.label} down`}
                      >
                        <svg
                          className="w-3.5 h-3.5"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <path d="m6 9 6 6 6-6" />
                        </svg>
                      </button>
                    </div>
                    <button
                      type="button"
                      role="switch"
                      aria-label={`Toggle ${meta.label}`}
                      aria-checked={enabled}
                      onClick={() =>
                        setForm({ ...form, [toggleKey]: enabled ? 'false' : 'true' })
                      }
                      className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                        enabled ? 'bg-emerald-500' : 'bg-zinc-700'
                      }`}
                    >
                      <span
                        className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
                          enabled ? 'translate-x-[22px]' : 'translate-x-0.5'
                        }`}
                      />
                    </button>
                  </div>
                );
              })}
            </div>
            {!arcodEnabled && !slskdEnabled && (
              <div className="rounded-xl bg-amber-950/35 border border-amber-500/30 px-3.5 py-2.5 text-xs text-amber-200">
                Both Arcod and slskd are disabled — automatic downloads and manual searches will fail until at least one source is enabled.
              </div>
            )}
          </SettingsSection>
          )}

          <SettingsSection
            title="Arcod"
            description="High-resolution FLAC source via arcod.xyz."
          >
            <label className="flex items-center justify-between gap-3 bg-zinc-950/60 border border-white/[0.06] rounded-xl px-3.5 py-3 mb-3 cursor-pointer">
              <div>
                <p className="text-sm font-medium text-zinc-100">Enable Arcod</p>
                <p className="text-[11px] text-zinc-400">
                  Allow direct lossless FLAC searches and downloads via arcod.xyz
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={arcodEnabled}
                onClick={() =>
                  setForm({ ...form, arcod_enabled: arcodEnabled ? 'false' : 'true' })
                }
                className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                  arcodEnabled ? 'bg-emerald-500' : 'bg-zinc-700'
                }`}
              >
                <span
                  className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
                    arcodEnabled ? 'translate-x-[22px]' : 'translate-x-0.5'
                  }`}
                />
              </button>
            </label>
            <div className="mb-4">
              {ARCOD_FIELDS.map((field) => (
                <SettingInput
                  key={field.key}
                  field={field}
                  value={form[field.key] || ''}
                  onChange={(v) => setForm({ ...form, [field.key]: v })}
                />
              ))}
            </div>

            {/* Account Pool */}
            <div className="space-y-3 pt-3 border-t border-white/[0.06]">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2.5">
                <div>
                  <div className="flex items-center gap-2">
                    <h4 className="text-sm font-semibold text-zinc-200">Account Pool</h4>
                    <span className="px-2 py-0.5 rounded-full text-[11px] font-medium bg-zinc-800 text-zinc-300 border border-white/[0.08]">
                      {arcodAccounts.length} account{arcodAccounts.length !== 1 ? 's' : ''}
                    </span>
                  </div>
                  <p className="text-xs text-zinc-400 mt-0.5">
                    When an account reaches the rate limit (14/min) or stream limit, Crate automatically rotates to the next available account.
                  </p>
                </div>

                <div className="flex items-center gap-2 shrink-0">
                  <button
                    type="button"
                    disabled={autoCreateAccount.isPending}
                    onClick={() => autoCreateAccount.mutate()}
                    className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-semibold bg-amber-400 hover:bg-amber-300 text-zinc-950 transition-colors disabled:opacity-50"
                    title="Automatically register a free account on arcod.xyz"
                  >
                    <IconRefresh className={`w-3.5 h-3.5 ${autoCreateAccount.isPending ? 'animate-spin' : ''}`} />
                    <span>{autoCreateAccount.isPending ? 'Creating…' : 'Auto-Create Free Account'}</span>
                  </button>

                  <button
                    type="button"
                    onClick={() => setAccountModalOpen(true)}
                    className="inline-flex items-center gap-1 px-3 py-1.5 rounded-xl text-xs font-semibold bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 transition-colors"
                  >
                    <span>+ Add Account</span>
                  </button>
                </div>
              </div>

              {arcodAccounts.length === 0 ? (
                <div className="rounded-xl bg-zinc-950/60 border border-dashed border-white/[0.1] p-6 text-center space-y-2">
                  <p className="text-sm font-medium text-zinc-300">No Arcod accounts configured</p>
                  <p className="text-xs text-zinc-500 max-w-md mx-auto">
                    Click &quot;Auto-Create Free Account&quot; to register a free account on arcod.xyz instantly, or &quot;Add Account&quot; to configure your own credentials.
                  </p>
                </div>
              ) : (
                <div className="space-y-2">
                  {arcodAccounts.map((acc) => {
                    const isRateLimited = acc.status === 'rate_limited';
                    const isError = acc.status === 'error';
                    const isTesting = testingAccountId === acc.id;

                    return (
                      <div
                        key={acc.id}
                        className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-zinc-950/60 border border-white/[0.06] rounded-xl p-3"
                      >
                        <div className="min-w-0 space-y-1">
                          <div className="flex items-center gap-2 flex-wrap">
                            <span className="text-sm font-medium text-zinc-100 truncate">{acc.email}</span>
                            {isRateLimited ? (
                              <span className="px-2 py-0.5 rounded-md text-[10px] font-semibold bg-amber-500/20 text-amber-300 border border-amber-500/30">
                                Rate Limited ({acc.cooldown_seconds}s cooldown)
                              </span>
                            ) : isError ? (
                              <span className="px-2 py-0.5 rounded-md text-[10px] font-semibold bg-rose-500/20 text-rose-300 border border-rose-500/30">
                                Auth Error
                              </span>
                            ) : (
                              <span className="px-2 py-0.5 rounded-md text-[10px] font-semibold bg-emerald-500/20 text-emerald-300 border border-emerald-500/30">
                                Active
                              </span>
                            )}
                          </div>
                          <div className="flex items-center gap-2 text-[11px] text-zinc-400">
                            <span>Password: ••••••••</span>
                            <span>·</span>
                            <span>Token: {acc.has_token ? 'Verified' : 'Pending'}</span>
                            {acc.last_error && isError && (
                              <>
                                <span>·</span>
                                <span className="text-rose-400 truncate max-w-xs">{acc.last_error}</span>
                              </>
                            )}
                          </div>
                        </div>

                        <div className="flex items-center gap-1.5 self-end sm:self-auto shrink-0">
                          <button
                            type="button"
                            disabled={isTesting}
                            onClick={() => testAccount.mutate(acc.id)}
                            className="px-2.5 py-1 rounded-lg text-xs font-medium bg-zinc-800 hover:bg-zinc-700 text-zinc-300 transition-colors disabled:opacity-50"
                          >
                            {isTesting ? 'Testing…' : 'Test'}
                          </button>
                          <button
                            type="button"
                            onClick={() => setEditingAccount(acc)}
                            className="px-2.5 py-1 rounded-lg text-xs font-medium bg-zinc-800 hover:bg-zinc-700 text-zinc-300 transition-colors"
                          >
                            Edit
                          </button>
                          <button
                            type="button"
                            onClick={() => setAccountToDelete(acc)}
                            className="p-1.5 rounded-lg text-zinc-400 hover:text-rose-400 hover:bg-rose-500/10 transition-colors"
                            title="Remove account"
                          >
                            <IconTrash className="w-4 h-4" />
                          </button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </SettingsSection>

          {showSection('downloads') && (
            <>
              <SettingsSection title="slskd (Soulseek)">
            <label className="flex items-center justify-between gap-3 bg-zinc-950/60 border border-white/[0.06] rounded-xl px-3.5 py-3 mb-3 cursor-pointer">
              <div>
                <p className="text-sm font-medium text-zinc-100">Enable slskd</p>
                <p className="text-[11px] text-zinc-400">
                  Allow peer-to-peer Soulseek searches and transfers via slskd
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={slskdEnabled}
                onClick={() =>
                  setForm({ ...form, slskd_enabled: slskdEnabled ? 'false' : 'true' })
                }
                className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                  slskdEnabled ? 'bg-emerald-500' : 'bg-zinc-700'
                }`}
              >
                <span
                  className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
                    slskdEnabled ? 'translate-x-[22px]' : 'translate-x-0.5'
                  }`}
                />
              </button>
            </label>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {SLSKD_FIELDS.map((field) => (
                <SettingInput
                  key={field.key}
                  field={field}
                  value={form[field.key] || ''}
                  onChange={(v) => setForm({ ...form, [field.key]: v })}
                />
              ))}
            </div>
          </SettingsSection>

          <SettingsSection
            title="Quality Tiers"
            description="Priority order for candidate scoring. Higher priority tiers always win; the upgrade scanner checks one artist per day."
          >
            <div className="space-y-2">
              {tiers.map((tier, i) => (
                <div
                  key={i}
                  className="flex items-center gap-3 bg-zinc-950/60 border border-white/[0.06] rounded-xl px-3.5 py-2.5"
                >
                  <span className="text-xs font-mono text-zinc-500 w-5 text-right shrink-0 tabular-nums">
                    #{i + 1}
                  </span>
                  <div className="flex-1 min-w-0 space-y-1.5">
                    <input
                      value={tier.label}
                      onChange={(e) => {
                        const updated = [...tiers];
                        updated[i] = { ...tier, label: e.target.value };
                        setTiers(updated);
                      }}
                      className="bg-transparent text-sm font-medium text-zinc-100 w-full outline-none"
                    />
                    <div className="flex flex-wrap gap-2">
                      <select
                        value={tier.format}
                        onChange={(e) => {
                          const updated = [...tiers];
                          const fmt = e.target.value;
                          const lossless = fmt === 'flac' || fmt === 'wav';
                          const bitrate = lossless ? undefined : tier.min_bitrate || 256;
                          updated[i] = {
                            ...tier,
                            format: fmt,
                            min_bitrate: bitrate,
                            label: tierLabel(fmt, bitrate),
                          };
                          setTiers(updated);
                        }}
                        className="bg-zinc-800 border border-white/[0.08] rounded-lg px-2.5 py-1 text-xs text-zinc-200 outline-none"
                      >
                        <option value="flac">FLAC</option>
                        <option value="mp3">MP3</option>
                        <option value="ogg">OGG</option>
                        <option value="opus">Opus</option>
                        <option value="aac">AAC</option>
                        <option value="m4a">M4A</option>
                        <option value="wav">WAV</option>
                      </select>
                      {tier.format !== 'flac' && tier.format !== 'wav' && (
                        <select
                          value={tier.min_bitrate || 256}
                          onChange={(e) => {
                            const updated = [...tiers];
                            const bitrate = Number(e.target.value);
                            updated[i] = {
                              ...tier,
                              min_bitrate: bitrate,
                              label: tierLabel(tier.format, bitrate),
                            };
                            setTiers(updated);
                          }}
                          className="bg-zinc-800 border border-white/[0.08] rounded-lg px-2.5 py-1 text-xs text-zinc-200 outline-none"
                        >
                          <option value={320}>320 kbps+</option>
                          <option value={256}>256 kbps+</option>
                          <option value={192}>192 kbps+</option>
                          <option value={128}>128 kbps+</option>
                        </select>
                      )}
                    </div>
                  </div>
                  <div className="flex flex-col gap-1 shrink-0">
                    <button
                      type="button"
                      onClick={() => moveTier(i, -1)}
                      disabled={i === 0}
                      className="p-1 text-zinc-400 hover:text-white disabled:opacity-20 transition-colors"
                      aria-label="Move tier up"
                    >
                      <svg
                        className="w-3.5 h-3.5"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      >
                        <path d="m18 15-6-6-6 6" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      onClick={() => moveTier(i, 1)}
                      disabled={i === tiers.length - 1}
                      className="p-1 text-zinc-400 hover:text-white disabled:opacity-20 transition-colors"
                      aria-label="Move tier down"
                    >
                      <svg
                        className="w-3.5 h-3.5"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      >
                        <path d="m6 9 6 6 6-6" />
                      </svg>
                    </button>
                  </div>
                  <button
                    type="button"
                    onClick={() => removeTier(i)}
                    className="p-1.5 text-zinc-500 hover:text-rose-400 transition-colors shrink-0"
                    aria-label="Remove tier"
                  >
                    <IconClose className="w-3.5 h-3.5" />
                  </button>
                </div>
              ))}
            </div>

            <div className="flex flex-wrap items-center justify-between gap-3 mt-3">
              <button
                type="button"
                onClick={addTier}
                className="px-3.5 py-2 bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 rounded-xl text-xs font-medium transition-colors"
              >
                + Add Quality Tier
              </button>

              <label className="flex items-center gap-2.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={form.quality_fallback_enabled !== 'false'}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      quality_fallback_enabled: e.target.checked ? 'true' : 'false',
                    })
                  }
                  className="w-4 h-4 rounded bg-zinc-800 border-zinc-600 accent-amber-400"
                />
                <span className="text-xs text-zinc-300">
                  Allow fallback formats outside configured tiers
                </span>
              </label>
            </div>
          </SettingsSection>

          <SettingsSection
            title="Negative Keywords"
            description="Auto-downloads skip files containing these keywords; manual search keeps them visible but dimmed."
          >
            <div className="flex flex-wrap gap-1.5 mb-3">
              {negativeKeywords.map((kw, i) => (
                <span
                  key={i}
                  className="inline-flex items-center gap-1.5 bg-zinc-950/70 border border-white/[0.08] rounded-lg px-2.5 py-1 text-xs text-zinc-200"
                >
                  {kw}
                  <button
                    type="button"
                    onClick={() =>
                      setNegativeKeywords(negativeKeywords.filter((_, j) => j !== i))
                    }
                    className="text-zinc-500 hover:text-rose-400 transition-colors"
                    aria-label={`Remove ${kw}`}
                  >
                    <IconClose className="w-3 h-3" />
                  </button>
                </span>
              ))}
              {negativeKeywords.length === 0 && (
                <p className="text-xs text-zinc-500">No negative keywords configured.</p>
              )}
            </div>
            <div className="flex gap-2">
              <input
                type="text"
                value={newKeyword}
                onChange={(e) => setNewKeyword(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && newKeyword.trim()) {
                    e.preventDefault();
                    const kw = newKeyword.trim().toLowerCase();
                    if (!negativeKeywords.includes(kw)) {
                      setNegativeKeywords([...negativeKeywords, kw]);
                    }
                    setNewKeyword('');
                  }
                }}
                placeholder="e.g. acapella, karaoke, instrumental"
                className="flex-1 bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2 text-base md:text-sm text-zinc-100 placeholder-zinc-600 outline-none focus:border-amber-500/50"
              />
              <button
                type="button"
                onClick={() => {
                  const kw = newKeyword.trim().toLowerCase();
                  if (kw && !negativeKeywords.includes(kw)) {
                    setNegativeKeywords([...negativeKeywords, kw]);
                  }
                  setNewKeyword('');
                }}
                className="px-4 py-2 bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 rounded-xl text-xs font-medium transition-colors"
              >
                + Add
              </button>
            </div>
          </SettingsSection>
            </>
          )}
        </div>
      )}

      {/* Integrations */}
      {showSection('integrations') && (
        <div className="space-y-5">
          {INTEGRATION_SECTIONS.map((section) => (
            <SettingsSection key={section.title} title={section.title}>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                {section.fields.map((field) => (
                  <SettingInput
                    key={field.key}
                    field={field}
                    value={form[field.key] || ''}
                    onChange={(v) => setForm({ ...form, [field.key]: v })}
                  />
                ))}
              </div>
            </SettingsSection>
          ))}
        </div>
      )}

      {/* Scheduling & Maintenance */}
      {showSection('maintenance') && (
        <div className="space-y-5">
          <SettingsSection title="Scheduling & Retention">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {SCHEDULING_FIELDS.map((field) => (
                <SettingInput
                  key={field.key}
                  field={field}
                  value={form[field.key] || ''}
                  onChange={(v) => setForm({ ...form, [field.key]: v })}
                />
              ))}
            </div>
          </SettingsSection>

          <SettingsSection title="Blocked Sources & Cache">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <button
                type="button"
                onClick={() => navigate('/settings/blocked')}
                className="flex items-center justify-between bg-zinc-950/60 hover:bg-zinc-900 border border-white/[0.07] rounded-xl px-4 py-3.5 text-left transition-colors"
              >
                <div>
                  <p className="text-sm font-medium text-zinc-100">Manage Blocked Sources</p>
                  <p className="text-[11px] text-zinc-500 mt-0.5">
                    Review temporary shadow bans and blacklisted files
                  </p>
                </div>
                <IconChevronRight className="w-4 h-4 text-zinc-500 shrink-0" />
              </button>

              <button
                type="button"
                onClick={() => clearCache.mutate()}
                disabled={clearCache.isPending}
                className="flex items-center justify-between bg-zinc-950/60 hover:bg-zinc-900 border border-white/[0.07] rounded-xl px-4 py-3.5 text-left transition-colors disabled:opacity-50"
              >
                <div>
                  <p className="text-sm font-medium text-zinc-100">
                    {clearCache.isPending ? 'Clearing Cache…' : 'Clear Provider Cache'}
                  </p>
                  <p className="text-[11px] text-zinc-500 mt-0.5">
                    Force fresh search & discography lookups on next query
                  </p>
                </div>
              </button>
            </div>
          </SettingsSection>
        </div>
      )}

      {/* Bottom Save Action */}
      <div className="flex flex-wrap items-center justify-between gap-3 pt-2">
        {status?.version && (
          <p className="text-xs font-mono text-zinc-600">Crate {status.version}</p>
        )}
        <button
          type="button"
          onClick={() => save.mutate()}
          disabled={save.isPending}
          className="px-6 py-2.5 bg-amber-400 hover:bg-amber-300 text-zinc-950 rounded-xl font-semibold text-sm active:scale-[0.98] transition-all disabled:opacity-50 ml-auto"
        >
          {saved ? '✓ Saved' : save.isPending ? 'Saving…' : 'Save Settings'}
        </button>
      </div>

      {/* Floating Unsaved Changes Bar */}
      {isDirty && (
        <div className="fixed bottom-20 left-4 right-4 md:bottom-6 md:left-72 md:right-8 z-30 max-w-4xl mx-auto">
          <div className="flex items-center justify-between gap-3 bg-zinc-900/95 backdrop-blur-md border border-amber-500/35 rounded-2xl px-4 py-3 shadow-2xl animate-fade-in">
            <p className="text-xs sm:text-sm font-medium text-amber-200">
              You have unsaved changes
            </p>
            <div className="flex items-center gap-2 shrink-0">
              <button
                type="button"
                onClick={discardChanges}
                className="px-3 py-1.5 rounded-xl bg-zinc-800 hover:bg-zinc-700 text-xs font-medium text-zinc-300 transition-colors"
              >
                Discard
              </button>
              <button
                type="button"
                onClick={() => save.mutate()}
                disabled={save.isPending}
                className="px-4 py-1.5 rounded-xl bg-amber-400 hover:bg-amber-300 text-zinc-950 text-xs font-semibold transition-colors disabled:opacity-50"
              >
                {save.isPending ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Arcod Account Add/Edit Modal */}
      <ArcodAccountModal
        open={accountModalOpen || !!editingAccount}
        account={editingAccount}
        loading={addAccount.isPending || updateAccount.isPending}
        onSave={(data) => {
          if (editingAccount) {
            updateAccount.mutate({ id: editingAccount.id, data });
          } else {
            addAccount.mutate({ email: data.email, password: data.password || '', token: data.token });
          }
        }}
        onClose={() => {
          setAccountModalOpen(false);
          setEditingAccount(null);
        }}
      />

      {/* Arcod Account Delete Modal */}
      <ConfirmModal
        open={!!accountToDelete}
        title="Remove Arcod Account?"
        description={
          accountToDelete
            ? `Remove ${accountToDelete.email} from the Arcod account pool? It will no longer be used for downloads.`
            : ''
        }
        confirmLabel="Remove Account"
        loading={deleteAccount.isPending}
        onConfirm={() => {
          if (accountToDelete) deleteAccount.mutate(accountToDelete.id);
        }}
        onCancel={() => setAccountToDelete(null)}
      />
    </div>
  );
}

function ArcodAccountModal({
  open,
  account,
  loading,
  onSave,
  onClose,
}: {
  open: boolean;
  account: ArcodAccount | null;
  loading: boolean;
  onSave: (data: { email: string; password?: string; token?: string }) => void;
  onClose: () => void;
}) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [token, setToken] = useState('');

  useEffect(() => {
    if (account) {
      setEmail(account.email || '');
      setPassword('');
      setToken('');
    } else {
      setEmail('');
      setPassword('');
      setToken('');
    }
  }, [account, open]);

  if (!open) return null;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!email.trim()) return;
    const data: { email: string; password?: string; token?: string } = {
      email: email.trim(),
    };
    if (password.trim()) data.password = password.trim();
    if (token.trim()) data.token = token.trim();
    onSave(data);
  };

  return (
    <div
      className="fixed inset-0 z-[95] flex items-end md:items-center justify-center md:p-4"
      role="dialog"
      aria-modal="true"
      onClick={() => !loading && onClose()}
    >
      <div className="absolute inset-0 bg-black/70 backdrop-blur-xs" />
      <div
        className="relative w-full md:max-w-md bg-zinc-900 border border-white/[0.08] rounded-t-2xl md:rounded-2xl shadow-2xl animate-slide-up md:animate-modal-in p-5 pb-[calc(1.25rem+env(safe-area-inset-bottom,0px))] md:pb-5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between pb-3 border-b border-white/[0.06]">
          <h3 className="text-base font-semibold text-zinc-100">
            {account ? 'Edit Arcod Account' : 'Add Arcod Account'}
          </h3>
          <button
            type="button"
            onClick={onClose}
            disabled={loading}
            className="text-zinc-500 hover:text-zinc-200 p-1 rounded-lg transition-colors"
          >
            <IconClose className="w-4 h-4" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4 pt-4">
          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">
              Account Email
            </label>
            <input
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@example.com"
              className="w-full bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-600 outline-none focus:border-amber-500/50"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">
              Account Password {account && <span className="text-zinc-500 font-normal">(leave blank to keep existing)</span>}
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              className="w-full bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-600 outline-none focus:border-amber-500/50"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">
              Bearer Token (Optional)
            </label>
            <input
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="Bearer token if known"
              className="w-full bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-600 outline-none focus:border-amber-500/50"
            />
            <p className="text-[11px] text-zinc-500 mt-1">
              If left blank, Crate will automatically fetch a token using your email and password.
            </p>
          </div>

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              disabled={loading}
              className="px-4 py-2 rounded-xl text-sm font-medium bg-zinc-800 hover:bg-zinc-700 text-zinc-300 transition-colors disabled:opacity-50"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={loading || !email.trim()}
              className="px-5 py-2 rounded-xl text-sm font-semibold bg-amber-400 hover:bg-amber-300 text-zinc-950 transition-colors disabled:opacity-50"
            >
              {loading ? 'Saving…' : account ? 'Update Account' : 'Add Account'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function SettingInput({
  field,
  value,
  onChange,
}: {
  field: FieldDef;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div>
      <label className="block text-xs font-medium text-zinc-300 mb-1.5">{field.label}</label>
      <input
        type={field.type || 'text'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={field.placeholder}
        className="w-full bg-zinc-950 border border-white/[0.08] rounded-xl px-3.5 py-2.5 text-base md:text-sm text-zinc-100 placeholder-zinc-600 outline-none focus:border-amber-500/50 transition-colors"
      />
      {field.description && (
        <p className="text-[11px] text-zinc-500 mt-1 leading-relaxed">{field.description}</p>
      )}
    </div>
  );
}

function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-2xl bg-zinc-900/65 border border-white/[0.06] p-4 md:p-5 space-y-3.5">
      <div>
        <h3 className="text-sm font-semibold text-zinc-100">{title}</h3>
        {description && <p className="text-xs text-zinc-400 mt-0.5">{description}</p>}
      </div>
      {children}
    </section>
  );
}

function Stat({ label, value, sub }: { label: string; value: number; sub?: string }) {
  return (
    <div className="rounded-xl bg-zinc-900/70 border border-white/[0.06] p-3.5">
      <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-500">{label}</p>
      <p className="text-xl font-bold tabular-nums text-zinc-100 mt-1">
        {value.toLocaleString()}
      </p>
      {sub && <p className="text-[11px] text-zinc-500 mt-0.5">{sub}</p>}
    </div>
  );
}

function MiniReportStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg bg-zinc-900 border border-white/[0.05] px-2.5 py-2">
      <p className="text-base font-bold tabular-nums text-zinc-100">{value}</p>
      <p className="text-[10px] text-zinc-500">{label}</p>
    </div>
  );
}
