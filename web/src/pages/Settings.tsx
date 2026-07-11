import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';

interface QualityTier {
  format: string;
  min_bitrate?: number;
  label: string;
}

function tierLabel(format: string, bitrate?: number): string {
  const name = format.toUpperCase();
  if (format === 'flac' || format === 'wav') return name;
  return bitrate ? `${name} ${bitrate}kbps` : name;
}

const settingsSections = [
  {
    title: 'slskd',
    fields: [
      { key: 'max_concurrent_slskd', label: 'Max Concurrent Downloads', placeholder: '10', type: 'number', description: 'How many files to download from Soulseek at the same time' },
      { key: 'shadow_ban_duration_minutes', label: 'Shadow Ban Duration (minutes)', placeholder: '60', type: 'number', description: 'How long to temporarily block a user after failed or stale downloads' },
    ],
  },
  {
    title: 'Navidrome (optional)',
    fields: [
      { key: 'navidrome_url', label: 'URL', placeholder: 'http://localhost:4533', description: 'Triggers a library scan after each download' },
      { key: 'navidrome_user', label: 'Username', placeholder: 'admin' },
      { key: 'navidrome_password', label: 'Password', placeholder: 'Navidrome password', type: 'password' },
    ],
  },
  {
    title: 'Music Assistant (optional)',
    fields: [
      { key: 'music_assistant_url', label: 'URL', placeholder: 'http://localhost:8095', description: 'Syncs the library after each download and watches the reject playlist (restart to connect)' },
      { key: 'music_assistant_token', label: 'API Token', placeholder: 'Long-lived MA token', type: 'password' },
      { key: 'music_assistant_reject_playlist', label: 'Reject Playlist', placeholder: 'Crate Reject', description: 'Add a track to this MA playlist to mark it bad — auto-created if missing' },
    ],
  },
  {
    title: 'Scheduling',
    fields: [
      { key: 'max_auto_queue', label: 'Max Auto-Queue Per Cycle', placeholder: '50', type: 'number', description: 'Limit how many wanted tracks the scheduler queues per cycle' },
      { key: 'requeue_cooldown_days', label: 'Re-queue Cooldown (days)', placeholder: '7', type: 'number', description: 'Wait this many days before retrying a previously failed track' },
      { key: 'cache_ttl_hours', label: 'Cache TTL (hours)', placeholder: '24', type: 'number', description: 'How long provider search results are cached' },
      { key: 'activity_retention_days', label: 'Activity Retention (days)', placeholder: '30', type: 'number', description: 'Delete activity log entries older than this' },
    ],
  },
];

export default function Settings() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const [form, setForm] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);
  const [tiers, setTiers] = useState<QualityTier[]>([]);
  const [negativeKeywords, setNegativeKeywords] = useState<string[]>([]);
  const [newKeyword, setNewKeyword] = useState('');
  const [namingPreview, setNamingPreview] = useState<{ path?: string; error?: string }>({});

  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: api.getSettings,
  });

  const { data: status } = useQuery({
    queryKey: ['status'],
    queryFn: api.getStatus,
  });

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: api.listProviders,
    refetchInterval: 30_000,
  });

  useEffect(() => {
    if (settings) {
      setForm(settings);
      if (settings.quality_tiers) {
        try {
          setTiers(JSON.parse(settings.quality_tiers));
        } catch { /* ignore */ }
      }
      if (settings.negative_keywords) {
        try {
          setNegativeKeywords(JSON.parse(settings.negative_keywords));
        } catch { /* ignore */ }
      }
    }
  }, [settings]);

  // Debounced live preview of the naming template (empty = server default).
  useEffect(() => {
    const handle = setTimeout(() => {
      api.namingPreview(form.naming_template ?? '')
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
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['library-import'] }),
    onError: (err: Error) => toast(err.message, 'error'),
  });

  // Refresh library stats once a real import lands.
  const importDone = importStatus.data?.status === 'done' && !importStatus.data.dry_run;
  useEffect(() => {
    if (importDone) {
      queryClient.invalidateQueries({ queryKey: ['status'] });
    }
  }, [importDone, queryClient]);

  const save = useMutation({
    mutationFn: () => api.updateSettings({
      ...form,
      quality_tiers: JSON.stringify(tiers),
      quality_fallback_enabled: form.quality_fallback_enabled ?? 'true',
      negative_keywords: JSON.stringify(negativeKeywords),
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] });
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const moveTier = useCallback((index: number, direction: -1 | 1) => {
    const next = index + direction;
    if (next < 0 || next >= tiers.length) return;
    const updated = [...tiers];
    [updated[index], updated[next]] = [updated[next], updated[index]];
    setTiers(updated);
  }, [tiers]);

  const removeTier = useCallback((index: number) => {
    setTiers(tiers.filter((_, i) => i !== index));
  }, [tiers]);

  const addTier = useCallback(() => {
    setTiers([...tiers, { format: 'mp3', min_bitrate: 256, label: 'MP3 256kbps' }]);
  }, [tiers]);

  const clearCache = useMutation({
    mutationFn: () => api.clearCache(),
    onSuccess: () => toast('Cache cleared', 'success'),
    onError: (err: Error) => toast(err.message, 'error'),
  });

  return (
    <div>
      <h2 className="text-lg font-bold mb-4">Settings</h2>

      {status && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6">
          <Stat label="Artists" value={status.artists_count} />
          <Stat label="Tracks" value={status.total_tracks ?? 0} />
          <Stat label="Pending" value={status.pending_downloads} />
          <Stat label="Active" value={status.active_downloads} />
        </div>
      )}

      {providers && providers.length > 0 && (
        <SettingsSection title="Providers">
          <div className="space-y-2 mb-4">
            {providers.map((p) => (
              <div key={p.name} className="flex items-center gap-3 bg-zinc-800/50 rounded-lg px-3 py-2.5">
                <span className={`w-2 h-2 rounded-full shrink-0 ${p.healthy ? 'bg-green-500' : 'bg-red-500'}`} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium">{p.display_name}</p>
                  <p className="text-[11px] text-zinc-500">v{p.version} · {p.address}</p>
                </div>
                <span className={`text-[10px] font-medium uppercase ${p.healthy ? 'text-green-400' : 'text-red-400'}`}>
                  {p.healthy ? 'healthy' : 'offline'}
                </span>
              </div>
            ))}
          </div>

          <div>
            <label className="block text-xs text-zinc-400 mb-1">Default Provider</label>
            <select
              value={form.provider_primary || ''}
              onChange={(e) => setForm({ ...form, provider_primary: e.target.value })}
              className="w-full bg-zinc-800 rounded-lg px-3 py-2.5 text-sm outline-none focus:ring-2 focus:ring-zinc-600"
            >
              <option value="">Default (musicbrainz)</option>
              {providers.map((p) => (
                <option key={p.name} value={p.name}>{p.display_name}</option>
              ))}
            </select>
          </div>
        </SettingsSection>
      )}

      <SettingsSection title="Library">
        <label className="block text-xs text-zinc-400 mb-1">Naming Template</label>
        <input
          type="text"
          value={form.naming_template || ''}
          onChange={(e) => setForm({ ...form, naming_template: e.target.value })}
          placeholder="{artist}/{album} ({year})/{track:2} - {title}"
          spellCheck={false}
          className="w-full bg-zinc-800 rounded-lg px-3 py-2.5 text-sm font-mono placeholder-zinc-600 outline-none focus:ring-2 focus:ring-zinc-600"
        />
        <p className="text-[11px] text-zinc-600 mt-1">
          Folder and file layout for new downloads. Tokens: {'{artist}'}, {'{albumartist}'}, {'{album}'}, {'{year}'}, {'{track}'}, {'{disc}'}, {'{title}'}.
          Zero-pad numbers with a width, e.g. {'{track:2}'}. The file extension is added automatically.
          Leave blank for the default. Existing files are not renamed.
        </p>
        <div className={`mt-2 rounded-lg px-3 py-2 text-[11px] font-mono break-all ${namingPreview.error ? 'bg-red-950/40 text-red-400' : 'bg-zinc-800/50 text-zinc-400'}`}>
          {namingPreview.error ?? namingPreview.path ?? '…'}
        </div>

        <div className="mt-5 pt-4 border-t border-zinc-800">
          <p className="text-xs text-zinc-400 mb-1">Import Existing Library</p>
          <p className="text-[11px] text-zinc-600 mb-3">
            Scans the embedded tags of MP3/FLAC files in your library folder and records what you
            already own. Files are never moved, renamed, or modified — folder structure doesn't
            matter, only tags. MusicBrainz-tagged files (Picard, beets) link to MusicBrainz
            automatically; everything else can be relinked to a provider later. Run a scan first
            to preview what would happen.
          </p>

          {importStatus.data?.status === 'running' && (
            <div className="mb-3 rounded-lg bg-zinc-800/50 px-3 py-2 text-[11px] text-zinc-400">
              {importStatus.data.dry_run ? 'Scanning' : 'Importing'}… {importStatus.data.processed}
              {importStatus.data.total > 0 && ` / ${importStatus.data.total}`} files
            </div>
          )}

          {importStatus.data?.status === 'failed' && (
            <div className="mb-3 rounded-lg bg-red-950/40 px-3 py-2 text-[11px] text-red-400">
              Import failed: {importStatus.data.error}
            </div>
          )}

          {importStatus.data?.status === 'done' && importStatus.data.report && (
            <div className="mb-3 rounded-lg bg-zinc-800/50 px-3 py-2.5 text-[11px] text-zinc-400 space-y-1">
              <p className="text-zinc-300 font-medium">
                {importStatus.data.dry_run ? 'Scan result (nothing was changed):' : 'Import complete:'}
              </p>
              <p>
                {importStatus.data.report.artists_added} artists · {importStatus.data.report.albums_added} albums · {importStatus.data.report.tracks_added} tracks
                {importStatus.data.dry_run ? ' would be added' : ' added'}
              </p>
              {importStatus.data.report.tracks_claimed > 0 && (
                <p>{importStatus.data.report.tracks_claimed} wanted tracks matched files you already have</p>
              )}
              {importStatus.data.report.musicbrainz_linked > 0 && (
                <p>{importStatus.data.report.musicbrainz_linked} tracks linked to MusicBrainz via tags</p>
              )}
              {importStatus.data.report.tracks_known > 0 && (
                <p>{importStatus.data.report.tracks_known} already known</p>
              )}
              {importStatus.data.report.duplicate_files > 0 && (
                <p>{importStatus.data.report.duplicate_files} duplicate files ignored</p>
              )}
              {importStatus.data.report.files_skipped > 0 && (
                <div>
                  <p className="text-amber-400/80">{importStatus.data.report.files_skipped} files skipped</p>
                  {importStatus.data.report.skipped_samples?.slice(0, 5).map((sf, i) => (
                    <p key={i} className="font-mono text-[10px] text-zinc-600 truncate">
                      {sf.path} — {sf.reason}
                    </p>
                  ))}
                </div>
              )}
            </div>
          )}

          <div className="flex gap-2">
            <button
              onClick={() => runImport.mutate(true)}
              disabled={runImport.isPending || importStatus.data?.status === 'running'}
              className="px-3 py-2 bg-zinc-800 text-zinc-300 rounded-lg text-xs font-medium active:bg-zinc-700 transition-colors disabled:opacity-50"
            >
              Scan (Dry Run)
            </button>
            <button
              onClick={() => runImport.mutate(false)}
              disabled={runImport.isPending || importStatus.data?.status === 'running'}
              className="px-3 py-2 bg-zinc-800 text-zinc-300 rounded-lg text-xs font-medium active:bg-zinc-700 transition-colors disabled:opacity-50"
            >
              Import Library
            </button>
          </div>
        </div>
      </SettingsSection>

      {settingsSections.map((section) => (
        <SettingsSection key={section.title} title={section.title}>
          <div className="space-y-3">
            {section.fields.map(({ key, label, placeholder, type, description }) => (
              <div key={key}>
                <label className="block text-xs text-zinc-400 mb-1">{label}</label>
                <input
                  type={type || 'text'}
                  value={form[key] || ''}
                  onChange={(e) => setForm({ ...form, [key]: e.target.value })}
                  placeholder={placeholder}
                  className="w-full bg-zinc-800 rounded-lg px-3 py-2.5 text-sm placeholder-zinc-600 outline-none focus:ring-2 focus:ring-zinc-600"
                />
                {description && <p className="text-[11px] text-zinc-600 mt-1">{description}</p>}
              </div>
            ))}
          </div>
        </SettingsSection>
      ))}

      <SettingsSection title="Quality Tiers">
        <p className="text-[11px] text-zinc-500 mb-3">
          Priority order for downloads. Higher = preferred. The upgrade scanner checks one artist per day.
        </p>
        <div className="space-y-1.5">
          {tiers.map((tier, i) => (
            <div key={i} className="flex items-center gap-2 bg-zinc-800/50 rounded-lg px-3 py-2">
              <span className="text-[11px] text-zinc-500 w-5 text-right shrink-0 tabular-nums">{i + 1}</span>
              <div className="flex-1 min-w-0">
                <input
                  value={tier.label}
                  onChange={(e) => {
                    const updated = [...tiers];
                    updated[i] = { ...tier, label: e.target.value };
                    setTiers(updated);
                  }}
                  className="bg-transparent text-sm w-full outline-none"
                />
                <div className="flex gap-2 mt-1">
                  <select
                    value={tier.format}
                    onChange={(e) => {
                      const updated = [...tiers];
                      const fmt = e.target.value;
                      const lossless = fmt === 'flac' || fmt === 'wav';
                      const bitrate = lossless ? undefined : (tier.min_bitrate || 256);
                      updated[i] = { ...tier, format: fmt, min_bitrate: bitrate, label: tierLabel(fmt, bitrate) };
                      setTiers(updated);
                    }}
                    className="bg-zinc-700 rounded px-2 py-0.5 text-[11px] outline-none"
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
                        updated[i] = { ...tier, min_bitrate: bitrate, label: tierLabel(tier.format, bitrate) };
                        setTiers(updated);
                      }}
                      className="bg-zinc-700 rounded px-2 py-0.5 text-[11px] outline-none"
                    >
                      <option value={320}>320 kbps+</option>
                      <option value={256}>256 kbps+</option>
                      <option value={192}>192 kbps+</option>
                      <option value={128}>128 kbps+</option>
                    </select>
                  )}
                </div>
              </div>
              <div className="flex flex-col gap-0.5 shrink-0">
                <button
                  onClick={() => moveTier(i, -1)}
                  disabled={i === 0}
                  className="text-zinc-500 active:text-white disabled:opacity-20 transition-colors"
                >
                  <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="m18 15-6-6-6 6" />
                  </svg>
                </button>
                <button
                  onClick={() => moveTier(i, 1)}
                  disabled={i === tiers.length - 1}
                  className="text-zinc-500 active:text-white disabled:opacity-20 transition-colors"
                >
                  <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="m6 9 6 6 6-6" />
                  </svg>
                </button>
              </div>
              <button
                onClick={() => removeTier(i)}
                className="text-zinc-600 active:text-red-400 transition-colors shrink-0"
              >
                <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M18 6 6 18" /><path d="m6 6 12 12" />
                </svg>
              </button>
            </div>
          ))}
        </div>
        <button
          onClick={addTier}
          className="mt-2 px-3 py-1.5 bg-zinc-800 text-zinc-400 rounded-lg text-xs font-medium active:bg-zinc-700 transition-colors"
        >
          + Add tier
        </button>
        <label className="flex items-center gap-2 mt-3 cursor-pointer">
          <input
            type="checkbox"
            checked={form.quality_fallback_enabled !== 'false'}
            onChange={(e) => setForm({ ...form, quality_fallback_enabled: e.target.checked ? 'true' : 'false' })}
            className="w-4 h-4 rounded bg-zinc-800 border-zinc-600 accent-white"
          />
          <span className="text-sm text-zinc-300">Allow downloads outside configured tiers</span>
        </label>
        <p className="text-[11px] text-zinc-600 mt-1 ml-6">
          When disabled, only files matching a configured tier will be downloaded
        </p>
      </SettingsSection>

      <SettingsSection title="Negative Keywords">
        <p className="text-[11px] text-zinc-500 mb-3">
          Files matching these keywords are skipped during auto-download but still shown in manual search results.
        </p>
        <div className="flex flex-wrap gap-1.5 mb-3">
          {negativeKeywords.map((kw, i) => (
            <span key={i} className="inline-flex items-center gap-1 bg-zinc-800/50 rounded-lg px-2.5 py-1.5 text-sm">
              {kw}
              <button
                onClick={() => setNegativeKeywords(negativeKeywords.filter((_, j) => j !== i))}
                className="text-zinc-600 active:text-red-400 transition-colors"
              >
                <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M18 6 6 18" /><path d="m6 6 12 12" />
                </svg>
              </button>
            </span>
          ))}
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
            placeholder="e.g. acapella"
            className="flex-1 bg-zinc-800 rounded-lg px-3 py-2 text-sm placeholder-zinc-600 outline-none focus:ring-2 focus:ring-zinc-600"
          />
          <button
            onClick={() => {
              const kw = newKeyword.trim().toLowerCase();
              if (kw && !negativeKeywords.includes(kw)) {
                setNegativeKeywords([...negativeKeywords, kw]);
              }
              setNewKeyword('');
            }}
            className="px-3 py-2 bg-zinc-800 text-zinc-400 rounded-lg text-xs font-medium active:bg-zinc-700 transition-colors"
          >
            + Add
          </button>
        </div>
      </SettingsSection>

      <SettingsSection title="Blocked Sources">
        <button
          onClick={() => navigate('/settings/blocked')}
          className="w-full flex items-center justify-between bg-zinc-800/50 rounded-lg px-4 py-3 active:bg-zinc-700/50 transition-colors"
        >
          <div>
            <p className="text-sm text-zinc-300 text-left">Manage blocked sources</p>
            <p className="text-[11px] text-zinc-600 text-left">Shadow bans and blacklisted files</p>
          </div>
          <svg className="w-4 h-4 text-zinc-600 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="m9 18 6-6-6-6" />
          </svg>
        </button>
      </SettingsSection>

      <div className="flex gap-3 mt-6">
        <button
          onClick={() => save.mutate()}
          disabled={save.isPending}
          className="px-6 py-2.5 bg-white text-zinc-900 rounded-lg font-medium text-sm active:scale-[0.97] transition-transform disabled:opacity-50"
        >
          {saved ? 'Saved!' : save.isPending ? 'Saving...' : 'Save Settings'}
        </button>
        <button
          onClick={() => clearCache.mutate()}
          disabled={clearCache.isPending}
          className="px-6 py-2.5 bg-zinc-800 text-zinc-300 rounded-lg font-medium text-sm active:bg-zinc-700 transition-colors disabled:opacity-50"
        >
          {clearCache.isPending ? 'Clearing...' : 'Clear Cache'}
        </button>
      </div>

      {status?.version && (
        <p className="text-center text-xs text-zinc-600 mt-8">{status.version}</p>
      )}
    </div>
  );
}

function SettingsSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-6">
      <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider mb-2">{title}</p>
      {children}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="bg-zinc-800/50 rounded-lg p-4 text-center">
      <p className="text-2xl font-bold tabular-nums">{value}</p>
      <p className="text-xs text-zinc-500 mt-1">{label}</p>
    </div>
  );
}
