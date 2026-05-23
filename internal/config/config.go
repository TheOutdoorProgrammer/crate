package config

import (
	"os"
	"time"
)

type Config struct {
	Port         string
	DatabasePath string
	CachePath    string
	ActivityPath string
	DownloadsDir string
	LibraryPath  string
	ScanInterval time.Duration

	SlskdURL    string
	SlskdAPIKey string

	Providers            string
	MusicBrainzUserAgent string

	DownloadFormatPreference []string
	MinBitrate               int
}

func Load() *Config {
	return &Config{
		Port:                     envOr("CRATE_PORT", "6969"),
		DatabasePath:             envOr("CRATE_DB_PATH", "./crate.db"),
		CachePath:                envOr("CRATE_CACHE_PATH", "./cache.db"),
		ActivityPath:             envOr("CRATE_ACTIVITY_PATH", "./activity.db"),
		DownloadsDir:             envOr("CRATE_DOWNLOADS_DIR", "./downloads"),
		LibraryPath:              envOr("CRATE_LIBRARY_PATH", "./library"),
		ScanInterval:             parseDuration(envOr("CRATE_SCAN_INTERVAL", "6h")),
		SlskdURL:                 envOr("CRATE_SLSKD_URL", "http://localhost:5030"),
		SlskdAPIKey:              os.Getenv("CRATE_SLSKD_API_KEY"),
		Providers:                envOr("CRATE_PROVIDERS", "musicbrainz:./provider-musicbrainz:50051,deezer:./provider-deezer:50052"),
		MusicBrainzUserAgent:     envOr("CRATE_MB_USER_AGENT", "Crate/0.1.0 (https://github.com/TheOutdoorProgrammer/crate)"),
		DownloadFormatPreference: []string{"flac", "mp3"},
		MinBitrate:               256,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 6 * time.Hour
	}
	return d
}
