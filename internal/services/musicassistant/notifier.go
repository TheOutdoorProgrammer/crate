package musicassistant

import (
	"context"
	"log/slog"
)

// TriggerScan implements downloader.PostDownloadNotifier: it asks Music
// Assistant to sync its library so a freshly-downloaded file appears. MA's
// filesystem sync is incremental (checksum-based), so a full sync only
// processes the new file.
//
// Best-effort: if the persistent connection is momentarily down (reconnecting),
// we log and move on — the next download's sync, or MA's own on-restart scan,
// catches up. This runs over the same connection the reject watcher uses.
func (c *Client) TriggerScan(ctx context.Context) {
	if _, err := c.Command(ctx, "music/sync", nil); err != nil {
		slog.Warn("music-assistant: sync trigger failed", "error", err)
	}
}
