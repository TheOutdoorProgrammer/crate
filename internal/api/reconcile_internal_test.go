package api

import (
	"testing"

	"github.com/TheOutdoorProgrammer/crate/internal/models"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

func yptr(i int) *int { return &i }

func TestFoldEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Album One", "album one", true},
		{"  Trimmed  ", "Trimmed", true},
		{"Café", "café", true},
		{"Album One", "Album Two", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := foldEqual(c.a, c.b); got != c.want {
			t.Errorf("foldEqual(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestMatchLocalAlbum(t *testing.T) {
	locals := []*models.Album{
		{ID: 1, Title: "Album One", Year: yptr(2023)},
		{ID: 2, Title: "No Year Album", Year: nil},
		{ID: 3, Title: "Reissued", Year: yptr(1999)},
	}

	t.Run("title and year match", func(t *testing.T) {
		used := map[int64]bool{}
		got := matchLocalAlbum(locals, used, &pb.AlbumSummary{Title: "album one", Year: 2023})
		if got == nil || got.ID != 1 {
			t.Fatalf("got %+v, want album 1", got)
		}
	})

	t.Run("year mismatch when both known does not match", func(t *testing.T) {
		used := map[int64]bool{}
		// Same title, but the provider release is a 2021 re-record — must not merge.
		if got := matchLocalAlbum(locals, used, &pb.AlbumSummary{Title: "Reissued", Year: 2021}); got != nil {
			t.Fatalf("got %+v, want nil (year guard)", got)
		}
	})

	t.Run("unknown local year is compatible", func(t *testing.T) {
		used := map[int64]bool{}
		got := matchLocalAlbum(locals, used, &pb.AlbumSummary{Title: "No Year Album", Year: 2020})
		if got == nil || got.ID != 2 {
			t.Fatalf("got %+v, want album 2", got)
		}
	})

	t.Run("unknown provider year is compatible", func(t *testing.T) {
		used := map[int64]bool{}
		got := matchLocalAlbum(locals, used, &pb.AlbumSummary{Title: "Reissued", Year: 0})
		if got == nil || got.ID != 3 {
			t.Fatalf("got %+v, want album 3", got)
		}
	})

	t.Run("used albums are skipped", func(t *testing.T) {
		used := map[int64]bool{1: true}
		if got := matchLocalAlbum(locals, used, &pb.AlbumSummary{Title: "Album One", Year: 2023}); got != nil {
			t.Fatalf("got %+v, want nil (already used)", got)
		}
	})

	t.Run("no title match", func(t *testing.T) {
		used := map[int64]bool{}
		if got := matchLocalAlbum(locals, used, &pb.AlbumSummary{Title: "Unknown", Year: 2023}); got != nil {
			t.Fatalf("got %+v, want nil", got)
		}
	})
}

func TestMatchLocalTrack(t *testing.T) {
	t.Run("prefers disc and track number tiebreak", func(t *testing.T) {
		// Two tracks fold to the same title; the number decides which is meant.
		locals := []*models.Track{
			{ID: 1, Title: "Interlude", TrackNumber: 5, DiscNumber: 1},
			{ID: 2, Title: "Interlude", TrackNumber: 9, DiscNumber: 1},
		}
		got := matchLocalTrack(locals, map[int64]bool{}, &pb.TrackInfo{Title: "interlude", TrackNumber: 9, DiscNumber: 1})
		if got == nil || got.ID != 2 {
			t.Fatalf("got %+v, want track 2", got)
		}
	})

	t.Run("title-only match when no number aligns", func(t *testing.T) {
		locals := []*models.Track{{ID: 3, Title: "Solo", TrackNumber: 4, DiscNumber: 1}}
		got := matchLocalTrack(locals, map[int64]bool{}, &pb.TrackInfo{Title: "Solo", TrackNumber: 7, DiscNumber: 1})
		if got == nil || got.ID != 3 {
			t.Fatalf("got %+v, want track 3 (title-only)", got)
		}
	})

	t.Run("no number-only fallback", func(t *testing.T) {
		// Numbers line up but titles differ — must NOT match (never force a track
		// onto a title it doesn't share).
		locals := []*models.Track{{ID: 4, Title: "Real Title", TrackNumber: 1, DiscNumber: 1}}
		if got := matchLocalTrack(locals, map[int64]bool{}, &pb.TrackInfo{Title: "Different", TrackNumber: 1, DiscNumber: 1}); got != nil {
			t.Fatalf("got %+v, want nil", got)
		}
	})

	t.Run("used tracks are skipped", func(t *testing.T) {
		locals := []*models.Track{{ID: 5, Title: "Once", TrackNumber: 1, DiscNumber: 1}}
		if got := matchLocalTrack(locals, map[int64]bool{5: true}, &pb.TrackInfo{Title: "Once", TrackNumber: 1, DiscNumber: 1}); got != nil {
			t.Fatalf("got %+v, want nil (already used)", got)
		}
	})
}
