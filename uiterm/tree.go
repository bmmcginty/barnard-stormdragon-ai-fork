package uiterm

import (
	"strings"

	"github.com/nsf/termbox-go"
)

type TreeItem interface {
	TreeItemStyle(fg, bg Attribute, active bool) (Attribute, Attribute)
	String() string
}

type renderedTreeItem struct {
	//String string
	Level int
	Item  TreeItem
}

type Tree struct {
	Fg, Bg            Attribute
	Generator         func(item TreeItem) []TreeItem
	KeyListener       func(ui *Ui, tree *Tree, item TreeItem, key Key)
	CharacterListener func(ui *Ui, tree *Tree, item TreeItem, chr rune)

	lines      []renderedTreeItem
	activeLine int

	ui             *Ui
	active         bool
	x0, y0, x1, y1 int
}

func bounded(i, lower, upper int) int {
	if i < lower {
		return lower
	}
	if i > upper {
		return upper
	}
	return i
}

func (t *Tree) uiInitialize(ui *Ui) {
	t.ui = ui
}

func (t *Tree) uiSetActive(active bool) {
	t.active = active
	t.uiDraw()
}

func (t *Tree) uiSetBounds(x0, y0, x1, y1 int) {
	t.x0 = x0
	t.y0 = y0
	t.x1 = x1
	t.y1 = y1
	t.uiDraw()
}

func (t *Tree) Rebuild() {
	t.rebuild(false, nil)
}

func (t *Tree) RebuildPreservingActiveItem(sameItem func(previous, current TreeItem) bool) {
	t.rebuild(true, sameItem)
}

func (t *Tree) SetActiveItem(target TreeItem, sameItem func(previous, current TreeItem) bool) bool {
	if target == nil || sameItem == nil {
		return false
	}
	for line, item := range t.lines {
		if sameItem(target, item.Item) {
			t.SetActiveLine(line, false)
			return true
		}
	}
	return false
}

func (t *Tree) rebuild(preserveActive bool, sameItem func(previous, current TreeItem) bool) {
	if t.Generator == nil {
		t.lines = []renderedTreeItem{}
		return
	}

	previousItem := t.ActiveItem()
	previousLine := t.activeLine
	lines := []renderedTreeItem{}
	for _, item := range t.Generator(nil) {
		if len(lines) >= maxTreeLines {
			break
		}
		lines = t.rebuild_rec(lines, item, 0)
	}
	t.lines = lines
	if preserveActive {
		t.SetActiveLine(previousLine, false)
		if previousItem != nil && sameItem != nil {
			for line, item := range t.lines {
				if sameItem(previousItem, item.Item) {
					t.SetActiveLine(line, false)
					break
				}
			}
		}
	} else {
		t.SetActiveLine(0, false)
	}
	if t.ui != nil {
		t.uiDraw()
	}
}

// A server is free to describe a channel graph in which a channel is its own
// ancestor. The generator follows parent/child links literally, so without
// these limits such a graph recurses until the process is out of memory.
// Real trees are orders of magnitude smaller than either bound.
const (
	maxTreeDepth = 64
	maxTreeLines = 100000
)

// rebuild_rec appends parent and its descendants to lines. Accumulating into
// one slice keeps maxTreeLines a budget for the whole tree rather than for
// each level, and avoids building a slice per node.
func (t *Tree) rebuild_rec(lines []renderedTreeItem, parent TreeItem, level int) []renderedTreeItem {
	if parent == nil || level >= maxTreeDepth || len(lines) >= maxTreeLines {
		return lines
	}
	lines = append(lines, renderedTreeItem{
		Level: level,
		Item:  parent,
	})
	for _, item := range t.Generator(parent) {
		if len(lines) >= maxTreeLines {
			break
		}
		lines = t.rebuild_rec(lines, item, level+1)
	}
	return lines
}

func (t *Tree) uiDraw() {
	t.ui.beginDraw()
	defer t.ui.endDraw()

	if t.lines == nil {
		t.Rebuild()
	}

	if t.y1-t.y0 <= 0 {
		return
	}

	var line = t.activeLine
	var height = t.y1 - t.y0
	var startline = 0
	//var total = len(t.lines)
	//I'd welcome a better algorithm for this; for that matter, I'd love a book or reference for all sorts of GUI algorithms.
	//if (startline+height) < line {
	for startline = 0; (startline + height) <= line; startline += height {
	}
	//}
	//if startline+height >= total {
	//var rem=(startline+height)-total
	//startline-=rem
	//}
	if startline < 0 {
		startline = 0
	}
	line = startline
	for y := t.y0; y < t.y1; y++ {
		var reader *strings.Reader
		var item TreeItem
		level := 0
		if line < len(t.lines) {
			item = t.lines[line].Item
			level = t.lines[line].Level
			reader = strings.NewReader(item.String())
		}
		for x := t.x0; x < t.x1; x++ {
			var chr rune = ' '
			fg := t.Fg
			bg := t.Bg
			dx := x - t.x0
			if reader != nil && level*2 <= dx {
				if ch, _, err := reader.ReadRune(); err == nil {
					chr = safeRune(ch)
					fg, bg = item.TreeItemStyle(fg, bg, t.active && t.activeLine == line)
				}
			}
			termbox.SetCell(x, y, chr, termbox.Attribute(fg), termbox.Attribute(bg))
		}
		if t.activeLine == (line) {
			termbox.SetCursor(t.x0, y)
		}
		line++
	}
}

func (t *Tree) SetActiveLine(num int, relative bool) {
	if relative {
		t.activeLine = bounded(t.activeLine+num, 0, len(t.lines)-1)
	} else {
		t.activeLine = bounded(num, 0, len(t.lines)-1)
	}
}

func (t *Tree) ActiveItem() TreeItem {
	if len(t.lines) == 0 {
		return nil
	}
	return t.lines[bounded(t.activeLine, 0, len(t.lines)-1)].Item
}

func (t *Tree) uiKeyEvent(key Key) {
	if len(t.lines) == 0 {
		return
	}
	var runHandler = true
	switch key {
	case KeyArrowUp:
		t.SetActiveLine(-1, true)
		runHandler = false
	case KeyArrowDown:
		t.SetActiveLine(1, true)
		runHandler = false
	}
	if runHandler == true && t.KeyListener != nil {
		t.KeyListener(t.ui, t, t.lines[t.activeLine].Item, key)
	}
	t.uiDraw()
}

func (t *Tree) uiCharacterEvent(ch rune) {
	if len(t.lines) == 0 {
		return
	}
	if t.CharacterListener != nil {
		t.CharacterListener(t.ui, t, t.lines[t.activeLine].Item, ch)
	}
}
