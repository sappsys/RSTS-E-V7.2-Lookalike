package rsts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiLineDef(t *testing.T) {
	out := runProgram(t, `10 DEF FNA(X)
20   Y = X + X
30   FNA = Y + 1
40 FNEND
50 PRINT FNA(5)
60 END
`)
	if strings.TrimSpace(out) != "11" {
		t.Fatalf("got %q, want 11", out)
	}
}

func TestMultiLineDefRecurses(t *testing.T) {
	out := runProgram(t, `10 DEF FNF(N)
20   FNF = 1
30   IF N > 1 THEN FNF = N * FNF(N-1)
40 FNEND
50 PRINT FNF(6)
60 END
`)
	if strings.TrimSpace(out) != "720" {
		t.Fatalf("6 factorial came out %q", out)
	}
}

func TestMultiLineDefString(t *testing.T) {
	out := runProgram(t, `10 DEF FNR$(S$)
20   T$ = ""
30   FOR I = LEN(S$) TO 1 STEP -1
40     T$ = T$ + MID$(S$,I,1)
50   NEXT I
60   FNR$ = T$
70 FNEND
80 PRINT FNR$("RSTS/E")
90 END
`)
	if strings.TrimSpace(out) != "E/STSR" {
		t.Fatalf("got %q", out)
	}
}

func TestFnExitReturnsEarly(t *testing.T) {
	out := runProgram(t, `10 DEF FNS(N)
20   FNS = -1
30   IF N < 0 THEN FNEXIT
40   FNS = SQR(N)
50 FNEND
60 PRINT FNS(16)
70 PRINT FNS(-1)
80 END
`)
	got := strings.Fields(out)
	if len(got) != 2 || got[0] != "4" || got[1] != "-1" {
		t.Fatalf("got %q, want 4 and -1", out)
	}
}

// The body is jumped around, so running past the definition must not
// execute it, and the caller's variables must survive the call.
func TestMultiLineDefDoesNotRunInline(t *testing.T) {
	out := runProgram(t, `10 Y = 99
20 DEF FNA(X)
30   Y = 1
40   FNA = X
50 FNEND
60 PRINT Y;
70 PRINT FNA(3);
80 PRINT Y
90 END
`)
	got := strings.Fields(out)
	if len(got) != 3 || got[0] != "99" || got[1] != "3" {
		t.Fatalf("got %q", out)
	}
}

func TestMultiLineDefErrors(t *testing.T) {
	for _, src := range []string{
		"10 FNEND\n20 END\n",
		"10 DEF FNA(X)\n20 FNA = X\n30 END\n",
		"10 DEF FNA(X)\n20 DEF FNB(Y)\n30 FNEND\n40 FNEND\n",
		"10 FNEXIT\n20 END\n",
	} {
		m := NewMachine(IO{})
		if err := m.LoadSource(src, "BAD"); err != nil {
			continue // a parse error is a fine way to reject it too
		}
		if err := m.RunProgram(); err == nil {
			t.Errorf("expected a complaint about:\n%s", src)
		}
	}
}

func TestRecountAfterInput(t *testing.T) {
	out := runProgram(t, "10 INPUT A$\n20 PRINT RECOUNT\n30 END\n", "HELLO")
	if !strings.Contains(out, "5") {
		t.Fatalf("RECOUNT after a five character reply: %q", out)
	}
}

func TestDetAfterMatInv(t *testing.T) {
	out := runProgram(t, `10 DIM A(2,2), B(2,2)
20 A(1,1)=4 \ A(1,2)=7 \ A(2,1)=2 \ A(2,2)=6
30 MAT B = INV(A)
40 PRINT DET
50 END
`)
	// 4*6 - 7*2 = 10
	if !strings.Contains(out, "10") {
		t.Fatalf("determinant: %q", out)
	}
}

func TestNumAfterMatInput(t *testing.T) {
	out := runProgram(t, `10 DIM A(2,3)
20 MAT INPUT A
30 PRINT NUM; NUM2
40 END
`, "1,2,3,4,5,6")
	got := strings.Fields(out)
	if len(got) < 2 || got[len(got)-2] != "2" || got[len(got)-1] != "3" {
		t.Fatalf("NUM and NUM2: %q", out)
	}
}

func TestStatusAfterOpen(t *testing.T) {
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
			m.Files[ch] = &chanFile{file: f, mode: mode, class: devDisk}
			return nil
		},
	})
	if err := m.LoadSource("10 OPEN \"S.DAT\" AS FILE 2\n20 PRINT STATUS\n30 CLOSE 2\n40 END\n", "ST"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	// Device class in the low byte, channel in the high byte.
	want := devDisk | 2<<8
	if !strings.Contains(out(c), itoa(want)) {
		t.Fatalf("STATUS = %q, want %d", c.out.String(), want)
	}
}

func out(c *capture) string { return c.out.String() }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
