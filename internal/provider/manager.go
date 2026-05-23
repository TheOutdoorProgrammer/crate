package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/TheOutdoorProgrammer/crate/internal/cache"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

type ProviderInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
	Address     string `json:"address"`
	Healthy     bool   `json:"healthy"`
}

type Manager struct {
	mu        sync.RWMutex
	providers map[string]*providerConn
	cache     *cache.Cache
	queries   *db.Queries
}

type providerConn struct {
	info   ProviderInfo
	conn   *grpc.ClientConn
	client pb.MusicProviderClient
}

func NewManager(cache *cache.Cache, queries *db.Queries) *Manager {
	return &Manager{
		providers: make(map[string]*providerConn),
		cache:     cache,
		queries:   queries,
	}
}

func (m *Manager) RegisterProvider(ctx context.Context, name, address string) error {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to provider %s at %s: %w", name, address, err)
	}

	client := pb.NewMusicProviderClient(conn)

	infoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	info, err := client.Info(infoCtx, &pb.InfoRequest{})
	if err != nil {
		conn.Close()
		return fmt.Errorf("info call to provider %s: %w", name, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if old, ok := m.providers[name]; ok {
		old.conn.Close()
	}

	m.providers[name] = &providerConn{
		info: ProviderInfo{
			Name:        info.Name,
			DisplayName: info.DisplayName,
			Version:     info.Version,
			Address:     address,
			Healthy:     true,
		},
		conn:   conn,
		client: client,
	}

	slog.Info("provider registered", "name", info.Name, "display_name", info.DisplayName, "version", info.Version)
	return nil
}

func (m *Manager) ListProviders() []ProviderInfo {
	m.mu.RLock()
	snapshot := make([]*providerConn, 0, len(m.providers))
	for _, p := range m.providers {
		snapshot = append(snapshot, p)
	}
	m.mu.RUnlock()

	var infos []ProviderInfo
	for _, p := range snapshot {
		info := p.info
		info.Healthy = m.checkHealth(p)
		infos = append(infos, info)
	}
	return infos
}

func (m *Manager) IsHealthy(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[name]
	if !ok {
		return false
	}
	return m.checkHealth(p)
}

func (m *Manager) checkHealth(p *providerConn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := p.client.Info(ctx, &pb.InfoRequest{})
	return err == nil
}

func (m *Manager) getClient(name string) (pb.MusicProviderClient, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}
	return p.client, nil
}

func (m *Manager) Primary() string {
	v, err := m.queries.GetSetting("provider_primary")
	if err != nil || v == "" {
		return "musicbrainz"
	}
	return v
}

func (m *Manager) CacheTTL() time.Duration {
	v, err := m.queries.GetSetting("cache_ttl_hours")
	if err != nil || v == "" {
		return 24 * time.Hour
	}
	var hours int
	fmt.Sscanf(v, "%d", &hours)
	if hours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(hours) * time.Hour
}

type SearchResult struct {
	Artists []*pb.ArtistResult `json:"artists"`
	Total   int32              `json:"total"`
}

func (m *Manager) Search(ctx context.Context, query string, limit, offset int32) (*SearchResult, error) {
	return m.SearchWithProvider(ctx, m.Primary(), query, limit, offset)
}

func (m *Manager) SearchWithProvider(ctx context.Context, providerName, query string, limit, offset int32) (*SearchResult, error) {
	if limit <= 0 {
		limit = 25
	}
	cacheKey := fmt.Sprintf("%s:search:%s:%d:%d", providerName, query, limit, offset)

	if data, ok := m.cache.Get(cacheKey); ok {
		var result SearchResult
		if json.Unmarshal(data, &result) == nil {
			return &result, nil
		}
	}

	client, err := m.getClient(providerName)
	if err != nil {
		return nil, err
	}

	resp, err := client.SearchArtists(ctx, &pb.SearchRequest{Query: query, Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("search via %s: %w", providerName, err)
	}

	artists := resp.Artists
	if artists == nil {
		artists = []*pb.ArtistResult{}
	}

	result := &SearchResult{Artists: artists, Total: resp.Total}

	if data, err := json.Marshal(result); err == nil {
		m.cache.Set(cacheKey, data, m.CacheTTL())
	}

	return result, nil
}

func (m *Manager) GetArtist(ctx context.Context, providerName, id string) (*pb.ArtistDetail, error) {
	cacheKey := providerName + ":artist:" + id

	if data, ok := m.cache.Get(cacheKey); ok {
		var result pb.ArtistDetail
		if json.Unmarshal(data, &result) == nil {
			return &result, nil
		}
	}

	client, err := m.getClient(providerName)
	if err != nil {
		return nil, err
	}

	result, err := client.GetArtist(ctx, &pb.EntityRequest{Id: id})
	if err != nil {
		return nil, fmt.Errorf("get artist from %s: %w", providerName, err)
	}

	if data, err := json.Marshal(result); err == nil {
		m.cache.Set(cacheKey, data, m.CacheTTL())
	}

	return result, nil
}

func (m *Manager) GetArtistAlbums(ctx context.Context, providerName, id string) (*pb.AlbumList, error) {
	cacheKey := providerName + ":artist-albums:" + id

	if data, ok := m.cache.Get(cacheKey); ok {
		var result pb.AlbumList
		if json.Unmarshal(data, &result) == nil {
			return &result, nil
		}
	}

	client, err := m.getClient(providerName)
	if err != nil {
		return nil, err
	}

	result, err := client.GetArtistAlbums(ctx, &pb.EntityRequest{Id: id})
	if err != nil {
		return nil, fmt.Errorf("get artist albums from %s: %w", providerName, err)
	}

	if data, err := json.Marshal(result); err == nil {
		m.cache.Set(cacheKey, data, m.CacheTTL())
	}

	return result, nil
}

func (m *Manager) GetAlbum(ctx context.Context, providerName, id string) (*pb.AlbumDetail, error) {
	cacheKey := providerName + ":album:" + id

	if data, ok := m.cache.Get(cacheKey); ok {
		var result pb.AlbumDetail
		if json.Unmarshal(data, &result) == nil {
			return &result, nil
		}
	}

	client, err := m.getClient(providerName)
	if err != nil {
		return nil, err
	}

	result, err := client.GetAlbum(ctx, &pb.EntityRequest{Id: id})
	if err != nil {
		return nil, fmt.Errorf("get album from %s: %w", providerName, err)
	}

	if data, err := json.Marshal(result); err == nil {
		m.cache.Set(cacheKey, data, m.CacheTTL())
	}

	return result, nil
}

func (m *Manager) SearchArtistTracks(ctx context.Context, providerName, artistID, query string, limit int32) (*pb.ArtistTrackSearchResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	cacheKey := fmt.Sprintf("%s:artist-tracks:%s:%s:%d", providerName, artistID, query, limit)

	if data, ok := m.cache.Get(cacheKey); ok {
		var result pb.ArtistTrackSearchResponse
		if json.Unmarshal(data, &result) == nil {
			return &result, nil
		}
	}

	client, err := m.getClient(providerName)
	if err != nil {
		return nil, err
	}

	result, err := client.SearchArtistTracks(ctx, &pb.ArtistTrackSearchRequest{
		ArtistId: artistID,
		Query:    query,
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search artist tracks from %s: %w", providerName, err)
	}

	if data, err := json.Marshal(result); err == nil {
		m.cache.Set(cacheKey, data, m.CacheTTL())
	}

	return result, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.providers {
		p.conn.Close()
	}
}
