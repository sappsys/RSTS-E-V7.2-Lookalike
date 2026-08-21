package rsts

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUntilLoop(t *testing.T) {
	out := runProgram(t, "10 I=1\n20 UNTIL I>3\n30 PRINT I\n40 I=I+1\n50 NEXT\n60 END\n")
	if strings.TrimSpace(out) != "1\n 2\n 3" {
		t.Fatalf("until: %q", out)
	}
}

func TestChange(t *testing.T) {
	out := runProgram(t, "10 A$=\"HI\"\n20 CHANGE A$ TO A\n30 PRINT A(0); A(1); A(2)\n40 CHANGE A TO B$\n50 PRINT B$\n60 END\n")
	if !strings.Contains(out, "2") || !strings.Contains(out, "72") || !strings.Contains(out, "73") {
		t.Fatalf("change nums: %q", out)
	}
	if !strings.Contains(out, "HI") {
		t.Fatalf("change back: %q", out)
	}
}

func TestPrintUsing(t *testing.T) {
	out := strings.TrimSpace(immediate(t, `PRINT USING "###.##", 12.5`))
	if !strings.Contains(out, "12.50") {
		t.Fatalf("using: %q", out)
	}
	out = strings.TrimSpace(immediate(t, `PRINT USING "!", "HELLO"`))
	if out != "H" {
		t.Fatalf("using bang: %q", out)
	}
}

func TestOnError(t *testing.T) {
	out := runProgram(t, "10 ON ERROR GOTO 50\n20 PRINT 1/0\n30 PRINT \"NO\"\n40 END\n50 PRINT ERR; ERL\n60 RESUME 40\n")
	if strings.Contains(out, "NO") {
		t.Fatalf("should skip line 30: %q", out)
	}
	if !strings.Contains(out, "48") {
		t.Fatalf("ERR not 48: %q", out)
	}
	if !strings.Contains(out, "20") {
		t.Fatalf("ERL not 20: %q", out)
	}
}

func TestModifiers(t *testing.T) {
	out := runProgram(t, "10 PRINT I IF I<>2 FOR I=1 TO 3\n20 PRINT \"X\" UNLESS 1=1\n30 END\n")
	got := strings.ReplaceAll(out, " ", "")
	if !strings.Contains(got, "1\n") || !strings.Contains(got, "3\n") {
		t.Fatalf("for/if: %q", out)
	}
	if strings.Contains(out, "X") {
		t.Fatalf("unless fired: %q", out)
	}
	out = strings.TrimSpace(immediate(t, `PRINT I FOR I=1 TO 3`))
	got = strings.ReplaceAll(out, " ", "")
	if !strings.Contains(got, "1") || !strings.Contains(got, "3") {
		t.Fatalf("immediate for: %q", out)
	}
}

func TestSysAndCvt(t *testing.T) {
	out := strings.TrimSpace(immediate(t, `PRINT SYS(CHR$(1))`))
	if !strings.Contains(out, "RSTS") || !strings.Contains(out, "V7.2") {
		t.Fatalf("sys: %q", out)
	}
	out = runProgram(t, "10 A$=CVT%$(1234)\n20 PRINT CVT$%(A$)\n30 END\n")
	if strings.TrimSpace(out) != "1234" {
		t.Fatalf("cvt: %q", out)
	}
	out = runProgram(t, "10 X=1.5\n20 A$=CVTF$(X)\n30 PRINT CVT$F(A$)\n40 END\n")
	if !strings.Contains(strings.TrimSpace(out), "1.5") {
		t.Fatalf("cvtf: %q", out)
	}
}

func TestPDP1170(t *testing.T) {
	if got := strings.TrimSpace(immediate(t, "PRINT SWAP%(1)")); got != "256" {
		t.Fatalf("swap 1: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, "PRINT SWAP%(256)")); got != "1" {
		t.Fatalf("swap 256: %q", got)
	}
	out := runProgram(t, "10 PRINT (PEEK(518%) AND 255%)/2%\n20 END\n")
	if strings.TrimSpace(out) != "1" {
		t.Fatalf("peek job: %q", out)
	}
	out = strings.TrimSpace(immediate(t, "PRINT TIME(0)"))
	n := 0.0
	if _, err := fmt.Sscanf(out, "%f", &n); err != nil || n < 0 || n >= 86400 {
		t.Fatalf("time(0): %q", out)
	}
	out = strings.TrimSpace(immediate(t, "PRINT DATE(0)"))
	if out == "" || out == "0" {
		t.Fatalf("date(0): %q", out)
	}
	out = runProgram(t, "10 CHANGE SYS(CHR$(6%)+CHR$(-3%)) TO T%\n20 PRINT T%(4%)\n30 PRINT T%(5%)+SWAP%(T%(6%))\n40 END\n")
	got := strings.Fields(out)
	if len(got) < 2 || got[0] != "63" || got[1] != "1920" {
		t.Fatalf("uu.tb1: %q", out)
	}
	ident := strings.TrimSpace(immediate(t, `PRINT RIGHT$(SYS(CHR$(6%)+CHR$(9%)+CHR$(0%)),3%)`))
	if !strings.Contains(ident, "RSTS") {
		t.Fatalf("ident: %q", ident)
	}
	if got := strings.TrimSpace(immediate(t, `PRINT RIGHT$("ABCD",3)`)); got != "CD" {
		t.Fatalf("right$: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, "PRINT PEEK(-2%)")); got != "0" {
		t.Fatalf("psw peek: %q", got)
	}
}

func TestRecordIO(t *testing.T) {
	dir := t.TempDir()
	c := &capture{}
	m := NewMachine(IO{
		Write: c.write,
		Read:  c.read,
		Open: func(m *Machine, ch int, path, mode string) error {
			f, err := os.OpenFile(filepath.Join(dir, path), os.O_RDWR|os.O_CREATE, 0o644)
			if err != nil {
				return err
			}
			m.Files[ch] = &chanFile{file: f, mode: mode}
			return nil
		},
	})
	src := `10 OPEN "REC.DAT" AS FILE 1, RECORDSIZE 16
20 FIELD #1, 8 AS A$, 8 AS B$
30 LSET A$ = "HELLO"
40 LSET B$ = "WORLD"
50 PUT #1, RECORD 1
60 LSET A$ = "XXXX"
70 GET #1, RECORD 1
80 PRINT A$; B$
90 CLOSE 1
100 END
`
	if err := m.LoadSource(src, "REC"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	out := c.out.String()
	if !strings.Contains(out, "HELLO") || !strings.Contains(out, "WORLD") {
		t.Fatalf("record: %q", out)
	}
}

func TestMatAdd(t *testing.T) {
	out := strings.TrimSpace(runProgram(t, `10 DIM A(3,3), B(3,3), C(3,3)
20 MAT READ A, B
35 DATA 1,2,3, 4,5,6, 7,8,9
36 DATA 9,8,7, 6,5,4, 3,2,1
40 MAT C = A + B       ! Add two matrices
50 MAT PRINT C;       ! Print result matrix
60 END
`))
	want := "10 10 10\n 10 10 10\n 10 10 10"
	if out != want {
		t.Fatalf("mat add:\n got %q\nwant %q", out, want)
	}
}

func TestMatFillAndScale(t *testing.T) {
	out := strings.TrimSpace(runProgram(t, `10 DIM A(2,2), B(2,2)
20 MAT A = CON
30 MAT B = (3)*A
40 MAT PRINT B;
50 END
`))
	want := "3 3\n 3 3"
	if out != want {
		t.Fatalf("mat scale: %q", out)
	}
}

func TestNum1(t *testing.T) {
	out := strings.TrimSpace(immediate(t, `PRINT "Record #"+NUM1$(12)`))
	if out != "Record #12" {
		t.Fatalf("num1$: %q", out)
	}
}

func TestVirtualMap(t *testing.T) {
	dir := t.TempDir()
	c := &capture{}
	m := NewMachine(IO{
		Write: c.write,
		Read:  c.read,
		Open: func(m *Machine, ch int, path, mode string) error {
			f, err := os.OpenFile(filepath.Join(dir, path), os.O_RDWR|os.O_CREATE, 0o644)
			if err != nil {
				return err
			}
			m.Files[ch] = &chanFile{file: f, mode: mode}
			return nil
		},
	})
	src := `10 EXTEND
20 OPEN "DATA.TMP" AS FILE 1%, ORGANIZATION VIRTUAL
30 MAP (DATMAP) LONG X%, STRING A$ = 20
40 FOR I% = 1% TO 1000%
50   X% = I% * 5% \ A$ = "Record #" + NUM1$(I%)
60 PUT #1%, RECORD I%
70 NEXT I%
80 GET #1%, RECORD 1
90 PRINT X%; A$
100 GET #1%, RECORD 1000
110 PRINT X%; A$
120 CLOSE #1%
130 END
`
	if err := m.LoadSource(src, "DATA"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	out := c.out.String()
	if !strings.Contains(out, "5") || !strings.Contains(out, "Record #1") {
		t.Fatalf("record 1: %q", out)
	}
	if !strings.Contains(out, "5000") || !strings.Contains(out, "Record #1000") {
		t.Fatalf("record 1000: %q", out)
	}
}

func TestComp(t *testing.T) {
	if samples["100,100"]["COMP.BAS"] != "" || samples["1,2"]["COMP.BAS"] != "" {
		t.Error("COMP.BAS belongs to [1,9] only")
	}
	src := samples["1,9"]["COMP.BAS"]
	if src == "" {
		t.Fatal("missing COMP.BAS sample")
	}
	dir := t.TempDir()
	// COMP.BAS chains to itself, so it has to be on the account it runs from.
	if err := os.WriteFile(filepath.Join(dir, "COMP.BAS"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &capture{}
	var m *Machine
	m = NewMachine(IO{
		Write: c.write,
		Read:  c.read,
		Open: func(m *Machine, ch int, path, mode string) error {
			real := filepath.Join(dir, path)
			var f *os.File
			var err error
			switch mode {
			case "INPUT":
				f, err = os.Open(real)
			case "OUTPUT":
				f, err = os.Create(real)
			case "APPEND":
				f, err = os.OpenFile(real, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			default:
				f, err = os.OpenFile(real, os.O_RDWR|os.O_CREATE, 0o644)
			}
			if err != nil {
				return err
			}
			cf := &chanFile{file: f, mode: mode}
			if mode == "INPUT" {
				cf.r = bufio.NewReader(f)
			}
			m.Files[ch] = cf
			return nil
		},
		Delete: func(path string) error { return os.Remove(filepath.Join(dir, path)) },
		Rename: func(old, new string) error {
			return os.Rename(filepath.Join(dir, old), filepath.Join(dir, new))
		},
		Load: func(name string) error {
			if !strings.Contains(name, ".") {
				name += ".BAS"
			}
			text, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return basicErr("Can't find file or account")
			}
			return m.LoadSource(string(text), "COMP")
		},
	})
	if err := m.LoadSource(src, "COMP"); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatalf("run: %v\n%s", err, c.out.String())
	}
	out := c.out.String()
	if strings.Contains(out, "FAIL") {
		t.Fatalf("comp failures:\n%s", out)
	}
	if !strings.Contains(out, "ALL PASSED") {
		t.Fatalf("comp did not pass:\n%s", out)
	}
	if !strings.Contains(out, SystemRelease) {
		t.Fatalf("comp did not report %s:\n%s", SystemRelease, out)
	}
	if !strings.Contains(out, "CHAINED TO LINE 8000") {
		t.Fatalf("comp did not chain:\n%s", out)
	}
	c.out.Reset()
	if _, err := m.Renumber(10, 10); err != nil {
		t.Fatalf("renum: %v", err)
	}
	if strings.Contains(listing(m), "RESTORE 1845") {
		t.Fatalf("RESTORE 1845 survived RENUM:\n%s", listing(m))
	}
	if strings.Contains(listing(m), `CHAIN "COMP" LINE 8000`) {
		t.Fatalf("CHAIN \"COMP\" LINE 8000 survived RENUM:\n%s", listing(m))
	}
	if err := os.WriteFile(filepath.Join(dir, "COMP.BAS"), []byte(m.SourceText()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatalf("run after renum: %v\n%s", err, c.out.String())
	}
	out = c.out.String()
	if strings.Contains(out, "FAIL") {
		t.Fatalf("comp after renum failures:\n%s", out)
	}
	if !strings.Contains(out, "ALL PASSED") {
		t.Fatalf("comp after renum did not pass:\n%s", out)
	}

	// The chained pass ends by KILLing everything it made.
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		if e.Name() != "COMP.BAS" {
			t.Errorf("scratch file left behind: %s", e.Name())
		}
	}
}

// Run from an account that has no COMP.BAS to chain to, the exerciser
// should say so and still report its totals rather than failing.
func TestCompWithoutChainTarget(t *testing.T) {
	dir := t.TempDir()
	c := &capture{}
	m := NewMachine(IO{
		Write: c.write,
		Read:  c.read,
		Open: func(m *Machine, ch int, path, mode string) error {
			real := filepath.Join(dir, path)
			var f *os.File
			var err error
			switch mode {
			case "INPUT":
				f, err = os.Open(real)
			case "OUTPUT":
				f, err = os.Create(real)
			case "APPEND":
				f, err = os.OpenFile(real, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			default:
				f, err = os.OpenFile(real, os.O_RDWR|os.O_CREATE, 0o644)
			}
			if err != nil {
				return err
			}
			cf := &chanFile{file: f, mode: mode}
			if mode == "INPUT" {
				cf.r = bufio.NewReader(f)
			}
			m.Files[ch] = cf
			return nil
		},
		Delete: func(path string) error { return os.Remove(filepath.Join(dir, path)) },
		Rename: func(old, new string) error {
			return os.Rename(filepath.Join(dir, old), filepath.Join(dir, new))
		},
	})
	if err := m.LoadSource(samples["1,9"]["COMP.BAS"], "COMP"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatalf("run: %v\n%s", err, c.out.String())
	}
	out := c.out.String()
	if !strings.Contains(out, "CHAIN SKIPPED") {
		t.Fatalf("expected the chain to be skipped:\n%s", out)
	}
	if !strings.Contains(out, "ALL PASSED") {
		t.Fatalf("comp did not pass:\n%s", out)
	}
}

func TestCompRenumThroughShell(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("LIBRARY", "LIBRARY")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("OLD COMP")
	out.Reset()
	sh.Dispatch("RENUM")
	if strings.Contains(out.String(), "Undefined") {
		t.Fatalf("renum: %s", out.String())
	}
	if strings.Contains(listing(sh.Basic), "RESTORE 1845") {
		t.Fatalf("RESTORE 1845 survived RENUM:\n%s", listing(sh.Basic))
	}
	if strings.Contains(listing(sh.Basic), `CHAIN "COMP" LINE 8000`) {
		t.Fatalf("CHAIN \"COMP\" LINE 8000 survived RENUM:\n%s", listing(sh.Basic))
	}
	out.Reset()
	sh.Dispatch("REPLACE")
	out.Reset()
	sh.Dispatch("RUN")
	got := out.String()
	if strings.Contains(got, "FAIL") {
		t.Fatalf("comp after shell RENUM:\n%s", got)
	}
	if !strings.Contains(got, "ALL PASSED") {
		t.Fatalf("comp after shell RENUM did not pass:\n%s", got)
	}
}

func TestBasicPlusV7Operators(t *testing.T) {
	out := runProgram(t, "10 PRINT 6% XOR 3%\n20 PRINT 1% EQV 1%\n30 PRINT 2**3\n40 PRINT ASCII(\"A\")\n50 A,B=9\n60 PRINT A;B\n70 PRINT 'HI'\n80 END\n")
	if !strings.Contains(out, "5") {
		t.Fatalf("xor: %q", out)
	}
	if !strings.Contains(out, "-1") {
		t.Fatalf("eqv: %q", out)
	}
	if !strings.Contains(out, "8") {
		t.Fatalf("pow: %q", out)
	}
	if !strings.Contains(out, "65") {
		t.Fatalf("ascii: %q", out)
	}
	if !strings.Contains(out, "9") {
		t.Fatalf("let multi: %q", out)
	}
	if !strings.Contains(out, "HI") {
		t.Fatalf("quotes: %q", out)
	}
}

func TestForUntilNoTo(t *testing.T) {
	out := runProgram(t, "10 S=0\n20 FOR I=1 UNTIL I>3\n30 S=S+I\n40 NEXT I\n50 PRINT S;I\n60 END\n")
	if !strings.Contains(out, "6") {
		t.Fatalf("sum: %q", out)
	}
	out = runProgram(t, "10 K=0\n20 K=K+1 FOR I=1 WHILE I<=4\n30 PRINT K\n40 END\n")
	if !strings.Contains(out, "4") {
		t.Fatalf("mod while: %q", out)
	}
}

func TestTimeTenthsAndKCT(t *testing.T) {
	out := runProgram(t, "10 FOR I=1 TO 200\n20 X=X+SQR(I)\n30 NEXT I\n40 T=TIME(1)\n50 K=TIME(3)\n60 PRINT T;K\n70 END\n")
	if strings.TrimSpace(out) == "" {
		t.Fatalf("empty: %q", out)
	}
}
