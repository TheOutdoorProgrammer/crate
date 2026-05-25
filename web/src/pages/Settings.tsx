import { useState, useEffect, useCallback } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import type { BlacklistEntry, UserCooldown } from '../types/index';

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
  const { toast } = useToast();
  const [form, setForm] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);
  const [tiers, setTiers] = useState<QualityTier[]>([]);

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
    }
  }, [settings]);

  const save = useMutation({
    mutationFn: () => api.updateSettings({
      ...form,
      quality_tiers: JSON.stringify(tiers),
      quality_fallback_enabled: form.quality_fallback_enabled ?? 'true',
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] });
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    },
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

  const { data: blacklist } = useQuery({
    queryKey: ['blacklist'],
    queryFn: api.listBlacklist,
  });

  const { data: cooldowns } = useQuery({
    queryKey: ['cooldowns'],
    queryFn: api.listCooldowns,
    refetchInterval: 30_000,
  });

  const removeBlacklist = useMutation({
    mutationFn: (id: number) => api.deleteBlacklistEntry(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['blacklist'] });
      toast('Blacklist entry removed', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const removeCooldown = useMutation({
    mutationFn: (id: number) => api.deleteCooldown(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cooldowns'] });
      toast('Cooldown removed', 'success');
    },
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

      <SettingsSection title="Blocked Sources">
        <p className="text-[11px] text-zinc-500 mb-3">
          Temporarily banned users (shadow bans) and permanently blocked files. Remove entries to allow downloads from these sources again.
        </p>

        {cooldowns && cooldowns.length > 0 && (
          <div className="mb-4">
            <p className="text-xs text-zinc-400 mb-1.5">Active Cooldowns</p>
            <div className="space-y-1">
              {cooldowns.map((c: UserCooldown) => (
                <div key={c.id} className="flex items-center gap-2 bg-zinc-800/50 rounded-lg px-3 py-2">
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium truncate">{c.username}</p>
                    <p className="text-[11px] text-zinc-500 truncate">{c.reason}</p>
                    <p className="text-[11px] text-zinc-600">Expires: {new Date(c.expires_at).toLocaleString()}</p>
                  </div>
                  <button
                    onClick={() => removeCooldown.mutate(c.id)}
                    className="text-zinc-600 active:text-red-400 transition-colors shrink-0"
                  >
                    <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M18 6 6 18" /><path d="m6 6 12 12" />
                    </svg>
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}

        {blacklist && blacklist.length > 0 && (
          <div>
            <p className="text-xs text-zinc-400 mb-1.5">Blacklisted Files</p>
            <div className="space-y-1">
              {blacklist.map((b: BlacklistEntry) => (
                <div key={b.id} className="flex items-center gap-2 bg-zinc-800/50 rounded-lg px-3 py-2">
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium truncate">{b.username}</p>
                    <p className="text-[11px] text-zinc-500 truncate">{b.filename.split(/[/\\]/).pop()}</p>
                    <p className="text-[11px] text-zinc-600 truncate">{b.reason}</p>
                  </div>
                  <button
                    onClick={() => removeBlacklist.mutate(b.id)}
                    className="text-zinc-600 active:text-red-400 transition-colors shrink-0"
                  >
                    <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M18 6 6 18" /><path d="m6 6 12 12" />
                    </svg>
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}

        {(!cooldowns || cooldowns.length === 0) && (!blacklist || blacklist.length === 0) && (
          <p className="text-sm text-zinc-600">No blocked sources</p>
        )}
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
