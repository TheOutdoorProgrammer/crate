// Package musicassistant integrates Crate with a Music Assistant server over
// its websocket API. A single reconnecting connection is used both to trigger
// library syncs after downloads and to watch a "reject" playlist so a track can
// be marked bad from the MA app.
//
// The integration is inert unless MA is configured in settings
// (music_assistant_url + music_assistant_token): NewClient returns nil, so no
// connection is opened and no goroutine runs. Enabling MA therefore takes effect
// on the next restart — the persistent connection can't be hot-started without
// leaving an idle goroutine polling settings.
package musicassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
)

// Settings keys (stored in the settings table, configured via the UI).
const (
	SettingURL            = "music_assistant_url"
	SettingToken          = "music_assistant_token"
	SettingRejectPlaylist = "music_assistant_reject_playlist"
	DefaultRejectPlaylist = "Crate Reject"
)

// EventMediaItemUpdated is the EventType MA emits when a library item changes —
// including a playlist gaining a track, the signal the reject watcher waits on.
const EventMediaItemUpdated = "media_item_updated"

const (
	dialTimeout    = 15 * time.Second
	authTimeout    = 15 * time.Second
	commandTimeout = 30 * time.Second
	maxBackoff     = 30 * time.Second
	readLimit      = 8 << 20 // MA library listings exceed the 32KiB default
)

// ErrNotConnected is returned by Command when the client has no live connection.
var ErrNotConnected = errors.New("music-assistant: not connected")

// Event is a MassEvent pushed by the server.
type Event struct {
	Event    string          `json:"event"`
	ObjectID string          `json:"object_id"`
	Data     json.RawMessage `json:"data"`
}

// serverMessage is the union of everything MA sends: server_info on connect
// (server_version set), command results (message_id + result|error_code), and
// events (event set).
type serverMessage struct {
	MessageID     *string         `json:"message_id"`
	Result        json.RawMessage `json:"result"`
	Partial       bool            `json:"partial"`
	ErrorCode     *int            `json:"error_code"`
	Details       string          `json:"details"`
	Event         string          `json:"event"`
	ObjectID      string          `json:"object_id"`
	Data          json.RawMessage `json:"data"`
	ServerVersion string          `json:"server_version"`
}

type commandMessage struct {
	MessageID string `json:"message_id"`
	Command   string `json:"command"`
	Args      any    `json:"args,omitempty"`
}

// Client is a reconnecting Music Assistant websocket client.
type Client struct {
	wsURL string
	token string

	onEvent func(Event)             // must not block: called from the read loop
	onReady func(context.Context)   // run (in a goroutine) after each authentication

	seq     atomic.Uint64
	writeMu sync.Mutex // serializes writes — coder/websocket permits one writer
	mu      sync.Mutex // guards conn + pending
	conn    *websocket.Conn
	pending map[string]chan serverMessage
}

// NewClient builds a client from settings, or returns nil when MA is not
// configured (both url and token required) so callers can no-op cleanly.
func NewClient(queries *db.Queries) *Client {
	rawURL, _ := queries.GetSetting(SettingURL)
	token, _ := queries.GetSetting(SettingToken)
	rawURL, token = strings.TrimSpace(rawURL), strings.TrimSpace(token)
	if rawURL == "" || token == "" {
		return nil
	}
	wsURL, err := normalizeWSURL(rawURL)
	if err != nil {
		slog.Warn("music-assistant: invalid url, integration disabled", "url", rawURL, "error", err)
		return nil
	}
	return &Client{wsURL: wsURL, token: token}
}

// SetEventHandler registers a callback for pushed events. It is invoked from the
// read loop and MUST NOT block (do slow work elsewhere). Call before Run.
func (c *Client) SetEventHandler(fn func(Event)) { c.onEvent = fn }

// SetReadyHandler registers a callback run each time the connection becomes
// authenticated (initial connect and every reconnect) — used to reconcile state
// that may have changed while disconnected. Call before Run.
func (c *Client) SetReadyHandler(fn func(context.Context)) { c.onReady = fn }

// Run maintains the connection until ctx is cancelled, reconnecting with
// exponential backoff. Intended to be called in a goroutine.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second // a real session lasted a while; reset backoff
		}
		slog.Warn("music-assistant: disconnected, reconnecting", "error", err, "retry_in", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

func (c *Client) session(ctx context.Context) error {
	dialCtx, dc := context.WithTimeout(ctx, dialTimeout)
	conn, _, err := websocket.Dial(dialCtx, c.wsURL, nil)
	dc()
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.wsURL, err)
	}
	defer conn.Close(websocket.StatusInternalError, "closing")
	conn.SetReadLimit(readLimit)

	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.mu.Lock()
	c.conn = conn
	c.pending = make(map[string]chan serverMessage)
	c.mu.Unlock()
	defer c.teardown()

	readErr := make(chan error, 1)
	go func() { readErr <- c.readLoop(sessCtx, conn) }()

	authCtx, ac := context.WithTimeout(sessCtx, authTimeout)
	_, err = c.command(authCtx, conn, "auth", map[string]any{"token": c.token})
	ac()
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	slog.Info("music-assistant: connected and authenticated", "url", c.wsURL)

	if c.onReady != nil {
		go c.onReady(sessCtx) // in its own goroutine so a slow reconcile can't stall reads
	}

	return <-readErr // unblocks only after the read loop returns
}

// teardown clears the active connection and fails any in-flight commands. It
// runs only after the read loop has returned, so no sender races the close.
func (c *Client) teardown() {
	c.mu.Lock()
	c.conn = nil
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.pending = nil
	c.mu.Unlock()
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg serverMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Warn("music-assistant: unparseable message", "error", err)
			continue
		}
		switch {
		case msg.Event != "":
			if c.onEvent != nil {
				c.onEvent(Event{Event: msg.Event, ObjectID: msg.ObjectID, Data: msg.Data})
			}
		case msg.MessageID != nil:
			c.mu.Lock()
			ch := c.pending[*msg.MessageID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default: // receiver already gave up (timeout); drop
				}
			}
		case msg.ServerVersion != "":
			slog.Debug("music-assistant: server_info", "version", msg.ServerVersion)
		}
	}
}

// command sends cmd on conn and waits for the correlated response, merging
// partial (chunked) results MA sends for large listings.
func (c *Client) command(ctx context.Context, conn *websocket.Conn, cmd string, args any) (json.RawMessage, error) {
	id := strconv.FormatUint(c.seq.Add(1), 10)
	ch := make(chan serverMessage, 8)

	c.mu.Lock()
	if c.pending == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	payload, err := json.Marshal(commandMessage{MessageID: id, Command: cmd, Args: args})
	if err != nil {
		return nil, err
	}
	c.writeMu.Lock()
	err = conn.Write(ctx, websocket.MessageText, payload)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", cmd, err)
	}

	var chunks []json.RawMessage
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil, ErrNotConnected // connection torn down mid-command
			}
			if msg.ErrorCode != nil {
				return nil, fmt.Errorf("music-assistant %s: error %d: %s", cmd, *msg.ErrorCode, msg.Details)
			}
			if !msg.Partial {
				if chunks == nil {
					return msg.Result, nil
				}
				chunks = appendItems(chunks, msg.Result)
				return json.Marshal(chunks)
			}
			chunks = appendItems(chunks, msg.Result)
		}
	}
}

// Command sends a command on the current connection, applying a default timeout
// when ctx has none. Returns ErrNotConnected if not currently connected.
func (c *Client) Command(ctx context.Context, cmd string, args any) (json.RawMessage, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return nil, ErrNotConnected
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, commandTimeout)
		defer cancel()
	}
	return c.command(ctx, conn, cmd, args)
}

func appendItems(chunks []json.RawMessage, result json.RawMessage) []json.RawMessage {
	var items []json.RawMessage
	if err := json.Unmarshal(result, &items); err == nil {
		return append(chunks, items...)
	}
	return chunks
}

// normalizeWSURL turns a user-entered MA URL (http(s):// or ws(s)://, with or
// without a path) into the websocket endpoint ws(s)://host/ws.
func normalizeWSURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already a websocket scheme
	case "":
		return "", fmt.Errorf("missing scheme in %q", raw)
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host in %q", raw)
	}
	if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/ws") {
		u.Path = strings.TrimRight(u.Path, "/") + "/ws"
	}
	return u.String(), nil
}
