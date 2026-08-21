package rsts

import (
	"strings"
	"testing"
)

func TestLoggedOutSystat(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if sh.Account != nil {
		t.Fatal("should be at Bye")
	}
	var out strings.Builder
	sh.out = &out
	sh.forceCh <- "SYSTAT"
	if err := sh.loggedOut(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Job") {
		t.Fatalf("logged-out SYSTAT: %q", out.String())
	}
	out.Reset()
	sh.forceCh <- "DIR"
	if err := sh.loggedOut(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Please say HELLO") {
		t.Fatalf("DIR at Bye: %q", out.String())
	}
}

func TestSwitchRSX(t *testing.T) {
	sh, out := guestShell(t)
	if err := sh.cmdSwitch("RSX"); err != nil {
		t.Fatal(err)
	}
	if sh.jobRTS() != "RSX" {
		t.Fatal("SWITCH RSX")
	}
	out.Reset()
	sh.cmdSystat("/R")
	text := out.String()
	if !strings.Contains(text, "BASIC") || !strings.Contains(text, "RSX") {
		t.Fatalf("SYSTAT/R: %q", text)
	}
	if strings.Contains(text, "RT11") {
		t.Fatalf("RT11 should not be listed: %q", text)
	}
	out.Reset()
	sh.cmdRun("FOO.TSK", false)
	if !strings.Contains(out.String(), "Can't find file") && !strings.Contains(out.String(), "Not a task") {
		t.Fatalf("RSX RUN TSK: %q", out.String())
	}
	out.Reset()
	sh.cmdHelp("")
	text = out.String()
	if !strings.Contains(text, "SWITCH") || !strings.Contains(text, "RUN") {
		t.Fatalf("RSX HELP: %q", text)
	}
	if strings.Contains(text, "DIR") || strings.Contains(text, "PIP") || strings.Contains(text, "NEW") {
		t.Fatalf("RSX HELP listed BASIC commands: %q", text)
	}
	out.Reset()
	sh.cmdHelp("DIR")
	if !strings.Contains(out.String(), "No help") {
		t.Fatalf("RSX HELP DIR: %q", out.String())
	}
	if err := sh.cmdSwitch("BASIC"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	sh.Dispatch("DIR")
	if strings.Contains(out.String(), "Wrong RTS") {
		t.Fatalf("DIR after SWITCH BASIC: %q", out.String())
	}
}

func TestInitStart(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	sh.out = &out
	sh.forceCh <- "START"
	if !sh.runINIT() {
		t.Fatal("START should begin timesharing")
	}
	st := sh.sys.loadInitState()
	if !st.Started {
		t.Fatal("START should record timesharing")
	}
	if !strings.Contains(out.String(), "Starting timesharing") {
		t.Fatalf("init: %q", out.String())
	}
}

func TestLibraryCUSP(t *testing.T) {
	sh, out := guestShell(t)
	spec, err := ParseFileSpec("$PIP.BAC", "BAC")
	if err != nil {
		t.Fatal(err)
	}
	if !sh.Disk.Exists(spec, 100, 100, false) {
		t.Fatal("PIP.BAC should be seeded in [1,2]")
	}
	prot, err := sh.Disk.Prot(spec, 1, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if prot != publicCompiledProt {
		t.Fatalf("PIP.BAC prot %d want %d", prot, publicCompiledProt)
	}
	out.Reset()
	sh.Dispatch("DIR")
	if strings.Contains(out.String(), "Please say HELLO") {
		t.Fatalf("DIR via CUSP: %q", out.String())
	}
	if strings.Contains(out.String(), "Protection violation") {
		t.Fatalf("guest DIR: %q", out.String())
	}
	bas, err := ParseFileSpec("$SYSTAT.BAS", "BAS")
	if err != nil {
		t.Fatal(err)
	}
	src, err := sh.Disk.ReadText(bas, 1, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(src, "CHR$(-19%)") {
		t.Fatal("SYSTAT.BAS is still a SYS trampoline")
	}
	if !strings.Contains(src, "CHR$(-22%)") || !strings.Contains(src, "PRINT") {
		t.Fatalf("SYSTAT.BAS should list jobs in BASIC: %q", src[:200])
	}
}

func TestCUSPFIP(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("SAVE HELLO")
	raw := "*.*" + "\x00" + string([]byte{0, 0})
	got, err := sh.fipDirScan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("fipDirScan empty")
	}
	sh.cclArg = "/D"
	sh.Basic.IO.CCLLine = "/D"
	out.Reset()
	src := `10 EXTEND
20 PRINT "C18[";SYS(CHR$(6%)+CHR$(-18%));"]"
30 R$=SYS(CHR$(6%)+CHR$(-20%)+"*.*"+CHR$(0%)+CVT%$(0%))
40 PRINT "D20[";LEN(R$);"]";LEFT$(R$,10)
50 END
`
	if err := sh.Basic.LoadSource(src, "T"); err != nil {
		t.Fatal(err)
	}
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "C18[/D]") {
		t.Fatalf("SYS -18 from BASIC: %q", text)
	}
	if !strings.Contains(text, "D20[") || strings.Contains(text, "D20[ 0]") {
		t.Fatalf("SYS -20 from BASIC: %q", text)
	}
	img, err := compileSourceText(src)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	sh.cclArg = "/D"
	if err := sh.Basic.LoadCompiled(wrapPcode(img), "T", false); err != nil {
		t.Fatal(err)
	}
	sh.cclArg = "/D"
	sh.Basic.IO.CCLLine = "/D"
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	text = out.String()
	if !strings.Contains(text, "C18[/D]") {
		t.Fatalf("SYS -18 from compiled: %q", text)
	}
	out.Reset()
	if err := sh.Basic.LoadSource("10 IF 0 THEN PRINT \"NO\" \\ PRINT \"SKIP\"\n20 IF -1 THEN PRINT \"YES\" \\ PRINT \"BOTH\"\n30 END\n", "I"); err != nil {
		t.Fatal(err)
	}
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	gotIF := out.String()
	if strings.Contains(gotIF, "NO") || strings.Contains(gotIF, "SKIP") {
		t.Fatalf("IF THEN \\ ran the false branch: %q", gotIF)
	}
	if !strings.Contains(gotIF, "YES") || !strings.Contains(gotIF, "BOTH") {
		t.Fatalf("IF THEN \\ missed the true branch: %q", gotIF)
	}
	out.Reset()
	sh.Dispatch("SYSTAT/D")
	if !strings.Contains(out.String(), "SYSDSK") && !strings.Contains(out.String(), "Disk") {
		t.Fatalf("SYSTAT/D via CUSP: %q", out.String())
	}
}
