package naming

import (
	"strings"
	"testing"
)

var fullMeta = Meta{
	Artist: "Radiohead",
	Album:  "OK Computer",
	Year:   1997,
	Track:  6,
	Disc:   1,
	Title:  "Karma Police",
}

func TestRender(t *testing.T) {
	tests := []struct {
		name     string
		template string
		meta     Meta
		want     string
	}{
		{
			name:     "default template",
			template: DefaultTemplate,
			meta:     fullMeta,
			want:     "Radiohead/OK Computer (1997)/06 - Karma Police",
		},
		{
			// Matches the old hardcoded behavior: no year -> no " (Year)" suffix.
			name:     "default template without year",
			template: DefaultTemplate,
			meta:     Meta{Artist: "Radiohead", Album: "OK Computer", Track: 6, Title: "Karma Police"},
			want:     "Radiohead/OK Computer/06 - Karma Police",
		},
		{
			name:     "dash convention from issue 1",
			template: "{artist}/{artist} - {year} - {album}/{track:2} {title}",
			meta:     fullMeta,
			want:     "Radiohead/Radiohead - 1997 - OK Computer/06 Karma Police",
		},
		{
			name:     "dash convention drops separator when year empty",
			template: "{artist}/{artist} - {year} - {album}/{title}",
			meta:     Meta{Artist: "Radiohead", Album: "OK Computer", Title: "Karma Police"},
			want:     "Radiohead/Radiohead - OK Computer/Karma Police",
		},
		{
			name:     "albumartist is an alias for artist",
			template: "{albumartist}/{track} - {title}",
			meta:     fullMeta,
			want:     "Radiohead/6 - Karma Police",
		},
		{
			name:     "slash in metadata becomes underscore",
			template: "{artist}/{track} - {title}",
			meta:     Meta{Artist: "AC/DC", Track: 1, Title: "T.N.T."},
			want:     "AC_DC/1 - T.N.T.",
		},
		{
			name:     "unsafe characters in title",
			template: DefaultTemplate,
			meta:     Meta{Artist: "X", Album: "Y", Year: 2000, Track: 1, Title: `What: "Is" This?`},
			want:     "X/Y (2000)/01 - What_ _Is_ This_",
		},
		{
			name:     "parens from metadata survive cleanup",
			template: "{artist}/{album} [{year}]/{track:2} - {title}",
			meta:     Meta{Artist: "Sigur Rós", Album: "( )", Year: 2002, Track: 1, Title: "Untitled 1"},
			want:     "Sigur Rós/( ) [2002]/01 - Untitled 1",
		},
		{
			name:     "empty brackets removed when year missing",
			template: "{artist}/{album} [{year}] Deluxe/{track:2} - {title}",
			meta:     Meta{Artist: "X", Album: "Y", Track: 1, Title: "T"},
			want:     "X/Y Deluxe/01 - T",
		},
		{
			name:     "disc renders when known",
			template: "{album}/Disc {disc}/{track:2} - {title}",
			meta:     Meta{Artist: "X", Album: "Y", Track: 3, Disc: 2, Title: "T"},
			want:     "Y/Disc 2/03 - T",
		},
		{
			name:     "padding widths",
			template: "{track:3}/{track} - {title}",
			meta:     fullMeta,
			want:     "006/6 - Karma Police",
		},
		{
			name:     "lidarr-style zero-prefixed pad width",
			template: "{artist}/{track:02} {title}",
			meta:     fullMeta,
			want:     "Radiohead/06 Karma Police",
		},
		{
			// No empty tokens in the segment -> internal whitespace is preserved
			// byte-for-byte (matches the old organizer behavior).
			name:     "internal double spaces preserved without empty tokens",
			template: "{artist}/{track:2} - {title}",
			meta:     Meta{Artist: "X", Track: 1, Title: "A  B"},
			want:     "X/01 - A  B",
		},
		{
			name:     "whitespace around segments trimmed",
			template: "{artist} / {track:2} - {title}",
			meta:     fullMeta,
			want:     "Radiohead/06 - Karma Police",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.template, tt.meta)
			if err != nil {
				t.Fatalf("Render(%q) error: %v", tt.template, err)
			}
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.template, got, tt.want)
			}
		})
	}
}

func TestRenderEmptySegmentFails(t *testing.T) {
	_, err := Render("{artist}/{year}/{track:2} - {title}", Meta{Artist: "X", Track: 1, Title: "T"})
	if err == nil {
		t.Fatal("expected error for segment that renders empty")
	}
	if !strings.Contains(err.Error(), "segment 2") {
		t.Errorf("error should identify the segment: %v", err)
	}
}

func TestValidate(t *testing.T) {
	valid := []string{
		DefaultTemplate,
		"{artist}/{album}/{title}",
		"{albumartist}/{album} ({year})/{disc:1}-{track:2} {title}",
		"{title}",
		"singles/{artist} - {title}",
	}
	for _, tmpl := range valid {
		if err := Validate(tmpl); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", tmpl, err)
		}
	}

	invalid := []struct {
		template string
		wantErr  string
	}{
		{"", "empty"},
		{"   ", "empty"},
		{"/abs/{title}", "relative"},
		{`\abs\{title}`, "relative"},
		{"{artist}//{title}", "empty path segment"},
		{"{artist}/{title}/", "empty path segment"},
		{"{artist}/../{title}", "not allowed"},
		{"{artist}/./{title}", "not allowed"},
		{"{artist}/{titel}", "unknown token"},
		{"{artist}/{title", "unclosed"},
		{"{artist}/{album}", "must contain {title} or {track}"},
		{"{artist:2}/{title}", "does not support a pad width"},
		{"{track:x} {title}", "invalid pad width"},
		{"{track:0} {title}", "invalid pad width"},
	}
	for _, tt := range invalid {
		err := Validate(tt.template)
		if err == nil {
			t.Errorf("Validate(%q) = nil, want error containing %q", tt.template, tt.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("Validate(%q) = %q, want error containing %q", tt.template, err, tt.wantErr)
		}
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"OK Computer", "OK Computer"},
		{`AC/DC`, "AC_DC"},
		{`What: "Is" This?`, "What_ _Is_ This_"},
		{"  spaces  ", "spaces"},
		{"a<b>c|d*e", "a_b_c_d_e"},
		{`back\slash`, "back_slash"},
	}
	for _, tt := range tests {
		if got := sanitize(tt.in); got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
