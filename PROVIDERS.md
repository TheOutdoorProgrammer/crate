# Building a Crate Provider

Crate uses gRPC to communicate with music metadata providers. You can build a custom provider in any language that supports gRPC.

## The Contract

Your provider implements the `MusicProvider` gRPC service defined in `proto/provider/provider.proto`:

```protobuf
service MusicProvider {
  rpc Info(InfoRequest) returns (InfoResponse);
  rpc SearchArtists(SearchRequest) returns (ArtistSearchResponse);
  rpc GetArtist(EntityRequest) returns (ArtistDetail);
  rpc GetArtistAlbums(EntityRequest) returns (AlbumList);
  rpc GetAlbum(EntityRequest) returns (AlbumDetail);
}
```

### RPCs

| RPC | Purpose |
|-----|---------|
| `Info` | Return provider name, display name, and version. Called on registration and health checks. |
| `SearchArtists` | Search for artists by name. Supports `limit` and `offset` for pagination. Return `total` for the full result count. |
| `GetArtist` | Get artist details by provider-specific ID. |
| `GetArtistAlbums` | List all albums/releases for an artist. |
| `GetAlbum` | Get album details including track listing. |

### Key Fields

**IDs** are strings. Use whatever your source API uses — MBIDs, Deezer int64s as strings, Spotify URIs, etc. Crate stores them opaquely.

**`rank`** controls display order. Set it on search results, albums, and tracks. Higher rank = shown first. Examples: MusicBrainz uses search relevance score, Deezer uses fan count.

**`metadata`** is `map<string, string>` on every entity. Put anything useful here — country, disambiguation, genre, fan count, release date. The frontend renders these as info tags. No schema needed.

**`record_type`** on albums should be one of: `album`, `single`, `ep`, `compilation`. The frontend displays this as a badge.

## Running Your Provider

### As a managed child process

Set the `CRATE_PROVIDERS` environment variable:

```
CRATE_PROVIDERS=musicbrainz:./provider-musicbrainz:50051,deezer:./provider-deezer:50052,myprovider:./my-provider:50053
```

Format: `name:binary:port`. Crate starts the binary with `PORT=<port>` in its environment, waits up to 10 seconds for the gRPC server to respond to `Info()`, then registers it.

### As an external process

Use `external` as the binary name and provide the full address:

```
CRATE_PROVIDERS=musicbrainz:./provider-musicbrainz:50051,spotify:external:192.168.1.10:50053
```

The external provider must already be running. Crate connects and calls `Info()` to verify.

## Example: Minimal Provider in Go

```go
package main

import (
    "context"
    "net"
    "os"

    "google.golang.org/grpc"
    pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

type server struct {
    pb.UnimplementedMusicProviderServer
}

func (s *server) Info(ctx context.Context, req *pb.InfoRequest) (*pb.InfoResponse, error) {
    return &pb.InfoResponse{
        Name:        "myprovider",
        DisplayName: "My Provider",
        Version:     "0.1.0",
    }, nil
}

func (s *server) SearchArtists(ctx context.Context, req *pb.SearchRequest) (*pb.ArtistSearchResponse, error) {
    // Call your API, map results to pb.ArtistResult
    return &pb.ArtistSearchResponse{
        Artists: []*pb.ArtistResult{
            {Id: "123", Name: "Example Artist", Rank: 100},
        },
        Total: 1,
    }, nil
}

// Implement GetArtist, GetArtistAlbums, GetAlbum similarly...

func main() {
    port := os.Getenv("PORT")
    if port == "" {
        port = "50053"
    }
    lis, _ := net.Listen("tcp", ":"+port)
    s := grpc.NewServer()
    pb.RegisterMusicProviderServer(s, &server{})
    s.Serve(lis)
}
```

## Rate Limiting

If your source API has rate limits, handle them inside your provider. Crate's cache layer (configurable TTL, default 24h) reduces repeat calls, but your provider should still enforce its own limits. See `golang.org/x/time/rate` for a simple token bucket limiter.

## Caching

Crate caches all provider responses in a separate SQLite database (`cache.db`). Cache keys include the provider name, so different providers never collide. Users can clear the cache and configure TTL from the settings page.

## Health Checks

Crate periodically calls `Info()` on each provider to check health. If a provider is unreachable, entities linked to it show as "orphaned" in the UI. They automatically recover when the provider comes back online. Users can also relink entities to a different provider.
