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
	want := `10 GOTO 70
20 GOSUB 90
30 ON X GOTO 70, 90, 110
40 ON X GOSUB 90, 110
50 IF A=1 THEN 70 ELSE 110
60 ON ERROR GOTO 100
70 PRINT "ONE"
80 RESUME 110
90 PRINT "TWO"
100 RESUME 70
110 END`
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
	m := loadFor(t, "10 PRINT 1\n20 CHAIN \"NEXT\" LINE 10\n30 END\n")
	if _, err := m.Renumber(100, 100); err != nil {
		t.Fatal(err)
	}
	if got := listing(m); !strings.Contains(got, `CHAIN "NEXT" LINE 10`) {
		t.Fatalf("chain target was renumbered:\n%s", got)
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
