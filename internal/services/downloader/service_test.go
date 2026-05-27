package downloader

import (
	"path/filepath"
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

type fakeAvailability struct {
	blocked   map[string]bool
	cooledDown map[string]bool
}

func (f *fakeAvailability) IsBlacklisted(username, filename string) bool {
	return f.blocked[username+"|"+filename]
}

func (f *fakeAvailability) IsUserCooledDown(username string) bool {
	if f.cooledDown == nil {
		return false
	}
	return f.cooledDown[username]
}

var defaultCfg = scoringConfig{fallbackEnabled: true}

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
	best := pickBestFile(results, track, nil, defaultCfg)
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
	best := pickBestFile(results, track, nil, defaultCfg)
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

	bl := &fakeAvailability{blocked: map[string]bool{
		"baduser|music/Artist - Track.flac": true,
	}}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	best := pickBestFile(results, track, bl, defaultCfg)
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

	bl := &fakeAvailability{blocked: map[string]bool{
		"user1|music/Artist - Track.flac": true,
	}}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	best := pickBestFile(results, track, bl, defaultCfg)
	if best != nil {
		t.Errorf("expected nil when all results blacklisted, got %s", best.file.Filename)
	}
}

func TestScoreCandidatesReturnsSortedResults(t *testing.T) {
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
	scored := scoreCandidates(results, track, nil, defaultCfg)

	if len(scored) != 3 {
		t.Fatalf("expected 3 results, got %d", len(scored))
	}
	if filepath.Ext(scored[0].file.Filename) != ".flac" {
		t.Errorf("expected FLAC first, got %s", filepath.Ext(scored[0].file.Filename))
	}
	for i := 1; i < len(scored); i++ {
		if scored[i].score > scored[i-1].score {
			t.Errorf("results not sorted: index %d score %d > index %d score %d",
				i, scored[i].score, i-1, scored[i-1].score)
		}
	}
}

func TestScoreCandidatesExcludesBlacklisted(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username: "user1",
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
	}

	bl := &fakeAvailability{blocked: map[string]bool{
		"user1|music/Artist - Track.flac": true,
	}}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	scored := scoreCandidates(results, track, bl, defaultCfg)

	if len(scored) != 0 {
		t.Errorf("expected 0 results when blacklisted, got %d", len(scored))
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

func TestTierBasedScoreWithTiers(t *testing.T) {
	cfg := scoringConfig{
		tiers: []models.QualityTier{
			{Format: "flac", Label: "FLAC"},
			{Format: "mp3", MinBitrate: 320, Label: "MP3 320"},
			{Format: "mp3", MinBitrate: 256, Label: "MP3 256"},
		},
		fallbackEnabled: true,
	}

	tests := []struct {
		ext       string
		bitRate   int
		wantScore int
		wantOK    bool
	}{
		{".flac", 0, 100, true},
		{".mp3", 320, 75, true},
		{".mp3", 256, 50, true},
		{".ogg", 256, 15, true},
	}

	for _, tt := range tests {
		score, ok := tierBasedScore(tt.ext, tt.bitRate, cfg)
		if ok != tt.wantOK {
			t.Errorf("tierBasedScore(%q, %d): ok=%v, want %v", tt.ext, tt.bitRate, ok, tt.wantOK)
		}
		if score != tt.wantScore {
			t.Errorf("tierBasedScore(%q, %d) = %d, want %d", tt.ext, tt.bitRate, score, tt.wantScore)
		}
	}
}

func TestTierBasedScoreRejectsWhenFallbackDisabled(t *testing.T) {
	cfg := scoringConfig{
		tiers: []models.QualityTier{
			{Format: "flac", Label: "FLAC"},
		},
		fallbackEnabled: false,
	}

	score, ok := tierBasedScore(".mp3", 320, cfg)
	if ok {
		t.Errorf("expected rejection for mp3 when fallback disabled, got score=%d", score)
	}
}

func TestTierBasedScoreNoTiersFallback(t *testing.T) {
	cfg := scoringConfig{fallbackEnabled: true}

	score, ok := tierBasedScore(".flac", 0, cfg)
	if !ok || score != 20 {
		t.Errorf("expected fallback score 20 for flac with no tiers, got %d ok=%v", score, ok)
	}
}

func TestQueueScore(t *testing.T) {
	tests := []struct {
		queueLen int
		want     int
	}{
		{0, 15},
		{1, 7},
		{2, 5},
		{5, 2},
		{14, 1},
		{15, 0},
	}

	for _, tt := range tests {
		got := queueScore(tt.queueLen)
		if got != tt.want {
			t.Errorf("queueScore(%d) = %d, want %d", tt.queueLen, got, tt.want)
		}
	}
}

func TestScoreCandidatesExcludesCooledDown(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username:          "banned_user",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
		{
			Username:          "good_user",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.mp3", Size: 8000000, BitRate: 320},
			},
		},
	}

	ac := &fakeAvailability{
		blocked:    map[string]bool{},
		cooledDown: map[string]bool{"banned_user": true},
	}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	scored := scoreCandidates(results, track, ac, defaultCfg)

	if len(scored) != 1 {
		t.Fatalf("expected 1 result (cooled down user excluded), got %d", len(scored))
	}
	if scored[0].username != "good_user" {
		t.Errorf("expected good_user, got %s", scored[0].username)
	}
}

func TestScoreCandidatesArtistBonus(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username:          "user1",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/SomeArtist - Track.mp3", Size: 8000000, BitRate: 320},
				{Filename: "music/Other - Track.mp3", Size: 8000000, BitRate: 320},
			},
		},
	}

	track := &models.Track{Title: "Track", ArtistName: "SomeArtist"}
	scored := scoreCandidates(results, track, nil, defaultCfg)

	if len(scored) != 2 {
		t.Fatalf("expected 2 results, got %d", len(scored))
	}
	if scored[0].score <= scored[1].score {
		t.Errorf("expected artist-matching file to score higher: %d vs %d", scored[0].score, scored[1].score)
	}
}

func TestScoreCandidatesFreeSlotBonus(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username:          "free_user",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.mp3", Size: 8000000, BitRate: 320},
			},
		},
		{
			Username:          "busy_user",
			HasFreeUploadSlot: false,
			QueueLength:       10,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track.mp3", Size: 8000000, BitRate: 320},
			},
		},
	}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	scored := scoreCandidates(results, track, nil, defaultCfg)

	if len(scored) != 2 {
		t.Fatalf("expected 2 results, got %d", len(scored))
	}
	if scored[0].username != "free_user" {
		t.Errorf("expected free_user first (free slot + queue bonus), got %s", scored[0].username)
	}
}

func TestStaleTimeoutForState(t *testing.T) {
	tests := []struct {
		state string
		want  time.Duration
	}{
		{"InProgress", 5 * time.Minute},
		{"Initializing", 5 * time.Minute},
		{"Queued, Remotely", 30 * time.Minute},
		{"Queued, Locally", 30 * time.Minute},
		{"Requested", 10 * time.Minute},
		{"SomeUnknownState", 5 * time.Minute},
	}

	for _, tt := range tests {
		got := staleTimeoutForState(tt.state)
		if got != tt.want {
			t.Errorf("staleTimeoutForState(%q) = %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestFallbackScoreNeverExceedsTierScores(t *testing.T) {
	cfg := scoringConfig{
		tiers: []models.QualityTier{
			{Format: "flac", Label: "FLAC"},
			{Format: "mp3", MinBitrate: 320, Label: "MP3 320"},
		},
		fallbackEnabled: true,
	}

	flacScore, _ := tierBasedScore(".flac", 0, cfg)
	mp3Score, _ := tierBasedScore(".mp3", 320, cfg)
	oggScore, _ := tierBasedScore(".ogg", 320, cfg)

	if oggScore >= mp3Score {
		t.Errorf("fallback ogg score (%d) should be less than tier mp3 score (%d)", oggScore, mp3Score)
	}
	if mp3Score >= flacScore {
		t.Errorf("tier mp3 score (%d) should be less than tier flac score (%d)", mp3Score, flacScore)
	}
}

func TestScoringBalance(t *testing.T) {
	cfg := scoringConfig{
		tiers: []models.QualityTier{
			{Format: "flac", Label: "FLAC"},
			{Format: "mp3", MinBitrate: 320, Label: "MP3 320"},
			{Format: "mp3", MinBitrate: 256, Label: "MP3 256"},
		},
		fallbackEnabled: true,
	}

	type source struct {
		ext         string
		bitRate     int
		hasArtist   bool
		freeSlot    bool
		queueLength int
	}

	score := func(s source) int {
		q, _ := tierBasedScore(s.ext, s.bitRate, cfg)
		total := q
		if s.hasArtist {
			total += 20
		}
		if s.freeSlot {
			total += 10
		}
		total += queueScore(s.queueLength)
		return total
	}

	tests := []struct {
		name   string
		better source
		worse  source
	}{
		{
			name:   "higher tier always beats lower tier, same availability",
			better: source{ext: ".flac", hasArtist: true, freeSlot: true, queueLength: 0},
			worse:  source{ext: ".mp3", bitRate: 320, hasArtist: true, freeSlot: true, queueLength: 0},
		},
		{
			name:   "higher tier wins even without artist match vs lower tier with artist",
			better: source{ext: ".flac", hasArtist: false, freeSlot: true, queueLength: 0},
			worse:  source{ext: ".mp3", bitRate: 320, hasArtist: true, freeSlot: true, queueLength: 0},
		},
		{
			name:   "one tier gap can be overcome by all bonuses combined",
			better: source{ext: ".mp3", bitRate: 320, hasArtist: true, freeSlot: true, queueLength: 0},
			worse:  source{ext: ".flac", hasArtist: false, freeSlot: false, queueLength: 5},
		},
		{
			name:   "artist match tips tie within same tier",
			better: source{ext: ".mp3", bitRate: 320, hasArtist: true, freeSlot: false, queueLength: 5},
			worse:  source{ext: ".mp3", bitRate: 320, hasArtist: false, freeSlot: false, queueLength: 5},
		},
		{
			name:   "free slot tips tie within same tier and artist",
			better: source{ext: ".mp3", bitRate: 320, hasArtist: true, freeSlot: true, queueLength: 5},
			worse:  source{ext: ".mp3", bitRate: 320, hasArtist: true, freeSlot: false, queueLength: 5},
		},
		{
			name:   "empty queue tips tie within same tier and artist",
			better: source{ext: ".flac", hasArtist: true, freeSlot: false, queueLength: 0},
			worse:  source{ext: ".flac", hasArtist: true, freeSlot: false, queueLength: 10},
		},
		{
			name:   "two tier gap cannot be overcome by all bonuses combined",
			better: source{ext: ".flac", hasArtist: false, freeSlot: false, queueLength: 15},
			worse:  source{ext: ".mp3", bitRate: 256, hasArtist: true, freeSlot: true, queueLength: 0},
		},
		{
			name:   "fallback format loses to lowest tier at equal availability",
			better: source{ext: ".mp3", bitRate: 256, hasArtist: true, freeSlot: true, queueLength: 0},
			worse:  source{ext: ".ogg", bitRate: 320, hasArtist: true, freeSlot: true, queueLength: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			betterScore := score(tt.better)
			worseScore := score(tt.worse)
			if betterScore <= worseScore {
				t.Errorf("expected %q (%d) > %q (%d)",
					tt.name, betterScore, "worse", worseScore)
			}
		})
	}
}

func TestMatchesNegativeKeyword(t *testing.T) {
	tests := []struct {
		filename string
		keywords []string
		want     bool
	}{
		{"music/artist - track (acapella).flac", []string{"acapella"}, true},
		{"music/artist - track (acapella).flac", []string{"Acapella"}, true},
		{"music/artist - track.flac", []string{"acapella"}, false},
		{"music/artist - track (acapella remix).mp3", []string{"acapella", "instrumental"}, true},
		{"music/artist - track (instrumental).mp3", []string{"acapella", "instrumental"}, true},
		{"music/artist - track.mp3", []string{"acapella", "instrumental"}, false},
		{"music/artist - track.flac", nil, false},
		{"music/artist - track.flac", []string{}, false},
	}

	for _, tt := range tests {
		got := matchesNegativeKeyword(tt.filename, tt.keywords)
		if got != tt.want {
			t.Errorf("matchesNegativeKeyword(%q, %v) = %v, want %v", tt.filename, tt.keywords, got, tt.want)
		}
	}
}

func TestScoreCandidatesExcludesNegativeKeywords(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username:          "user1",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track (Acapella).flac", Size: 30000000},
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
	}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	cfg := scoringConfig{
		fallbackEnabled:  true,
		negativeKeywords: []string{"acapella"},
		excludeNegative:  true,
	}
	scored := scoreCandidates(results, track, nil, cfg)

	if len(scored) != 1 {
		t.Fatalf("expected 1 result (acapella excluded), got %d", len(scored))
	}
	if scored[0].file.Filename != "music/Artist - Track.flac" {
		t.Errorf("expected non-acapella file, got %s", scored[0].file.Filename)
	}
}

func TestScoreCandidatesReturnsNegativeKeywordsWhenNotExcluding(t *testing.T) {
	results := []slskd.SearchResult{
		{
			Username:          "user1",
			HasFreeUploadSlot: true,
			Files: []slskd.SearchFile{
				{Filename: "music/Artist - Track (Acapella).flac", Size: 30000000},
				{Filename: "music/Artist - Track.flac", Size: 30000000},
			},
		},
	}

	track := &models.Track{Title: "Track", ArtistName: "Artist"}
	cfg := scoringConfig{
		fallbackEnabled:  true,
		negativeKeywords: []string{"acapella"},
		excludeNegative:  false,
	}
	scored := scoreCandidates(results, track, nil, cfg)

	if len(scored) != 2 {
		t.Fatalf("expected 2 results (manual search keeps negative matches), got %d", len(scored))
	}
}

func TestInferExt(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"music/artist - track.flac", ".flac"},
		{"@@user\\music\\track.mp3", ".mp3"},
		{"no_extension", ""},
		{"path/to/file.wav", ".wav"},
	}

	for _, tt := range tests {
		got := inferExt(tt.name)
		if got != tt.want {
			t.Errorf("inferExt(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
