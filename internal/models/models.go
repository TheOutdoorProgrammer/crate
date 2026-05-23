package models

type ArtistStatus string

const (
	ArtistStatusWatched ArtistStatus = "watched"
	ArtistStatusPartial ArtistStatus = "partial"
	ArtistStatusOwned   ArtistStatus = "owned"
)

type AlbumStatus string

const (
	AlbumStatusWatched AlbumStatus = "watched"
	AlbumStatusOwned   AlbumStatus = "owned"
	AlbumStatusIgnored AlbumStatus = "ignored"
)

type TrackStatus string

const (
	TrackStatusWanted      TrackStatus = "wanted"
	TrackStatusDownloading TrackStatus = "downloading"
	TrackStatusOwned       TrackStatus = "owned"
	TrackStatusIgnored     TrackStatus = "ignored"
)

type DownloadStatus string

const (
	DownloadStatusPending     DownloadStatus = "pending"
	DownloadStatusSearching   DownloadStatus = "searching"
	DownloadStatusDownloading DownloadStatus = "downloading"
	DownloadStatusOrganizing  DownloadStatus = "organizing"
	DownloadStatusComplete    DownloadStatus = "complete"
	DownloadStatusFailed      DownloadStatus = "failed"
)

type Artist struct {
	ID                    int64        `json:"id" db:"id"`
	Name                  string       `json:"name" db:"name"`
	Provider              string       `json:"provider" db:"provider"`
	ProviderID            string       `json:"provider_id" db:"provider_id"`
	ImageURL              *string      `json:"image_url,omitempty" db:"image_url"`
	Status                ArtistStatus `json:"status" db:"status"`
	WatchNewReleases      bool         `json:"watch_new_releases" db:"watch_new_releases"`
	WatchNewReleasesSince *string      `json:"watch_new_releases_since,omitempty" db:"watch_new_releases_since"`
	CreatedAt             string       `json:"created_at" db:"created_at"`
	UpdatedAt             string       `json:"updated_at" db:"updated_at"`
	Albums                []Album      `json:"albums,omitempty"`
	TotalTracks           int          `json:"total_tracks,omitempty"`
	OwnedTracks           int          `json:"owned_tracks,omitempty"`
	Orphaned              bool         `json:"orphaned,omitempty"`
}

type Album struct {
	ID         int64       `json:"id" db:"id"`
	ArtistID   int64       `json:"artist_id" db:"artist_id"`
	Title      string      `json:"title" db:"title"`
	Year       *int        `json:"year,omitempty" db:"year"`
	Provider   string      `json:"provider" db:"provider"`
	ProviderID string      `json:"provider_id" db:"provider_id"`
	CoverURL   *string     `json:"cover_url,omitempty" db:"cover_url"`
	RecordType string      `json:"record_type" db:"record_type"`
	Status     AlbumStatus `json:"status" db:"status"`
	CreatedAt  string      `json:"created_at" db:"created_at"`
	UpdatedAt  string      `json:"updated_at" db:"updated_at"`
	ArtistName string      `json:"artist_name,omitempty"`
	Tracks     []Track     `json:"tracks,omitempty"`
}

type Track struct {
	ID              int64       `json:"id" db:"id"`
	AlbumID         int64       `json:"album_id" db:"album_id"`
	Title           string      `json:"title" db:"title"`
	TrackNumber     int         `json:"track_number" db:"track_number"`
	DiscNumber      int         `json:"disc_number" db:"disc_number"`
	DurationMs      int         `json:"duration_ms" db:"duration_ms"`
	Provider        string      `json:"provider" db:"provider"`
	ProviderID      string      `json:"provider_id" db:"provider_id"`
	Status          TrackStatus `json:"status" db:"status"`
	FilePath        *string     `json:"file_path,omitempty" db:"file_path"`
	DownloadedFrom  *string     `json:"downloaded_from,omitempty" db:"downloaded_from"`
	DownloadFormat  *string     `json:"download_format,omitempty" db:"download_format"`
	DownloadBitrate *int        `json:"download_bitrate,omitempty" db:"download_bitrate"`
	CreatedAt       string      `json:"created_at" db:"created_at"`
	UpdatedAt       string      `json:"updated_at" db:"updated_at"`
	AlbumTitle      string      `json:"album_title,omitempty"`
	ArtistName      string      `json:"artist_name,omitempty"`
}

type QualityTier struct {
	Format     string `json:"format"`
	MinBitrate int    `json:"min_bitrate,omitempty"`
	Label      string `json:"label"`
}

type DownloadQueueItem struct {
	ID            int64          `json:"id" db:"id"`
	TrackID       int64          `json:"track_id" db:"track_id"`
	SlskdSearchID *string        `json:"slskd_search_id,omitempty" db:"slskd_search_id"`
	Status        DownloadStatus `json:"status" db:"status"`
	Attempts      int            `json:"attempts" db:"attempts"`
	LastAttempt   *string        `json:"last_attempt,omitempty" db:"last_attempt"`
	Error         *string        `json:"error,omitempty" db:"error"`
	NextRetryAt   *string        `json:"next_retry_at,omitempty" db:"next_retry_at"`
	CreatedAt     string         `json:"created_at" db:"created_at"`
	Track         *Track         `json:"track,omitempty"`
}

type ActivityLog struct {
	ID        int64  `json:"id"`
	Action    string `json:"action"`
	EntityType string `json:"entity_type"`
	EntityID  int64  `json:"entity_id"`
	Details   string `json:"details"`
	CreatedAt string `json:"created_at"`
}
