import type { Artist, Album, Track, SearchResponse, BrowseArtistResult, BrowseAlbumDetail, DownloadQueueItem, DownloadProgress, SystemStatus, ProviderInfo, ActivityResponse, ManualSearchStart, ManualSearchResponse, LibrarySearchResult, TrackSearchResult, BlacklistEntry, UserCooldown } from '../types/index';

const BASE = '/api';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || res.statusText);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  search: (q: string, provider?: string, limit = 25, offset = 0) =>
    request<SearchResponse>(`/search?q=${encodeURIComponent(q)}&limit=${limit}&offset=${offset}${provider ? `&provider=${encodeURIComponent(provider)}` : ''}`),

  browseArtist: (id: string, provider?: string) =>
    request<BrowseArtistResult>(`/browse/artist/${id}${provider ? `?provider=${encodeURIComponent(provider)}` : ''}`),
  browseAlbum: (id: string, provider?: string) =>
    request<BrowseAlbumDetail>(`/browse/album/${id}${provider ? `?provider=${encodeURIComponent(provider)}` : ''}`),

  watchArtist: (id: string, opts?: { watch_new_releases?: boolean; provider?: string }) =>
    request<Artist>(`/watch/artist/${id}`, {
      method: 'POST',
      body: JSON.stringify(opts ?? {}),
    }),
  watchAlbum: (id: string, artistInfo: { artist_provider_id: string; artist_name: string; artist_image_url: string; provider?: string }) =>
    request<Album>(`/watch/album/${id}`, {
      method: 'POST',
      body: JSON.stringify(artistInfo),
    }),
  watchTrack: (id: string, trackInfo: Record<string, unknown>) =>
    request<Track>(`/watch/track/${id}`, {
      method: 'POST',
      body: JSON.stringify(trackInfo),
    }),

  listArtists: () => request<Artist[]>('/artists'),
  getArtist: (id: number) => request<Artist>(`/artists/${id}`),
  getAlbum: (id: number) => request<Album>(`/albums/${id}`),

  queueTrack: (id: number) =>
    request<void>(`/tracks/${id}/queue`, { method: 'POST' }),
  startManualSearch: (id: number, query?: string) =>
    request<ManualSearchStart>(`/tracks/${id}/search`, {
      method: 'POST',
      body: query ? JSON.stringify({ query }) : undefined,
    }),
  pollManualSearch: (trackId: number, searchId: string) =>
    request<ManualSearchResponse>(`/tracks/${trackId}/search/${searchId}`),
  deleteManualSearch: (trackId: number, searchId: string) =>
    request<void>(`/tracks/${trackId}/search/${searchId}`, { method: 'DELETE' }),
  manualDownloadTrack: (id: number, username: string, filename: string, size: number, bitRate: number) =>
    request<{ status: string }>(`/tracks/${id}/download`, {
      method: 'POST',
      body: JSON.stringify({ username, filename, size, bit_rate: bitRate }),
    }),
  queueArtistTracks: (id: number) =>
    request<{ queued: number }>(`/artists/${id}/queue`, { method: 'POST' }),
  queueAlbumTracks: (id: number) =>
    request<{ queued: number }>(`/albums/${id}/queue`, { method: 'POST' }),

  toggleNewReleases: (id: number, enabled: boolean) =>
    request<{ watch_new_releases: boolean }>(`/artists/${id}/new-releases`, {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    }),

  unwatchArtist: (id: number) =>
    request<void>(`/artists/${id}`, { method: 'DELETE' }),
  unwatchAlbum: (id: number) =>
    request<void>(`/albums/${id}`, { method: 'DELETE' }),
  unwatchTrack: (id: number) =>
    request<void>(`/tracks/${id}`, { method: 'DELETE' }),

  ignoreAlbum: (id: number) =>
    request<void>(`/albums/${id}/ignore`, { method: 'PUT' }),
  unignoreAlbum: (id: number) =>
    request<void>(`/albums/${id}/ignore`, { method: 'DELETE' }),
  ignoreTrack: (id: number) =>
    request<void>(`/tracks/${id}/ignore`, { method: 'PUT' }),
  unignoreTrack: (id: number) =>
    request<void>(`/tracks/${id}/ignore`, { method: 'DELETE' }),

  listDownloads: (status?: string) =>
    request<DownloadQueueItem[]>(`/downloads${status ? `?status=${status}` : ''}`),
  getDownloadProgress: () =>
    request<DownloadProgress[]>('/downloads/progress'),
  queueDownloads: () =>
    request<{ queued: number }>('/downloads/queue', { method: 'POST' }),
  retryDownload: (id: number) =>
    request<void>(`/downloads/${id}/retry`, { method: 'POST' }),
  deleteDownload: (id: number) =>
    request<void>(`/downloads/${id}`, { method: 'DELETE' }),
  clearDownloadsByStatus: (status: string) =>
    request<{ deleted: number }>(`/downloads/clear?status=${status}`, { method: 'DELETE' }),

  getSettings: () => request<Record<string, string>>('/settings'),
  updateSettings: (settings: Record<string, string>) =>
    request<Record<string, string>>('/settings', {
      method: 'PUT',
      body: JSON.stringify(settings),
    }),

  getStatus: () => request<SystemStatus>('/status'),

  listProviders: () => request<ProviderInfo[]>('/providers'),
  listActivity: (limit = 50, offset = 0) =>
    request<ActivityResponse>(`/activity?limit=${limit}&offset=${offset}`),
  clearCache: () => request<void>('/cache', { method: 'DELETE' }),
  relinkEntity: (type: 'artist' | 'album' | 'track', id: number, providerID: string) =>
    request<void>(`/relink/${type}/${id}`, {
      method: 'POST',
      body: JSON.stringify({ provider_id: providerID }),
    }),

  searchLibrary: (q: string) =>
    request<LibrarySearchResult[]>(`/library/search?q=${encodeURIComponent(q)}`),
  searchBrowseArtistTracks: (id: string, q: string, provider?: string) =>
    request<{ tracks: TrackSearchResult[] }>(`/browse/artist/${id}/tracks?q=${encodeURIComponent(q)}${provider ? `&provider=${encodeURIComponent(provider)}` : ''}`),

  listBlacklist: () => request<BlacklistEntry[]>('/blacklist'),
  deleteBlacklistEntry: (id: number) =>
    request<void>(`/blacklist/${id}`, { method: 'DELETE' }),
  clearBlacklist: () => request<void>('/blacklist', { method: 'DELETE' }),

  listCooldowns: () => request<UserCooldown[]>('/cooldowns'),
  deleteCooldown: (id: number) =>
    request<void>(`/cooldowns/${id}`, { method: 'DELETE' }),
  clearCooldowns: () => request<void>('/cooldowns', { method: 'DELETE' }),
};
