package db

import (
	"database/sql"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/models"
)

type Queries struct {
	db *sql.DB
}

func NewQueries(db *sql.DB) *Queries {
	return &Queries{db: db}
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// Artists

func (q *Queries) CreateArtist(a *models.Artist) error {
	ts := now()
	result, err := q.db.Exec(
		`INSERT INTO artists (name, provider, provider_id, image_url, status, watch_new_releases, watch_new_releases_since, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.Provider, a.ProviderID, a.ImageURL, a.Status, a.WatchNewReleases, a.WatchNewReleasesSince, ts, ts,
	)
	if err != nil {
		return err
	}
	a.ID, _ = result.LastInsertId()
	a.CreatedAt = ts
	a.UpdatedAt = ts
	return nil
}

func (q *Queries) GetArtist(id int64) (*models.Artist, error) {
	a := &models.Artist{}
	err := q.db.QueryRow(
		`SELECT id, name, provider, provider_id, image_url, status, watch_new_releases, watch_new_releases_since, created_at, updated_at
		 FROM artists WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.Provider, &a.ProviderID, &a.ImageURL, &a.Status, &a.WatchNewReleases, &a.WatchNewReleasesSince, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (q *Queries) ListArtists() ([]models.Artist, error) {
	rows, err := q.db.Query(
		`SELECT a.id, a.name, a.provider, a.provider_id, a.image_url, a.status, a.watch_new_releases, a.watch_new_releases_since, a.created_at, a.updated_at,
		        COUNT(t.id) as total_tracks,
		        COALESCE(SUM(CASE WHEN t.status = 'owned' THEN 1 ELSE 0 END), 0) as owned_tracks
		 FROM artists a
		 LEFT JOIN albums al ON al.artist_id = a.id
		 LEFT JOIN tracks t ON t.album_id = al.id
		 GROUP BY a.id
		 ORDER BY a.name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []models.Artist
	for rows.Next() {
		var a models.Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.Provider, &a.ProviderID, &a.ImageURL, &a.Status,
			&a.WatchNewReleases, &a.WatchNewReleasesSince, &a.CreatedAt, &a.UpdatedAt, &a.TotalTracks, &a.OwnedTracks); err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}
	return artists, rows.Err()
}

func (q *Queries) UpdateArtistStatus(id int64, status models.ArtistStatus) error {
	_, err := q.db.Exec(`UPDATE artists SET status = ?, updated_at = ? WHERE id = ?`, status, now(), id)
	return err
}

func (q *Queries) SetArtistWatchNewReleases(id int64, enabled bool) error {
	ts := now()
	var since *string
	if enabled {
		since = &ts
	}
	_, err := q.db.Exec(
		`UPDATE artists SET watch_new_releases = ?, watch_new_releases_since = COALESCE(?, watch_new_releases_since), updated_at = ? WHERE id = ?`,
		enabled, since, ts, id,
	)
	return err
}

func (q *Queries) DeleteArtist(id int64) error {
	_, err := q.db.Exec(`DELETE FROM artists WHERE id = ?`, id)
	return err
}

func (q *Queries) FindArtistByProvider(provider, providerID string) (*models.Artist, error) {
	a := &models.Artist{}
	err := q.db.QueryRow(
		`SELECT id, name, provider, provider_id, image_url, status, watch_new_releases, watch_new_releases_since, created_at, updated_at
		 FROM artists WHERE provider = ? AND provider_id = ?`, provider, providerID,
	).Scan(&a.ID, &a.Name, &a.Provider, &a.ProviderID, &a.ImageURL, &a.Status, &a.WatchNewReleases, &a.WatchNewReleasesSince, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (q *Queries) ListWatchedArtists() ([]models.Artist, error) {
	rows, err := q.db.Query(
		`SELECT id, name, provider, provider_id, image_url, status, watch_new_releases, watch_new_releases_since, created_at, updated_at
		 FROM artists WHERE watch_new_releases = 1 ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []models.Artist
	for rows.Next() {
		var a models.Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.Provider, &a.ProviderID, &a.ImageURL, &a.Status,
			&a.WatchNewReleases, &a.WatchNewReleasesSince, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}
	return artists, rows.Err()
}

func (q *Queries) RelinkArtist(id int64, provider, providerID string) error {
	_, err := q.db.Exec(`UPDATE artists SET provider = ?, provider_id = ?, updated_at = ? WHERE id = ?`,
		provider, providerID, now(), id)
	return err
}

func (q *Queries) RelinkAlbum(id int64, provider, providerID string) error {
	_, err := q.db.Exec(`UPDATE albums SET provider = ?, provider_id = ?, updated_at = ? WHERE id = ?`,
		provider, providerID, now(), id)
	return err
}

func (q *Queries) RelinkTrack(id int64, provider, providerID string) error {
	_, err := q.db.Exec(`UPDATE tracks SET provider = ?, provider_id = ?, updated_at = ? WHERE id = ?`,
		provider, providerID, now(), id)
	return err
}

// Albums

func (q *Queries) CreateAlbum(a *models.Album) error {
	ts := now()
	result, err := q.db.Exec(
		`INSERT INTO albums (artist_id, title, year, provider, provider_id, cover_url, record_type, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ArtistID, a.Title, a.Year, a.Provider, a.ProviderID, a.CoverURL, a.RecordType, a.Status, ts, ts,
	)
	if err != nil {
		return err
	}
	a.ID, _ = result.LastInsertId()
	return nil
}

func (q *Queries) GetAlbum(id int64) (*models.Album, error) {
	a := &models.Album{}
	err := q.db.QueryRow(
		`SELECT al.id, al.artist_id, al.title, al.year, al.provider, al.provider_id, al.cover_url, al.record_type, al.status,
		        al.created_at, al.updated_at, ar.name
		 FROM albums al JOIN artists ar ON ar.id = al.artist_id
		 WHERE al.id = ?`, id,
	).Scan(&a.ID, &a.ArtistID, &a.Title, &a.Year, &a.Provider, &a.ProviderID, &a.CoverURL, &a.RecordType, &a.Status,
		&a.CreatedAt, &a.UpdatedAt, &a.ArtistName)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (q *Queries) ListAlbumsByArtist(artistID int64) ([]models.Album, error) {
	rows, err := q.db.Query(
		`SELECT id, artist_id, title, year, provider, provider_id, cover_url, record_type, status, created_at, updated_at
		 FROM albums WHERE artist_id = ? ORDER BY year, title`, artistID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []models.Album
	for rows.Next() {
		var a models.Album
		if err := rows.Scan(&a.ID, &a.ArtistID, &a.Title, &a.Year, &a.Provider, &a.ProviderID, &a.CoverURL, &a.RecordType,
			&a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		albums = append(albums, a)
	}
	return albums, rows.Err()
}

func (q *Queries) UpdateAlbumStatus(id int64, status models.AlbumStatus) error {
	_, err := q.db.Exec(`UPDATE albums SET status = ?, updated_at = ? WHERE id = ?`, status, now(), id)
	return err
}

func (q *Queries) FindAlbumByProvider(provider, providerID string) (*models.Album, error) {
	a := &models.Album{}
	err := q.db.QueryRow(
		`SELECT id, artist_id, title, year, provider, provider_id, cover_url, record_type, status, created_at, updated_at
		 FROM albums WHERE provider = ? AND provider_id = ?`, provider, providerID,
	).Scan(&a.ID, &a.ArtistID, &a.Title, &a.Year, &a.Provider, &a.ProviderID, &a.CoverURL, &a.RecordType, &a.Status,
		&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (q *Queries) DeleteAlbum(id int64) error {
	_, err := q.db.Exec(`DELETE FROM albums WHERE id = ?`, id)
	return err
}

// Tracks

func (q *Queries) CreateTrack(t *models.Track) error {
	ts := now()
	result, err := q.db.Exec(
		`INSERT INTO tracks (album_id, title, track_number, disc_number, duration_ms, provider, provider_id, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.AlbumID, t.Title, t.TrackNumber, t.DiscNumber, t.DurationMs, t.Provider, t.ProviderID, t.Status, ts, ts,
	)
	if err != nil {
		return err
	}
	t.ID, _ = result.LastInsertId()
	return nil
}

func (q *Queries) FindTrackByProvider(provider, providerID string) (*models.Track, error) {
	t := &models.Track{}
	err := q.db.QueryRow(
		`SELECT id, album_id, title, track_number, disc_number, duration_ms, provider, provider_id,
		        status, file_path, downloaded_from, download_format, download_bitrate, created_at, updated_at
		 FROM tracks WHERE provider = ? AND provider_id = ?`, provider, providerID,
	).Scan(&t.ID, &t.AlbumID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMs,
		&t.Provider, &t.ProviderID, &t.Status, &t.FilePath, &t.DownloadedFrom,
		&t.DownloadFormat, &t.DownloadBitrate, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (q *Queries) ListTracksByAlbum(albumID int64) ([]models.Track, error) {
	rows, err := q.db.Query(
		`SELECT id, album_id, title, track_number, disc_number, duration_ms, provider, provider_id,
		        status, file_path, downloaded_from, download_format, download_bitrate, created_at, updated_at
		 FROM tracks WHERE album_id = ? ORDER BY disc_number, track_number`, albumID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []models.Track
	for rows.Next() {
		var t models.Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMs,
			&t.Provider, &t.ProviderID, &t.Status, &t.FilePath, &t.DownloadedFrom,
			&t.DownloadFormat, &t.DownloadBitrate, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (q *Queries) UpdateTrackStatus(id int64, status models.TrackStatus) error {
	_, err := q.db.Exec(`UPDATE tracks SET status = ?, updated_at = ? WHERE id = ?`, status, now(), id)
	return err
}

func (q *Queries) DeleteTrack(id int64) error {
	_, err := q.db.Exec(`DELETE FROM tracks WHERE id = ?`, id)
	return err
}

func (q *Queries) UpdateTrackFilePath(id int64, filePath string) error {
	_, err := q.db.Exec(`UPDATE tracks SET file_path = ?, status = 'owned', updated_at = ? WHERE id = ?`,
		filePath, now(), id)
	return err
}

func (q *Queries) UpdateTrackDownloadedFrom(id int64, username string) error {
	_, err := q.db.Exec(`UPDATE tracks SET downloaded_from = ?, updated_at = ? WHERE id = ?`,
		username, now(), id)
	return err
}

func (q *Queries) GetTrackWithMeta(id int64) (*models.Track, error) {
	t := &models.Track{}
	err := q.db.QueryRow(
		`SELECT t.id, t.album_id, t.title, t.track_number, t.disc_number, t.duration_ms,
		        t.provider, t.provider_id, t.status, t.file_path, t.downloaded_from,
		        t.download_format, t.download_bitrate, t.created_at, t.updated_at,
		        al.title, ar.name
		 FROM tracks t
		 JOIN albums al ON al.id = t.album_id
		 JOIN artists ar ON ar.id = al.artist_id
		 WHERE t.id = ?`, id,
	).Scan(&t.ID, &t.AlbumID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMs,
		&t.Provider, &t.ProviderID, &t.Status, &t.FilePath, &t.DownloadedFrom,
		&t.DownloadFormat, &t.DownloadBitrate, &t.CreatedAt, &t.UpdatedAt,
		&t.AlbumTitle, &t.ArtistName)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (q *Queries) ListWantedTracks() ([]models.Track, error) {
	rows, err := q.db.Query(
		`SELECT t.id, t.album_id, t.title, t.track_number, t.disc_number, t.duration_ms,
		        t.provider, t.provider_id, t.status, t.file_path, t.downloaded_from,
		        t.download_format, t.download_bitrate, t.created_at, t.updated_at,
		        al.title, ar.name
		 FROM tracks t
		 JOIN albums al ON al.id = t.album_id
		 JOIN artists ar ON ar.id = al.artist_id
		 WHERE t.status = 'wanted'
		 ORDER BY ar.name, al.year, t.disc_number, t.track_number`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []models.Track
	for rows.Next() {
		var t models.Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMs,
			&t.Provider, &t.ProviderID, &t.Status, &t.FilePath, &t.DownloadedFrom,
			&t.DownloadFormat, &t.DownloadBitrate, &t.CreatedAt, &t.UpdatedAt,
			&t.AlbumTitle, &t.ArtistName); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (q *Queries) ListWantedTracksByArtist(artistID int64) ([]models.Track, error) {
	rows, err := q.db.Query(
		`SELECT t.id, t.album_id, t.title, t.track_number, t.disc_number, t.duration_ms,
		        t.provider, t.provider_id, t.status, t.file_path, t.downloaded_from,
		        t.download_format, t.download_bitrate, t.created_at, t.updated_at,
		        al.title, ar.name
		 FROM tracks t
		 JOIN albums al ON al.id = t.album_id
		 JOIN artists ar ON ar.id = al.artist_id
		 WHERE t.status = 'wanted' AND al.artist_id = ?
		 ORDER BY al.year, t.disc_number, t.track_number`, artistID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []models.Track
	for rows.Next() {
		var t models.Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMs,
			&t.Provider, &t.ProviderID, &t.Status, &t.FilePath, &t.DownloadedFrom,
			&t.DownloadFormat, &t.DownloadBitrate, &t.CreatedAt, &t.UpdatedAt,
			&t.AlbumTitle, &t.ArtistName); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (q *Queries) ListWantedTracksByAlbum(albumID int64) ([]models.Track, error) {
	rows, err := q.db.Query(
		`SELECT t.id, t.album_id, t.title, t.track_number, t.disc_number, t.duration_ms,
		        t.provider, t.provider_id, t.status, t.file_path, t.downloaded_from,
		        t.download_format, t.download_bitrate, t.created_at, t.updated_at,
		        al.title, ar.name
		 FROM tracks t
		 JOIN albums al ON al.id = t.album_id
		 JOIN artists ar ON ar.id = al.artist_id
		 WHERE t.status = 'wanted' AND t.album_id = ?
		 ORDER BY t.disc_number, t.track_number`, albumID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []models.Track
	for rows.Next() {
		var t models.Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMs,
			&t.Provider, &t.ProviderID, &t.Status, &t.FilePath, &t.DownloadedFrom,
			&t.DownloadFormat, &t.DownloadBitrate, &t.CreatedAt, &t.UpdatedAt,
			&t.AlbumTitle, &t.ArtistName); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (q *Queries) ListOwnedTracksWithPaths() ([]models.Track, error) {
	rows, err := q.db.Query(
		`SELECT id, album_id, title, track_number, disc_number, duration_ms,
		        provider, provider_id, status, file_path, downloaded_from,
		        download_format, download_bitrate, created_at, updated_at
		 FROM tracks WHERE status = 'owned' AND file_path IS NOT NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []models.Track
	for rows.Next() {
		var t models.Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMs,
			&t.Provider, &t.ProviderID, &t.Status, &t.FilePath, &t.DownloadedFrom,
			&t.DownloadFormat, &t.DownloadBitrate, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (q *Queries) UpdateTrackQuality(id int64, format string, bitrate int) error {
	_, err := q.db.Exec(`UPDATE tracks SET download_format = ?, download_bitrate = ?, updated_at = ? WHERE id = ?`,
		format, bitrate, now(), id)
	return err
}

func (q *Queries) ListOwnedTracksByArtist(artistID int64) ([]models.Track, error) {
	rows, err := q.db.Query(
		`SELECT t.id, t.album_id, t.title, t.track_number, t.disc_number, t.duration_ms,
		        t.provider, t.provider_id, t.status, t.file_path, t.downloaded_from,
		        t.download_format, t.download_bitrate, t.created_at, t.updated_at,
		        al.title, ar.name
		 FROM tracks t
		 JOIN albums al ON al.id = t.album_id
		 JOIN artists ar ON ar.id = al.artist_id
		 WHERE t.status = 'owned' AND al.artist_id = ?
		 ORDER BY al.year, t.disc_number, t.track_number`, artistID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []models.Track
	for rows.Next() {
		var t models.Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMs,
			&t.Provider, &t.ProviderID, &t.Status, &t.FilePath, &t.DownloadedFrom,
			&t.DownloadFormat, &t.DownloadBitrate, &t.CreatedAt, &t.UpdatedAt,
			&t.AlbumTitle, &t.ArtistName); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (q *Queries) NextArtistForUpgrade(lastID int64) (*models.Artist, error) {
	a := &models.Artist{}
	err := q.db.QueryRow(
		`SELECT id, name, provider, provider_id, image_url, status, watch_new_releases, watch_new_releases_since, created_at, updated_at
		 FROM artists WHERE id > ? ORDER BY id LIMIT 1`, lastID,
	).Scan(&a.ID, &a.Name, &a.Provider, &a.ProviderID, &a.ImageURL, &a.Status, &a.WatchNewReleases, &a.WatchNewReleasesSince, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Download Queue

func (q *Queries) EnqueueDownload(trackID int64) error {
	_, err := q.db.Exec(
		`INSERT INTO download_queue (track_id, status, created_at)
		 VALUES (?, 'pending', ?)
		 ON CONFLICT (track_id) WHERE status IN ('pending', 'searching', 'downloading', 'organizing') DO NOTHING`,
		trackID, now(),
	)
	return err
}

func (q *Queries) EnqueueDownloadReturningID(trackID int64) (int64, error) {
	result, err := q.db.Exec(
		`INSERT INTO download_queue (track_id, status, created_at)
		 VALUES (?, 'pending', ?)
		 ON CONFLICT (track_id) WHERE status IN ('pending', 'searching', 'downloading', 'organizing') DO NOTHING`,
		trackID, now(),
	)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return id, nil
}

func (q *Queries) ListDownloads(status string) ([]models.DownloadQueueItem, error) {
	query := `SELECT d.id, d.track_id, d.slskd_search_id, d.status, d.attempts, d.last_attempt, d.error, d.created_at
		 FROM download_queue d`
	var args []any
	if status != "" {
		query += " WHERE d.status = ?"
		args = append(args, status)
	}
	query += " ORDER BY d.created_at DESC"

	rows, err := q.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.DownloadQueueItem
	for rows.Next() {
		var d models.DownloadQueueItem
		if err := rows.Scan(&d.ID, &d.TrackID, &d.SlskdSearchID, &d.Status, &d.Attempts,
			&d.LastAttempt, &d.Error, &d.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (q *Queries) ListDownloadsWithTrack(status string) ([]models.DownloadQueueItem, error) {
	query := `SELECT d.id, d.track_id, d.slskd_search_id, d.status, d.attempts, d.last_attempt, d.error, d.created_at,
		        t.title, ar.name, al.title
		 FROM download_queue d
		 JOIN tracks t ON t.id = d.track_id
		 JOIN albums al ON al.id = t.album_id
		 JOIN artists ar ON ar.id = al.artist_id`
	var args []any
	if status != "" {
		query += " WHERE d.status = ?"
		args = append(args, status)
	}
	query += " ORDER BY d.created_at DESC"

	rows, err := q.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.DownloadQueueItem
	for rows.Next() {
		var d models.DownloadQueueItem
		var trackTitle, artistName, albumTitle string
		if err := rows.Scan(&d.ID, &d.TrackID, &d.SlskdSearchID, &d.Status, &d.Attempts,
			&d.LastAttempt, &d.Error, &d.CreatedAt,
			&trackTitle, &artistName, &albumTitle); err != nil {
			return nil, err
		}
		d.Track = &models.Track{ID: d.TrackID, Title: trackTitle, ArtistName: artistName, AlbumTitle: albumTitle}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (q *Queries) DeleteDownload(id int64) error {
	_, err := q.db.Exec(`DELETE FROM download_queue WHERE id = ?`, id)
	return err
}

func (q *Queries) UpdateDownloadStatus(id int64, status models.DownloadStatus, searchID *string, err *string) error {
	_, e := q.db.Exec(
		`UPDATE download_queue SET status = ?, slskd_search_id = COALESCE(?, slskd_search_id),
		 error = ?, last_attempt = ? WHERE id = ?`,
		status, searchID, err, now(), id,
	)
	return e
}

func (q *Queries) ScheduleRetry(id int64, retryAt string, errMsg string) error {
	_, err := q.db.Exec(
		`UPDATE download_queue SET status = 'failed', error = ?, next_retry_at = ?,
		 attempts = attempts + 1, last_attempt = ? WHERE id = ?`,
		errMsg, retryAt, now(), id,
	)
	return err
}

func (q *Queries) ListRetryableDownloads() ([]models.DownloadQueueItem, error) {
	rows, err := q.db.Query(
		`SELECT id, track_id, slskd_search_id, status, attempts, last_attempt, error, next_retry_at, created_at
		 FROM download_queue
		 WHERE status = 'failed' AND next_retry_at IS NOT NULL AND next_retry_at <= ?
		 ORDER BY next_retry_at`, now(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.DownloadQueueItem
	for rows.Next() {
		var d models.DownloadQueueItem
		if err := rows.Scan(&d.ID, &d.TrackID, &d.SlskdSearchID, &d.Status, &d.Attempts,
			&d.LastAttempt, &d.Error, &d.NextRetryAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

// Blacklist

func (q *Queries) BlacklistFile(username, filename, reason string) error {
	_, err := q.db.Exec(
		`INSERT INTO slskd_blacklist (username, filename, reason, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(username, filename) DO NOTHING`,
		username, filename, reason, now(),
	)
	return err
}

func (q *Queries) IsBlacklisted(username, filename string) bool {
	var count int
	q.db.QueryRow(
		`SELECT COUNT(*) FROM slskd_blacklist WHERE username = ? AND filename = ?`,
		username, filename,
	).Scan(&count)
	return count > 0
}

func (q *Queries) ListBlacklisted() ([]map[string]string, error) {
	rows, err := q.db.Query(`SELECT username, filename, reason, created_at FROM slskd_blacklist ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []map[string]string
	for rows.Next() {
		var u, f, r, c string
		if err := rows.Scan(&u, &f, &r, &c); err != nil {
			return nil, err
		}
		items = append(items, map[string]string{"username": u, "filename": f, "reason": r, "created_at": c})
	}
	return items, rows.Err()
}

// Settings

func (q *Queries) GetSetting(key string) (string, error) {
	var value string
	err := q.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	return value, err
}

func (q *Queries) SetSetting(key, value string) error {
	_, err := q.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?`,
		key, value, value,
	)
	return err
}

func (q *Queries) AllSettings() (map[string]string, error) {
	rows, err := q.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}
