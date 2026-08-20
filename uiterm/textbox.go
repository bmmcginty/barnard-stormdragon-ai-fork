package uiterm

import (
	"strings"
	"unicode/utf8"

	"github.com/nsf/termbox-go"
)

type Textbox struct {
	Text   string
	Fg, Bg Attribute

	Input func(ui *Ui, textbox *Textbox, text string)

	ui             *Ui
	active         bool
	x0, y0, x1, y1 int
	pos            int
	history        []string
	historyIndex   int
	historyDraft   string
}

func (t *Textbox) uiInitialize(ui *Ui) {
	t.ui = ui
	t.pos = 0
}

func (t *Textbox) uiSetActive(active bool) {
	t.active = active
	t.uiDraw()
}

func (t *Textbox) uiSetBounds(x0, y0, x1, y1 int) {
	t.x0 = x0
	t.y0 = y0
	t.x1 = x1
	t.y1 = y1
	t.uiDraw()
}

func (t *Textbox) uiDraw() {
	if t.ui == nil {
		return
	}
	t.ui.beginDraw()
	defer t.ui.endDraw()

	reader := strings.NewReader(t.Text)
	if t.pos < 0 {
		t.pos = 0
	}
	if t.pos > len(t.Text) {
		t.pos = len(t.Text)
	}
	for t.pos > 0 && t.pos < len(t.Text) && !utf8.RuneStart(t.Text[t.pos]) {
		t.pos--
	}
	for y := t.y0; y < t.y1; y++ {
		for x := t.x0; x < t.x1; x++ {
			var chr rune
			if ch, _, err := reader.ReadRune(); err != nil {
				chr = ' '
			} else {
				chr = safeRune(ch)
			}
			termbox.SetCell(x, y, chr, termbox.Attribute(t.Fg), termbox.Attribute(t.Bg))
		}
	}
	if t.active {
		var x = 0
		var y = 0
		var idx = -1
		var flag = false
		for y = t.y0; y < t.y1; y++ {
			for x = t.x0; x < t.x1; x++ {
				idx += 1
				if idx == t.pos {
					flag = true
				}
				if flag == true {
					break
				}
			}
			if flag == true {
				break
			}
		}
		termbox.SetCursor(x, y)
	}
}

func (t *Textbox) uiKeyEvent(key Key) {
	redraw := false
	switch key {
	case KeyHome:
		t.pos = 0
		redraw = true
	case KeyEnd:
		t.pos = len(t.Text)
		redraw = true
	case KeyArrowLeft:
		if t.pos > 0 {
			_, size := utf8.DecodeLastRuneInString(t.Text[:t.pos])
			t.pos -= size
		}
		redraw = true
	case KeyArrowRight:
		if t.pos < len(t.Text) {
			_, size := utf8.DecodeRuneInString(t.Text[t.pos:])
			t.pos += size
		}
		redraw = true
	case KeyCtrlC:
		t.Text = ""
		t.pos = 0
		t.resetHistoryNavigation()
		redraw = true
	case KeyEnter:
		text := t.Text
		if t.Input != nil {
			t.Input(t.ui, t, text)
		}
		t.addHistory(text)
		t.Text = ""
		t.pos = 0
		t.resetHistoryNavigation()
		redraw = true
	case KeyArrowUp, KeyAltArrowUp, KeyArrowDown, KeyAltArrowDown:
		redraw = t.handleHistoryKey(key)
	case KeySpace:
		t.uiCharacterEvent(' ')
	case KeyBackspace, KeyBackspace2:
		if len(t.Text) > 0 {
			if t.pos > 0 {
				_, size := utf8.DecodeLastRuneInString(t.Text[:t.pos])
				t.Text = t.Text[:t.pos-size] + t.Text[t.pos:]
				t.pos -= size
			}
		}
		//			if r, size := utf8.DecodeLastRuneInString(t.Text); r != utf8.RuneError {
		//				t.Text = t.Text[:len(t.Text)-size]
		//t.pos-=size
		redraw = true
		//			}
		//		}
	}
	if redraw {
		// Input callbacks may update another view (for example, append a chat
		// message). Redraw every view after submission, not just this textbox.
		if key == KeyEnter && t.ui != nil {
			t.ui.Refresh()
		} else {
			t.uiDraw()
		}
	}
}

func (t *Textbox) uiCharacterEvent(chr rune) {
	var s = string(chr)
	t.Text = t.Text[:t.pos] + s + t.Text[t.pos:]
	t.pos += len(s)
	t.uiDraw()
}

func (t *Textbox) setText(text string) {
	t.Text = text
	t.pos = len(text)
}

func (t *Textbox) addHistory(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if len(t.history) > 0 && t.history[len(t.history)-1] == text {
		t.historyIndex = len(t.history)
		return
	}
	t.history = append(t.history, text)
	t.historyIndex = len(t.history)
}

func (t *Textbox) handleHistoryKey(key Key) bool {
	switch key {
	case KeyArrowUp, KeyAltArrowUp:
		return t.previousHistory()
	case KeyArrowDown, KeyAltArrowDown:
		return t.nextHistory()
	default:
		return false
	}
}

func (t *Textbox) previousHistory() bool {
	if len(t.history) == 0 {
		return false
	}
	if t.historyIndex == len(t.history) {
		t.historyDraft = t.Text
	}
	if t.historyIndex > 0 {
		t.historyIndex--
	}
	t.setText(t.history[t.historyIndex])
	return true
}

func (t *Textbox) nextHistory() bool {
	if len(t.history) == 0 || t.historyIndex == len(t.history) {
		return false
	}
	if t.historyIndex < len(t.history)-1 {
		t.historyIndex++
		t.setText(t.history[t.historyIndex])
		return true
	}
	t.historyIndex = len(t.history)
	t.setText(t.historyDraft)
	t.historyDraft = ""
	return true
}

func (t *Textbox) resetHistoryNavigation() {
	t.historyIndex = len(t.history)
	t.historyDraft = ""
}
