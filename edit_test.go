package rsts

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptTerm plays a fixed sequence of keystrokes at the editor and keeps
// whatever it draws.
type scriptTerm struct {
	keys []byte
	out  bytes.Buffer
	cols int
	rows int
	kind string
	raw  bool
}

func (t *scriptTerm) ReadByte() (byte, error) {
	if len(t.keys) == 0 {
		return 0, io.EOF
	}
	b := t.keys[0]
	t.keys = t.keys[1:]
	return b, nil
}

func (t *scriptTerm) Write(p []byte) (int, error) { return t.out.Write(p) }

func (t *scriptTerm) Size() (int, int) {
	if t.cols == 0 {
		return 80, 24
	}
	return t.cols, t.rows
}

func (t *scriptTerm) TermType() string { return t.kind }
func (t *scriptTerm) StartRaw() error  { t.raw = true; return nil }
func (t *scriptTerm) StopRaw()         { t.raw = false }

// The shell also expects a line-mode terminal, which the editor never uses.
func (t *scriptTerm) ReadLine(string) (string, error)     { return "", io.EOF }
func (t *scriptTerm) ReadPassword(string) (string, error) { return "", io.EOF }

const (
	ctrlC          = 3
	ctrlG          = 7
	ctrlK          = 11
	ctrlO          = 15
	ctrlQ          = 17
	ctrlR          = 18
	ctrlS          = 19
	ctrlT          = 20
	ctrlU          = 21
	ctrlV          = 22
	ctrlW          = 23
	ctrlX          = 24
	ctrlY          = 25
	ctrlBackslash  = 28
	ctrlRBrack     = 29
	ctrlCaret      = 30
	ctrlUnderscore = 31
)

func runEditor(t *testing.T, text string, keys []byte) (string, *scriptTerm, bool) {
	t.Helper()
	term := &scriptTerm{keys: keys}
	var saved string
	ed := newEditor(term, "TEST", text, func(body string) error {
		saved = body
		return nil
	})
	ok, err := ed.Run()
	if err != nil && err != io.EOF {
		t.Fatalf("editor: %v", err)
	}
	if term.raw {
		t.Error("terminal left in raw mode")
	}
	return saved, term, ok
}

func TestEditInsertAndSave(t *testing.T) {
	keys := append([]byte("HELLO"), ctrlX)
	saved, _, ok := runEditor(t, "", keys)
	if !ok {
		t.Fatal("Ctrl-X should save and exit")
	}
	if saved != "HELLO\n" {
		t.Fatalf("saved %q", saved)
	}
}

func TestEditQuitDiscardsChanges(t *testing.T) {
	// One Ctrl-C asks, the second leaves without writing.
	keys := append([]byte("XYZ"), ctrlC, ctrlC)
	saved, _, ok := runEditor(t, "ORIGINAL\n", keys)
	if ok {
		t.Fatal("quitting should not report a save")
	}
	if saved != "" {
		t.Fatalf("quit wrote %q", saved)
	}
}

func TestEditQuitAsksOnceWhenModified(t *testing.T) {
	term := &scriptTerm{keys: append([]byte("X"), ctrlC)}
	ed := newEditor(term, "T", "A\n", func(string) error { return nil })
	if _, err := ed.Run(); err != io.EOF {
		t.Fatalf("a single Ctrl-C should not have left the editor: %v", err)
	}
	if !strings.Contains(term.out.String(), "Ctrl-C again") {
		t.Fatal("expected a confirmation prompt after one Ctrl-C")
	}
}

func TestEditNavigationAndEditing(t *testing.T) {
	var keys []byte
	keys = append(keys, 27, '[', 'B')   // down to line two
	keys = append(keys, 5)              // Ctrl-E, end of line
	keys = append(keys, []byte("!")...) // append
	keys = append(keys, 27, '[', 'A')   // up to line one
	keys = append(keys, 1)              // Ctrl-A, start of line
	keys = append(keys, []byte(">")...) // prepend
	keys = append(keys, ctrlX)
	saved, _, _ := runEditor(t, "ONE\nTWO\n", keys)
	if saved != ">ONE\nTWO!\n" {
		t.Fatalf("saved %q", saved)
	}
}

func TestEditVT52ArrowsAlsoWork(t *testing.T) {
	// A real VT52 sends ESC B with no bracket.
	keys := []byte{27, 'B', 5, '!', ctrlX}
	saved, _, _ := runEditor(t, "ONE\nTWO\n", keys)
	if saved != "ONE\nTWO!\n" {
		t.Fatalf("saved %q", saved)
	}
}

func TestEditSplitJoinAndKill(t *testing.T) {
	keys := []byte{27, '[', 'C', 27, '[', 'C', '\r', ctrlX} // right, right, Enter
	saved, _, _ := runEditor(t, "ABCD\n", keys)
	if saved != "AB\nCD\n" {
		t.Fatalf("split: %q", saved)
	}

	keys = []byte{27, '[', 'B', 8, ctrlX} // down then backspace joins
	saved, _, _ = runEditor(t, "AB\nCD\n", keys)
	if saved != "ABCD\n" {
		t.Fatalf("join: %q", saved)
	}

	keys = []byte{ctrlK, ctrlX}
	saved, _, _ = runEditor(t, "ONE\nTWO\n", keys)
	if saved != "TWO\n" {
		t.Fatalf("kill line: %q", saved)
	}
}

// A Telnet client sends CR LF, or CR NUL, for one press of Return.
func TestEditReturnOverTelnetIsOneNewline(t *testing.T) {
	keys := []byte{'A', '\r', '\n', 'B', '\r', 0, 'C', ctrlX}
	saved, _, _ := runEditor(t, "", keys)
	if saved != "A\nB\nC\n" {
		t.Fatalf("saved %q, want %q", saved, "A\nB\nC\n")
	}
}

func TestEditDeleteForward(t *testing.T) {
	keys := []byte{4, ctrlX} // Ctrl-D
	saved, _, _ := runEditor(t, "ABC\n", keys)
	if saved != "BC\n" {
		t.Fatalf("saved %q", saved)
	}
	keys = []byte{27, '[', '3', '~', ctrlX} // Delete key
	saved, _, _ = runEditor(t, "ABC\n", keys)
	if saved != "BC\n" {
		t.Fatalf("delete key: %q", saved)
	}
}

// A failed save keeps the editor open with the reason on screen, rather
// than throwing the text away.
func TestEditFailedSaveKeepsBuffer(t *testing.T) {
	term := &scriptTerm{keys: []byte{'A', ctrlX, ctrlC, ctrlC}}
	tries := 0
	ed := newEditor(term, "T", "", func(string) error {
		tries++
		return fsErr("Protection violation")
	})
	saved, err := ed.Run()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if saved {
		t.Fatal("a failed write must not report success")
	}
	if tries != 1 {
		t.Fatalf("save attempts = %d", tries)
	}
	if !strings.Contains(term.out.String(), "Protection violation") {
		t.Fatal("the reason should be on the status line")
	}
}

func TestEditUsesVT52SequencesForVT52(t *testing.T) {
	term := &scriptTerm{keys: []byte{ctrlC}, kind: "VT52"}
	ed := newEditor(term, "T", "HI\n", nil)
	if _, err := ed.Run(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	out := term.out.String()
	if !strings.Contains(out, "\x1bY") {
		t.Fatalf("expected VT52 cursor addressing:\n%q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("VT52 terminal should not get ANSI:\n%q", out)
	}
}

func TestEditUsesANSIByDefault(t *testing.T) {
	term := &scriptTerm{keys: []byte{ctrlC}, kind: "xterm-256color"}
	ed := newEditor(term, "T", "HI\n", nil)
	if _, err := ed.Run(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if !strings.Contains(term.out.String(), "\x1b[") {
		t.Fatal("expected ANSI sequences")
	}
}

func TestEditScrollsLongerThanScreen(t *testing.T) {
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, "LINE "+string(rune('0'+i%10)))
	}
	term := &scriptTerm{keys: []byte{ctrlC}, cols: 80, rows: 10}
	ed := newEditor(term, "T", strings.Join(lines, "\n")+"\n", nil)
	ed.buf.cy = 55
	if _, err := ed.Run(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if ed.buf.top == 0 {
		t.Fatal("the window should have scrolled to reach line 56")
	}
	if ed.buf.cy < ed.buf.top || ed.buf.cy >= ed.buf.top+ed.textRows() {
		t.Fatalf("cursor at %d is outside the window %d..%d",
			ed.buf.cy, ed.buf.top, ed.buf.top+ed.textRows())
	}
}

func TestEditBufferRoundTrip(t *testing.T) {
	for _, text := range []string{"", "A\n", "A\nB\n", "A\nB", "\n"} {
		b := newEditBuffer(text)
		got := b.Text()
		want := text
		if want == "" {
			want = "\n"
		}
		if !strings.HasSuffix(want, "\n") {
			want += "\n"
		}
		if got != want {
			t.Errorf("round trip of %q gave %q, want %q", text, got, want)
		}
	}
}

func TestEditProgramInMemory(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	sh.Dispatch("NEW EDDEMO")
	sh.Dispatch("10 PRINT 1")
	sh.Dispatch("20 END")

	// Append a line, then write and exit.
	keys := []byte{27, '[', 'B', 5, '\r'}
	keys = append(keys, []byte("15 PRINT 2")...)
	keys = append(keys, ctrlX)
	sh.term = &scriptTerm{keys: keys}

	if err := sh.cmdEdit(""); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(sh.Basic.Listing(0, 0, false, false), "\n")
	if got != "10 PRINT 1\n15 PRINT 2\n20 END" {
		t.Fatalf("program after edit:\n%s", got)
	}
	if sh.Basic.ProgramName != "EDDEMO" {
		t.Fatalf("program name became %q", sh.Basic.ProgramName)
	}
}

// A syntax error must not be stored, or the edit would silently lose work.
func TestEditProgramRejectsBadSyntax(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	sh.Dispatch("NEW EDBAD")
	sh.Dispatch("10 PRINT 1")

	keys := []byte{5, '\r'}
	keys = append(keys, []byte("20 PRINT )(")...)
	keys = append(keys, ctrlX, ctrlC, ctrlC)
	term := &scriptTerm{keys: keys}
	sh.term = term
	if err := sh.cmdEdit(""); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(sh.Basic.Listing(0, 0, false, false), "\n")
	if got != "10 PRINT 1" {
		t.Fatalf("a bad edit was stored:\n%s", got)
	}
}

func TestEditFileOnDisk(t *testing.T) {
	root := t.TempDir()
	sh, err := NewShell(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")

	keys := append([]byte("NEW TEXT"), ctrlX)
	sh.term = &scriptTerm{keys: keys}
	if err := sh.cmdEdit("NOTES.TXT"); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(root, "SY", "100,100", "NOTES.TXT"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "NEW TEXT\n" {
		t.Fatalf("file holds %q", body)
	}
}

func TestEditKeepsFileProtection(t *testing.T) {
	root := t.TempDir()
	sh, err := NewShell(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	spec := mustSpec(t, "SECRET.TXT")
	if err := sh.Disk.WriteText(spec, 100, 100, false, "one\n", 0); err != nil {
		t.Fatal(err)
	}
	// 40 denies group and world write but leaves the owner alone.
	if err := sh.Disk.SetProt(spec, 100, 100, false, 40); err != nil {
		t.Fatal(err)
	}

	sh.term = &scriptTerm{keys: append([]byte("X"), ctrlX)}
	if err := sh.cmdEdit("SECRET.TXT"); err != nil {
		t.Fatal(err)
	}
	prot, err := sh.Disk.Prot(spec, 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if prot != 40 {
		t.Fatalf("protection became %d, want 40", prot)
	}
}

// A file the owner may read but not write can be opened and not saved.
func TestEditCannotWriteProtectedFile(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	spec := mustSpec(t, "RONLY.TXT")
	if err := sh.Disk.WriteText(spec, 100, 100, false, "keep\n", 0); err != nil {
		t.Fatal(err)
	}
	if err := sh.Disk.SetProt(spec, 100, 100, false, protOwnerWrite); err != nil {
		t.Fatal(err)
	}

	// Type a character, try to write, then give up and quit.
	sh.term = &scriptTerm{keys: []byte{'X', ctrlX, ctrlC, ctrlC}}
	if err := sh.cmdEdit("RONLY.TXT"); err != nil {
		t.Fatal(err)
	}
	text, err := sh.Disk.ReadText(spec, 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if text != "keep\n" {
		t.Fatalf("a write-protected file was changed to %q", text)
	}
}

// The editor must not be a way around file protection.
func TestEditRespectsProtection(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	sh.term = &scriptTerm{keys: []byte{ctrlC}}
	if err := sh.cmdEdit("[1,9]WHOAMI.BAC"); err == nil {
		t.Fatal("a guest should not be able to edit a privileged .BAC")
	}
}

func TestEditRefusesCompiledProgram(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	sh.Dispatch("NEW C")
	sh.Dispatch("10 END")
	if err := sh.cmdCompile("C"); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdOld("C.BAC"); err != nil {
		t.Fatal(err)
	}
	sh.term = &scriptTerm{keys: []byte{ctrlC}}
	if err := sh.cmdEdit(""); err == nil {
		t.Fatal("EDIT should refuse a compiled program")
	}
}

func TestEditNeedsATerminal(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	sh.term = &plainTerm{}
	if err := sh.cmdEdit(""); err == nil {
		t.Fatal("EDIT should decline a terminal it cannot drive")
	}
}

func TestEditFindAndFindAgain(t *testing.T) {
	keys := []byte{ctrlS}
	keys = append(keys, []byte("BBB")...)
	keys = append(keys, '\r', 'X', ctrlX)
	saved, _, _ := runEditor(t, "AAA\nBBB\nAAA\n", keys)
	if saved != "AAA\nXBBB\nAAA\n" {
		t.Fatalf("find: %q", saved)
	}

	keys = []byte{ctrlS}
	keys = append(keys, []byte("AAA")...)
	keys = append(keys, '\r', ctrlS, '\r', 'X', ctrlX)
	saved, _, _ = runEditor(t, "AAA\nBBB\nAAA\n", keys)
	if saved != "AAA\nBBB\nXAAA\n" {
		t.Fatalf("find again: %q", saved)
	}
}

func TestEditFindWraps(t *testing.T) {
	keys := []byte{ctrlS}
	keys = append(keys, []byte("AAA")...)
	keys = append(keys, '\r', ctrlS, '\r', ctrlS, '\r', 'X', ctrlX)
	saved, term, _ := runEditor(t, "AAA\nBBB\nAAA\n", keys)
	if saved != "XAAA\nBBB\nAAA\n" {
		t.Fatalf("wrapped find: %q", saved)
	}
	if !strings.Contains(term.out.String(), "wrapped") {
		t.Fatal("expected wrapped on the status line")
	}
}

func TestEditFindNotFound(t *testing.T) {
	keys := []byte{ctrlS}
	keys = append(keys, []byte("ZZZ")...)
	keys = append(keys, '\r', ctrlC)
	_, term, ok := runEditor(t, "AAA\n", keys)
	if ok {
		t.Fatal("should have quit without saving")
	}
	if !strings.Contains(term.out.String(), "not found") {
		t.Fatal("expected not found on the status line")
	}
}

func TestEditFindCancel(t *testing.T) {
	keys := []byte{ctrlS, ctrlG, 'X', ctrlX}
	saved, _, _ := runEditor(t, "AAA\n", keys)
	if saved != "XAAA\n" {
		t.Fatalf("cancelled find should leave the cursor: %q", saved)
	}
}

func TestEditReverseFind(t *testing.T) {
	keys := []byte{ctrlG}
	keys = append(keys, []byte("3")...)
	keys = append(keys, '\r', 5, ctrlR) // line 3, end of line, reverse
	keys = append(keys, []byte("AAA")...)
	keys = append(keys, '\r', 'X', ctrlX)
	saved, _, _ := runEditor(t, "AAA\nBBB\nAAA\n", keys)
	if saved != "AAA\nBBB\nXAAA\n" {
		t.Fatalf("reverse find: %q", saved)
	}
}

func TestEditReplaceAll(t *testing.T) {
	keys := []byte{ctrlBackslash}
	keys = append(keys, []byte("FOO")...)
	keys = append(keys, '\r')
	keys = append(keys, []byte("BAZ")...)
	keys = append(keys, '\r', 'A', ctrlX)
	saved, _, _ := runEditor(t, "FOO BAR FOO\n", keys)
	if saved != "BAZ BAR BAZ\n" {
		t.Fatalf("replace all: %q", saved)
	}
}

func TestEditReplaceOneThenStop(t *testing.T) {
	keys := []byte{ctrlBackslash}
	keys = append(keys, []byte("FOO")...)
	keys = append(keys, '\r')
	keys = append(keys, []byte("BAZ")...)
	keys = append(keys, '\r', 'Y', 'Q', ctrlX)
	saved, _, _ := runEditor(t, "FOO BAR FOO\n", keys)
	if saved != "BAZ BAR FOO\n" {
		t.Fatalf("replace one: %q", saved)
	}
}

func TestEditReplaceSkip(t *testing.T) {
	keys := []byte{ctrlBackslash}
	keys = append(keys, []byte("FOO")...)
	keys = append(keys, '\r')
	keys = append(keys, []byte("BAZ")...)
	keys = append(keys, '\r', 'N', 'Y', ctrlX)
	saved, _, _ := runEditor(t, "FOO BAR FOO\n", keys)
	if saved != "FOO BAR BAZ\n" {
		t.Fatalf("replace skip: %q", saved)
	}
}

func TestEditKillYank(t *testing.T) {
	keys := []byte{ctrlK, 14, 1, ctrlY, ctrlX} // kill ONE, down, start of THREE, yank
	saved, _, _ := runEditor(t, "ONE\nTWO\nTHREE\n", keys)
	if saved != "TWO\nONE\nTHREE\n" {
		t.Fatalf("yank: %q", saved)
	}
}

func TestEditKillYankConsecutive(t *testing.T) {
	keys := []byte{ctrlK, ctrlK, 1, ctrlY, ctrlX}
	saved, _, _ := runEditor(t, "ONE\nTWO\nTHREE\n", keys)
	if saved != "ONE\nTWO\nTHREE\n" {
		t.Fatalf("consecutive kill: %q", saved)
	}
}

func TestEditGotoLine(t *testing.T) {
	keys := []byte{ctrlG}
	keys = append(keys, []byte("3")...)
	keys = append(keys, '\r', 'X', ctrlX)
	saved, _, _ := runEditor(t, "A\nB\nC\n", keys)
	if saved != "A\nB\nXC\n" {
		t.Fatalf("goto: %q", saved)
	}
}

func TestEditWordMotion(t *testing.T) {
	keys := []byte{ctrlRBrack, 'X', ctrlX}
	saved, _, _ := runEditor(t, "PRINT 1\n", keys)
	if saved != "PRINTX 1\n" {
		t.Fatalf("word forward: %q", saved)
	}

	keys = []byte{5, ctrlUnderscore, 'X', ctrlX}
	saved, _, _ = runEditor(t, "PRINT 1\n", keys)
	if saved != "PRINT X1\n" {
		t.Fatalf("word back: %q", saved)
	}
}

func TestEditMarkCopyYank(t *testing.T) {
	keys := []byte{ctrlCaret, 6, 6, ctrlV, 5, ctrlY, ctrlX}
	saved, _, _ := runEditor(t, "HELLO\n", keys)
	if saved != "HELLOHE\n" {
		t.Fatalf("copy region: %q", saved)
	}
}

func TestEditMarkCut(t *testing.T) {
	keys := []byte{ctrlCaret, 6, 6, ctrlQ, ctrlX}
	saved, _, _ := runEditor(t, "HELLO\n", keys)
	if saved != "LLO\n" {
		t.Fatalf("cut region: %q", saved)
	}
}

func TestEditTranspose(t *testing.T) {
	keys := []byte{ctrlT, ctrlX}
	saved, _, _ := runEditor(t, "AB\n", keys)
	if saved != "BA\n" {
		t.Fatalf("transpose: %q", saved)
	}
}

func TestEditOpenLine(t *testing.T) {
	keys := []byte{ctrlO, ctrlX}
	saved, _, _ := runEditor(t, "AB\n", keys)
	if saved != "\nAB\n" {
		t.Fatalf("open line: %q", saved)
	}
}

func TestEditKillToBOL(t *testing.T) {
	keys := []byte{5, ctrlU, ctrlY, ctrlX}
	saved, _, _ := runEditor(t, "HELLO\n", keys)
	if saved != "HELLO\n" {
		t.Fatalf("kill to BOL then yank: %q", saved)
	}

	keys = []byte{5, ctrlU, ctrlX}
	saved, _, _ = runEditor(t, "HELLO\n", keys)
	if saved != "\n" {
		t.Fatalf("kill to BOL: %q", saved)
	}
}

func TestEditOverwrite(t *testing.T) {
	keys := []byte{27, '[', '2', '~', 'X', ctrlX}
	saved, _, _ := runEditor(t, "ABC\n", keys)
	if saved != "XBC\n" {
		t.Fatalf("overwrite: %q", saved)
	}
}

func TestEditBufferFind(t *testing.T) {
	b := newEditBuffer("AAA\nBBB\nAAA\n")
	ok, wrapped := b.find("BBB", true, false)
	if !ok || wrapped || b.cy != 1 || b.cx != 0 {
		t.Fatalf("forward find at %d,%d ok=%v wrap=%v", b.cy, b.cx, ok, wrapped)
	}
	ok, wrapped = b.find("AAA", true, false)
	if !ok || b.cy != 2 {
		t.Fatalf("next AAA at line %d (wrap %v)", b.cy+1, wrapped)
	}
	ok, wrapped = b.find("AAA", true, true)
	if !ok || !wrapped || b.cy != 0 {
		t.Fatalf("wrap to %d,%d ok=%v wrap=%v", b.cy, b.cx, ok, wrapped)
	}
	b.cy, b.cx = 2, 3
	ok, wrapped = b.find("AAA", false, false)
	if !ok || wrapped || b.cy != 2 || b.cx != 0 {
		t.Fatalf("reverse on last line at %d,%d wrap=%v", b.cy, b.cx, wrapped)
	}
	ok, wrapped = b.find("AAA", false, true)
	if !ok || wrapped || b.cy != 0 {
		t.Fatalf("reverse again at line %d wrap=%v", b.cy+1, wrapped)
	}
	ok, _ = b.find("ZZZ", true, false)
	if ok {
		t.Fatal("ZZZ should not be found")
	}
}

// plainTerm can read lines but has no raw mode.
type plainTerm struct{}

func (plainTerm) ReadLine(string) (string, error)     { return "", io.EOF }
func (plainTerm) ReadPassword(string) (string, error) { return "", io.EOF }
