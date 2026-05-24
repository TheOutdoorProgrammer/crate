import { useState, useMemo } from 'react';
import { useParams, Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import FilterBar from '../components/FilterBar';
import MetadataPills from '../components/MetadataPills';

export default function BrowseArtist() {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const [watchNewReleases, setWatchNewReleases] = useState(false);
  const [localWatchedAlbums, setLocalWatchedAlbums] = useState<Set<string>>(new Set());
  const [trackFilter, setTrackFilter] = useState('');
  const [debouncedFilter, setDebouncedFilter] = useState('');

  const providerOverride = searchParams.get('provider') || undefined;

  const { data: artist, isLoading } = useQuery({
    queryKey: ['browse-artist', id, providerOverride],
    queryFn: () => api.browseArtist(id!, providerOverride),
    enabled: !!id,
  });

  const watchArtist = useMutation({
    mutationFn: () => api.watchArtist(id!, { watch_new_releases: watchNewReleases, provider: providerOverride }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      navigate('/');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const watchAlbum = useMutation({
    mutationFn: (albumID: string) =>
      api.watchAlbum(albumID, {
        artist_provider_id: artist!.id,
        artist_name: artist!.name,
        artist_image_url: artist!.image_url,
        provider: providerOverride,
      }),
    onSuccess: (_data, albumID) => {
      setLocalWatchedAlbums((prev) => new Set(prev).add(albumID));
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const { data: trackSearchResults } = useQuery({
    queryKey: ['browse-artist-tracks', id, debouncedFilter, providerOverride],
    queryFn: () => api.searchBrowseArtistTracks(id!, debouncedFilter, providerOverride),
    enabled: !!id && debouncedFilter.length >= 2,
  });

  const matchingAlbumIds = useMemo(() => {
    if (!trackSearchResults?.tracks || !debouncedFilter) return null;
    const ids = new Set<string>();
    for (const t of trackSearchResults.tracks) ids.add(t.album_id);
    return ids;
  }, [trackSearchResults, debouncedFilter]);

  const filteredAlbums = useMemo(() => {
    if (!artist?.albums) return [];
    if (!debouncedFilter) return artist.albums;
    const q = debouncedFilter.toLowerCase();
    return artist.albums.filter((album) => {
      if (album.title.toLowerCase().includes(q)) return true;
      return matchingAlbumIds?.has(album.id) ?? false;
    });
  }, [artist, debouncedFilter, matchingAlbumIds]);

  if (isLoading) {
    return (
      <div className="animate-pulse">
        <div className="flex items-center gap-3 mb-4">
          <div className="w-14 h-14 rounded-full bg-zinc-800" />
          <div className="flex-1 space-y-2">
            <div className="h-5 bg-zinc-800 rounded w-36" />
            <div className="h-3 bg-zinc-800 rounded w-20" />
          </div>
        </div>
        <div className="h-10 bg-zinc-800 rounded-lg mb-2" />
        <div className="h-11 bg-zinc-800 rounded-lg mb-4" />
        <div className="space-y-1">
          {[...Array(6)].map((_, i) => (
            <div key={i} className="flex items-center gap-2.5 bg-zinc-800/40 rounded-lg p-2 h-14" />
          ))}
        </div>
      </div>
    );
  }

  if (!artist) return <div className="text-zinc-500 text-sm py-8 text-center">Artist not found</div>;

  return (
    <div>
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
          <p className="text-xs text-zinc-500">{artist.albums?.length || 0} releases</p>
          <div className="mt-1">
            <MetadataPills metadata={artist.metadata} />
          </div>
        </div>
      </div>

      <label className="flex items-center justify-between bg-zinc-800/50 rounded-lg px-3 py-2.5 mb-2 cursor-pointer">
        <div>
          <p className="text-sm font-medium">Watch for new releases</p>
          <p className="text-[11px] text-zinc-500">Auto-add future albums from this artist</p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={watchNewReleases}
          onClick={() => setWatchNewReleases(!watchNewReleases)}
          className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
            watchNewReleases ? 'bg-green-600' : 'bg-zinc-700'
          }`}
        >
          <span
            className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
              watchNewReleases ? 'translate-x-[22px]' : 'translate-x-0.5'
            }`}
          />
        </button>
      </label>

      <button
        onClick={() => watchArtist.mutate()}
        disabled={watchArtist.isPending || watchArtist.isSuccess || artist.artist_watched}
        className={`w-full mb-4 py-2.5 rounded-lg font-semibold text-sm transition-transform disabled:opacity-50 ${
          artist.artist_watched || watchArtist.isSuccess
            ? 'bg-green-900/50 text-green-400'
            : 'bg-white text-zinc-900 active:scale-[0.98]'
        }`}
      >
        {artist.artist_watched ? '✓ Watching All' : watchArtist.isSuccess ? '✓ Watching All' : watchArtist.isPending ? 'Adding...' : 'Watch All Albums'}
      </button>

      {artist.albums && artist.albums.length > 0 && (
        <div>
          <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Discography</p>
          <FilterBar
            value={trackFilter}
            onChange={(v) => {
              setTrackFilter(v);
              setDebouncedFilter(v);
            }}
            debounceMs={400}
            placeholder="Filter by song or album title..."
          />
          {debouncedFilter && filteredAlbums.length === 0 && (
            <p className="text-zinc-500 text-sm text-center py-4">No matching albums or tracks</p>
          )}
          <div className="space-y-1">
            {filteredAlbums.map((album) => {
              const serverWatched = artist.watched_album_ids?.includes(album.id);
              const isWatched = serverWatched || localWatchedAlbums.has(album.id);
              return (
                <div
                  key={album.id}
                  className="flex items-center gap-2.5 bg-zinc-800/40 rounded-lg p-2"
                >
                  <Link
                    to={`/browse/album/${album.id}?artist_id=${artist.id}&artist_name=${encodeURIComponent(artist.name)}&artist_image_url=${encodeURIComponent(artist.image_url || '')}${providerOverride ? `&provider=${providerOverride}` : ''}`}
                    className="flex items-center gap-2.5 flex-1 min-w-0"
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
                      <p className="font-medium text-sm truncate">{album.title}</p>
                      <div className="flex items-center gap-1.5">
                        <p className="text-[11px] text-zinc-500">
                          {album.year > 0 && `${album.year}`}
                        </p>
                        {album.record_type && album.record_type !== 'album' && (
                          <span className="text-[9px] font-medium uppercase text-zinc-500 bg-zinc-800 px-1 py-px rounded">
                            {album.record_type}
                          </span>
                        )}
                      </div>
                    </div>
                  </Link>
                  {isWatched ? (
                    <svg className="w-5 h-5 text-green-400 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  ) : (
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        watchAlbum.mutate(album.id);
                      }}
                      className="px-2.5 py-1 rounded text-xs font-medium transition-colors shrink-0 bg-zinc-700 text-zinc-200 active:bg-zinc-600"
                    >
                      Watch
                    </button>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

