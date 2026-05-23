import { useMemo, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import { formatDuration, formatTotalDuration, formatFileSize } from '../lib/format';
import FilterBar from '../components/FilterBar';
import DetailSheet, { DetailRow } from '../components/DetailSheet';
import ProviderBadge from '../components/ProviderBadge';
import ProgressBar from '../components/ProgressBar';
import type { ManualSearchResult, Track } from '../types/index';

export default function AlbumDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { toast } = useToast();

  const { data: album, isLoading } = useQuery({
    queryKey: ['album', id],
    queryFn: () => api.getAlbum(Number(id)),
    enabled: !!id,
    refetchInterval: 10_000,
  });

  const { data: downloads } = useQuery({
    queryKey: ['downloads'],
    queryFn: () => api.listDownloads(),
    refetchInterval: 5000,
  });

  const activeTrackIds = new Set(
    downloads
      ?.filter((d) => d.status === 'pending' || d.status === 'searching' || d.status === 'downloading')
      .map((d) => d.track_id) ?? []
  );

  const unwatchAlbum = useMutation({
    mutationFn: () => api.unwatchAlbum(Number(id)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      if (window.history.length > 1) { navigate(-1); } else { navigate('/'); }
    },
  });

  const unwatchTrack = useMutation({
    mutationFn: (trackId: number) => api.unwatchTrack(trackId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['album', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    },
  });

  const queueTrack = useMutation({
    mutationFn: (trackId: number) => api.queueTrack(trackId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['downloads'] }),
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const queueAll = useMutation({
    mutationFn: () => api.queueAlbumTracks(Number(id)),
    onSuccess: (data) => {
      toast(`Queued ${data.queued} tracks`, 'success');
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const [trackFilter, setTrackFilter] = useState('');
  const [selectedTrack, setSelectedTrack] = useState<Track | null>(null);
  const [manualSearchTrackId, setManualSearchTrackId] = useState<number | null>(null);
  const [manualResults, setManualResults] = useState<ManualSearchResult[]>([]);
  const [manualSearching, setManualSearching] = useState(false);

  const startManualSearch = async (trackId: number) => {
    if (manualSearchTrackId === trackId) {
      setManualSearchTrackId(null);
      setManualResults([]);
      return;
    }
    setManualSearchTrackId(trackId);
    setManualResults([]);
    setManualSearching(true);
    try {
      const results = await api.manualSearchTrack(trackId);
      setManualResults(results);
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Search failed', 'error');
    } finally {
      setManualSearching(false);
    }
  };

  const manualDownload = useMutation({
    mutationFn: (r: ManualSearchResult) =>
      api.manualDownloadTrack(manualSearchTrackId!, r.username, r.filename, r.size, r.bit_rate),
    onSuccess: () => {
      toast('Download started', 'success');
      setManualSearchTrackId(null);
      setManualResults([]);
      queryClient.invalidateQueries({ queryKey: ['album', id] });
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const handleUnwatchAlbum = () => {
    if (confirm(`Stop watching ${album?.title}?`)) {
      unwatchAlbum.mutate();
    }
  };

  const stats = useMemo(() => {
    if (!album?.tracks) return null;
    let totalDuration = 0;
    let ownedTracks = 0;
    let wantedTracks = 0;
    for (const track of album.tracks) {
      totalDuration += track.duration_ms;
      if (track.status === 'owned') ownedTracks++;
      if (track.status === 'wanted') wantedTracks++;
    }
    return { totalDuration, ownedTracks, wantedTracks, totalTracks: album.tracks.length };
  }, [album]);

  const filteredTracks = useMemo((): Track[] => {
    if (!album?.tracks) return [];
    if (!trackFilter) return album.tracks;
    const q = trackFilter.toLowerCase();
    return album.tracks.filter((t) => t.title.toLowerCase().includes(q));
  }, [album, trackFilter]);

  if (isLoading) {
    return (
      <div className="animate-pulse">
        <div className="flex items-center gap-3 mb-4">
          <div className="w-14 h-14 rounded-lg bg-zinc-800" />
          <div className="flex-1 space-y-2">
            <div className="h-5 bg-zinc-800 rounded w-36" />
            <div className="h-3 bg-zinc-800 rounded w-24" />
          </div>
        </div>
        <div className="space-y-0">
          {[...Array(8)].map((_, i) => (
            <div key={i} className="flex items-center gap-2.5 px-2.5 py-2 h-12 border-b border-zinc-800/50" />
          ))}
        </div>
      </div>
    );
  }

  if (!album) return <div className="text-zinc-500 text-sm py-8 text-center">Album not found</div>;

  return (
    <div>
      <div className="flex items-center gap-3 mb-4">
        <div className="w-14 h-14 rounded-lg bg-zinc-800 overflow-hidden shrink-0">
          {album.cover_url ? (
            <img src={album.cover_url} alt={album.title} className="w-full h-full object-cover" onError={(e) => (e.target as HTMLImageElement).style.display = 'none'} />
          ) : (
            <div className="w-full h-full flex items-center justify-center text-zinc-600">
              <svg className="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="12" cy="12" r="4" /><circle cx="12" cy="12" r="1" /></svg>
            </div>
          )}
        </div>
        <div className="flex-1 min-w-0">
          <h2 className="text-lg font-bold truncate">{album.title}</h2>
          <p className="text-xs text-zinc-500">
            {album.artist_name}{album.year && ` · ${album.year}`}
          </p>
          <div className="flex items-center gap-1.5 mt-0.5">
            <ProviderBadge provider={album.provider} />
            {album.record_type && album.record_type !== 'album' && (
              <span className="text-[10px] font-medium uppercase text-zinc-500 bg-zinc-800 px-1.5 py-0.5 rounded">
                {album.record_type}
              </span>
            )}
          </div>
        </div>
        <button
          onClick={handleUnwatchAlbum}
          disabled={unwatchAlbum.isPending}
          className="px-3 py-1.5 rounded-lg text-xs font-medium bg-zinc-800 text-zinc-400 active:bg-red-900/40 active:text-red-400 transition-colors shrink-0"
        >
          Unwatch
        </button>
      </div>

      {stats && stats.totalTracks > 0 && (
        <div className="bg-zinc-800/50 rounded-lg px-3 py-2.5 mb-4 space-y-2">
          <ProgressBar owned={stats.ownedTracks} total={stats.totalTracks} />
          <div className="flex items-center justify-between text-[11px] text-zinc-500">
            <span>{stats.totalTracks} tracks</span>
            <span>{formatTotalDuration(stats.totalDuration)}</span>
          </div>
        </div>
      )}

      {stats && stats.wantedTracks > 0 && (
        <button
          onClick={() => queueAll.mutate()}
          disabled={queueAll.isPending || queueAll.isSuccess}
          className="w-full mb-4 py-2.5 bg-zinc-800 text-zinc-200 rounded-lg font-medium text-sm active:bg-zinc-700 transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" />
          </svg>
          {queueAll.isSuccess ? 'Queued!' : queueAll.isPending ? 'Queuing...' : `Search ${stats.wantedTracks} wanted tracks`}
        </button>
      )}

      {album.tracks && album.tracks.length > 0 && (
        <div>
          <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Tracks</p>
          {album.tracks.length > 3 && (
            <FilterBar
              value={trackFilter}
              onChange={setTrackFilter}
              placeholder="Filter tracks..."
            />
          )}
          {trackFilter && filteredTracks.length === 0 && (
            <p className="text-zinc-500 text-sm text-center py-4">No matching tracks</p>
          )}
          <div className="rounded-lg overflow-hidden">
            {filteredTracks.map((track) => {
              const isQueued = activeTrackIds.has(track.id);
              const isSearching = queueTrack.isPending && queueTrack.variables === track.id;
              const isManualOpen = manualSearchTrackId === track.id;
              return (
                <div key={track.id}>
                  <div className="flex items-center gap-2.5 px-2.5 py-2 border-b border-zinc-800/50 last:border-0 cursor-pointer active:bg-zinc-800/50 transition-colors" onClick={() => setSelectedTrack(track)}>
                    <span className="text-[11px] text-zinc-600 w-5 text-right shrink-0 tabular-nums">
                      {track.track_number}
                    </span>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm truncate">{track.title}</p>
                      <p className="text-[11px] text-zinc-600 tabular-nums">{formatDuration(track.duration_ms)}</p>
                      {track.status === 'owned' && (track.file_path || track.downloaded_from || track.download_format) && (
                        <p className="text-[10px] text-zinc-600 truncate">
                          {track.download_format && (
                            <span className="text-zinc-500 uppercase">{track.download_format}{track.download_bitrate ? ` ${track.download_bitrate}k` : ''}</span>
                          )}
                          {track.file_path && (
                            <span>{track.download_format ? ' · ' : ''}{track.file_path}</span>
                          )}
                          {track.downloaded_from && (
                            <span className="text-zinc-700">{(track.file_path || track.download_format) ? ' · ' : ''}from {track.downloaded_from}</span>
                          )}
                        </p>
                      )}
                    </div>
                    <StatusBadge status={track.status} />
                    {track.status === 'wanted' && (
                      <>
                        <button
                          onClick={(e) => { e.stopPropagation(); queueTrack.mutate(track.id); }}
                          disabled={isQueued || isSearching}
                          className={`shrink-0 transition-colors ${
                            isQueued || isSearching ? 'opacity-30 cursor-not-allowed' : 'text-zinc-500 active:text-white'
                          }`}
                          title={isQueued ? 'Already queued' : 'Auto search'}
                        >
                          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" />
                          </svg>
                        </button>
                        <button
                          onClick={(e) => { e.stopPropagation(); startManualSearch(track.id); }}
                          disabled={manualSearching && isManualOpen}
                          className={`shrink-0 transition-colors ${
                            isManualOpen ? 'text-blue-400' : 'text-zinc-500 active:text-white'
                          }`}
                          title="Manual search"
                        >
                          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" /><circle cx="12" cy="7" r="4" />
                          </svg>
                        </button>
                      </>
                    )}
                    <button
                      onClick={(e) => { e.stopPropagation(); unwatchTrack.mutate(track.id); }}
                      className="ml-1 text-zinc-600 active:text-red-400 transition-colors shrink-0"
                      title="Unwatch track"
                    >
                      <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M18 6 6 18" /><path d="m6 6 12 12" />
                      </svg>
                    </button>
                  </div>
                  {isManualOpen && (
                    <div className="bg-zinc-900/50 border-b border-zinc-800/50 px-3 py-2 animate-fade-in">
                      {manualSearching && (
                        <div className="flex items-center gap-2 py-3 justify-center">
                          <div className="w-4 h-4 border-2 border-zinc-600 border-t-zinc-300 rounded-full animate-spin" />
                          <span className="text-xs text-zinc-500">Searching slskd...</span>
                        </div>
                      )}
                      {!manualSearching && manualResults.length === 0 && (
                        <p className="text-xs text-zinc-500 text-center py-3">No results found</p>
                      )}
                      {!manualSearching && manualResults.length > 0 && (
                        <div className="space-y-0.5 max-h-64 overflow-y-auto">
                          <p className="text-[10px] text-zinc-500 mb-1">{manualResults.length} results</p>
                          {manualResults.map((r, i) => (
                            <button
                              key={`${r.username}-${i}`}
                              onClick={() => !r.blacklisted && manualDownload.mutate(r)}
                              disabled={r.blacklisted || manualDownload.isPending}
                              className={`w-full text-left rounded-lg px-2.5 py-2 transition-colors ${
                                r.blacklisted
                                  ? 'opacity-40 cursor-not-allowed bg-zinc-800/30'
                                  : 'bg-zinc-800/50 active:bg-zinc-700'
                              }`}
                            >
                              <div className="flex items-center gap-2">
                                <div className="flex-1 min-w-0">
                                  <p className="text-[11px] truncate">{r.filename.split(/[/\\]/).pop()}</p>
                                  <p className="text-[10px] text-zinc-500 truncate">
                                    {r.username}
                                    {' · '}{r.format}
                                    {' · '}{formatFileSize(r.size)}
                                    {r.free_slot && ' · free slot'}
                                    {r.queue_length > 0 && ` · ${r.queue_length} queued`}
                                    {r.blacklisted && ' · blacklisted'}
                                  </p>
                                </div>
                                <span className={`text-[10px] font-medium tabular-nums shrink-0 ${
                                  r.score >= 100 ? 'text-green-400' : r.score >= 50 ? 'text-blue-400' : 'text-zinc-500'
                                }`}>
                                  {r.score}
                                </span>
                              </div>
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}

      <DetailSheet
        open={!!selectedTrack}
        onClose={() => setSelectedTrack(null)}
        title={selectedTrack?.title ?? ''}
      >
        {selectedTrack && (
          <div className="space-y-0.5">
            <DetailRow label="Track">{selectedTrack.disc_number > 1 ? `Disc ${selectedTrack.disc_number}, ` : ''}#{selectedTrack.track_number}</DetailRow>
            <DetailRow label="Duration">{formatDuration(selectedTrack.duration_ms)}</DetailRow>
            <DetailRow label="Status"><StatusBadge status={selectedTrack.status} /></DetailRow>
            {selectedTrack.download_format && (
              <DetailRow label="Format">
                {selectedTrack.download_format.toUpperCase()}
                {selectedTrack.download_bitrate ? ` ${selectedTrack.download_bitrate} kbps` : ''}
              </DetailRow>
            )}
            {selectedTrack.downloaded_from && <DetailRow label="Source">{selectedTrack.downloaded_from}</DetailRow>}
            {selectedTrack.file_path && <DetailRow label="Path">{selectedTrack.file_path}</DetailRow>}
          </div>
        )}
      </DetailSheet>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const styles: Record<string, string> = {
    owned: 'bg-green-900/50 text-green-400',
    wanted: 'bg-amber-900/50 text-amber-400',
    downloading: 'bg-blue-900/50 text-blue-400',
    ignored: 'bg-zinc-800 text-zinc-500',
  };

  return (
    <span className={`px-2 py-0.5 rounded text-[10px] font-medium uppercase ${styles[status] || styles.wanted}`}>
      {status}
    </span>
  );
}
