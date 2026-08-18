package rsts

import (
	"strings"
	"testing"
)

func listing(m *Machine) string {
	return strings.Join(m.Listing(0, 0, false, false), "\n")
}

func loadFor(t *testing.T, src string) *Machine {
	t.Helper()
	m := NewMachine(IO{})
	if err := m.LoadSource(src, "T"); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRenumDefaults(t *testing.T) {
	m := loadFor(t, "5 PRINT 1\n7 PRINT 2\n9 END\n")
	if _, err := m.Renumber(10, 10); err != nil {
		t.Fatal(err)
	}
	want := "10 PRINT 1\n20 PRINT 2\n30 END"
	if got := listing(m); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenumRewritesEveryReferenceForm(t *testing.T) {
	src := `100 GOTO 130
105 GOSUB 140
110 ON X GOTO 130, 140, 150
115 ON X GOSUB 140, 150
120 IF A=1 THEN 130 ELSE 150
125 ON ERROR GOTO 145
128 RESTORE 150
130 PRINT "ONE"
135 RESUME 150
140 PRINT "TWO"
145 RESUME 130
150 END
`
	m := loadFor(t, src)
	missing, err := m.Renumber(10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("unexpected undefined references: %v", missing)
	}
	want := `10 GOTO 80
20 GOSUB 100
30 ON X GOTO 80, 100, 120
40 ON X GOSUB 100, 120
50 IF A=1 THEN 80 ELSE 120
60 ON ERROR GOTO 110
70 RESTORE 120
80 PRINT "ONE"
90 RESUME 120
100 PRINT "TWO"
110 RESUME 80
120 END`
	if got := listing(m); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenumCustomStartAndStep(t *testing.T) {
	m := loadFor(t, "1 GOTO 3\n2 PRINT 1\n3 END\n")
	if _, err := m.Renumber(100, 5); err != nil {
		t.Fatal(err)
	}
	want := "100 GOTO 110\n105 PRINT 1\n110 END"
	if got := listing(m); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// ON ERROR GOTO 0 and RESUME 0 are not line references.
func TestRenumLeavesZeroAlone(t *testing.T) {
	m := loadFor(t, "10 ON ERROR GOTO 0\n20 RESUME 0\n30 END\n")
	missing, err := m.Renumber(100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("zero was treated as a line: %v", missing)
	}
	want := "100 ON ERROR GOTO 0\n200 RESUME 0\n300 END"
	if got := listing(m); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A CHAIN line number belongs to the other program, not this one.
func TestRenumLeavesChainLineAlone(t *testing.T) {
	m := loadFor(t, "10 PRINT 1\n20 CHAIN \"NEXT\" LINE 999\n30 END\n")
	missing, err := m.Renumber(100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("CHAIN to another program was treated as a missing line: %v", missing)
	}
	if got := listing(m); !strings.Contains(got, `CHAIN "NEXT" LINE 999`) {
		t.Fatalf("chain target was renumbered:\n%s", got)
	}
}

func TestRenumRewritesSelfChainLine(t *testing.T) {
	src := `10 PRINT 1
20 CHAIN "T" LINE 40
30 PRINT "SKIP"
40 PRINT "LAND"
50 END
`
	m := loadFor(t, src)
	if _, err := m.Renumber(100, 10); err != nil {
		t.Fatal(err)
	}
	got := listing(m)
	if !strings.Contains(got, `CHAIN "T" LINE 130`) {
		t.Fatalf("self CHAIN LINE was not rewritten:\n%s", got)
	}
	if strings.Contains(got, `LINE 40`) {
		t.Fatalf("old CHAIN LINE survived:\n%s", got)
	}
}

func TestRenumRewritesCompChainLine(t *testing.T) {
	src := "10 REM\n20 CHAIN \"COMP\" LINE 8000\n8000 END\n"
	m := NewMachine(IO{})
	if err := m.LoadSource(src, "COMP"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Renumber(10, 10); err != nil {
		t.Fatal(err)
	}
	got := listing(m)
	if !strings.Contains(got, `CHAIN "COMP" LINE 30`) {
		t.Fatalf("CHAIN \"COMP\" LINE 8000 must become LINE 30:\n%s", got)
	}
	if strings.Contains(got, "LINE 8000") {
		t.Fatalf("LINE 8000 survived RENUM:\n%s", got)
	}
}

func TestRenumReportsUndefinedReference(t *testing.T) {
	m := loadFor(t, "10 GOTO 999\n20 END\n")
	missing, err := m.Renumber(10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != 999 {
		t.Fatalf("missing = %v, want [999]", missing)
	}
	if got := listing(m); !strings.Contains(got, "GOTO 999") {
		t.Fatalf("a dangling reference should be left as it was:\n%s", got)
	}
}

func TestRenumRejectsOverflowAndBadArgs(t *testing.T) {
	m := loadFor(t, "10 PRINT 1\n20 PRINT 2\n30 END\n")
	if _, err := m.Renumber(32760, 10); err == nil {
		t.Fatal("expected the last line to overflow 32767")
	}
	if _, err := m.Renumber(0, 10); err == nil {
		t.Fatal("expected a start of 0 to be rejected")
	}
	if _, err := m.Renumber(10, 0); err == nil {
		t.Fatal("expected an increment of 0 to be rejected")
	}
	if got := listing(m); got != "10 PRINT 1\n20 PRINT 2\n30 END" {
		t.Fatalf("a rejected RENUM must not change the program:\n%s", got)
	}
}

func TestRenumKeepsProgramRunnable(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	src := `1 GOSUB 8
2 GOTO 6
3 PRINT "SKIPPED"
6 PRINT "END OF IT"
7 GOTO 9
8 PRINT "SUB" \ RETURN
9 END
`
	if err := m.LoadSource(src, "T"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Renumber(10, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatalf("run after renumber: %v\n%s", err, listing(m))
	}
	out := c.out.String()
	if !strings.Contains(out, "SUB") || !strings.Contains(out, "END OF IT") {
		t.Fatalf("output after renumber: %q\n%s", out, listing(m))
	}
	if strings.Contains(out, "SKIPPED") {
		t.Fatalf("control flow changed: %q", out)
	}
}

func TestRenumRewritesRestore(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	src := `10 DATA 1
20 DATA 2
30 RESTORE 20
40 READ X
50 PRINT X
60 END
`
	if err := m.LoadSource(src, "T"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Renumber(100, 10); err != nil {
		t.Fatal(err)
	}
	if got := listing(m); !strings.Contains(got, "RESTORE 110") {
		t.Fatalf("RESTORE was not rewritten:\n%s", got)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatalf("run after renumber: %v\n%s", err, listing(m))
	}
	if strings.TrimSpace(c.out.String()) != "2" {
		t.Fatalf("restore after renumber: %q\n%s", c.out.String(), listing(m))
	}
}

func TestRestoreRefOffsetsSkipsBareRestore(t *testing.T) {
	if got := restoreRefOffsets(`REM DATA READ RESTORE`); len(got) != 0 {
		t.Fatalf("comment: %v", got)
	}
	if got := restoreRefOffsets(`RESTORE`); len(got) != 0 {
		t.Fatalf("bare: %v", got)
	}
	text := `RESTORE 1845`
	got := restoreRefOffsets(text)
	if len(got) != 1 || text[got[0]:got[0]+4] != "1845" {
		t.Fatalf("restore n: %v in %q", got, text)
	}
}

func TestRenumThroughShell(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out

	sh.Dispatch("NEW R")
	sh.Dispatch("5 GOTO 15")
	sh.Dispatch("15 END")

	sh.Dispatch("RENUM")
	if got := listing(sh.Basic); got != "10 GOTO 20\n20 END" {
		t.Fatalf("RENUM: %s", got)
	}

	sh.Dispatch("RENUM 100,50")
	if got := listing(sh.Basic); got != "100 GOTO 150\n150 END" {
		t.Fatalf("RENUM 100,50: %s", got)
	}

	sh.Dispatch("RENUMBER 1000 5")
	if got := listing(sh.Basic); got != "1000 GOTO 1005\n1005 END" {
		t.Fatalf("RENUMBER 1000 5: %s", got)
	}

	// RENU is shared only by RENUM and RENUMBER, so it is not ambiguous.
	sh.Dispatch("RENU 1,1")
	if got := listing(sh.Basic); got != "1 GOTO 2\n2 END" {
		t.Fatalf("RENU: %s", got)
	}
}

// CONT cannot resume a program whose lines have moved.
func TestRenumBlocksContinue(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	if err := m.LoadSource("10 A=1\n20 STOP\n30 PRINT A\n40 END\n", "T"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !m.Stopped {
		t.Fatal("expected STOP")
	}
	if _, err := m.Renumber(100, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.Continue(); err == nil {
		t.Fatal("CONT should not resume a renumbered program")
	}
}
