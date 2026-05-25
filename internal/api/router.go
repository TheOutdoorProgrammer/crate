package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/cache"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
	"github.com/TheOutdoorProgrammer/crate/internal/services/downloader"
)

type Server struct {
	queries     *db.Queries
	providers   *provider.Manager
	cache       *cache.Cache
	downloader  *downloader.Service
	activityLog *activity.Log
	router      chi.Router
	frontendFS  fs.FS
	bgWork      sync.WaitGroup
	startTime   time.Time
	libraryDir  string
	version     string
}

func NewServer(queries *db.Queries, providers *provider.Manager, c *cache.Cache, dl *downloader.Service, actLog *activity.Log, frontendFS fs.FS, libraryDir string, version string) *Server {
	s := &Server{
		queries:     queries,
		providers:   providers,
		cache:       c,
		downloader:  dl,
		activityLog: actLog,
		frontendFS:  frontendFS,
		startTime:   time.Now().UTC(),
		libraryDir:  libraryDir,
		version:     version,
	}
	s.router = s.setupRouter()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) WaitBackground() {
	s.bgWork.Wait()
}

func (s *Server) setupRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(structuredLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(120 * time.Second))
	r.Use(maxBodySize(5 << 20))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "http://localhost:6969"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Route("/api", func(r chi.Router) {
		r.Get("/status", s.handleStatus)

		r.Get("/search", s.handleSearch)
		r.Get("/library/search", s.handleLibrarySearch)

		r.Route("/browse", func(r chi.Router) {
			r.Get("/artist/{id}", s.handleBrowseArtist)
			r.Get("/artist/{id}/tracks", s.handleBrowseArtistTrackSearch)
			r.Get("/album/{id}", s.handleBrowseAlbum)
		})

		r.Route("/watch", func(r chi.Router) {
			r.Post("/artist/{id}", s.handleWatchArtist)
			r.Post("/album/{id}", s.handleWatchAlbum)
			r.Post("/track/{id}", s.handleWatchTrack)
		})

		r.Route("/artists", func(r chi.Router) {
			r.Get("/", s.handleListArtists)
			r.Get("/{id}", s.handleGetArtist)
			r.Put("/{id}/new-releases", s.handleToggleNewReleases)
			r.Post("/{id}/queue", s.handleQueueArtistTracks)
			r.Delete("/{id}", s.handleUnwatchArtist)
		})

		r.Route("/albums", func(r chi.Router) {
			r.Get("/{id}", s.handleGetAlbum)
			r.Post("/{id}/queue", s.handleQueueAlbumTracks)
			r.Put("/{id}/ignore", s.handleIgnoreAlbum)
			r.Delete("/{id}/ignore", s.handleUnignoreAlbum)
			r.Delete("/{id}", s.handleUnwatchAlbum)
		})

		r.Route("/tracks", func(r chi.Router) {
			r.Delete("/{id}", s.handleUnwatchTrack)
			r.Post("/{id}/queue", s.handleQueueTrack)
			r.Post("/{id}/search", s.handleManualSearch)
			r.Post("/{id}/download", s.handleManualDownload)
			r.Put("/{id}/ignore", s.handleIgnoreTrack)
			r.Delete("/{id}/ignore", s.handleUnignoreTrack)
		})

		r.Route("/downloads", func(r chi.Router) {
			r.Get("/", s.handleListDownloads)
			r.Get("/progress", s.handleDownloadProgress)
			r.Post("/queue", s.handleQueueDownloads)
			r.Delete("/clear", s.handleClearDownloadsByStatus)
			r.Post("/{id}/retry", s.handleRetryDownload)
			r.Delete("/{id}", s.handleDeleteDownload)
		})

		r.Route("/blacklist", func(r chi.Router) {
			r.Get("/", s.handleListBlacklist)
			r.Delete("/", s.handleClearBlacklist)
			r.Delete("/{id}", s.handleDeleteBlacklistEntry)
		})

		r.Route("/cooldowns", func(r chi.Router) {
			r.Get("/", s.handleListCooldowns)
			r.Delete("/", s.handleClearCooldowns)
			r.Delete("/{id}", s.handleDeleteCooldown)
		})

		r.Get("/providers", s.handleListProviders)
		r.Get("/activity", s.handleListActivity)
		r.Delete("/cache", s.handleClearCache)

		r.Route("/relink", func(r chi.Router) {
			r.Post("/artist/{id}", s.handleRelinkEntity)
			r.Post("/album/{id}", s.handleRelinkEntity)
			r.Post("/track/{id}", s.handleRelinkEntity)
		})

		r.Route("/settings", func(r chi.Router) {
			r.Get("/", s.handleGetSettings)
			r.Put("/", s.handleUpdateSettings)
		})
	})

	s.mountLidarrRoutes(r)

	if s.frontendFS != nil {
		r.NotFound(SPAHandler(s.frontendFS))
	}

	return r
}

func maxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

func structuredLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration", time.Since(start),
		)
	})
}
