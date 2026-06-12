// Package naming renders library paths from user-configurable templates.
//
// A template is a relative path whose segments mix literal text with
// {token} placeholders, e.g. "{artist}/{album} ({year})/{track:2} - {title}".
// Numeric tokens accept a zero-pad width ({track:2} renders 3 as "03").
// The file extension is never part of the template — the organizer appends
// the source file's extension to the rendered path.
package naming

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DefaultTemplate matches Crate's original hardcoded layout:
// Artist/Album (Year)/NN - Title.ext
const DefaultTemplate = "{artist}/{album} ({year})/{track:2} - {title}"

// Meta holds the metadata available to naming templates.
type Meta struct {
	Artist string
	Album  string
	Year   int // <= 0 renders {year} as empty
	Track  int
	Disc   int // <= 0 renders {disc} as empty
	Title  string
}

// SettingKey is the settings-table key holding the active template.
const SettingKey = "naming_template"

type part struct {
	literal string // set when token is empty
	token   string
	pad     int // zero-pad width for numeric tokens, 0 = unpadded
}

// tokens maps token name -> whether it supports a pad width.
var tokens = map[string]bool{
	"artist":      false,
	"albumartist": false, // alias for {artist}; Crate tracks album artists only
	"album":       false,
	"year":        false,
	"title":       false,
	"track":       true,
	"disc":        true,
}

// Validate checks template syntax without rendering. It returns a
// user-presentable error describing the first problem found.
func Validate(template string) error {
	_, err := parse(template)
	return err
}

// Render produces a library-relative path (without extension) for m.
// It fails if the template is invalid or if any path segment renders empty,
// which can happen when a segment contains only blank-able tokens like {year}.
func Render(template string, m Meta) (string, error) {
	segs, err := parse(template)
	if err != nil {
		return "", err
	}
	out := make([]string, len(segs))
	for i, parts := range segs {
		s := renderSegment(parts, m)
		if s == "" || s == "." || s == ".." {
			return "", fmt.Errorf("path segment %d renders as %q — segments must not consist only of tokens that can be empty (like {year} or {disc})", i+1, s)
		}
		out[i] = s
	}
	return filepath.Join(out...), nil
}

func parse(template string) ([][]part, error) {
	t := strings.TrimSpace(template)
	if t == "" {
		return nil, fmt.Errorf("template is empty")
	}
	if strings.HasPrefix(t, "/") || strings.HasPrefix(t, `\`) {
		return nil, fmt.Errorf("template must be a relative path")
	}
	rawSegs := strings.Split(t, "/")
	segs := make([][]part, len(rawSegs))
	for i, raw := range rawSegs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, fmt.Errorf("empty path segment %d — check for doubled or trailing '/'", i+1)
		}
		if raw == "." || raw == ".." {
			return nil, fmt.Errorf("path segment %q is not allowed", raw)
		}
		parts, err := parseSegment(raw)
		if err != nil {
			return nil, err
		}
		segs[i] = parts
	}
	if !hasToken(segs[len(segs)-1], "title", "track") {
		return nil, fmt.Errorf("the filename (last segment) must contain {title} or {track} so tracks get unique names")
	}
	return segs, nil
}

func parseSegment(seg string) ([]part, error) {
	var parts []part
	for seg != "" {
		i := strings.IndexByte(seg, '{')
		if i < 0 {
			parts = append(parts, part{literal: seg})
			break
		}
		if i > 0 {
			parts = append(parts, part{literal: seg[:i]})
		}
		end := strings.IndexByte(seg[i:], '}')
		if end < 0 {
			return nil, fmt.Errorf("unclosed '{' in %q", seg)
		}
		body := seg[i+1 : i+end]
		name, spec, hasSpec := strings.Cut(body, ":")
		p := part{token: strings.ToLower(strings.TrimSpace(name))}
		padOK, known := tokens[p.token]
		if !known {
			return nil, fmt.Errorf("unknown token {%s} — valid tokens: {artist}, {albumartist}, {album}, {year}, {track}, {disc}, {title}", body)
		}
		if hasSpec {
			if !padOK {
				return nil, fmt.Errorf("{%s} does not support a pad width", p.token)
			}
			width, err := strconv.Atoi(strings.TrimSpace(spec))
			if err != nil || width < 1 || width > 10 {
				return nil, fmt.Errorf("invalid pad width %q in {%s} — use e.g. {%s:2}", spec, body, p.token)
			}
			p.pad = width
		}
		parts = append(parts, p)
		seg = seg[i+end+1:]
	}
	return parts, nil
}

func hasToken(parts []part, names ...string) bool {
	for _, p := range parts {
		for _, n := range names {
			if p.token == n {
				return true
			}
		}
	}
	return false
}

// mark stands in for tokens that rendered empty, so cleanup can strip the
// template decoration around them (e.g. the parens in " ({year})") without
// touching the same characters when they come from real metadata values.
const mark = "\x00"

var (
	emptyBrackets = regexp.MustCompile(`\([\x00\s]*\x00[\x00\s]*\)|\[[\x00\s]*\x00[\x00\s]*\]`)
	sepBefore     = regexp.MustCompile(`[\s]*[-–—_·,:][\s]*\x00`)
	sepAfter      = regexp.MustCompile(`\x00[\s]*[-–—_·,:][\s]*`)
	bareMark      = regexp.MustCompile(`\x00`)
	spaceRuns     = regexp.MustCompile(`\s{2,}`)
)

func renderSegment(parts []part, m Meta) string {
	var b strings.Builder
	hasEmpty := false
	for _, p := range parts {
		if p.token == "" {
			b.WriteString(p.literal)
			continue
		}
		v := tokenValue(p, m)
		if v == "" {
			b.WriteString(mark)
			hasEmpty = true
		} else {
			b.WriteString(strings.ReplaceAll(v, mark, ""))
		}
	}
	s := b.String()
	if hasEmpty {
		s = cleanup(s)
	}
	return sanitize(s)
}

func tokenValue(p part, m Meta) string {
	switch p.token {
	case "artist", "albumartist":
		return m.Artist
	case "album":
		return m.Album
	case "title":
		return m.Title
	case "year":
		if m.Year <= 0 {
			return ""
		}
		return strconv.Itoa(m.Year)
	case "track":
		return padded(m.Track, p.pad)
	case "disc":
		if m.Disc <= 0 {
			return ""
		}
		return padded(m.Disc, p.pad)
	}
	return ""
}

func padded(n, width int) string {
	if width > 0 {
		return fmt.Sprintf("%0*d", width, n)
	}
	return strconv.Itoa(n)
}

// cleanup strips decoration left behind by empty tokens: bracket pairs that
// contained only empty tokens, separators dangling next to them, and the
// whitespace runs that removal leaves. Iterates to a fixed point because one
// removal can expose another (e.g. "(\x00 - \x00)").
func cleanup(s string) string {
	for {
		prev := s
		s = emptyBrackets.ReplaceAllString(s, "")
		s = sepBefore.ReplaceAllString(s, "")
		s = sepAfter.ReplaceAllString(s, "")
		if s == prev {
			break
		}
	}
	s = bareMark.ReplaceAllString(s, "")
	return spaceRuns.ReplaceAllString(s, " ")
}

var unsafeChars = regexp.MustCompile(`[<>:"/\\|?*]`)

// sanitize replaces filesystem-unsafe characters and trims the segment.
// It runs on the fully rendered segment, so it also neutralizes unsafe
// characters in template literals, not just metadata values.
func sanitize(s string) string {
	return strings.TrimSpace(unsafeChars.ReplaceAllString(s, "_"))
}
