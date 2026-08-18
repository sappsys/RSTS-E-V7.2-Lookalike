package rsts

import (
	"bufio"
	"errors"
	"net"
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

func TestExtendNoExtend(t *testing.T) {
	out := runProgram(t, "10 EXTEND\n20 NOEXTEND\n30 PRINT \"OK\"\n40 END\n")
	if strings.TrimSpace(out) != "OK" {
		t.Fatalf("extend: %q", out)
	}
	if err := NewMachine(IO{}).LoadSource("10 COUNT=1\n20 END\n", "T"); err == nil {
		t.Fatal("NOEXTEND should reject COUNT")
	}
	out = runProgram(t, "10 EXTEND\n20 COUNT=3\n30 PRINT COUNT\n40 END\n")
	if strings.TrimSpace(out) != "3" {
		t.Fatalf("extend names: %q", out)
	}
	m := NewMachine(IO{})
	if err := m.ExecImmediate("COUNT=1"); err == nil {
		t.Fatal("immediate NOEXTEND should reject COUNT")
	}
	if err := m.ExecImmediate("EXTEND"); err != nil {
		t.Fatal(err)
	}
	if err := m.ExecImmediate("COUNT=4"); err != nil {
		t.Fatalf("immediate EXTEND: %v", err)
	}
	if err := m.LoadSource("10 NOEXTEND\n20 A=1\n30 END\n", "T"); err != nil {
		t.Fatal(err)
	}
	if err := NewMachine(IO{}).LoadSource("10 NOEXTEND \\ COUNT=1\n20 END\n", "T"); err == nil {
		t.Fatal("mid-line NOEXTEND should reject COUNT")
	}
	if err := NewMachine(IO{}).LoadSource("10 NOEXTEND \\ EXTEND \\ COUNT=1\n20 END\n", "T"); err != nil {
		t.Fatalf("NOEXTEND \\ EXTEND: %v", err)
	}
	if err := NewMachine(IO{}).LoadSource("10 DEF FNSQ(X)=X*X\n20 END\n", "T"); err == nil {
		t.Fatal("NOEXTEND should reject DEF FNSQ")
	}
	if err := NewMachine(IO{}).LoadSource("10 DEF FNA(X)=X*X\n20 END\n", "T"); err != nil {
		t.Fatalf("DEF FNA: %v", err)
	}
	if err := NewMachine(IO{}).LoadSource("10 DIM A(2,2)\n20 MAT A = CON\n30 END\n", "T"); err != nil {
		t.Fatalf("MAT CON under NOEXTEND: %v", err)
	}
}

func TestMidAssign(t *testing.T) {
	out := runProgram(t, "10 A$=\"HELLO\"\n20 MID$(A$,2,3)=\"XYZ\"\n30 PRINT A$\n40 END\n")
	if strings.TrimSpace(out) != "HXYZO" {
		t.Fatalf("mid$: %q", out)
	}
}

func TestXlate(t *testing.T) {
	out := runProgram(t, "10 PRINT XLATE$(\"AAA\", STRING$(256,\"B\"))\n20 END\n")
	if strings.TrimSpace(out) != "BBB" {
		t.Fatalf("xlate: %q", out)
	}
}

func TestCommonChain(t *testing.T) {
	c := &capture{}
	progs := map[string]string{
		"A": "10 COMMON X\n20 X=9\n30 CHAIN \"B\"\n",
		"B": "10 COMMON X\n20 PRINT X\n30 END\n",
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
	if strings.TrimSpace(c.out.String()) != "9" {
		t.Fatalf("common chain: %q", c.out.String())
	}
}

func TestIfEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "T.DAT")
	if err := os.WriteFile(path, []byte("HI\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read, Open: func(m *Machine, ch int, name, mode string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		m.Files[ch] = &chanFile{file: f, path: path, mode: "INPUT", r: bufio.NewReader(f)}
		return nil
	}})
	if err := m.LoadSource("10 OPEN \"T.DAT\" FOR INPUT AS FILE 1\n20 IF END #1 THEN 60\n30 LINE INPUT #1, A$\n40 IF END #1 THEN 70\n50 PRINT \"NO\"\n60 PRINT \"EARLY\"\n65 END\n70 PRINT A$\n80 END\n", "E"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(c.out.String())
	if got != "HI" {
		t.Fatalf("if end: %q", got)
	}
}

func TestWaitTimeout(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: func(string) (string, error) {
		time.Sleep(2 * time.Second)
		return "X", nil
	}})
	if err := m.LoadSource("10 ON ERROR GOTO 40\n20 WAIT 0.2\n30 INPUT A$\n35 PRINT \"NO\"\n40 PRINT ERR\n50 END\n", "W"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.out.String(), "15") {
		t.Fatalf("wait: %q", c.out.String())
	}
}

func TestCtrlCTrap(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	src := "10 ON ERROR GOTO 50\n20 A$=SYS(CHR$(6%)+CHR$(-7%)+CHR$(1%))\n30 GOTO 30\n50 PRINT ERR\n60 END\n"
	if err := m.LoadSource(src, "TRAP"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.RunProgram() }()
	time.Sleep(40 * time.Millisecond)
	m.Interrupt()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	if !strings.Contains(c.out.String(), "28") {
		t.Fatalf("trap: %q", c.out.String())
	}
}

func TestRecordInterlock(t *testing.T) {
	dir := t.TempDir()
	disk, err := OpenDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "REC.DAT")
	if err := os.WriteFile(path, make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	open := func(job int) *Machine {
		return NewMachine(IO{
			Disk: disk,
			Job:  job,
			Open: func(m *Machine, ch int, name, mode string) error {
				f, err := os.OpenFile(path, os.O_RDWR, 0o644)
				if err != nil {
					return err
				}
				m.Files[ch] = &chanFile{file: f, path: path, mode: mode, recSize: 512, buf: make([]byte, 512)}
				return nil
			},
		})
	}
	m1 := open(1)
	m2 := open(2)
	if err := m1.LoadSource("10 OPEN \"REC.DAT\" AS FILE 1, RECORDSIZE 512\n20 GET #1, RECORD 1\n30 STOP\n40 UNLOCK #1\n50 END\n", "L"); err != nil {
		t.Fatal(err)
	}
	if err := m1.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !m1.Stopped {
		t.Fatal("expected STOP holding the lock")
	}
	c := &capture{}
	m2.IO.Write = c.write
	if err := m2.LoadSource("10 ON ERROR GOTO 40\n20 OPEN \"REC.DAT\" AS FILE 1, RECORDSIZE 512\n30 GET #1, RECORD 1\n35 PRINT \"NO\"\n40 PRINT ERR\n50 END\n", "L2"); err != nil {
		t.Fatal(err)
	}
	if err := m2.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.out.String(), "19") {
		t.Fatalf("interlock: %q", c.out.String())
	}
	if err := m1.Continue(); err != nil {
		t.Fatal(err)
	}
}

func TestPKCtrlReadInterrupted(t *testing.T) {
	link := newPKLink()
	done := make(chan error, 1)
	go func() {
		_, err := link.ctrlReadLine(func() bool { return true })
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupt) {
			t.Fatalf("want interrupt, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PK read ignored Ctrl-C")
	}
}

func TestDirPipSwitches(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("SAVE HELLO")
	out.Reset()
	sh.Dispatch("DIR/W")
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("dir/w: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("PIP/LI HELLO.BAS")
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("pip/li: %q", out.String())
	}
}

func TestQueAndCCL(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("SYSTEM", "SYSTEM")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("SAVE WHO")
	out.Reset()
	sh.Dispatch("CCL WHO=WHO.BAS")
	out.Reset()
	sh.Dispatch("CCL")
	if !strings.Contains(out.String(), "WHO") {
		t.Fatalf("ccl list: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("QUE NOTICE.TXT")
	if !strings.Contains(out.String(), "queued") {
		t.Fatalf("que: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("QUE")
	if !strings.Contains(out.String(), "NOTICE") {
		t.Fatalf("que list: %q", out.String())
	}
	lp0 := filepath.Join(sh.Disk.Root, "LP0")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if jobs := sh.sys.listQueue(); len(jobs) == 0 {
			if data, err := os.ReadFile(lp0); err == nil && strings.Contains(string(data), "NOTICE") {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("spooler did not drain to LP0; queue=%v", sh.sys.listQueue())
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

func TestScale(t *testing.T) {
	out := strings.TrimSpace(runProgram(t, "10 SCALE 2\n20 X=1/3\n30 PRINT X\n40 SCALE 0\n50 END\n"))
	if !strings.Contains(out, ".33") {
		t.Fatalf("scale: %q", out)
	}
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	if err := m.LoadSource("10 SCALE 7\n20 END\n", "S"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err == nil {
		t.Fatal("SCALE 7 should fail")
	} else if !strings.Contains(err.Error(), "Illegal number") {
		t.Fatalf("scale range: %v", err)
	}
}

func TestTTYSETAndQuolst(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("SET SCOPE GAG TYPE VT52 SPEED 2400")
	out.Reset()
	sh.Dispatch("SET")
	got := out.String()
	if !strings.Contains(got, "SCOPE") || !strings.Contains(got, "GAG") || !strings.Contains(got, "VT52") || !strings.Contains(got, "2400") {
		t.Fatalf("ttyset: %q", got)
	}
	out.Reset()
	sh.Dispatch("DIR/F")
	if !strings.Contains(out.String(), "HELLO") && !strings.Contains(out.String(), "Name") {
		t.Fatalf("dir/f: %q", out.String())
	}
	acct := sh.Accounts.Find("GUEST")
	if acct == nil {
		t.Fatal("guest")
	}
	folder, err := sh.Disk.AccountDir(acct.Proj, acct.Prog)
	if err != nil {
		t.Fatal(err)
	}
	used := sh.Disk.folderBlocks(folder)
	acct.Quota = used
	acct.JobQuota = 1
	out.Reset()
	sh.Dispatch("QUOLST")
	if !strings.Contains(out.String(), "Disk quota") {
		t.Fatalf("quolst: %q", out.String())
	}
	spec, err := ParseFileSpec("HUGE.TXT", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.Disk.WriteText(spec, acct.Proj, acct.Prog, false, strings.Repeat("X", 1024), defaultProt); err == nil {
		t.Fatal("quota should refuse a new file")
	} else if !strings.Contains(err.Error(), "No room for user") {
		t.Fatalf("quota err: %v", err)
	}
}

func TestSubmitBatch(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	acct := sh.Account
	spec, err := ParseFileSpec("JOB.CMD", "")
	if err != nil {
		t.Fatal(err)
	}
	body := "NEW T\n10 OPEN \"BAT.OK\" FOR OUTPUT AS FILE 1\n20 PRINT #1, \"OK\"\n30 CLOSE 1\n40 END\nSAVE\nRUNNH\n"
	if err := sh.Disk.WriteText(spec, acct.Proj, acct.Prog, true, body, defaultProt); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("SUBMIT JOB.CMD")
	if !strings.Contains(out.String(), "submitted") {
		t.Fatalf("submit: %q", out.String())
	}
	folder, err := sh.Disk.AccountDir(acct.Proj, acct.Prog)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "BAT.OK")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), "OK") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("batch job did not write BAT.OK")
}

func TestHelloDetachAndShutup(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.term = &batchTerm{lines: []string{"GUEST"}}
	var out strings.Builder
	sh.out = &out
	sh.cmdHello("/DETACH GUEST")
	if sh.Account == nil {
		t.Fatalf("hello/detach login: %q", out.String())
	}
	if !sh.parked {
		t.Fatal("hello/detach did not detach")
	}
	sh2, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh2.Login("SYSTEM", "SYSTEM")
	sh2.out = &out
	if err := sh2.cmdShutup(""); err != nil {
		t.Fatal(err)
	}
	if !sh2.sys.Halted() {
		t.Fatal("shutup should halt")
	}
}

func TestCtrlODiscardsWrite(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	tn := &telnetConn{c: a, r: bufio.NewReader(a), echo: true, cols: 80, rows: 24}
	tn.toggleDiscard()
	if _, err := tn.Write([]byte("SECRET")); err != nil {
		t.Fatal(err)
	}
	_ = b.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 16)
	n, _ := b.Read(buf)
	if n > 0 && strings.Contains(string(buf[:n]), "SECRET") {
		t.Fatalf("ctrl-o leaked: %q", buf[:n])
	}
}

func TestRestoreAtLine(t *testing.T) {
	out := strings.TrimSpace(runProgram(t, ""+
		"10 DATA 1, 2\n"+
		"20 DATA 3, 4\n"+
		"30 READ A, B\n"+
		"40 RESTORE 20\n"+
		"50 READ C, D\n"+
		"60 PRINT A; B; C; D\n"+
		"70 END\n"))
	if !strings.Contains(out, "1") || !strings.Contains(out, "3") {
		t.Fatalf("restore n: %q", out)
	}
	if strings.Contains(out, " 2  2") {
		t.Fatalf("restore n reread first DATA: %q", out)
	}
}

func TestOpenModeUpdate(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("NEW T")
	sh.Dispatch("10 OPEN \"U.DAT\" FOR OUTPUT AS FILE 1")
	sh.Dispatch("20 PRINT #1, \"HELLO\"")
	sh.Dispatch("30 CLOSE 1")
	sh.Dispatch("40 OPEN \"U.DAT\" FOR INPUT AS FILE 1, MODE 1")
	sh.Dispatch("50 INPUT #1, A$")
	sh.Dispatch("60 PRINT #1, \"WORLD\"")
	sh.Dispatch("70 CLOSE 1")
	sh.Dispatch("80 END")
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	sh.Dispatch("TYPE U.DAT")
	if !strings.Contains(out.String(), "WORLD") {
		t.Fatalf("mode update: %q", out.String())
	}
}

func TestOpenModeExclusive(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("SYSTEM", "SYSTEM")
	j2, err := sh.sys.Attach("T2")
	if err != nil {
		t.Fatal(err)
	}
	var out2 strings.Builder
	sh2 := sh.sys.newSession(j2, &out2, nil)
	sh2.Login("SYSTEM", "SYSTEM")
	sh.Dispatch("NEW L")
	sh.Dispatch("10 OPEN \"EX.DAT\" FOR OUTPUT AS FILE 1, MODE 16")
	sh.Dispatch("20 STOP")
	sh.Dispatch("30 CLOSE 1")
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !sh.Basic.Stopped {
		t.Fatal("expected STOP holding exclusive")
	}
	sh2.Dispatch("NEW L2")
	sh2.Dispatch("10 ON ERROR GOTO 40")
	sh2.Dispatch("20 OPEN \"EX.DAT\" FOR OUTPUT AS FILE 1, MODE 16")
	sh2.Dispatch("30 PRINT \"NO\"")
	sh2.Dispatch("40 PRINT ERR")
	sh2.Dispatch("50 END")
	if err := sh2.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2.String(), "3") {
		t.Fatalf("exclusive: %q", out2.String())
	}
	if err := sh.Basic.Continue(); err != nil {
		t.Fatal(err)
	}
}

func TestSequentialInputInterlock(t *testing.T) {
	dir := t.TempDir()
	disk, err := OpenDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SEQ.DAT")
	if err := os.WriteFile(path, []byte("AAAA\nBBBB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	open := func(job int) *Machine {
		return NewMachine(IO{
			Disk: disk,
			Job:  job,
			Open: func(m *Machine, ch int, name, mode string) error {
				f, err := os.OpenFile(path, os.O_RDWR, 0o644)
				if err != nil {
					return err
				}
				m.Files[ch] = &chanFile{file: f, path: path, mode: "INPUT", r: bufio.NewReader(f)}
				return nil
			},
		})
	}
	m1 := open(1)
	m2 := open(2)
	if err := m1.LoadSource("10 OPEN \"SEQ.DAT\" FOR INPUT AS FILE 1\n20 INPUT #1, A$\n30 STOP\n40 END\n", "L"); err != nil {
		t.Fatal(err)
	}
	if err := m1.RunProgram(); err != nil {
		t.Fatal(err)
	}
	c := &capture{}
	m2.IO.Write = c.write
	if err := m2.LoadSource("10 ON ERROR GOTO 40\n20 OPEN \"SEQ.DAT\" FOR INPUT AS FILE 1\n30 INPUT #1, A$\n35 PRINT \"NO\"\n40 PRINT ERR\n50 END\n", "L2"); err != nil {
		t.Fatal(err)
	}
	if err := m2.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.out.String(), "19") {
		t.Fatalf("seq interlock: %q", c.out.String())
	}
}

func TestBackupMagtapeAndSpec(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("SAVE HELLO")
	out.Reset()
	sh.Dispatch("BACKUP HELLO.BAS MT0:")
	if !strings.Contains(out.String(), "files copied") {
		t.Fatalf("backup: %q", out.String())
	}
	sh.Dispatch("KILL HELLO.BAS")
	out.Reset()
	sh.Dispatch("BACKUP/RE MT0:")
	if !strings.Contains(out.String(), "restored") {
		t.Fatalf("restore: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("DIR HELLO.BAS")
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("restored file missing: %q", out.String())
	}
	sh.Dispatch("NEW S")
	sh.Dispatch("10 OPEN \"MT0:\" AS FILE 1")
	sh.Dispatch("20 PRINT SPEC%(1%,0%)")
	sh.Dispatch("30 CLOSE 1")
	sh.Dispatch("40 END")
	out.Reset()
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Fatalf("spec%%: %q", out.String())
	}
}

func TestReactQuotaAndPrintGrowth(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("SYSTEM", "SYSTEM")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("REACT QUOTA GUEST 2")
	if !strings.Contains(out.String(), "Quota") {
		t.Fatalf("react quota: %q", out.String())
	}
	acct := sh.Accounts.Find("GUEST")
	if acct == nil || acct.Quota != 2 {
		t.Fatalf("quota stored: %+v", acct)
	}
	j2, err := sh.sys.Attach("G")
	if err != nil {
		t.Fatal(err)
	}
	var gout strings.Builder
	g := sh.sys.newSession(j2, &gout, nil)
	g.Login("GUEST", "GUEST")
	g.Dispatch("NEW Q")
	g.Dispatch("10 ON ERROR GOTO 70")
	g.Dispatch("20 OPEN \"BIG.DAT\" FOR OUTPUT AS FILE 1")
	g.Dispatch("30 FOR I=1 TO 400")
	g.Dispatch("40 PRINT #1, \"XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX\"")
	g.Dispatch("50 NEXT I")
	g.Dispatch("60 PRINT \"NO\"")
	g.Dispatch("70 PRINT ERR")
	g.Dispatch("80 CLOSE 1")
	g.Dispatch("90 END")
	if err := g.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gout.String(), "4") {
		t.Fatalf("print quota: %q", gout.String())
	}
}

func TestTTYSETNotab(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("SET NOTAB NOFORM FILL 2")
	out.Reset()
	sh.write("A\tB\fC", true)
	got := out.String()
	if strings.Contains(got, "\t") || strings.Contains(got, "\f") {
		t.Fatalf("tty filter: %q", got)
	}
	if !strings.Contains(got, "A       B") && !strings.Contains(got, "A B") {
		if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
			t.Fatalf("notab: %q", got)
		}
	}
	if !strings.Contains(got, "\x00") {
		t.Fatalf("fill: %q", got)
	}
}

func TestNoSupersedeAndDirPipPlease(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch("SAVE HELLO")
	out.Reset()
	sh.Dispatch("NEW T")
	sh.Dispatch("10 ON ERROR GOTO 50")
	sh.Dispatch("20 OPEN \"HELLO.BAS\" FOR OUTPUT AS FILE 1, MODE 128")
	sh.Dispatch("30 PRINT \"NO\"")
	sh.Dispatch("40 END")
	sh.Dispatch("50 PRINT ERR")
	sh.Dispatch("60 END")
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "16") {
		t.Fatalf("mode 128: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("DIR *.BAS/W")
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("dir trailing /w: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("DIR/SU")
	if !strings.Contains(out.String(), "Total of") {
		t.Fatalf("dir/su: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("PIP HELLO.BAS/LI")
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("pip file/li: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("PLEASE printer jammed")
	got := out.String()
	if !strings.Contains(got, "operator") && !strings.Contains(got, "PLEASE") {
		t.Fatalf("please: %q", got)
	}
	out.Reset()
	sh.Dispatch("PLEASE/LI")
	if !strings.Contains(out.String(), "printer jammed") {
		t.Fatalf("please/li: %q", out.String())
	}
}

func TestFIPPackAndLookup(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("SYSTEM", "SYSTEM")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch(`PRINT SYS(CHR$(6%)+CHR$(6%))`)
	if !strings.Contains(out.String(), "SYSDSK") {
		t.Fatalf("fip pack: %q", out.String())
	}
	out.Reset()
	sh.Dispatch(`PRINT LEN(SYS(CHR$(6%)+CHR$(99%)))`)
	if !strings.Contains(out.String(), "30") {
		t.Fatalf("fip zeros: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("SAVE HELLO")
	out.Reset()
	sh.Dispatch(`PRINT LEFT$(SYS(CHR$(6%)+CHR$(-17%)+"HELLO.BAS"),9%)`)
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("fip lookup: %q", out.String())
	}
	out.Reset()
	sh.Dispatch(`PRINT SYS(CHR$(6%)+CHR$(11%))`)
	if !strings.Contains(out.String(), "BASIC") {
		t.Fatalf("fip rts: %q", out.String())
	}
}
