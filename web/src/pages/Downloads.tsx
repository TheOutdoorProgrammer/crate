import { useState, useCallback } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { formatSpeed } from '../lib/format';
import type { DownloadQueueItem, ActivityLog } from '../types/index';

const ACTIVITY_PAGE_SIZE = 50;

export default function Downloads() {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<'queue' | 'activity'>('queue');

  const { data: downloads, isLoading } = useQuery({
    queryKey: ['downloads'],
    queryFn: () => api.listDownloads(),
    refetchInterval: 5000,
  });

  const hasActiveDownloads = downloads?.some(
    (d) => d.status === 'downloading' || d.status === 'searching' || d.status === 'pending' || d.status === 'organizing'
  ) ?? false;

  const { data: progress } = useQuery({
    queryKey: ['download-progress'],
    queryFn: api.getDownloadProgress,
    refetchInterval: hasActiveDownloads ? 2000 : false,
    enabled: hasActiveDownloads,
  });

  const [extraActivity, setExtraActivity] = useState<ActivityLog[]>([]);
  const [activityOffset, setActivityOffset] = useState(ACTIVITY_PAGE_SIZE);
  const [loadingMoreActivity, setLoadingMoreActivity] = useState(false);

  const { data: activityData } = useQuery({
    queryKey: ['activity'],
    queryFn: () => api.listActivity(ACTIVITY_PAGE_SIZE, 0),
    enabled: tab === 'activity',
    refetchInterval: 10_000,
  });

  const activityItems = [...(activityData?.items || []), ...extraActivity];
  const activityTotal = activityData?.total ?? 0;

  const loadMoreActivity = useCallback(async () => {
    if (loadingMoreActivity || activityOffset >= activityTotal) return;
    setLoadingMoreActivity(true);
    try {
      const data = await api.listActivity(ACTIVITY_PAGE_SIZE, activityOffset);
      setExtraActivity((prev) => [...prev, ...(data.items || [])]);
      setActivityOffset((prev) => prev + ACTIVITY_PAGE_SIZE);
    } finally {
      setLoadingMoreActivity(false);
    }
  }, [activityOffset, activityTotal, loadingMoreActivity]);

  const queue = useMutation({
    mutationFn: api.queueDownloads,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['downloads'] }),
  });

  const retry = useMutation({
    mutationFn: (id: number) => api.retryDownload(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['downloads'] }),
  });

  const dismiss = useMutation({
    mutationFn: (id: number) => api.deleteDownload(id),
    onMutate: async (id) => {
      await queryClient.cancelQueries({ queryKey: ['downloads'] });
      queryClient.setQueryData(['downloads'], (old: DownloadQueueItem[] | undefined) =>
        old?.filter((d) => d.id !== id)
      );
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['downloads'] }),
  });

  const clearByStatus = useMutation({
    mutationFn: (status: string) => api.clearDownloadsByStatus(status),
    onMutate: async (status) => {
      await queryClient.cancelQueries({ queryKey: ['downloads'] });
      queryClient.setQueryData(['downloads'], (old: DownloadQueueItem[] | undefined) =>
        old?.filter((d) => d.status !== status)
      );
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['downloads'] }),
  });

  const active = downloads?.filter((d) => d.status === 'downloading' || d.status === 'searching') ?? [];
  const pending = downloads?.filter((d) => d.status === 'pending') ?? [];
  const failed = downloads?.filter((d) => d.status === 'failed') ?? [];
  const complete = downloads?.filter((d) => d.status === 'complete') ?? [];

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-bold">Downloads</h2>
          <div className="flex bg-zinc-800 rounded-lg p-0.5">
            <button
              onClick={() => setTab('queue')}
              className={`px-3 py-1 rounded-md text-xs font-medium transition-colors ${
                tab === 'queue' ? 'bg-zinc-700 text-white' : 'text-zinc-400'
              }`}
            >
              Queue
            </button>
            <button
              onClick={() => setTab('activity')}
              className={`px-3 py-1 rounded-md text-xs font-medium transition-colors ${
                tab === 'activity' ? 'bg-zinc-700 text-white' : 'text-zinc-400'
              }`}
            >
              Activity
            </button>
          </div>
        </div>
        {tab === 'queue' && (
          <button
            onClick={() => queue.mutate()}
            disabled={queue.isPending}
            className="px-3 py-1.5 bg-white text-zinc-900 rounded-lg text-xs font-semibold active:scale-[0.97] transition-transform disabled:opacity-50"
          >
            {queue.isPending ? 'Queuing...' : queue.data ? `Queued ${queue.data.queued}` : 'Queue All Wanted'}
          </button>
        )}
      </div>

      {tab === 'queue' && (
        <>
          {isLoading && (
            <div className="space-y-1">
              {[...Array(3)].map((_, i) => (
                <div key={i} className="flex items-center gap-2.5 bg-zinc-800/40 rounded-lg px-3 py-2 h-14 animate-pulse" />
              ))}
            </div>
          )}

          {progress && progress.length > 0 && (
            <div className="mb-4 space-y-2">
              <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">In Progress</p>
              {progress.map((p, i) => (
                <div key={i} className="bg-zinc-800/40 rounded-lg px-3 py-2">
                  <div className="flex items-center justify-between mb-1.5">
                    <p className="text-sm truncate flex-1 min-w-0 mr-2">{p.filename}</p>
                    <span className="text-xs text-zinc-400 shrink-0 tabular-nums">{p.percent_complete.toFixed(0)}%</span>
                  </div>
                  <div className="h-1 bg-zinc-700 rounded-full overflow-hidden">
                    <div
                      className="h-full bg-blue-500 rounded-full transition-all duration-500"
                      style={{ width: `${p.percent_complete}%` }}
                    />
                  </div>
                  <p className="text-[11px] text-zinc-500 mt-1">{formatSpeed(p.average_speed_bps)} · {p.username}</p>
                </div>
              ))}
            </div>
          )}

          {!isLoading && !downloads?.length && (
            <div className="text-center py-16">
              <svg className="w-12 h-12 mx-auto text-zinc-700 mb-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline points="7 10 12 15 17 10" /><line x1="12" y1="15" x2="12" y2="3" />
              </svg>
              <p className="text-zinc-500">No downloads yet</p>
              <p className="text-zinc-600 text-sm mt-1.5">Hit "Queue All Wanted" to start downloading watched tracks</p>
            </div>
          )}

          {active.length > 0 && (
            <Section title="Active">
              {active.map((dl) => <DownloadRow key={dl.id} dl={dl} onRetry={retry.mutate} onDismiss={dismiss.mutate} />)}
            </Section>
          )}
          {pending.length > 0 && (
            <Section title="Pending" onClear={() => clearByStatus.mutate('pending')} clearLabel="Clear All">
              {pending.map((dl) => <DownloadRow key={dl.id} dl={dl} onRetry={retry.mutate} onDismiss={dismiss.mutate} />)}
            </Section>
          )}
          {failed.length > 0 && (
            <Section title="Failed" onClear={() => clearByStatus.mutate('failed')} clearLabel="Clear All">
              {failed.map((dl) => <DownloadRow key={dl.id} dl={dl} onRetry={retry.mutate} onDismiss={dismiss.mutate} />)}
            </Section>
          )}
          {complete.length > 0 && (
            <Section title="Complete" onClear={() => clearByStatus.mutate('complete')} clearLabel="Clear All">
              {complete.map((dl) => <DownloadRow key={dl.id} dl={dl} onRetry={retry.mutate} onDismiss={dismiss.mutate} />)}
            </Section>
          )}
        </>
      )}

      {tab === 'activity' && (
        <ActivityList
          activity={activityItems}
          total={activityTotal}
          offset={activityOffset}
          loadingMore={loadingMoreActivity}
          onLoadMore={loadMoreActivity}
        />
      )}
    </div>
  );
}

function ActivityList({ activity, total, offset, loadingMore, onLoadMore }: {
  activity: ActivityLog[];
  total: number;
  offset: number;
  loadingMore: boolean;
  onLoadMore: () => void;
}) {
  if (!activity.length) {
    return (
      <div className="text-center py-16">
        <svg className="w-12 h-12 mx-auto text-zinc-700 mb-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 8v4l3 3" /><circle cx="12" cy="12" r="10" />
        </svg>
        <p className="text-zinc-500">No activity yet</p>
      </div>
    );
  }

  const actionIcons: Record<string, string> = {
    search_started: 'text-blue-400',
    download_started: 'text-blue-400',
    download_complete: 'text-green-400',
    download_failed: 'text-red-400',
    file_missing: 'text-amber-400',
  };

  return (
    <div className="space-y-0.5">
      {activity.map((a) => (
        <div key={a.id} className="flex items-start gap-2.5 px-2.5 py-2 rounded-lg">
          <div className={`w-1.5 h-1.5 rounded-full mt-1.5 shrink-0 ${actionIcons[a.action] ? actionIcons[a.action].replace('text-', 'bg-') : 'bg-zinc-600'}`} />
          <div className="flex-1 min-w-0">
            <p className="text-sm truncate">{a.details}</p>
            <p className="text-[10px] text-zinc-600">
              {new Date(a.created_at).toLocaleString()}
            </p>
          </div>
        </div>
      ))}
      {offset < total && (
        <button
          onClick={onLoadMore}
          disabled={loadingMore}
          className="w-full py-2.5 text-sm text-zinc-400 bg-zinc-800/40 rounded-lg active:bg-zinc-800 transition-colors disabled:opacity-50 mt-2"
        >
          {loadingMore ? 'Loading...' : `Load More (${activity.length} of ${total})`}
        </button>
      )}
    </div>
  );
}

function Section({ title, children, onClear, clearLabel }: { title: string; children: React.ReactNode; onClear?: () => void; clearLabel?: string }) {
  return (
    <div className="mb-4">
      <div className="flex items-center justify-between mb-1.5">
        <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider">{title}</p>
        {onClear && (
          <button
            onClick={onClear}
            className="text-[11px] text-zinc-500 active:text-red-400 transition-colors"
          >
            {clearLabel || 'Clear'}
          </button>
        )}
      </div>
      <div className="space-y-1">{children}</div>
    </div>
  );
}

function DownloadRow({ dl, onRetry, onDismiss }: { dl: DownloadQueueItem; onRetry: (id: number) => void; onDismiss: (id: number) => void }) {
  const trackTitle = dl.track?.title ?? `Track #${dl.track_id}`;
  const artistName = dl.track?.artist_name;
  const albumTitle = dl.track?.album_title;

  return (
    <div className="flex items-center gap-2.5 bg-zinc-800/40 rounded-lg px-3 py-2">
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium truncate">{trackTitle}</p>
        <p className="text-[11px] text-zinc-500 truncate">
          {artistName}{albumTitle && ` · ${albumTitle}`}
          {dl.error && <span className="text-red-400"> · {dl.error}</span>}
          {dl.status === 'failed' && dl.next_retry_at && (
            <span className="text-zinc-600"> · retries {new Date(dl.next_retry_at).toLocaleTimeString()}</span>
          )}
        </p>
      </div>
      <StatusBadge status={dl.status} />
      {dl.status === 'failed' && (
        <button
          onClick={() => onRetry(dl.id)}
          className="px-2.5 py-1 bg-zinc-700 rounded text-xs active:bg-zinc-600 transition-colors shrink-0"
        >
          Retry
        </button>
      )}
      {(dl.status === 'failed' || dl.status === 'complete' || dl.status === 'pending') && (
        <button
          onClick={() => onDismiss(dl.id)}
          className="text-zinc-600 active:text-red-400 transition-colors shrink-0"
          title="Dismiss"
        >
          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M18 6 6 18" /><path d="m6 6 12 12" />
          </svg>
        </button>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const styles: Record<string, string> = {
    complete: 'bg-green-900/50 text-green-400',
    pending: 'bg-zinc-700 text-zinc-400',
    searching: 'bg-blue-900/50 text-blue-300',
    downloading: 'bg-blue-900/50 text-blue-400',
    failed: 'bg-red-900/50 text-red-400',
  };

  return (
    <span className={`px-2 py-0.5 rounded text-[10px] font-medium uppercase shrink-0 ${styles[status] || ''}`}>
      {status}
    </span>
  );
}
