import { useState } from 'react';
import { useParams, useSearchParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import MetadataPills from '../components/MetadataPills';
import { formatDuration, formatTotalDuration } from '../lib/format';
import type { BrowseTrackResult } from '../types/index';

export default function BrowseAlbum() {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const [localWatchedTracks, setLocalWatchedTracks] = useState<Set<string>>(new Set());

  const artistID = searchParams.get('artist_id') || '';
  const artistName = searchParams.get('artist_name') || '';
  const artistImageURL = searchParams.get('artist_image_url') || '';
  const providerOverride = searchParams.get('provider') || undefined;

  const { data: album, isLoading } = useQuery({
    queryKey: ['browse-album', id, providerOverride],
    queryFn: () => api.browseAlbum(id!, providerOverride),
    enabled: !!id,
  });

  const watchAlbum = useMutation({
    mutationFn: () =>
      api.watchAlbum(id!, {
        artist_provider_id: artistID,
        artist_name: artistName,
        artist_image_url: artistImageURL,
        provider: providerOverride,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      navigate('/');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const watchTrack = useMutation({
    mutationFn: (track: BrowseTrackResult) =>
      api.watchTrack(track.id, {
        artist_provider_id: artistID,
        artist_name: artistName,
        artist_image_url: artistImageURL,
        album_provider_id: id,
        album_title: album!.title,
        album_cover_url: album!.cover_url,
        album_year: album!.year || null,
        title: track.title,
        track_number: track.track_number,
        disc_number: track.disc_number,
        duration_ms: track.duration_ms,
        provider: providerOverride,
      }),
    onSuccess: (_data, track) => {
      setLocalWatchedTracks((prev) => new Set(prev).add(track.id));
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

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
        <div className="h-11 bg-zinc-800 rounded-lg mb-4" />
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
            <img src={album.cover_url} alt={album.title} className="w-full h-full object-cover" />
          ) : (
            <div className="w-full h-full flex items-center justify-center text-zinc-600">
              <svg className="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="12" cy="12" r="4" /><circle cx="12" cy="12" r="1" /></svg>
            </div>
          )}
        </div>
        <div className="flex-1 min-w-0">
          <h2 className="text-lg font-bold truncate">{album.title}</h2>
          <p className="text-xs text-zinc-500">
            {album.artist_name || artistName}{album.year > 0 && ` · ${album.year}`} · {album.tracks?.length || 0} tracks
            {album.tracks && album.tracks.length > 0 && ` · ${formatTotalDuration(album.tracks.reduce((sum, t) => sum + t.duration_ms, 0))}`}
          </p>
          {album.metadata && <div className="mt-1"><MetadataPills metadata={album.metadata} /></div>}
        </div>
      </div>

      <button
        onClick={() => watchAlbum.mutate()}
        disabled={watchAlbum.isPending || watchAlbum.isSuccess || album.album_watched}
        className={`w-full mb-4 py-2.5 rounded-lg font-semibold text-sm transition-transform disabled:opacity-50 ${
          album.album_watched || watchAlbum.isSuccess
            ? 'bg-green-900/50 text-green-400'
            : 'bg-white text-zinc-900 active:scale-[0.98]'
        }`}
      >
        {album.album_watched ? '✓ Watching Album' : watchAlbum.isSuccess ? '✓ Watching Album' : watchAlbum.isPending ? 'Adding...' : 'Watch Entire Album'}
      </button>

      {album.tracks && album.tracks.length > 0 && (
        <div>
          <p className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Tracks</p>
          <div className="rounded-lg overflow-hidden">
            {album.tracks.map((track) => {
              const serverWatched = album.watched_track_ids?.includes(track.id);
              const isWatched = serverWatched || localWatchedTracks.has(track.id);
              return (
                <div
                  key={track.id}
                  className="flex items-center gap-2.5 px-2.5 py-2 border-b border-zinc-800/50 last:border-0"
                >
                  <span className="text-[11px] text-zinc-600 w-5 text-right shrink-0 tabular-nums">
                    {track.track_number}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm truncate">{track.title}</p>
                    <p className="text-[11px] text-zinc-600 tabular-nums">{formatDuration(track.duration_ms)}</p>
                  </div>
                  {isWatched ? (
                    <svg className="w-5 h-5 text-green-400 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  ) : (
                    <button
                      onClick={() => watchTrack.mutate(track)}
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

