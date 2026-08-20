package uiterm

import "unicode"

// safeRune prevents text supplied by a server or another user from being
// interpreted as a terminal control sequence when termbox flushes its cells.
func safeRune(r rune) rune {
	if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
		return ' '
	}
	return r
}

type View interface {
	uiInitialize(ui *Ui)
	uiSetActive(active bool)
	uiSetBounds(x0, y0, x1, y1 int)
	uiDraw()
	uiKeyEvent(key Key)
	uiCharacterEvent(ch rune)
	// commandEvent(cmd string)
}
