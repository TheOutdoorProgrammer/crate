export interface Artist {
  id: number;
  name: string;
  provider: string;
  provider_id: string;
  image_url?: string;
  status: 'watched' | 'partial' | 'owned';
  watch_new_releases: boolean;
  watch_new_releases_since?: string;
  created_at: string;
  updated_at: string;
  albums?: Album[];
  total_tracks?: number;
  owned_tracks?: number;
  orphaned?: boolean;
}

export interface Album {
  id: number;
  artist_id: number;
  title: string;
  year?: number;
  provider: string;
  provider_id: string;
  cover_url?: string;
  record_type: string;
  status: 'watched' | 'owned' | 'ignored';
  created_at: string;
  updated_at: string;
  artist_name?: string;
  tracks?: Track[];
}

export interface Track {
  id: number;
  album_id: number;
  title: string;
  track_number: number;
  disc_number: number;
  duration_ms: number;
  provider: string;
  provider_id: string;
  status: 'wanted' | 'downloading' | 'owned' | 'ignored';
  file_path?: string;
  downloaded_from?: string;
  download_format?: string;
  download_bitrate?: number;
  created_at: string;
  updated_at: string;
  album_title?: string;
  artist_name?: string;
}

export interface DownloadQueueItem {
  id: number;
  track_id: number;
  slskd_search_id?: string;
  status: 'pending' | 'searching' | 'downloading' | 'organizing' | 'complete' | 'failed';
  attempts: number;
  last_attempt?: string;
  error?: string;
  next_retry_at?: string;
  source: string;
  created_at: string;
  track?: Track;
}

export interface ArtistSearchResult {
  id: string;
  name: string;
  image_url: string;
  album_count: number;
  rank: number;
  metadata?: Record<string, string>;
}

export interface BrowseArtistResult {
  id: string;
  name: string;
  image_url: string;
  album_count: number;
  metadata?: Record<string, string>;
  albums: BrowseAlbumResult[];
  watched_album_ids?: string[];
  artist_watched?: boolean;
}

export interface BrowseAlbumResult {
  id: string;
  title: string;
  cover_url: string;
  year: number;
  record_type: string;
  rank: number;
  metadata?: Record<string, string>;
}

export interface BrowseAlbumDetail {
  id: string;
  title: string;
  cover_url: string;
  year: number;
  artist_name: string;
  tracks: BrowseTrackResult[];
  metadata?: Record<string, string>;
  album_watched?: boolean;
  watched_track_ids?: string[];
}

export interface BrowseTrackResult {
  id: string;
  title: string;
  track_number: number;
  disc_number: number;
  duration_ms: number;
  rank: number;
  metadata?: Record<string, string>;
}

export interface DownloadProgress {
  username: string;
  filename: string;
  percent_complete: number;
  average_speed_bps: number;
  bytes_transferred: number;
  size: number;
  state: string;
}

export interface SystemStatus {
  status: string;
  version?: string;
  artists_count: number;
  total_tracks?: number;
  owned_tracks?: number;
  pending_downloads: number;
  active_downloads: number;
}

export interface SearchResponse {
  artists: ArtistSearchResult[];
  total: number;
}

export interface ActivityResponse {
  items: ActivityLog[];
  total: number;
}

export interface ActivityLog {
  id: number;
  action: string;
  entity_type: string;
  entity_id: number;
  details: string;
  created_at: string;
}

export interface ManualSearchResult {
  username: string;
  filename: string;
  size: number;
  bit_rate: number;
  sample_rate: number;
  bit_depth: number;
  duration: number;
  format: string;
  score: number;
  free_slot: boolean;
  queue_length: number;
  blacklisted: boolean;
  negative_match: boolean;
}

export interface ManualSearchResponse {
  results: ManualSearchResult[];
  is_complete: boolean;
  file_count: number;
}

export interface ManualSearchStart {
  search_id: string;
  track_id: number;
  query: string;
}

export interface ProviderInfo {
  name: string;
  display_name: string;
  version: string;
  address: string;
  healthy: boolean;
}

export interface LibrarySearchResult {
  artist_id: number;
  artist_name: string;
  album_id: number;
  album_title: string;
  track_id: number;
  track_title: string;
}

export interface TrackSearchResult {
  id: string;
  title: string;
  duration_ms: number;
  album_id: string;
  album_title: string;
  album_cover_url: string;
  album_year: number;
}

export interface BlacklistEntry {
  id: number;
  username: string;
  filename: string;
  reason: string;
  created_at: string;
}

export interface UserCooldown {
  id: number;
  username: string;
  reason: string;
  expires_at: string;
  created_at: string;
}

export interface ImportReport {
  artists_added: number;
  albums_added: number;
  tracks_added: number;
  tracks_claimed: number;
  tracks_known: number;
  musicbrainz_linked: number;
  duplicate_files: number;
  files_skipped: number;
  skipped_samples?: { path: string; reason: string }[];
}

export interface ImportState {
  status: 'idle' | 'running' | 'done' | 'failed';
  dry_run: boolean;
  path?: string;
  started_at?: string;
  processed: number;
  total: number;
  report?: ImportReport;
  error?: string;
}
