package navidrome

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
)

type Client struct {
	queries *db.Queries
	http    *http.Client
}

func NewClient(queries *db.Queries) *Client {
	return &Client{
		queries: queries,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) config() (url, user, pass string, ok bool) {
	url, _ = c.queries.GetSetting("navidrome_url")
	user, _ = c.queries.GetSetting("navidrome_user")
	pass, _ = c.queries.GetSetting("navidrome_password")
	if url == "" || user == "" || pass == "" {
		return "", "", "", false
	}
	return url, user, pass, true
}

func (c *Client) TriggerScan(ctx context.Context) {
	url, user, pass, ok := c.config()
	if !ok {
		return
	}

	salt := randomSalt()
	token := md5Hash(pass + salt)

	endpoint := fmt.Sprintf("%s/rest/startScan?u=%s&t=%s&s=%s&v=1.16.1&c=crate&f=json",
		url, user, token, salt)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		slog.Warn("navidrome: build request", "error", err)
		return
	}

	resp, err := c.http.Do(req)
	if err != nil {
		slog.Warn("navidrome: scan request failed", "error", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		slog.Info("navidrome: scan triggered")
	} else {
		slog.Warn("navidrome: scan returned non-200", "status", resp.StatusCode)
	}
}

func randomSalt() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
