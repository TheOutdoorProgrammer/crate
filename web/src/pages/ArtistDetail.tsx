import { useMemo, useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import { formatTotalDuration } from '../lib/format';
import FilterBar from '../components/FilterBar';
import ProviderBadge from '../components/ProviderBadge';
import ProgressBar from '../components/ProgressBar';
import type { Track, Album, ProviderInfo } from '../types/index';

export default function ArtistDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [trackFilter, setTrackFilter] = useState('');
  const [showRelinkSearch, setShowRelinkSearch] = useState(false);
  const [relinkQuery, setRelinkQuery] = useState('');
  const [relinkProvider, setRelinkProvider] = useState('');

  const { data: artist, isLoading } = useQuery({
    queryKey: ['artist', id],
    queryFn: () => api.getArtist(Number(id)),
    enabled: !!id,
    refetchInterval: (query) => {
      const a = query.state.data;
      if (!a?.albums) return false;
      const hasActive = a.albums.some((al: any) =>
        al.tracks?.some((t: any) => t.status === 'downloading' || t.status === 'searching')
      );
      return hasActive ? 10_000 : false;
    },
  });

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: api.listProviders,
  });

  const { data: relinkResults } = useQuery({
    queryKey: ['relink-search', relinkQuery, relinkProvider],
    queryFn: () => api.search(relinkQuery, relinkProvider || undefined),
    enabled: !!relinkQuery && showRelinkSearch,
  });

  const toggleNewReleases = useMutation({
    mutationFn: (enabled: boolean) => api.toggleNewReleases(Number(id), enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: ['artist', id] });
      const prev = queryClient.getQueryData(['artist', id]);
      queryClient.setQueryData(['artist', id], (old: any) =>
        old ? { ...old, watch_new_releases: enabled } : old
      );
      return { prev };
    },
    onError: (_err, _enabled, context) => {
      if (context?.prev) queryClient.setQueryData(['artist', id], context.prev);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    },
  });

  const unwatch = useMutation({
    mutationFn: () => api.unwatchArtist(Number(id)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      navigate('/');
    },
  });

  const queueAll = useMutation({
    mutationFn: () => api.queueArtistTracks(Number(id)),
    onSuccess: (data) => {
      toast(`Queued ${data.queued} tracks`, 'success');
      queryClient.invalidateQueries({ queryKey: ['downloads'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const relink = useMutation({
    mutationFn: (providerID: string) => api.relinkEntity('artist', Number(id), providerID),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      setShowRelinkSearch(false);
      toast('Artist relinked', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const handleUnwatch = () => {
    if (confirm(`Stop watching ${artist?.name}? This removes all their albums and tracks.`)) {
      unwatch.mutate();
    }
  };

  const stats = useMemo(() => {
    if (!artist?.albums) return null;
    let totalTracks = 0;
    let ownedTracks = 0;
    let totalDuration = 0;
    let wantedTracks = 0;
    const recordTypes: Record<string, number> = {};

    for (const album of artist.albums) {
      const type = album.record_type || 'album';
      recordTypes[type] = (recordTypes[type] || 0) + 1;
      if (album.tracks) {
        for (const track of album.tracks) {
          totalTracks++;
          totalDuration += track.duration_ms;
          if (track.status === 'owned') ownedTracks++;
          if (track.status === 'wanted') wantedTracks++;
        }
      }
    }

    return { totalTracks, ownedTracks, totalDuration, wantedTracks, recordTypes };
  }, [artist]);

  const filteredAlbums = useMemo((): Album[] => {
    if (!artist?.albums) return [];
    if (!trackFilter) return artist.albums;
    const q = trackFilter.toLowerCase();
    return artist.albums.filter((album) => {
      if (album.title.toLowerCase().includes(q)) return true;
      return album.tracks?.some((t) => t.title.toLowerCase().includes(q)) ?? false;
    });
  }, [artist?.albums, trackFilter]);

  if (isLoading) {
    return (
      <div className="animate-pulse">
        <div className="flex items-center gap-3 mb-4">
          <div className="w-14 h-14 rounded-full bg-zinc-800" />
          <div className="flex-1 space-y-2">
            <div className="h-5 bg-zinc-800 rounded w-36" />
            <div className="h-3 bg-zinc-800 rounded w-24" />
          </div>
        </div>
        <div className="grid grid-cols-3 gap-3 mb-4">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-16 bg-zinc-800/50 rounded-lg" />
          ))}
        </div>
        <div className="h-10 bg-zinc-800 rounded-lg mb-3" />
        <div className="space-y-1">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="flex items-center gap-2.5 bg-zinc-800/40 rounded-lg p-2 h-14" />
          ))}
        </div>
      </div>
    );
  }

  if (!artist) return <div className="text-zinc-500 text-sm py-8 text-center">Artist not found</div>;

  return (
    <div>
      {artist.orphaned && (
        <div className="flex items-start gap-2.5 bg-red-900/30 border border-red-800/50 rounded-lg px-3 py-2.5 mb-3">
          <svg className="w-4 h-4 text-red-400 shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
          <div>
            <p className="text-sm text-red-400 font-medium">Provider offline</p>
            <p className="text-[11px] text-red-400/70">
              This artist's provider ({artist.provider}) is unreachable. Relink to continue tracking.
            </p>
          </div>
        </div>
      )}

      <div className="flex items-center gap-3 mb-4">
        <div className="w-14 h-14 rounded-full bg-zinc-800 overflow-hidden shrink-0">
          {artist.image_url ? (
            <img src={artist.image_url} alt={artist.name} className="w-full h-full object-cover" />
          ) : (
            <div className="w-full h-full flex items-center justify-center text-zinc-600">
              <svg className="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M9 18V5l12-2v13" /><circle cx="6" cy="18" r="3" /><circle cx="18" cy="16" r="3" /></svg>
            </div>
          )}
        </div>
        <div className="flex-1 min-w-0">
          <h2 className="text-lg font-bold truncate">{artist.name}</h2>
          <div className="flex items-center gap-1.5 mt-0.5">
            <ProviderBadge provider={artist.provider} />
            <span className="text-[10px] text-zinc-600">
              {artist.status === 'watched' ? 'Full discography' : artist.status === 'partial' ? 'Partial' : 'Owned'}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          <button
            onClick={() => setShowRelinkSearch(!showRelinkSearch)}
            className="px-3 py-1.5 rounded-lg text-xs font-medium bg-zinc-800 text-zinc-400 active:bg-zinc-700 transition-colors"
            title="Relink to different provider"
          >
            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" /><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
            </svg>
          </button>
          <button
            onClick={handleUnwatch}
            disabled={unwatch.isPending}
            className="px-3 py-1.5 rounded-lg text-xs font-medium bg-zinc-800 text-zinc-400 active:bg-red-900/40 active:text-red-400 transition-colors"
          >
            Unwatch
          </button>
        </div>
      </div>

      {showRelinkSearch && (
        <div className="bg-zinc-800/50 rounded-lg p-3 mb-4 animate-fade-in">
          <p className="text-xs font-medium text-zinc-400 mb-2">Relink to a different provider</p>
          <div className="flex gap-2 mb-2">
            <input
              type="text"
              value={relinkQuery}
              onChange={(e) => setRelinkQuery(e.target.value)}
              placeholder={`Search for "${artist.name}"...`}
              className="flex-1 bg-zinc-900 rounded-lg px-3 py-2 text-sm placeholder-zinc-600 outline-none focus:ring-2 focus:ring-zinc-600"
              autoFocus
            />
            {providers && providers.length > 1 && (
              <select
                value={relinkProvider}
                onChange={(e) => setRelinkProvider(e.target.value)}
                className="bg-zinc-900 rounded-lg px-2 py-2 text-sm outline-none text-zinc-300"
              >
                <option value="">All</option>
                {providers.filter((p: ProviderInfo) => p.healthy).map((p: ProviderInfo) => (
                  <option key={p.name} value={p.name}>{p.display_name}</option>
                ))}
              </select>
            )}
          </div>
          {relinkResults && relinkResults.artists.length > 0 && (
            <div className="space-y-1 max-h-48 overflow-y-auto">
              {relinkResults.artists.map((r) => (
                <button
                  key={r.id}
                  onClick={() => relink.mutate(r.id)}
                  disabled={relink.isPending}
                  className="w-full flex items-center gap-2 bg-zinc-900 rounded-lg p-2 text-left active:bg-zinc-700 transition-colors"
                >
                  <div className="w-8 h-8 rounded-full bg-zinc-700 overflow-hidden shrink-0">
                    {r.image_url && <img src={r.image_url} alt="" className="w-full h-full object-cover" />}
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm truncate">{r.name}</p>
                    <p className="text-[10px] text-zinc-500">{r.album_count} albums</p>
                  </div>
                </button>
              ))}
            </div>
          )}
          {relinkQuery && relinkResults && relinkResults.artists.length === 0 && (
            <p className="text-xs text-zinc-500 text-center py-2">No results</p>
          )}
        </div>
      )}

      {stats && (
        <div className="mb-4">
          <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Collection</p>
          <div className="bg-zinc-800/50 rounded-lg px-3 py-2.5 space-y-2.5">
            <ProgressBar owned={stats.ownedTracks} total={stats.totalTracks} />
            <div className="flex items-center justify-between text-[11px] text-zinc-500">
              <span>{artist.albums?.length || 0} releases</span>
              <span>{formatTotalDuration(stats.totalDuration)}</span>
              {Object.entries(stats.recordTypes).length > 1 && (
                <span>
                  {Object.entries(stats.recordTypes).map(([type, count]) =>
                    `${count} ${type}${count > 1 ? 's' : ''}`
                  ).join(', ')}
                </span>
              )}
            </div>
          </div>
        </div>
      )}

      {stats && stats.wantedTracks > 0 && (
        <button
          onClick={() => queueAll.mutate()}
          disabled={queueAll.isPending || queueAll.isSuccess}
          className="w-full mb-3 py-2.5 bg-zinc-800 text-zinc-200 rounded-lg font-medium text-sm active:bg-zinc-700 transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" />
          </svg>
          {queueAll.isSuccess ? 'Queued!' : queueAll.isPending ? 'Queuing...' : `Search ${stats.wantedTracks} wanted tracks`}
        </button>
      )}

      <label className="flex items-center justify-between bg-zinc-800/50 rounded-lg px-3 py-2.5 mb-3 cursor-pointer">
        <div>
          <p className="text-sm font-medium">Watch for new releases</p>
          <p className="text-[11px] text-zinc-500">Auto-add new albums from this artist</p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={artist.watch_new_releases}
          onClick={() => toggleNewReleases.mutate(!artist.watch_new_releases)}
          className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
            artist.watch_new_releases ? 'bg-green-600' : 'bg-zinc-700'
          }`}
        >
          <span
            className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
              artist.watch_new_releases ? 'translate-x-[22px]' : 'translate-x-0.5'
            }`}
          />
        </button>
      </label>

      {artist.albums && artist.albums.length > 0 && (
        <div>
          <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Albums</p>
          <FilterBar
            value={trackFilter}
            onChange={setTrackFilter}
            placeholder="Filter by song or album title..."
          />
          {trackFilter && filteredAlbums.length === 0 && (
            <p className="text-zinc-500 text-sm text-center py-4">No matching albums or tracks</p>
          )}
          <div className="space-y-1">
            {filteredAlbums.map((album) => (
              <Link
                key={album.id}
                to={`/album/${album.id}`}
                className="flex items-center gap-2.5 bg-zinc-800/40 rounded-lg p-2 active:bg-zinc-800 transition-colors"
              >
                <div className="w-10 h-10 rounded bg-zinc-700 overflow-hidden shrink-0">
                  {album.cover_url ? (
                    <img src={album.cover_url} alt="" className="w-full h-full object-cover" onError={(e) => (e.target as HTMLImageElement).style.display = 'none'} />
                  ) : (
                    <div className="w-full h-full flex items-center justify-center text-zinc-500">
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="12" cy="12" r="4" /><circle cx="12" cy="12" r="1" /></svg>
                    </div>
                  )}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-1.5">
                    <p className="font-medium text-sm truncate">{album.title}</p>
                    {album.record_type && album.record_type !== 'album' && (
                      <span className="text-[9px] font-medium uppercase text-zinc-500 bg-zinc-800 px-1 py-px rounded shrink-0">
                        {album.record_type}
                      </span>
                    )}
                  </div>
                  <p className="text-[11px] text-zinc-500">
                    {album.year && `${album.year} · `}{album.tracks?.length || 0} tracks
                    {trackFilter && album.tracks && (() => {
                      const q = trackFilter.toLowerCase();
                      const count = album.tracks.filter((t) => t.title.toLowerCase().includes(q)).length;
                      return count > 0 ? ` · ${count} match${count > 1 ? 'es' : ''}` : '';
                    })()}
                  </p>
                </div>
                <AlbumStatusSummary tracks={album.tracks} />
                <svg className="w-4 h-4 text-zinc-600 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m9 18 6-6-6-6" /></svg>
              </Link>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function AlbumStatusSummary({ tracks }: { tracks?: Track[] }) {
  if (!tracks?.length) return null;
  const owned = tracks.filter((t) => t.status === 'owned').length;
  const total = tracks.length;

  if (owned === total) {
    return <span className="text-[10px] font-medium text-green-400 shrink-0">owned</span>;
  }
  if (owned > 0) {
    return (
      <span className="text-[11px] text-zinc-500 tabular-nums shrink-0">
        {owned}/{total}
      </span>
    );
  }
  return null;
}
