package rsts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSleepZero(t *testing.T) {
	start := time.Now()
	out := runProgram(t, "10 SLEEP 0\n20 PRINT \"OK\"\n30 END\n")
	if strings.TrimSpace(out) != "OK" {
		t.Fatalf("sleep: %q", out)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("SLEEP 0 took too long")
	}
}

func TestChainLine(t *testing.T) {
	c := &capture{}
	progs := map[string]string{
		"A": "10 PRINT \"A\"\n20 CHAIN \"B\" LINE 20\n30 PRINT \"NOA\"\n",
		"B": "10 PRINT \"NOB\"\n20 PRINT \"B\"\n30 END\n",
	}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	m.IO.Load = func(name string) error {
		src, ok := progs[strings.ToUpper(strings.TrimSuffix(name, ".BAS"))]
		if !ok {
			return basicErr("Can't find file or account")
		}
		return m.LoadSource(src, name)
	}
	if err := m.LoadSource(progs["A"], "A"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(c.out.String())
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Fatalf("chain: %q", got)
	}
	if strings.Contains(got, "NOA") || strings.Contains(got, "NOB") {
		t.Fatalf("chain jumped wrong: %q", got)
	}
}

func TestContAfterStop(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	src := "10 A=7\n20 STOP\n30 PRINT A\n40 END\n"
	if err := m.LoadSource(src, "T"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !m.Stopped {
		t.Fatal("expected STOP")
	}
	if strings.TrimSpace(c.out.String()) != "" {
		t.Fatalf("printed before CONT: %q", c.out.String())
	}
	if err := m.Continue(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(c.out.String()) != "7" {
		t.Fatalf("cont: %q", c.out.String())
	}
	if m.Stopped {
		t.Fatal("still stopped")
	}
}

func TestContFailsAfterEdit(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	if err := m.LoadSource("10 A=1\n20 STOP\n30 PRINT A\n40 END\n", "T"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if err := m.StoreLine(30, `PRINT "X"`); err != nil {
		t.Fatal(err)
	}
	err := m.Continue()
	if err == nil || !strings.Contains(err.Error(), "Can't continue") {
		t.Fatalf("want can't continue, got %v", err)
	}
}

func TestMatRedim(t *testing.T) {
	out := strings.TrimSpace(runProgram(t, `10 DIM A(2)
20 MAT A = ZER(2,2)
30 A(2,2)=5
40 PRINT A(2,2)
50 MAT A = IDN(2)
60 PRINT A(1,1); A(1,2)
70 END
`))
	if !strings.Contains(out, "5") {
		t.Fatalf("zer redim: %q", out)
	}
	if !strings.Contains(out, "1") {
		t.Fatalf("idn redim: %q", out)
	}
}

func TestVirtualArray(t *testing.T) {
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
	src := `10 OPEN "VIRT.DAT" AS FILE 1
20 DIM #1, A%(10), B$(4)=8
30 A%(5%)=42
40 B$(1%)="HELLO"
50 PRINT A%(5%); B$(1%)
60 CLOSE #1
70 OPEN "VIRT.DAT" AS FILE 1
80 DIM #1, A%(10), B$(4)=8
90 PRINT A%(5%); B$(1%)
100 CLOSE #1
110 END
`
	if err := m.LoadSource(src, "VIRT"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	got := c.out.String()
	if strings.Count(got, "42") < 2 {
		t.Fatalf("virtual int: %q", got)
	}
	if strings.Count(got, "HELLO") < 2 {
		t.Fatalf("virtual str: %q", got)
	}
}

func TestNameKillStatements(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "OLD.BAS")
	newPath := filepath.Join(dir, "NEW.BAS")
	if err := os.WriteFile(oldPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &capture{}
	m := NewMachine(IO{
		Write: c.write,
		Read:  c.read,
		Rename: func(old, new string) error {
			return os.Rename(filepath.Join(dir, old), filepath.Join(dir, new))
		},
		Delete: func(path string) error {
			return os.Remove(filepath.Join(dir, path))
		},
	})
	src := `10 NAME "OLD.BAS" AS "NEW.BAS"
20 KILL "NEW.BAS"
30 PRINT "GONE"
40 END
`
	if err := m.LoadSource(src, "NK"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old still there: %v", err)
	}
	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new still there: %v", err)
	}
	if !strings.Contains(c.out.String(), "GONE") {
		t.Fatalf("name/kill: %q", c.out.String())
	}
}

func TestAssignAndSet(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	if err := sh.cmdAssign("DB1: WORK"); err != nil {
		t.Fatal(err)
	}
	spec, err := sh.parseSpec("WORK:FOO.BAS", "BAS")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Device != "DB" || spec.Unit != 1 || spec.Name != "FOO" {
		t.Fatalf("logical: %+v", spec)
	}
	out.Reset()
	if err := sh.cmdSet("WIDTH 72"); err != nil {
		t.Fatal(err)
	}
	if sh.width != 72 || sh.Basic.IO.Width != 72 {
		t.Fatalf("width %d io %d", sh.width, sh.Basic.IO.Width)
	}
	if err := sh.cmdSet("NOECHO"); err != nil {
		t.Fatal(err)
	}
	if sh.echo || sh.Basic.IO.Echo {
		t.Fatal("expected noecho")
	}
	if err := sh.cmdDeassign("WORK"); err != nil {
		t.Fatal(err)
	}
	if _, ok := sh.logicals["WORK"]; ok {
		t.Fatal("deassign should drop WORK")
	}
}

func TestSysFIPExtra(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read, Job: 3, KB: "KB2:", Width: 72, Echo: true})
	if err := m.LoadSource(`10 PRINT CVT$%(SYS(CHR$(6%)+CHR$(2%)))
20 PRINT SYS(CHR$(6%)+CHR$(3%))
30 A$=SYS(CHR$(6%)+CHR$(-10%))
40 PRINT CVT$%(A$)
50 END
`, "FIP"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	got := c.out.String()
	if !strings.Contains(got, "3") {
		t.Fatalf("job: %q", got)
	}
	if !strings.Contains(got, "KB2:") {
		t.Fatalf("kb: %q", got)
	}
	if !strings.Contains(got, "72") {
		t.Fatalf("width: %q", got)
	}
}

func TestLoginStartupFile(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseFileSpec("[100,100]LOGIN.BAS", "BAS")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.Disk.WriteText(spec, 100, 100, true, "10 PRINT \"STARTUP-OK\"\n20 END\n", defaultProt); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	sh.out = &out
	sh.Login("GUEST", "GUEST")
	got := out.String()
	if !strings.Contains(got, "STARTUP-OK") {
		t.Fatalf("login file: %q", got)
	}
	if !strings.Contains(got, "Welcome") {
		t.Fatalf("notice: %q", got)
	}
}

func TestContKeyboard(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("NEW T")
	sh.Dispatch("10 A=9")
	sh.Dispatch("20 STOP")
	sh.Dispatch("30 PRINT A")
	sh.Dispatch("40 END")
	out.Reset()
	sh.Dispatch("RUNNH")
	if !strings.Contains(out.String(), "Stop at line 20") {
		t.Fatalf("stop: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("CONT")
	if !strings.Contains(out.String(), "9") {
		t.Fatalf("cont: %q", out.String())
	}
}
