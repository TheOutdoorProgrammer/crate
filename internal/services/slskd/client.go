package slskd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Search

type SearchRequest struct {
	SearchText string `json:"searchText"`
	FileLimit  int    `json:"fileLimit"`
}

type SearchResponse struct {
	ID          string           `json:"id"`
	SearchText  string           `json:"searchText"`
	State       string           `json:"state"`
	IsComplete  bool             `json:"isComplete"`
	FileCount   int              `json:"fileCount"`
	Responses   []SearchResult   `json:"responses"`
}

type SearchResult struct {
	Username          string       `json:"username"`
	UploadSpeed       int64        `json:"uploadSpeed"`
	HasFreeUploadSlot bool         `json:"hasFreeUploadSlot"`
	QueueLength       int          `json:"queueLength"`
	Files             []SearchFile `json:"files"`
}

type SearchFile struct {
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	BitRate    int    `json:"bitRate"`
	BitDepth   int    `json:"bitDepth"`
	SampleRate int    `json:"sampleRate"`
	Length     int    `json:"length"`
	IsLocked   bool   `json:"isLocked"`
}

func (c *Client) StartSearch(ctx context.Context, query string) (*SearchResponse, error) {
	body, _ := json.Marshal(SearchRequest{SearchText: query, FileLimit: 100})
	var resp SearchResponse
	if err := c.do(ctx, "POST", "/api/v0/searches", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetSearch(ctx context.Context, id string) (*SearchResponse, error) {
	var resp SearchResponse
	if err := c.do(ctx, "GET", "/api/v0/searches/"+id+"?includeResponses=true", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteSearch(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v0/searches/"+id, nil, nil)
}

// Downloads

type DownloadRequest struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type EnqueueResponse struct {
	Enqueued []Transfer `json:"enqueued"`
	Failed   []any      `json:"failed"`
}

type Transfer struct {
	ID               string  `json:"id"`
	Username         string  `json:"username"`
	Filename         string  `json:"filename"`
	Size             int64   `json:"size"`
	State            string  `json:"state"`
	BytesTransferred int64   `json:"bytesTransferred"`
	PercentComplete  float64 `json:"percentComplete"`
	AverageSpeed     float64 `json:"averageSpeed"`
}

type UserDownloads struct {
	Username    string              `json:"username"`
	Directories []DownloadDirectory `json:"directories"`
}

type DownloadDirectory struct {
	Directory string     `json:"directory"`
	Files     []Transfer `json:"files"`
}

func (c *Client) StartDownload(ctx context.Context, username, filename string, size int64) (*Transfer, error) {
	body, _ := json.Marshal([]DownloadRequest{{Filename: filename, Size: size}})
	var resp EnqueueResponse
	if err := c.do(ctx, "POST", "/api/v0/transfers/downloads/"+username, body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Enqueued) == 0 {
		return nil, fmt.Errorf("slskd did not enqueue download")
	}
	t := resp.Enqueued[0]
	return &t, nil
}

func (c *Client) GetAllDownloads(ctx context.Context) ([]UserDownloads, error) {
	var dirs []UserDownloads
	if err := c.do(ctx, "GET", "/api/v0/transfers/downloads", nil, &dirs); err != nil {
		return nil, err
	}
	return dirs, nil
}

func (c *Client) CancelDownload(ctx context.Context, username, id string) error {
	return c.do(ctx, "DELETE", "/api/v0/transfers/downloads/"+username+"/"+id+"?remove=true", nil, nil)
}

func (c *Client) GetDownload(ctx context.Context, username, id string) (*Transfer, error) {
	var dirs []UserDownloads
	if err := c.do(ctx, "GET", "/api/v0/transfers/downloads", nil, &dirs); err != nil {
		return nil, err
	}
	for _, ud := range dirs {
		if ud.Username != username {
			continue
		}
		for _, dir := range ud.Directories {
			for _, f := range dir.Files {
				if f.ID == id {
					return &f, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("transfer %s not found", id)
}

// HTTP helpers

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slskd %s %s: %d %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
