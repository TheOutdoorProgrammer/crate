package navidrome

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
)

func TestTriggerScanCallsNavidrome(t *testing.T) {
	var called bool
	var receivedPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		receivedPath = r.URL.Path
		if !strings.Contains(r.URL.RawQuery, "u=admin") {
			t.Errorf("expected username param, got %s", r.URL.RawQuery)
		}
		if !strings.Contains(r.URL.RawQuery, "t=") {
			t.Errorf("expected token param, got %s", r.URL.RawQuery)
		}
		if !strings.Contains(r.URL.RawQuery, "s=") {
			t.Errorf("expected salt param, got %s", r.URL.RawQuery)
		}
		if !strings.Contains(r.URL.RawQuery, "c=crate") {
			t.Errorf("expected client param, got %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer fake.Close()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	queries := db.NewQueries(database)
	queries.SetSetting("navidrome_url", fake.URL)
	queries.SetSetting("navidrome_user", "admin")
	queries.SetSetting("navidrome_password", "secret")

	c := NewClient(queries)
	c.TriggerScan(context.Background())

	if !called {
		t.Error("expected navidrome to be called")
	}
	if receivedPath != "/rest/startScan" {
		t.Errorf("expected /rest/startScan, got %s", receivedPath)
	}
}

func TestTriggerScanSkipsWhenNotConfigured(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	queries := db.NewQueries(database)
	c := NewClient(queries)

	// Should not panic or error when settings are missing
	c.TriggerScan(context.Background())
}

func TestTriggerScanSkipsPartialConfig(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	queries := db.NewQueries(database)
	queries.SetSetting("navidrome_url", "http://localhost:4533")
	// Missing user and password

	c := NewClient(queries)
	c.TriggerScan(context.Background())
}

func TestMd5Hash(t *testing.T) {
	// Known value: md5("sesamec19b2d") = "26719a1196d2a940705a59634eb18eab"
	got := md5Hash("sesamec19b2d")
	want := "26719a1196d2a940705a59634eb18eab"
	if got != want {
		t.Errorf("md5Hash(\"sesamec19b2d\") = %q, want %q", got, want)
	}
}

func TestRandomSaltLength(t *testing.T) {
	s := randomSalt()
	if len(s) != 12 { // 6 bytes = 12 hex chars
		t.Errorf("expected 12 char salt, got %d: %q", len(s), s)
	}

	s2 := randomSalt()
	if s == s2 {
		t.Error("expected different salts on successive calls")
	}
}
