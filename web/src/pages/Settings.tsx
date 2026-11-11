import { useState, useEffect, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useSystemProcess } from '../context/SystemContext';
import { useToast } from '../components/Toast';
import { IconChevronRight, IconClose } from '../components/Icons';

interface QualityTier {
  format: string;
  min_bitrate?: number;
  label: string;
}

type SettingsTab = 'all' | 'providers' | 'library' | 'downloads' | 'integrations' | 'maintenance';

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
  {
    key: 'arcod_email',
    label: 'Account Email',
    placeholder: 'user@example.com',
    description: 'Your arcod.xyz email for authentication',
  },
  {
    key: 'arcod_password',
    label: 'Account Password',
    placeholder: 'Password',
    type: 'password',
  },
  {
    key: 'arcod_token',
    label: 'Auth Token (Optional)',
    placeholder: 'JWT Token',
    type: 'password',
    description: 'Direct Bearer token (auto-fetched if email/password provided)',
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
    key: 'requeue_cooldown_days',
    label: 'Re-queue Cooldown (days)',
    placeholder: '7',
    type: 'number',
    description: 'Wait this many days before retrying a previously failed track',
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
  const [negativeKeywords, setNegativeKeywords] = useState<string[]>([]);
  const [baselinePayload, setBaselinePayload] = useState<string>('');
  const [saved, setSaved] = useState(false);
  const [newKeyword, setNewKeyword] = useState('');
  const [namingPreview, setNamingPreview] = useState<{ path?: string; error?: string }>({});

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
      setTiers(parsedTiers);
      setNegativeKeywords(parsedKw);
      setBaselinePayload(
        JSON.stringify({
          ...settings,
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
        quality_tiers: JSON.stringify(tiers),
        quality_fallback_enabled: form.quality_fallback_enabled ?? 'true',
        negative_keywords: JSON.stringify(negativeKeywords),
      }),
    [form, tiers, negativeKeywords]
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
  const arcodEnabled = form.arcod_enabled === 'true';

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
            { id: 'providers', label: 'Providers' },
            { id: 'library', label: 'Library & Import' },
            { id: 'downloads', label: 'Downloaders & Quality' },
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
      {showSection('downloads') && (
        <div className="space-y-5">
          <SettingsSection
            title="Arcod (Primary FLAC Downloader)"
            description="Optional high-resolution FLAC source checked before falling back to Soulseek peers."
          >
            <label className="flex items-center justify-between gap-3 bg-zinc-950/60 border border-white/[0.06] rounded-xl px-3.5 py-3 mb-3 cursor-pointer">
              <div>
                <p className="text-sm font-medium text-zinc-100">Enable Arcod</p>
                <p className="text-[11px] text-zinc-400">
                  Check arcod.xyz for direct lossless FLAC before querying slskd
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
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {ARCOD_FIELDS.map((field) => (
                <SettingInput
                  key={field.key}
                  field={field}
                  value={form[field.key] || ''}
                  onChange={(v) => setForm({ ...form, [field.key]: v })}
                />
              ))}
            </div>
          </SettingsSection>

          <SettingsSection title="slskd (Soulseek)">
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
