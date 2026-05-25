import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import type { BlacklistEntry, UserCooldown } from '../types/index';

export default function BlockedSources() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();

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
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const removeCooldown = useMutation({
    mutationFn: (id: number) => api.deleteCooldown(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cooldowns'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const clearAllBlacklist = useMutation({
    mutationFn: () => api.clearBlacklist(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['blacklist'] });
      toast('Blacklist cleared', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const clearAllCooldowns = useMutation({
    mutationFn: () => api.clearCooldowns(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cooldowns'] });
      toast('Cooldowns cleared', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const hasCooldowns = cooldowns && cooldowns.length > 0;
  const hasBlacklist = blacklist && blacklist.length > 0;

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <button
          onClick={() => navigate('/settings')}
          className="text-zinc-500 active:text-white transition-colors"
        >
          <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="m15 18-6-6 6-6" />
          </svg>
        </button>
        <h2 className="text-lg font-bold">Blocked Sources</h2>
      </div>

      <p className="text-sm text-zinc-500 mb-6">
        Temporarily banned users (shadow bans) and permanently blocked files. Remove entries to allow downloads from these sources again.
      </p>

      {hasCooldowns && (
        <div className="mb-6">
          <div className="flex items-center justify-between mb-2">
            <p className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">Active Cooldowns ({cooldowns.length})</p>
            <button
              onClick={() => clearAllCooldowns.mutate()}
              disabled={clearAllCooldowns.isPending}
              className="text-[11px] text-zinc-500 active:text-red-400 transition-colors disabled:opacity-50"
            >
              {clearAllCooldowns.isPending ? 'Clearing...' : 'Clear All'}
            </button>
          </div>
          <div className="space-y-1.5">
            {cooldowns.map((c: UserCooldown) => (
              <div key={c.id} className="flex items-center gap-2 bg-zinc-800/50 rounded-lg px-3 py-2.5">
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate">{c.username}</p>
                  <p className="text-[11px] text-zinc-500 truncate">{c.reason}</p>
                  <p className="text-[11px] text-zinc-600">Expires: {new Date(c.expires_at).toLocaleString()}</p>
                </div>
                <button
                  onClick={() => removeCooldown.mutate(c.id)}
                  className="text-zinc-600 active:text-red-400 transition-colors shrink-0"
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M18 6 6 18" /><path d="m6 6 12 12" />
                  </svg>
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {hasBlacklist && (
        <div className="mb-6">
          <div className="flex items-center justify-between mb-2">
            <p className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">Blacklisted Files ({blacklist.length})</p>
            <button
              onClick={() => clearAllBlacklist.mutate()}
              disabled={clearAllBlacklist.isPending}
              className="text-[11px] text-zinc-500 active:text-red-400 transition-colors disabled:opacity-50"
            >
              {clearAllBlacklist.isPending ? 'Clearing...' : 'Clear All'}
            </button>
          </div>
          <div className="space-y-1.5">
            {blacklist.map((b: BlacklistEntry) => (
              <div key={b.id} className="flex items-center gap-2 bg-zinc-800/50 rounded-lg px-3 py-2.5">
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate">{b.username}</p>
                  <p className="text-[11px] text-zinc-500 truncate">{b.filename.split(/[/\\]/).pop()}</p>
                  <p className="text-[11px] text-zinc-600 truncate">{b.reason}</p>
                </div>
                <button
                  onClick={() => removeBlacklist.mutate(b.id)}
                  className="text-zinc-600 active:text-red-400 transition-colors shrink-0"
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M18 6 6 18" /><path d="m6 6 12 12" />
                  </svg>
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {!hasCooldowns && !hasBlacklist && (
        <div className="text-center py-12">
          <p className="text-zinc-600">No blocked sources</p>
        </div>
      )}
    </div>
  );
}
