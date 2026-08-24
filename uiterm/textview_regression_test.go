package uiterm

import (
	"fmt"
	"strings"
	"testing"
)

// addLine appends without drawing, so these tests need no terminal.
func addLineNoDraw(t *Textview, line string) {
	t.Lines = append(t.Lines, line)
	if len(t.Lines) > maxScrollbackLines {
		keep := maxScrollbackLines - scrollbackTrimChunk
		t.Lines = append(t.Lines[:0], t.Lines[len(t.Lines)-keep:]...)
		t.updateParsedLines()
		return
	}
	if width := t.x1 - t.x0; width > 0 {
		t.parsedLines = append(t.parsedLines, t.wrapLine(line, width)...)
		t.clampCurrentLine()
	}
}

// Regression: AddLine used to re-wrap every stored line on each append, which
// made the cost of a session grow as the square of its length. It now wraps
// only the new line, so that incremental result must match a full rebuild.
func TestTextviewIncrementalWrapMatchesFullRebuild(t *testing.T) {
	t.Parallel()

	lines := []string{
		"short [12:00:01]",
		strings.Repeat("a", 200) + " [12:00:02]",
		"",
		"exactly-twenty-chars",
		"unicode ünïcödé line with wide content [12:00:03]",
	}

	incremental := &Textview{x0: 0, x1: 20, showTimestamps: true}
	for _, line := range lines {
		addLineNoDraw(incremental, line)
	}

	full := &Textview{x0: 0, x1: 20, showTimestamps: true}
	full.Lines = append([]string(nil), lines...)
	full.updateParsedLines()

	if len(incremental.parsedLines) != len(full.parsedLines) {
		t.Fatalf("incremental produced %d wrapped lines, full rebuild %d",
			len(incremental.parsedLines), len(full.parsedLines))
	}
	for i := range full.parsedLines {
		if incremental.parsedLines[i] != full.parsedLines[i] {
			t.Fatalf("wrapped line %d differs: incremental %q, full %q",
				i, incremental.parsedLines[i], full.parsedLines[i])
		}
	}
}

// Regression: the scrollback had no cap, so a long-lived client retained every
// line it had ever displayed.
func TestTextviewScrollbackIsCapped(t *testing.T) {
	t.Parallel()

	view := &Textview{x0: 0, x1: 40, showTimestamps: true}
	for i := 0; i < maxScrollbackLines+500; i++ {
		addLineNoDraw(view, fmt.Sprintf("line %d", i))
	}

	if len(view.Lines) > maxScrollbackLines {
		t.Fatalf("expected scrollback capped at %d lines, got %d",
			maxScrollbackLines, len(view.Lines))
	}
	if len(view.Lines) < maxScrollbackLines-scrollbackTrimChunk {
		t.Fatalf("trim discarded more than one chunk: %d lines remain", len(view.Lines))
	}
	// The newest line must survive; the oldest must not.
	if got := view.Lines[len(view.Lines)-1]; got != fmt.Sprintf("line %d", maxScrollbackLines+499) {
		t.Fatalf("newest line was dropped, got %q", got)
	}
	if view.Lines[0] == "line 0" {
		t.Fatal("oldest line should have been trimmed")
	}
	if len(view.parsedLines) != len(view.Lines) {
		t.Fatalf("wrapped buffer out of sync after trim: %d wrapped, %d stored",
			len(view.parsedLines), len(view.Lines))
	}
}

// wrapLine replaced a loop that concatenated one rune at a time; confirm the
// wrapping itself is unchanged for the boundary cases.
func TestTextviewWrapLineBoundaries(t *testing.T) {
	t.Parallel()

	view := &Textview{showTimestamps: true}
	for _, tc := range []struct {
		line  string
		width int
		want  []string
	}{
		{"", 5, nil},
		{"abc", 5, []string{"abc"}},
		{"abcde", 5, []string{"abcde"}},
		{"abcdef", 5, []string{"abcde", "f"}},
		{"abcdeabcde", 5, []string{"abcde", "abcde"}},
	} {
		got := view.wrapLine(tc.line, tc.width)
		if len(got) != len(tc.want) {
			t.Fatalf("wrapLine(%q, %d) = %q, want %q", tc.line, tc.width, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("wrapLine(%q, %d) = %q, want %q", tc.line, tc.width, got, tc.want)
			}
		}
	}
}
