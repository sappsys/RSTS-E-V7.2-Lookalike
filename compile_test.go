package rsts

import (
	"strings"
	"testing"
)

func TestParseFileSpecProt(t *testing.T) {
	spec, err := ParseFileSpec("PAYROL.BAC<232>", "BAS")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "PAYROL" || spec.Ext != "BAC" || !spec.ProtSet || spec.Prot != 232 || !spec.ExtGiven {
		t.Fatalf("got %+v", spec)
	}
	spec, err = ParseFileSpec("$WHOAMI<124>", "BAC")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Proj == nil || *spec.Proj != 1 || *spec.Prog != 2 {
		t.Fatalf("dollar: %+v", spec)
	}
	if spec.Name != "WHOAMI" || spec.Ext != "BAC" || spec.Prot != 124 {
		t.Fatalf("dollar name: %+v", spec)
	}
	if _, err := ParseFileSpec("FOO<999>", "BAS"); err == nil {
		t.Fatal("expected illegal protection")
	}
}

func TestCompileAndPrivilege(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}

	sh.Login("GUEST", "GUEST")
	if sh.Account == nil {
		t.Fatal("guest login")
	}

	sh.Dispatch("NEW FOO")
	sh.Dispatch("10 PRINT CVT$%(SYS(CHR$(8%)))")
	sh.Dispatch("20 END")
	if err := sh.cmdCompile("FOO<232>"); err == nil {
		t.Fatal("guest should not set privilege bit")
	}
	if err := sh.cmdCompile("FOO"); err != nil {
		t.Fatal(err)
	}
	prot, err := sh.Disk.Prot(mustSpec(t, "FOO.BAC"), 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if prot != compiledProt {
		t.Fatalf("compile prot: %d", prot)
	}
	if err := sh.cmdName("FOO.BAC AS FOO.BAC<232>"); err == nil {
		t.Fatal("guest NAME <232> should fail")
	}

	var out strings.Builder
	sh.out = &out
	sh.cmdRun("FOO.BAC", false)
	if strings.Contains(out.String(), "-1") {
		t.Fatalf("non-priv .BAC should not grant privilege: %q", out.String())
	}

	if err := sh.cmdOld("[1,9]WHOAMI.BAC"); err == nil {
		t.Fatal("guest OLD of privileged .BAC")
	}
	if err := sh.cmdType("[1,9]WHOAMI.BAC"); err == nil {
		t.Fatal("guest TYPE of privileged .BAC")
	}

	out.Reset()
	sh.cmdRun("[1,9]WHOAMI", false)
	got := out.String()
	if !strings.Contains(got, "[100,100]") {
		t.Fatalf("whoami ppn: %q", got)
	}
	if !strings.Contains(got, "-1") {
		t.Fatalf("expected temp privilege (-1): %q", got)
	}
	if sh.tempPriv || sh.Basic.IO.Privileged {
		t.Fatal("temp privilege should drop after RUN")
	}
	if len(sh.Basic.Program) != 0 {
		t.Fatal("privileged image should be destroyed")
	}

	sh.cmdBye("")
	sh.Login("SYSTEM", "SYSTEM")
	if err := sh.Disk.SetProt(mustSpec(t, "[100,100]HELLO.BAS"), 1, 2, true, protExecutable|protPrivileged|40); err != nil {
		t.Fatal(err)
	}
	sh.cmdBye("")
	sh.Login("GUEST", "GUEST")
	out.Reset()
	sh.cmdRun("HELLO.BAS", false)
	if strings.Contains(out.String(), "-1") {
		t.Fatalf(".BAS must not confer privilege: %q", out.String())
	}

	sh.cmdBye("")
	sh.Login("SYSTEM", "SYSTEM")
	sh.Dispatch("NEW PAYROL")
	sh.Dispatch("10 PRINT CVT$%(SYS(CHR$(8%)))")
	sh.Dispatch("20 END")
	if err := sh.cmdCompile("PAYROL<232>"); err != nil {
		t.Fatal(err)
	}
	prot, err = sh.Disk.Prot(mustSpec(t, "PAYROL.BAC"), 1, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if prot != privCompiledProt {
		t.Fatalf("priv compile prot: %d", prot)
	}
	raw, err := sh.Disk.ReadText(mustSpec(t, "PAYROL.BAC"), 1, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := unwrapBAC(raw)
	if _, err := unmarshalPcode(payload); err != nil {
		t.Fatalf("PAYROL.BAC should be bytecode: %v", err)
	}
	if strings.Contains(payload, `PRINT CVT$%`) {
		t.Fatal("compiled image still contains source")
	}

	sh.cmdBye("")
	sh.Login("GUEST", "GUEST")
	out.Reset()
	sh.cmdRun("$PAYROL", false)
	if !strings.Contains(out.String(), "-1") {
		t.Fatalf("RUN of SYSTEM-compiled <232>: %q", out.String())
	}
}

func TestPcodeCompileRun(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	sh.Dispatch("NEW FOO")
	sh.Dispatch(`10 PRINT "HI"`)
	sh.Dispatch("20 END")
	if err := sh.cmdCompile(""); err != nil {
		t.Fatal(err)
	}
	raw, err := sh.Disk.ReadText(mustSpec(t, "FOO.BAC"), 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := unwrapBAC(raw)
	if !ok {
		t.Fatal("missing BAC magic")
	}
	if _, err := unmarshalPcode(payload); err != nil {
		t.Fatalf("not bytecode: %v", err)
	}

	sh.Dispatch("NEW")
	var out strings.Builder
	sh.out = &out
	sh.cmdRun("FOO.BAC", false)
	if !strings.Contains(out.String(), "HI") {
		t.Fatalf("run bac: %q", out.String())
	}
	if !sh.Basic.Compiled {
		t.Fatal("RUN of .BAC should load a compiled image")
	}
	if len(sh.Basic.Program) != 0 {
		t.Fatal("compiled image should not keep source")
	}
}

func TestCompileLeavesSource(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("SYSTEM", "SYSTEM")
	sh.Dispatch("NEW DEMO")
	sh.Dispatch(`10 PRINT "HI"`)
	sh.Dispatch("20 END")
	if err := sh.cmdCompile(""); err != nil {
		t.Fatal(err)
	}
	listing := strings.Join(sh.Basic.Listing(0, 0, false, false), "\n")
	if !strings.Contains(listing, `PRINT "HI"`) {
		t.Fatalf("COMPILE should leave source in memory: %s", listing)
	}
	if sh.Basic.Compiled {
		t.Fatal("memory should not be marked compiled after COMPILE")
	}
}

func TestPublicNoticeReadable(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	if err := sh.cmdType("$NOTICE.TXT"); err != nil {
		t.Fatal(err)
	}
}
