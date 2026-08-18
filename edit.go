package rsts

import (
	"fmt"
	"strconv"
	"strings"
)

// A screen editor in the spirit of VTEDIT, the macro package RSTS sites
// layered on TECO to get full-screen editing on a VT52. This one is not
// TECO underneath, and it cannot reach outside the emulator: it edits a
// buffer and hands the text back to the caller to store through the
// normal file and program paths, so protection codes and account rules
// still apply.

const (
	keyNone = iota + 0x1000
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyPgUp
	keyPgDn
	keyDelete
	keyInsert
)

type editBuffer struct {
	lines      []string
	cx         int // column within the current line, in bytes
	cy         int // index into lines
	top        int // first line on screen
	dirty      bool
	overwrite  bool
	killBuf    string
	killAppend bool
	markX      int
	markY      int
	markSet    bool
}

func newEditBuffer(text string) *editBuffer {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// A trailing newline is a line terminator, not an empty last line.
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &editBuffer{lines: lines}
}

func (b *editBuffer) Text() string {
	return strings.Join(b.lines, "\n") + "\n"
}

func (b *editBuffer) line() string {
	if b.cy < 0 || b.cy >= len(b.lines) {
		return ""
	}
	return b.lines[b.cy]
}

func (b *editBuffer) setLine(s string) {
	if b.cy >= 0 && b.cy < len(b.lines) {
		b.lines[b.cy] = s
		b.dirty = true
	}
}

func (b *editBuffer) clamp() {
	if b.cy < 0 {
		b.cy = 0
	}
	if b.cy >= len(b.lines) {
		b.cy = len(b.lines) - 1
	}
	if b.cx < 0 {
		b.cx = 0
	}
	if n := len(b.line()); b.cx > n {
		b.cx = n
	}
}

func (b *editBuffer) insert(ch byte) {
	b.killAppend = false
	s := b.line()
	if b.overwrite && b.cx < len(s) {
		b.setLine(s[:b.cx] + string(ch) + s[b.cx+1:])
		b.cx++
		return
	}
	b.setLine(s[:b.cx] + string(ch) + s[b.cx:])
	b.cx++
}

func (b *editBuffer) insertString(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	over := b.overwrite
	b.overwrite = false
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			b.newline()
		} else {
			b.insert(s[i])
		}
	}
	b.overwrite = over
}

func (b *editBuffer) newline() {
	b.killAppend = false
	s := b.line()
	head, tail := s[:b.cx], s[b.cx:]
	b.lines = append(b.lines, "")
	copy(b.lines[b.cy+2:], b.lines[b.cy+1:])
	b.lines[b.cy] = head
	b.lines[b.cy+1] = tail
	b.cy++
	b.cx = 0
	b.dirty = true
}

func (b *editBuffer) backspace() {
	b.killAppend = false
	if b.cx > 0 {
		s := b.line()
		b.setLine(s[:b.cx-1] + s[b.cx:])
		b.cx--
		return
	}
	if b.cy == 0 {
		return
	}
	prev := b.lines[b.cy-1]
	cur := b.line()
	b.cx = len(prev)
	b.lines[b.cy-1] = prev + cur
	b.lines = append(b.lines[:b.cy], b.lines[b.cy+1:]...)
	b.cy--
	b.dirty = true
}

func (b *editBuffer) deleteForward() {
	b.killAppend = false
	s := b.line()
	if b.cx < len(s) {
		b.setLine(s[:b.cx] + s[b.cx+1:])
		return
	}
	if b.cy+1 >= len(b.lines) {
		return
	}
	b.lines[b.cy] = s + b.lines[b.cy+1]
	b.lines = append(b.lines[:b.cy+1], b.lines[b.cy+2:]...)
	b.dirty = true
}

func (b *editBuffer) deleteLine() {
	s := b.line()
	if len(b.lines) == 1 {
		b.killSet(s)
		b.setLine("")
		b.cx = 0
		return
	}
	b.killSet(s + "\n")
	b.lines = append(b.lines[:b.cy], b.lines[b.cy+1:]...)
	if b.cy >= len(b.lines) {
		b.cy = len(b.lines) - 1
	}
	b.cx = 0
	b.dirty = true
}

func (b *editBuffer) killSet(s string) {
	if b.killAppend {
		b.killBuf += s
	} else {
		b.killBuf = s
	}
	b.killAppend = true
}

func (b *editBuffer) killToBOL() {
	if b.cx <= 0 {
		return
	}
	s := b.line()
	b.killSet(s[:b.cx])
	b.setLine(s[b.cx:])
	b.cx = 0
}

func (b *editBuffer) yank() {
	if b.killBuf == "" {
		return
	}
	b.insertString(b.killBuf)
}

func (b *editBuffer) openLine() {
	b.killAppend = false
	s := b.line()
	head, tail := s[:b.cx], s[b.cx:]
	b.lines = append(b.lines, "")
	copy(b.lines[b.cy+2:], b.lines[b.cy+1:])
	b.lines[b.cy] = head
	b.lines[b.cy+1] = tail
	b.dirty = true
}

func (b *editBuffer) transpose() {
	s := b.line()
	if len(s) < 2 {
		return
	}
	i := b.cx
	if i == 0 {
		i = 1
	}
	if i >= len(s) {
		a, c := s[len(s)-2], s[len(s)-1]
		b.setLine(s[:len(s)-2] + string(c) + string(a))
		b.cx = len(s)
		b.killAppend = false
		return
	}
	a, c := s[i-1], s[i]
	b.setLine(s[:i-1] + string(c) + string(a) + s[i+1:])
	b.cx = i + 1
	b.killAppend = false
}

func isWordChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '$' || c == '%'
}

func (b *editBuffer) wordForward() {
	b.clamp()
	for {
		s := b.line()
		if b.cx < len(s) {
			if isWordChar(s[b.cx]) {
				for b.cx < len(s) && isWordChar(s[b.cx]) {
					b.cx++
				}
				return
			}
			b.cx++
			continue
		}
		if b.cy+1 >= len(b.lines) {
			return
		}
		b.cy++
		b.cx = 0
	}
}

func (b *editBuffer) wordBack() {
	b.clamp()
	for {
		if b.cx > 0 {
			b.cx--
			s := b.line()
			if !isWordChar(s[b.cx]) {
				continue
			}
			for b.cx > 0 && isWordChar(s[b.cx-1]) {
				b.cx--
			}
			return
		}
		if b.cy == 0 {
			return
		}
		b.cy--
		b.cx = len(b.line())
	}
}

func (b *editBuffer) setMark() {
	b.clamp()
	b.markX = b.cx
	b.markY = b.cy
	b.markSet = true
}

func (b *editBuffer) orderedPos() (y1, x1, y2, x2 int) {
	y1, x1 = b.markY, b.markX
	y2, x2 = b.cy, b.cx
	if y1 > y2 || (y1 == y2 && x1 > x2) {
		return y2, x2, y1, x1
	}
	return
}

func (b *editBuffer) regionText() string {
	if !b.markSet {
		return ""
	}
	y1, x1, y2, x2 := b.orderedPos()
	if y1 < 0 {
		y1 = 0
	}
	if y2 >= len(b.lines) {
		y2 = len(b.lines) - 1
	}
	if y1 == y2 {
		line := b.lines[y1]
		if x1 > len(line) {
			x1 = len(line)
		}
		if x2 > len(line) {
			x2 = len(line)
		}
		if x1 > x2 {
			x1, x2 = x2, x1
		}
		return line[x1:x2]
	}
	var sb strings.Builder
	l1 := b.lines[y1]
	if x1 > len(l1) {
		x1 = len(l1)
	}
	sb.WriteString(l1[x1:])
	sb.WriteByte('\n')
	for y := y1 + 1; y < y2; y++ {
		sb.WriteString(b.lines[y])
		sb.WriteByte('\n')
	}
	l2 := b.lines[y2]
	if x2 > len(l2) {
		x2 = len(l2)
	}
	sb.WriteString(l2[:x2])
	return sb.String()
}

func (b *editBuffer) copyRegion() bool {
	if !b.markSet {
		return false
	}
	b.killBuf = b.regionText()
	b.killAppend = false
	return true
}

func (b *editBuffer) killRegion() bool {
	if !b.markSet {
		return false
	}
	y1, x1, y2, x2 := b.orderedPos()
	if y1 < 0 {
		y1 = 0
		x1 = 0
	}
	if y2 >= len(b.lines) {
		y2 = len(b.lines) - 1
		x2 = len(b.lines[y2])
	}
	text := b.regionText()
	head := b.lines[y1]
	if x1 > len(head) {
		x1 = len(head)
	}
	tail := b.lines[y2]
	if x2 > len(tail) {
		x2 = len(tail)
	}
	b.killBuf = text
	b.killAppend = true
	joined := head[:x1] + tail[x2:]
	newLines := append([]string{}, b.lines[:y1]...)
	newLines = append(newLines, joined)
	newLines = append(newLines, b.lines[y2+1:]...)
	if len(newLines) == 0 {
		newLines = []string{""}
	}
	b.lines = newLines
	b.cy = y1
	b.cx = x1
	b.dirty = true
	b.markSet = false
	return true
}

func (b *editBuffer) matchAt(pat string) bool {
	if pat == "" {
		return false
	}
	s := b.line()
	if b.cx < 0 || b.cx+len(pat) > len(s) {
		return false
	}
	return s[b.cx:b.cx+len(pat)] == pat
}

func (b *editBuffer) replaceAt(pat, repl string) {
	if !b.matchAt(pat) {
		return
	}
	s := b.line()
	b.setLine(s[:b.cx] + s[b.cx+len(pat):])
	over := b.overwrite
	b.overwrite = false
	b.insertString(repl)
	b.overwrite = over
}

// find locates pat. skip starts just after the cursor so Find again
// does not land on the same match. wrap reports that the search
// continued from the other end of the buffer.
func (b *editBuffer) find(pat string, forward, skip bool) (ok, wrapped bool) {
	if pat == "" || len(b.lines) == 0 {
		return false, false
	}
	b.clamp()
	n := len(b.lines)
	if forward {
		y, x := b.cy, b.cx
		if skip {
			x++
		}
		for pass := 0; pass < 2; pass++ {
			for y < n {
				line := b.lines[y]
				if x < 0 {
					x = 0
				}
				if x <= len(line) {
					if i := strings.Index(line[x:], pat); i >= 0 {
						b.cy = y
						b.cx = x + i
						return true, pass == 1
					}
				}
				y++
				x = 0
			}
			y, x = 0, 0
		}
		return false, false
	}
	y, x := b.cy, b.cx
	if skip {
		x--
	}
	for pass := 0; pass < 2; pass++ {
		for y >= 0 {
			line := b.lines[y]
			if x > len(line) {
				x = len(line)
			}
			if x < 0 {
				y--
				if y >= 0 {
					x = len(b.lines[y])
				}
				continue
			}
			end := x + len(pat)
			if end > len(line) {
				end = len(line)
			}
			i := strings.LastIndex(line[:end], pat)
			for i > x {
				if i == 0 {
					i = -1
					break
				}
				i = strings.LastIndex(line[:i], pat)
			}
			if i >= 0 {
				b.cy = y
				b.cx = i
				return true, pass == 1
			}
			y--
			if y >= 0 {
				x = len(b.lines[y])
			}
		}
		y = n - 1
		x = len(b.lines[y])
	}
	return false, false
}

// scroll keeps the cursor on screen, given how many text rows there are.
func (b *editBuffer) scroll(rows int) {
	if rows < 1 {
		rows = 1
	}
	if b.cy < b.top {
		b.top = b.cy
	}
	if b.cy >= b.top+rows {
		b.top = b.cy - rows + 1
	}
	if b.top < 0 {
		b.top = 0
	}
}

// vt writes screen control sequences. A VT52 gets the sequences a VT52
// understands; everything else gets ANSI, which is what a modern Telnet
// client speaks.
type vt struct {
	w    rawTerm
	vt52 bool
}

func newVT(t rawTerm) *vt {
	return &vt{w: t, vt52: strings.Contains(strings.ToUpper(t.TermType()), "VT52")}
}

func (v *vt) out(s string) { _, _ = v.w.Write([]byte(s)) }

func (v *vt) moveTo(row, col int) {
	if v.vt52 {
		v.out(fmt.Sprintf("\x1bY%c%c", byte(31+row), byte(31+col)))
		return
	}
	v.out(fmt.Sprintf("\x1b[%d;%dH", row, col))
}

func (v *vt) clearEOL() {
	if v.vt52 {
		v.out("\x1bK")
		return
	}
	v.out("\x1b[K")
}

func (v *vt) clearAll() {
	if v.vt52 {
		v.out("\x1bH\x1bJ")
		return
	}
	v.out("\x1b[H\x1b[2J")
}

func (v *vt) reverse(on bool) {
	if v.vt52 {
		return
	}
	if on {
		v.out("\x1b[7m")
	} else {
		v.out("\x1b[0m")
	}
}

type editor struct {
	term    rawTerm
	vt      *vt
	buf     *editBuffer
	title   string
	status  string
	cols    int
	rows    int
	afterCR bool
	search  string
	// save is called for Ctrl-W and Ctrl-X. Returning an error leaves the
	// editor open with the error on the status line, so a program with a
	// syntax error is not lost.
	save func(text string) error
}

func newEditor(t rawTerm, title, text string, save func(string) error) *editor {
	cols, rows := t.Size()
	if cols < 20 {
		cols = 80
	}
	if rows < 4 {
		rows = 24
	}
	return &editor{
		term:  t,
		vt:    newVT(t),
		buf:   newEditBuffer(text),
		title: title,
		cols:  cols,
		rows:  rows,
		save:  save,
	}
}

func (e *editor) textRows() int { return e.rows - 1 }

func (e *editor) draw() {
	e.buf.scroll(e.textRows())
	e.vt.clearAll()
	for i := 0; i < e.textRows(); i++ {
		n := e.buf.top + i
		if n >= len(e.buf.lines) {
			break
		}
		line := e.buf.lines[n]
		if len(line) > e.cols {
			line = line[:e.cols]
		}
		e.vt.moveTo(i+1, 1)
		e.vt.clearEOL()
		e.vt.out(line)
	}
	e.drawStatus()
	e.vt.moveTo(e.buf.cy-e.buf.top+1, e.buf.cx+1)
}

func (e *editor) drawStatus() {
	mark := " "
	if e.buf.dirty {
		mark = "*"
	}
	left := fmt.Sprintf("%s%s  line %d of %d  col %d",
		mark, e.title, e.buf.cy+1, len(e.buf.lines), e.buf.cx+1)
	right := e.status
	if right == "" {
		right = "^W write  ^X exit  ^S find  ^\\ repl  ^Y yank"
	}
	gap := e.cols - len(left) - len(right) - 1
	if gap < 1 {
		gap = 1
		if n := e.cols - len(left) - gap - 1; n > 0 && n < len(right) {
			right = right[:n]
		} else if n <= 0 {
			right = ""
		}
	}
	text := left + strings.Repeat(" ", gap) + right
	if len(text) > e.cols {
		text = text[:e.cols]
	}
	e.vt.moveTo(e.rows, 1)
	e.vt.clearEOL()
	e.vt.reverse(true)
	e.vt.out(text)
	e.vt.reverse(false)
}

// nextByte reads one byte, dropping the LF or NUL that a Telnet client
// sends after a CR. Without this, one press of Return would open two
// lines.
func (e *editor) nextByte() (byte, error) {
	for {
		b, err := e.term.ReadByte()
		if err != nil {
			return 0, err
		}
		if e.afterCR {
			e.afterCR = false
			if b == '\n' || b == 0 {
				continue
			}
		}
		if b == '\r' {
			e.afterCR = true
		}
		if b == 0 {
			continue
		}
		return b, nil
	}
}

// readKey turns a byte, or an arrow key's escape sequence, into one code.
func (e *editor) readKey() (int, error) {
	b, err := e.nextByte()
	if err != nil {
		return 0, err
	}
	if b != 27 {
		return int(b), nil
	}
	b, err = e.term.ReadByte()
	if err != nil {
		return 0, err
	}
	switch b {
	case '[', 'O':
		// ANSI or SS3: ESC [ A, and ESC [ 3 ~ for Delete.
		c, err := e.term.ReadByte()
		if err != nil {
			return 0, err
		}
		if c >= '0' && c <= '9' {
			num := int(c - '0')
			for {
				d, err := e.term.ReadByte()
				if err != nil {
					return 0, err
				}
				if d >= '0' && d <= '9' {
					num = num*10 + int(d-'0')
					continue
				}
				break
			}
			switch num {
			case 1, 7:
				return keyHome, nil
			case 2:
				return keyInsert, nil
			case 3:
				return keyDelete, nil
			case 4, 8:
				return keyEnd, nil
			case 5:
				return keyPgUp, nil
			case 6:
				return keyPgDn, nil
			}
			return keyNone, nil
		}
		return ansiFinal(c), nil
	default:
		// VT52 sends ESC A directly.
		return ansiFinal(b), nil
	}
}

func ansiFinal(c byte) int {
	switch c {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	case 'C':
		return keyRight
	case 'D':
		return keyLeft
	case 'H':
		return keyHome
	case 'F', 'K':
		return keyEnd
	}
	return keyNone
}

// Run drives the editor until the user leaves it. It reports whether the
// text was saved.
func (e *editor) Run() (bool, error) {
	if err := e.term.StartRaw(); err != nil {
		return false, err
	}
	defer func() {
		e.term.StopRaw()
		e.vt.reverse(false)
		e.vt.clearAll()
	}()

	saved := false
	confirm := false
	for {
		e.draw()
		k, err := e.readKey()
		if err != nil {
			return saved, err
		}
		if k != 3 {
			confirm = false
		}
		switch k {
		case 23: // Ctrl-W, write
			if err := e.doSave(); err == nil {
				saved = true
			}
		case 24: // Ctrl-X, write and exit
			if err := e.doSave(); err != nil {
				continue
			}
			return true, nil
		case 3: // Ctrl-C, quit
			if e.buf.dirty && !confirm {
				confirm = true
				e.status = "Ctrl-C again to lose changes"
				continue
			}
			return saved, nil
		case 12: // Ctrl-L, redraw
			e.status = ""
		case 19: // Ctrl-S, find
			e.doFind(true)
		case 18: // Ctrl-R, reverse find
			e.doFind(false)
		case 28: // Ctrl-\, replace
			e.doReplace()
		case 7: // Ctrl-G, goto line
			e.doGoto()
		case 25: // Ctrl-Y, yank
			if e.buf.killBuf == "" {
				e.status = "nothing to yank"
			} else {
				e.buf.yank()
				e.status = ""
			}
		case 11: // Ctrl-K, kill line
			e.buf.deleteLine()
			e.status = ""
		case 21: // Ctrl-U, kill to start of line
			e.buf.killToBOL()
			e.status = ""
		case 15: // Ctrl-O, open line
			e.buf.openLine()
			e.status = ""
		case 20: // Ctrl-T, transpose
			e.buf.transpose()
			e.status = ""
		case 29: // Ctrl-], word forward
			e.buf.wordForward()
			e.status = ""
		case 31: // Ctrl-_, word back
			e.buf.wordBack()
			e.status = ""
		case 30: // Ctrl-^, set mark
			e.buf.setMark()
			e.status = "mark set"
		case 22: // Ctrl-V, copy region
			if e.buf.copyRegion() {
				e.status = "copied"
			} else {
				e.status = "no mark"
			}
		case 17: // Ctrl-Q, cut region
			if e.buf.killRegion() {
				e.status = "cut"
			} else {
				e.status = "no mark"
			}
		case keyInsert:
			e.buf.overwrite = !e.buf.overwrite
			if e.buf.overwrite {
				e.status = "overwrite"
			} else {
				e.status = "insert"
			}
		case 1: // Ctrl-A
			e.buf.cx = 0
		case 5: // Ctrl-E
			e.buf.cx = len(e.buf.line())
		case 4, keyDelete:
			e.buf.deleteForward()
		case 8, 127:
			e.buf.backspace()
		case '\r', '\n':
			e.buf.newline()
		case '\t':
			for i := 0; i < 4; i++ {
				e.buf.insert(' ')
			}
		case 16, keyUp:
			e.buf.cy--
		case 14, keyDown:
			e.buf.cy++
		case 2, keyLeft:
			if e.buf.cx == 0 && e.buf.cy > 0 {
				e.buf.cy--
				e.buf.cx = len(e.buf.line())
			} else {
				e.buf.cx--
			}
		case 6, keyRight:
			if e.buf.cx >= len(e.buf.line()) && e.buf.cy+1 < len(e.buf.lines) {
				e.buf.cy++
				e.buf.cx = 0
			} else {
				e.buf.cx++
			}
		case keyHome:
			e.buf.cx = 0
		case keyEnd:
			e.buf.cx = len(e.buf.line())
		case keyPgUp:
			e.buf.cy -= e.textRows()
		case keyPgDn:
			e.buf.cy += e.textRows()
		case keyNone:
			// an escape sequence we do not use
		default:
			if k >= 32 && k < 127 {
				e.buf.insert(byte(k))
			}
		}
		e.buf.clamp()
	}
}

func (e *editor) readPrompt(prompt string) (string, bool) {
	var buf []byte
	for {
		e.status = prompt + string(buf)
		e.draw()
		k, err := e.readKey()
		if err != nil {
			return "", false
		}
		switch {
		case k == 7 || k == 3:
			e.status = "cancelled"
			return "", false
		case k == '\r' || k == '\n':
			e.status = ""
			return string(buf), true
		case k == 8 || k == 127 || k == keyDelete:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
			}
		case k >= 32 && k < 127:
			buf = append(buf, byte(k))
		}
	}
}

func (e *editor) doFind(forward bool) {
	prompt := "Find: "
	if !forward {
		prompt = "Reverse: "
	}
	s, ok := e.readPrompt(prompt)
	if !ok {
		return
	}
	skip := false
	if s == "" {
		s = e.search
		skip = true
	}
	if s == "" {
		e.status = "no search"
		return
	}
	e.search = s
	found, wrapped := e.buf.find(s, forward, skip)
	if !found {
		e.status = "not found"
		return
	}
	if wrapped {
		e.status = "wrapped"
		return
	}
	e.status = ""
}

func (e *editor) doReplace() {
	find, ok := e.readPrompt("Replace: ")
	if !ok {
		return
	}
	if find == "" {
		find = e.search
	}
	if find == "" {
		e.status = "no search"
		return
	}
	repl, ok := e.readPrompt("With: ")
	if !ok {
		return
	}
	e.search = find
	n := 0
	all := false
	skip := false
	for {
		found, wrapped := e.buf.find(find, true, skip)
		if !found || wrapped {
			break
		}
		skip = true
		if !all {
			e.status = "Replace? Y/N/A/^G"
			e.draw()
			k, err := e.readKey()
			if err != nil {
				break
			}
			switch {
			case k == 'y' || k == 'Y':
				e.buf.replaceAt(find, repl)
				n++
			case k == 'n' || k == 'N':
			case k == 'a' || k == 'A':
				all = true
				e.buf.replaceAt(find, repl)
				n++
			case k == 7 || k == 3 || k == 'q' || k == 'Q':
				e.status = fmt.Sprintf("%d replaced", n)
				return
			default:
				skip = false
			}
		} else {
			e.buf.replaceAt(find, repl)
			n++
		}
	}
	if n == 0 {
		e.status = "not found"
		return
	}
	e.status = fmt.Sprintf("%d replaced", n)
}

func (e *editor) doGoto() {
	s, ok := e.readPrompt("Line: ")
	if !ok {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		e.status = "bad line"
		return
	}
	e.buf.cy = n - 1
	e.buf.cx = 0
	e.buf.clamp()
	e.status = ""
}

func (e *editor) doSave() error {
	if e.save == nil {
		e.status = "nowhere to write"
		return fsErr("I/O error")
	}
	if err := e.save(e.buf.Text()); err != nil {
		e.status = strings.TrimPrefix(err.Error(), "?")
		return err
	}
	e.buf.dirty = false
	e.status = "written"
	return nil
}
