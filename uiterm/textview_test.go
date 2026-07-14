package uiterm

import "testing"

func TestTextviewVisibleStartLineShowsBottomWhenNotScrolled(t *testing.T) {
	t.Parallel()

	textview := Textview{
		parsedLines: []string{"one", "two", "three", "four"},
	}

	if got := textview.visibleStartLine(3); got != 1 {
		t.Fatalf("expected visible lines to start at 1, got %d", got)
	}
}

func TestTextviewVisibleStartLineHonorsScrollOffset(t *testing.T) {
	t.Parallel()

	textview := Textview{
		CurrentLine: 2,
		parsedLines: []string{"one", "two", "three", "four", "five"},
	}

	if got := textview.visibleStartLine(3); got != 0 {
		t.Fatalf("expected scrolled view to start at 0, got %d", got)
	}
}

func TestTextviewUpdateParsedLinesClampsScrollOffset(t *testing.T) {
	t.Parallel()

	textview := Textview{
		CurrentLine:    10,
		Lines:          []string{"one", "two"},
		showTimestamps: true,
		x0:             0,
		x1:             20,
	}

	textview.updateParsedLines()

	if textview.CurrentLine != 1 {
		t.Fatalf("expected current line to clamp to 1, got %d", textview.CurrentLine)
	}
}
