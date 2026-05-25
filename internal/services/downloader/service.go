package downloader

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/services/slskd"
)

const defaultMaxConcurrentSlskd = 10
const staleDownloadTimeout = 5 * time.Minute

var supportedExts = map[string]bool{
	".flac": true,
	".mp3":  true,
	".ogg":  true,
	".opus": true,
	".aac":  true,
	".m4a":  true,
	".wav":  true,
}

var losslessExts = map[string]bool{
	".flac": true,
	".wav":  true,
}

type Organizer interface {
	Organize(track *models.Track) error
}

type PostDownloadNotifier interface {
	TriggerScan(ctx context.Context)
}

type Service struct {
	queries      *db.Queries
	slskd        *slskd.Client
	organizer    Organizer
	activityLog  *activity.Log
	notifiers    []PostDownloadNotifier
}

func NewService(queries *db.Queries, slskdClient *slskd.Client, org Organizer, actLog *activity.Log) *Service {
	return &Service{queries: queries, slskd: slskdClient, organizer: org, activityLog: actLog}
}

func (s *Service) AddNotifier(n PostDownloadNotifier) {
	s.notifiers = append(s.notifiers, n)
}

type ProgressItem struct {
	Username        string  `json:"username"`
	Filename        string  `json:"filename"`
	PercentComplete float64 `json:"percent_complete"`
	AverageSpeed    float64 `json:"average_speed_bps"`
	BytesTransferred int64  `json:"bytes_transferred"`
	Size            int64   `json:"size"`
	State           string  `json:"state"`
}

func (s *Service) GetProgress(ctx context.Context) ([]ProgressItem, error) {
	dirs, err := s.slskd.GetAllDownloads(ctx)
	if err != nil {
		return nil, err
	}
	var items []ProgressItem
	for _, ud := range dirs {
		for _, dir := range ud.Directories {
			for _, f := range dir.Files {
				if strings.Contains(f.State, "Completed") {
					continue
				}
				items = append(items, ProgressItem{
					Username:         ud.Username,
					Filename:         filepath.Base(f.Filename),
					PercentComplete:  f.PercentComplete,
					AverageSpeed:     f.AverageSpeed,
					BytesTransferred: f.BytesTransferred,
					Size:             f.Size,
					State:            f.State,
				})
			}
		}
	}
	return items, nil
}

// Run processes the download queue on a ticker. Call in a goroutine.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	retryable, err := s.queries.ListRetryableDownloads()
	if err == nil {
		for _, d := range retryable {
			if d.Attempts >= 4 && d.Source == "scheduler" {
				slog.Info("downloader: removing exhausted scheduler download", "download_id", d.ID)
				s.queries.DeleteDownload(d.ID)
				continue
			}
			slog.Info("downloader: retrying", "download_id", d.ID, "attempts", d.Attempts)
			s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusPending, nil, d.Error)
		}
	}

	maxConcurrent := s.getMaxConcurrent()
	activeCount, err := s.queries.CountActiveDownloads()
	if err != nil {
		slog.Error("downloader: count active", "error", err)
		activeCount = 0
	}
	slots := maxConcurrent - activeCount

	pending, err := s.queries.ListDownloads("pending")
	if err != nil {
		slog.Error("downloader: list pending", "error", err)
		return
	}
	for _, d := range pending {
		if slots <= 0 {
			break
		}
		if d.Attempts >= 4 {
			if d.Source == "scheduler" {
				slog.Info("downloader: removing exhausted scheduler download", "download_id", d.ID)
				s.queries.DeleteDownload(d.ID)
				continue
			}
			reason := "exceeded maximum retry attempts"
			if d.Error != nil && *d.Error != "" {
				reason += ": " + *d.Error
			}
			s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &reason)
			continue
		}
		if err := s.process(ctx, d); err != nil {
			slog.Error("downloader: process", "download_id", d.ID, "error", err)
		}
		slots--
	}

	searching, err := s.queries.ListDownloads("searching")
	if err != nil {
		return
	}
	for _, d := range searching {
		if err := s.checkSearch(ctx, d); err != nil {
			slog.Error("downloader: check search", "download_id", d.ID, "error", err)
		}
	}

	downloading, err := s.queries.ListDownloads("downloading")
	if err != nil {
		return
	}
	for _, d := range downloading {
		if err := s.checkDownload(ctx, d); err != nil {
			slog.Error("downloader: check download", "download_id", d.ID, "error", err)
		}
	}

	organizing, err := s.queries.ListDownloads("organizing")
	if err != nil {
		return
	}
	for _, d := range organizing {
		if err := s.retryOrganize(d); err != nil {
			slog.Error("downloader: retry organize", "download_id", d.ID, "error", err)
		}
	}
}

func (s *Service) getMaxConcurrent() int {
	v, err := s.queries.GetSetting("max_concurrent_slskd")
	if err != nil {
		return defaultMaxConcurrentSlskd
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultMaxConcurrentSlskd
	}
	return n
}

// process kicks off a search for a pending download item.
func (s *Service) process(ctx context.Context, d models.DownloadQueueItem) error {
	track, err := s.queries.GetTrackWithMeta(d.TrackID)
	if err != nil {
		return err
	}

	query := track.ArtistName + " " + track.Title
	slog.Info("downloader: searching", "query", query, "track_id", track.ID)
	s.logActivity("search_started", "track", track.ID,
		fmt.Sprintf("Searching for: %s - %s", track.ArtistName, track.Title))

	search, err := s.slskd.StartSearch(ctx, query)
	if err != nil {
		return s.failWithRetry(d, err.Error())
	}

	return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusSearching, &search.ID, nil)
}

// checkSearch polls a search and initiates download when results are ready.
func (s *Service) checkSearch(ctx context.Context, d models.DownloadQueueItem) error {
	if d.SlskdSearchID == nil {
		errStr := "missing search id"
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
	}

	search, err := s.slskd.GetSearch(ctx, *d.SlskdSearchID)
	if err != nil {
		return err
	}

	if !search.IsComplete {
		if d.LastAttempt != nil && time.Since(mustParseTime(*d.LastAttempt)) < 5*time.Minute {
			return nil
		}
	}

	track, err := s.queries.GetTrackWithMeta(d.TrackID)
	if err != nil {
		return err
	}

	best := pickBestFile(search.Responses, track, s.queries)
	if best == nil {
		_ = s.slskd.DeleteSearch(ctx, *d.SlskdSearchID)
		errStr := "no suitable file found"
		s.logActivity("download_failed", "track", track.ID,
			fmt.Sprintf("No results for: %s - %s", track.ArtistName, track.Title))
		_ = s.queries.UpdateTrackStatus(track.ID, models.TrackStatusWanted)
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
	}

	slog.Info("downloader: starting download",
		"username", best.username,
		"filename", best.file.Filename,
		"track_id", track.ID,
	)
	s.logActivity("download_started", "track", track.ID,
		fmt.Sprintf("Downloading from %s: %s", best.username, filepath.Base(best.file.Filename)))

	_ = s.queries.UpdateTrackDownloadedFrom(track.ID, best.username)

	ext := strings.ToLower(filepath.Ext(best.file.Filename))
	if format := strings.TrimPrefix(ext, "."); format != "" {
		_ = s.queries.UpdateTrackQuality(track.ID, format, best.file.BitRate)
	}

	transfer, err := s.slskd.StartDownload(ctx, best.username, best.file.Filename, best.file.Size)
	if err != nil {
		return s.failWithRetry(d, err.Error())
	}

	_ = s.slskd.DeleteSearch(ctx, *d.SlskdSearchID)

	// Store transfer ID in slskd_search_id field (reuse the column)
	transferKey := best.username + "|" + transfer.ID
	if err := s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusDownloading, &transferKey, nil); err != nil {
		return err
	}
	return s.queries.UpdateTrackStatus(track.ID, models.TrackStatusDownloading)
}

// checkDownload polls an active transfer and marks complete when done.
func (s *Service) checkDownload(ctx context.Context, d models.DownloadQueueItem) error {
	if d.SlskdSearchID == nil {
		return nil
	}

	parts := strings.SplitN(*d.SlskdSearchID, "|", 2)
	if len(parts) != 2 {
		return nil
	}
	username, transferID := parts[0], parts[1]

	transfer, err := s.slskd.GetDownload(ctx, username, transferID)
	if err != nil {
		// Transfer may have been cleaned up; mark failed so it retries
		errStr := err.Error()
		_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
	}

	state := transfer.State
	switch {
	case strings.Contains(state, "Succeeded"):
		rawName := transfer.Filename
		if idx := strings.LastIndexAny(rawName, `/\`); idx >= 0 {
			rawName = rawName[idx+1:]
		}
		if err := s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusOrganizing, d.SlskdSearchID, nil); err != nil {
			return err
		}
		slog.Info("downloader: organizing", "filename", rawName, "track_id", d.TrackID)
		track, err := s.queries.GetTrackWithMeta(d.TrackID)
		if err != nil {
			return err
		}
		track.FilePath = &rawName

		if err := s.organizer.Organize(track); err != nil {
			slog.Error("downloader: organize failed, will retry", "filename", rawName, "track_id", d.TrackID, "error", err)
			return err
		}
		s.logActivity("download_complete", "track", d.TrackID,
			fmt.Sprintf("Downloaded: %s - %s from %s", track.ArtistName, track.Title, username))
		for _, n := range s.notifiers {
			n.TriggerScan(ctx)
		}
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusComplete, d.SlskdSearchID, nil)

	case strings.Contains(state, "Errored"), strings.Contains(state, "Rejected"), strings.Contains(state, "Cancelled"):
		_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
		_ = s.queries.BlacklistFile(username, transfer.Filename, "transfer "+state)
		slog.Info("downloader: blacklisted file", "username", username, "filename", filepath.Base(transfer.Filename))
		s.logActivity("download_failed", "track", d.TrackID,
			fmt.Sprintf("Transfer failed: %s from %s (blacklisted)", state, username))
		return s.failWithRetry(d, "transfer failed: "+state)
	}

	// Check for stale transfers — no progress for too long means peer is gone
	if transfer.BytesTransferred > d.LastProgressBytes {
		_ = s.queries.UpdateDownloadProgress(d.ID, transfer.BytesTransferred)
		return nil
	}
	if d.LastAttempt != nil {
		lastProgress := mustParseTime(*d.LastAttempt)
		if !lastProgress.IsZero() && time.Since(lastProgress) > staleDownloadTimeout {
			_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
			_ = s.queries.BlacklistFile(username, transfer.Filename, "stale transfer: no progress for "+staleDownloadTimeout.String())
			slog.Info("downloader: stale transfer, blacklisting", "username", username, "filename", filepath.Base(transfer.Filename), "download_id", d.ID)
			s.logActivity("download_failed", "track", d.TrackID,
				fmt.Sprintf("Stale transfer (no progress for %s) from %s (blacklisted)", staleDownloadTimeout, username))
			return s.failWithRetry(d, "stale transfer: no progress for "+staleDownloadTimeout.String())
		}
	}
	return nil
}

// retryOrganize is called for downloads stuck in "organizing" (e.g. after a crash).
// It re-derives the filename from the slskd transfer key and retries the move.
func (s *Service) retryOrganize(d models.DownloadQueueItem) error {
	if d.SlskdSearchID == nil {
		return nil
	}
	parts := strings.SplitN(*d.SlskdSearchID, "|", 2)
	if len(parts) != 2 {
		return nil
	}
	// The transfer key is "username|transferID". We stored it before transitioning
	// to organizing, so parse the original slskd filename from the transfer record.
	// We don't have the filename directly, so look it up from the track if available,
	// otherwise fall back to a no-op (the file is either already moved or needs a manual fix).
	track, err := s.queries.GetTrackWithMeta(d.TrackID)
	if err != nil {
		return err
	}
	if track.FilePath == nil {
		// No filename recorded yet; nothing to retry
		return nil
	}
	slog.Info("downloader: retrying organize", "filename", *track.FilePath, "track_id", d.TrackID)
	if err := s.organizer.Organize(track); err != nil {
		slog.Error("downloader: retry organize failed", "filename", *track.FilePath, "track_id", d.TrackID, "error", err)
		return err
	}
	return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusComplete, d.SlskdSearchID, nil)
}

func (s *Service) failWithRetry(d models.DownloadQueueItem, errMsg string) error {
	backoff := retryDelay(d.Attempts)
	// 5m + 15m + 30m + 1h = 1h50m cumulative at attempt 4; stop after ~2h
	if d.Attempts >= 4 {
		_ = s.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
		if d.Source == "scheduler" {
			slog.Info("downloader: removing exhausted scheduler download", "download_id", d.ID)
			return s.queries.DeleteDownload(d.ID)
		}
		slog.Warn("downloader: giving up after 2h of retries", "download_id", d.ID, "attempts", d.Attempts)
		errFinal := errMsg + " (giving up after 2h)"
		return s.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errFinal)
	}
	retryAt := time.Now().UTC().Add(backoff).Format(time.RFC3339)
	slog.Info("downloader: scheduling retry", "download_id", d.ID, "attempts", d.Attempts, "retry_in", backoff)
	return s.queries.ScheduleRetry(d.ID, retryAt, errMsg)
}

func retryDelay(attempts int) time.Duration {
	delays := []time.Duration{
		5 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
		1 * time.Hour,
	}
	if attempts >= len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempts]
}

func (s *Service) logActivity(action, entityType string, entityID int64, details string) {
	if s.activityLog != nil {
		s.activityLog.Record(action, entityType, entityID, details)
	}
}

// ManualSearchResult is a scored slskd result returned to the frontend.
type ManualSearchResult struct {
	Username       string `json:"username"`
	Filename       string `json:"filename"`
	Size           int64  `json:"size"`
	BitRate        int    `json:"bit_rate"`
	SampleRate     int    `json:"sample_rate"`
	BitDepth       int    `json:"bit_depth"`
	Duration       int    `json:"duration"`
	Format         string `json:"format"`
	Score          int    `json:"score"`
	FreeSlot       bool   `json:"free_slot"`
	QueueLength    int    `json:"queue_length"`
	Blacklisted    bool   `json:"blacklisted"`
}

// ManualSearch runs a slskd search for a track and returns all scored results.
func (s *Service) ManualSearch(ctx context.Context, trackID int64) ([]ManualSearchResult, error) {
	track, err := s.queries.GetTrackWithMeta(trackID)
	if err != nil {
		return nil, err
	}

	query := track.ArtistName + " " + track.Title
	search, err := s.slskd.StartSearch(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("start search: %w", err)
	}

	// Poll until complete (max ~30s)
	for i := 0; i < 15; i++ {
		search, err = s.slskd.GetSearch(ctx, search.ID)
		if err != nil {
			break
		}
		if search.IsComplete {
			break
		}
		time.Sleep(2 * time.Second)
	}

	if search == nil {
		return nil, fmt.Errorf("search failed")
	}

	candidates := scoreCandidates(search.Responses, track, nil)

	_ = s.slskd.DeleteSearch(ctx, search.ID)

	results := make([]ManualSearchResult, 0, len(candidates))
	for _, c := range candidates {
		ext := strings.ToLower(filepath.Ext(c.file.Filename))
		if ext == "" {
			ext = inferExt(strings.ToLower(c.file.Filename))
		}
		format := strings.TrimPrefix(ext, ".")
		if format == "mp3" && c.file.BitRate > 0 {
			format = fmt.Sprintf("MP3 %dkbps", c.file.BitRate)
		} else {
			format = strings.ToUpper(format)
		}

		results = append(results, ManualSearchResult{
			Username:    c.username,
			Filename:    c.file.Filename,
			Size:        c.file.Size,
			BitRate:     c.file.BitRate,
			SampleRate:  c.file.SampleRate,
			BitDepth:    c.file.BitDepth,
			Duration:    c.file.Length,
			Format:      format,
			Score:       c.score,
			FreeSlot:    c.freeSlot,
			QueueLength: c.queueLength,
			Blacklisted: s.queries.IsBlacklisted(c.username, c.file.Filename),
		})
	}

	return results, nil
}

// ManualDownload starts a download of a specific file from a specific user.
func (s *Service) ManualDownload(ctx context.Context, trackID int64, username, filename string, size int64, bitrate int) error {
	track, err := s.queries.GetTrackWithMeta(trackID)
	if err != nil {
		return err
	}

	_ = s.queries.UpdateTrackDownloadedFrom(trackID, username)

	ext := strings.ToLower(filepath.Ext(filename))
	if format := strings.TrimPrefix(ext, "."); format != "" {
		_ = s.queries.UpdateTrackQuality(trackID, format, bitrate)
	}

	transfer, err := s.slskd.StartDownload(ctx, username, filename, size)
	if err != nil {
		return fmt.Errorf("start download: %w", err)
	}

	dlID, err := s.queries.EnqueueDownloadReturningID(trackID)
	if err != nil {
		return err
	}

	transferKey := username + "|" + transfer.ID
	_ = s.queries.UpdateDownloadStatus(dlID, models.DownloadStatusDownloading, &transferKey, nil)
	_ = s.queries.UpdateTrackStatus(trackID, models.TrackStatusDownloading)

	s.logActivity("download_started", "track", trackID,
		fmt.Sprintf("Manual download from %s: %s - %s", username, track.ArtistName, track.Title))

	return nil
}

func scoreCandidates(results []slskd.SearchResult, track *models.Track, bl blacklistChecker) []candidate {
	titleLower := strings.ToLower(track.Title)
	artistLower := strings.ToLower(track.ArtistName)

	var candidates []candidate

	for _, result := range results {
		for _, f := range result.Files {
			if f.IsLocked {
				continue
			}
			if bl != nil && bl.IsBlacklisted(result.Username, f.Filename) {
				continue
			}
			nameLower := strings.ToLower(f.Filename)
			if !strings.Contains(nameLower, titleLower) {
				continue
			}

			ext := strings.ToLower(filepath.Ext(f.Filename))
			if ext == "" {
				ext = inferExt(nameLower)
				if ext == "" {
					continue
				}
			}
			if !supportedExts[ext] {
				continue
			}

			score := formatScore(ext, f.BitRate)
			if strings.Contains(nameLower, artistLower) {
				score += 30
			}
			if result.HasFreeUploadSlot {
				score += 20
			}
			if result.QueueLength < 10 {
				score += 10
			}

			candidates = append(candidates, candidate{
				username:    result.Username,
				file:        f,
				score:       score,
				freeSlot:    result.HasFreeUploadSlot,
				queueLength: result.QueueLength,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	return candidates
}

// QualityTierRank returns the rank of a format/bitrate in the given tiers.
// Lower rank = higher priority. Returns -1 if not matched.
func QualityTierRank(tiers []models.QualityTier, format string, bitrate int) int {
	format = strings.ToLower(format)
	for i, tier := range tiers {
		if strings.ToLower(tier.Format) != format {
			continue
		}
		if tier.MinBitrate > 0 && bitrate > 0 && bitrate < tier.MinBitrate {
			continue
		}
		return i
	}
	return -1
}

// IsUpgradeable returns true if the track's current quality can be improved
// according to the tier list (a lower-index tier exists that the track doesn't match).
func IsUpgradeable(tiers []models.QualityTier, track *models.Track) bool {
	if track.DownloadFormat == nil {
		return len(tiers) > 0
	}
	currentRank := QualityTierRank(tiers, *track.DownloadFormat, derefInt(track.DownloadBitrate))
	if currentRank < 0 {
		return len(tiers) > 0
	}
	return currentRank > 0
}

func mustParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

type candidate struct {
	username    string
	file        slskd.SearchFile
	score       int
	freeSlot    bool
	queueLength int
}

type blacklistChecker interface {
	IsBlacklisted(username, filename string) bool
}

func pickBestFile(results []slskd.SearchResult, track *models.Track, bl blacklistChecker) *candidate {
	candidates := scoreCandidates(results, track, bl)
	if len(candidates) == 0 {
		return nil
	}
	return &candidates[0]
}

func inferExt(nameLower string) string {
	basename := nameLower
	if idx := strings.LastIndexAny(nameLower, `/\`); idx >= 0 {
		basename = nameLower[idx+1:]
	}
	for ext := range supportedExts {
		if strings.HasSuffix(basename, ext) {
			return ext
		}
	}
	return ""
}

func formatScore(ext string, bitRate int) int {
	if losslessExts[ext] {
		return 100
	}
	switch ext {
	case ".ogg", ".opus", ".aac", ".m4a":
		if bitRate >= 256 {
			return 60
		}
		if bitRate >= 192 {
			return 40
		}
		return 25
	default:
		if bitRate >= 320 {
			return 50
		}
		if bitRate >= 256 {
			return 30
		}
		return 10
	}
}
