package uiterm

import (
	"strings"

	"github.com/nsf/termbox-go"
)

type Textview struct {
	Lines          []string
	CurrentLine    int
	Fg, Bg         Attribute
	showTimestamps bool
	parsedLines    []string

	ui             *Ui
	x0, y0, x1, y1 int
}

func (t *Textview) uiInitialize(ui *Ui) {
	t.ui = ui
	t.showTimestamps = true
}

func (t *Textview) ToggleTimestamps() {
	if t.showTimestamps == true {
		t.showTimestamps = false
	} else {
		t.showTimestamps = true
	}
	t.updateParsedLines()
	t.uiDraw()
}

func (t *Textview) uiSetActive(active bool) {
}

func (t *Textview) uiSetBounds(x0, y0, x1, y1 int) {
	t.x0 = x0
	t.y0 = y0
	t.x1 = x1
	t.y1 = y1
	t.updateParsedLines()
	t.uiDraw()
}

func (t *Textview) ScrollUp() {
	if newLine := t.CurrentLine + 1; newLine < len(t.parsedLines) {
		t.CurrentLine = newLine
	}
	t.uiDraw()
}

func (t *Textview) ScrollDown() {
	if newLine := t.CurrentLine - 1; newLine >= 0 {
		t.CurrentLine = newLine
	}
	t.uiDraw()
}

func (t *Textview) ScrollTop() {
	if newLine := len(t.parsedLines) - 1; newLine > 0 {
		t.CurrentLine = newLine
	} else {
		t.CurrentLine = 0
	}
	t.uiDraw()
}

func (t *Textview) ScrollBottom() {
	t.CurrentLine = 0
	t.uiDraw()
}

const (
	// maxScrollbackLines bounds the retained chat history. It used to grow for
	// the life of the process, and every line added re-wrapped the whole
	// buffer, so the cost of a session grew as the square of its length. This
	// is far more history than a reader ever scrolls back through.
	maxScrollbackLines = 10000
	// scrollbackTrimChunk is how much history is discarded once the cap is
	// reached. Trimming a block at a time means the rebuild it forces happens
	// once every scrollbackTrimChunk lines rather than on every line, which
	// keeps the amortised cost of an append constant.
	scrollbackTrimChunk = 1000
)

// wrapLine renders one stored line as the display lines it occupies.
func (t *Textview) wrapLine(line string, width int) []string {
	l := line
	if !t.showTimestamps {
		// Server and local messages need not have a timestamp prefix.
		if _, text, ok := strings.Cut(line, "]"); ok {
			l = strings.TrimSpace(text)
		}
	}
	var wrapped []string
	// A Builder keeps this linear; appending a rune at a time to a string
	// reallocates once per character.
	var current strings.Builder
	chars := 0
	for _, ch := range l {
		if chars >= width {
			wrapped = append(wrapped, current.String())
			current.Reset()
			chars = 0
		}
		current.WriteRune(ch)
		chars++
	}
	if chars > 0 {
		wrapped = append(wrapped, current.String())
	}
	return wrapped
}

func (t *Textview) updateParsedLines() {
	width := t.x1 - t.x0

	if t.Lines == nil || width <= 0 {
		t.parsedLines = nil
		t.CurrentLine = 0
		return
	}

	parsed := make([]string, 0, len(t.Lines))
	for _, line := range t.Lines {
		parsed = append(parsed, t.wrapLine(line, width)...)
	}
	t.parsedLines = parsed
	t.clampCurrentLine()
}

func (t *Textview) AddLine(line string) {
	t.Lines = append(t.Lines, line)
	if len(t.Lines) > maxScrollbackLines {
		// Trimming invalidates the wrapped buffer and forces a rebuild, so
		// discard a block rather than a single line; otherwise every append
		// past the cap would re-wrap the whole buffer.
		keep := maxScrollbackLines - scrollbackTrimChunk
		t.Lines = append(t.Lines[:0], t.Lines[len(t.Lines)-keep:]...)
		t.updateParsedLines()
	} else if width := t.x1 - t.x0; width > 0 {
		// Wrap just the new line. Rebuilding every stored line on each append
		// is what made a long-lived session stall the terminal.
		t.parsedLines = append(t.parsedLines, t.wrapLine(line, width)...)
		t.clampCurrentLine()
	}
	t.uiDraw()
}

func (t *Textview) Clear() {
	t.Lines = nil
	t.CurrentLine = 0
	t.parsedLines = nil
	t.uiDraw()
}

func (t *Textview) uiDraw() {
	t.ui.beginDraw()
	defer t.ui.endDraw()

	var reader *strings.Reader
	writeableLines := t.y1 - t.y0
	lineNum := t.visibleStartLine(writeableLines)
	//Beep()
	for y := t.y0; y < t.y1; y++ {
		if lineNum < len(t.parsedLines) {
			reader = strings.NewReader(t.parsedLines[lineNum])
		} else {
			reader = nil
		}
		for x := t.x0; x < t.x1; x++ {
			var chr rune = ' '
			if reader != nil {
				if ch, _, err := reader.ReadRune(); err == nil {
					chr = safeRune(ch)
				} //no err
			} //reader != nil
			termbox.SetCell(x, y, chr, termbox.Attribute(t.Fg), termbox.Attribute(t.Bg))
		} //each x
		lineNum++
	} //each y
} //func

func (t *Textview) visibleStartLine(writeableLines int) int {
	if writeableLines <= 0 || len(t.parsedLines) == 0 {
		return 0
	}

	bottomStart := len(t.parsedLines) - writeableLines
	if bottomStart < 0 {
		bottomStart = 0
	}
	start := bottomStart - t.CurrentLine
	if start < 0 {
		return 0
	}
	return start
}

func (t *Textview) clampCurrentLine() {
	if len(t.parsedLines) == 0 {
		t.CurrentLine = 0
		return
	}
	t.CurrentLine = bounded(t.CurrentLine, 0, len(t.parsedLines)-1)
}

func (t *Textview) uiKeyEvent(key Key) {
}

func (t *Textview) uiCharacterEvent(chr rune) {
}
