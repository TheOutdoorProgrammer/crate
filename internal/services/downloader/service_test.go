package downloader

import (
	"testing"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/services/slskd"
)

func TestRetryDelay(t *testing.T) {
	expected := []time.Duration{
		5 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
		1 * time.Hour,
	}
	for i, want := range expected {
		got := retryDelay(i)
		if got != want {
			t.Errorf("retryDelay(%d) = %v, want %v", i, got, want)
		}
	}
	if got := retryDelay(10); got != 1*time.Hour {
		t.Errorf("retryDelay(10) = %v, want 1h", got)
	}
}

func TestRetryDelayTotalUnder2Hours(t *testing.T) {
	var total time.Duration
	for i := 0; i < 4; i++ {
		total += retryDelay(i)
	}
	if total > 2*time.Hour+10*time.Minute {
		t.Errorf("total retry time %v exceeds 2h10m", total)
	}
}

type fakeBlacklist struct {
	blocked map[string]bool
}

func (f *fakeBlacklist) IsBlacklisted(username, filename string) bool {
	return f.blocked[username+"|"+filename]
}

func TestPickBestFilePrefersFLAC(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username:          "user1",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.mp3", Size: 8000000, BitRate: 320},
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
	}
	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	best := pickBestFile(results, track, nil)
	if best == nil {
		t.Fatal("expected a result")
	}
	if best.file.Filename != "music/Artist - Track.flac" {
		t.Errorf("expected FLAC, got %s", best.file.Filename)
	}
}

func TestPickBestFileSkipsLocked(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username: "user1",
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.flac", Size: 30000000, IsLocked: true},
				{Filename: "music/Artist - Track.mp3", Size: 8000000, BitRate: 256},
			},
		},
	}
	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	best := pickBestFile(results, track, nil)
	if best == nil {
		t.Fatal("expected a result")
	}
	if best.file.Filename != "music/Artist - Track.mp3" {
		t.Errorf("expected mp3 (flac locked), got %s", best.file.Filename)
	}
}

func TestPickBestFileSkipsBlacklisted(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username:          "baduser",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
		{
			Username:          "gooduser",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.mp3", Size: 8000000, BitRate: 320},
			},
		},
	}

	bl := &fakeBlacklist{blocked: map[string]bool{
		"baduser|music/Artist - Track.flac": true,
	}}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	best := pickBestFile(results, track, bl)
	if best == nil {
		t.Fatal("expected a result")
	}
	if best.username != "gooduser" {
		t.Errorf("expected gooduser (baduser blacklisted), got %s", best.username)
	}
}

func TestQualityTierRank(t *testing.T) {
	tiers := []models.QualityTier{
		{Format: "flac", Label: "FLAC"},
		{Format: "mp3", MinBitrate: 320, Label: "MP3 320kbps"},
		{Format: "mp3", MinBitrate: 256, Label: "MP3 256kbps"},
	}

	tests := []struct {
		format  string
		bitrate int
		want    int
	}{
		{"flac", 0, 0},
		{"FLAC", 0, 0},
		{"mp3", 320, 1},
		{"mp3", 256, 2},
		{"mp3", 128, -1},
		{"ogg", 0, -1},
	}

	for _, tt := range tests {
		got := QualityTierRank(tiers, tt.format, tt.bitrate)
		if got != tt.want {
			t.Errorf("QualityTierRank(%q, %d) = %d, want %d", tt.format, tt.bitrate, got, tt.want)
		}
	}
}

func TestIsUpgradeable(t *testing.T) {
	tiers := []models.QualityTier{
		{Format: "flac", Label: "FLAC"},
		{Format: "mp3", MinBitrate: 320, Label: "MP3 320kbps"},
	}

	mp3 := "mp3"
	flac := "flac"
	br320 := 320

	tests := []struct {
		name   string
		track  *models.Track
		want   bool
	}{
		{"mp3 is upgradeable to flac", &models.Track{DownloadFormat: &mp3, DownloadBitrate: &br320}, true},
		{"flac is not upgradeable", &models.Track{DownloadFormat: &flac}, false},
		{"no format is upgradeable", &models.Track{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUpgradeable(tiers, tt.track)
			if got != tt.want {
				t.Errorf("IsUpgradeable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPickBestFileReturnsNilWhenAllBlacklisted(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username: "user1",
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
	}

	bl := &fakeBlacklist{blocked: map[string]bool{
		"user1|music/Artist - Track.flac": true,
	}}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	best := pickBestFile(results, track, bl)
	if best != nil {
		t.Errorf("expected nil when all results blacklisted, got %s", best.file.Filename)
	}
}

func TestScoreAllFilesReturnsSortedResults(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username:          "user1",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.mp3", Size: 8000000, BitRate: 256},
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
		{
			Username: "user2",
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.mp3", Size: 7000000, BitRate: 320},
			},
		},
	}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	scored := scoreAllFiles(results, track, nil)

	if len(scored) != 3 {
		t.Fatalf("expected 3 results, got %d", len(scored))
	}
	if scored[0].Format != "FLAC" {
		t.Errorf("expected FLAC first, got %s", scored[0].Format)
	}
	for i := 1; i < len(scored); i++ {
		if scored[i].Score > scored[i-1].Score {
			t.Errorf("results not sorted: index %d score %d > index %d score %d",
				i, scored[i].Score, i-1, scored[i-1].Score)
		}
	}
}

func TestScoreAllFilesMarksBlacklisted(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username: "user1",
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
	}

	bl := &fakeBlacklist{blocked: map[string]bool{
		"user1|music/Artist - Track.flac": true,
	}}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	scored := scoreAllFiles(results, track, bl)

	if len(scored) != 1 {
		t.Fatalf("expected 1 result, got %d", len(scored))
	}
	if !scored[0].Blacklisted {
		t.Error("expected result to be marked as blacklisted")
	}
}

func TestQualityTierRankEmptyTiers(t *testing.T) {
	rank := QualityTierRank(nil, "flac", 0)
	if rank != -1 {
		t.Errorf("expected -1 for empty tiers, got %d", rank)
	}
}

func TestIsUpgradeableEmptyTiers(t *testing.T) {
	mp3 := "mp3"
	track := &models.Track{DownloadFormat: &mp3}
	if IsUpgradeable(nil, track) {
		t.Error("expected not upgradeable with empty tiers")
	}
}

func TestQualityTierRankMatchesBitrateThreshold(t *testing.T) {
	tiers := []models.QualityTier{
		{Format: "mp3", MinBitrate: 320, Label: "MP3 320"},
		{Format: "mp3", MinBitrate: 192, Label: "MP3 192"},
	}

	if r := QualityTierRank(tiers, "mp3", 320); r != 0 {
		t.Errorf("320kbps should match tier 0, got %d", r)
	}
	if r := QualityTierRank(tiers, "mp3", 256); r != 1 {
		t.Errorf("256kbps should match tier 1 (192+), got %d", r)
	}
	if r := QualityTierRank(tiers, "mp3", 128); r != -1 {
		t.Errorf("128kbps should not match any tier, got %d", r)
	}
}
