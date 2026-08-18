package rsts

import (
	"strings"
	"testing"
)

func guestShell(t *testing.T) (*Shell, *strings.Builder) {
	t.Helper()
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	return sh, &out
}

func TestParseCmdSwitches(t *testing.T) {
	sw, arg := parseCmdSwitches("*.BAS/W")
	if arg != "*.BAS" || !switchMin(sw, "WIDE", 1) {
		t.Fatalf("trailing /W: arg=%q sw=%v", arg, sw)
	}
	sw, arg = parseCmdSwitches("DST=SRC/PROT:40")
	if arg != "DST=SRC" || switchValue(sw, "PROT") != "40" {
		t.Fatalf("pip prot: arg=%q sw=%v", arg, sw)
	}
	sw, arg = parseCmdSwitches("FILE.BAS/DE")
	if arg != "FILE.BAS" || !switchOn(sw, "DE", "DELETE") {
		t.Fatalf("pip /de: arg=%q sw=%v", arg, sw)
	}
	if switchMin(map[string]string{"S": ""}, "SUMMARY", 2) {
		t.Fatal("/S must not be SUMMARY")
	}
	if !switchMin(map[string]string{"S": ""}, "SIZE", 1) {
		t.Fatal("/S should be SIZE")
	}
	if switchMin(map[string]string{"NE": ""}, "NOSUPERSEDE", 2) {
		t.Fatal("/NE is not a prefix of NOSUPERSEDE")
	}
	if !switchOn(map[string]string{"NE": ""}, "NE", "NOSUPERSEDE") {
		t.Fatal("/NE should select no-supersede")
	}
}

func TestPipCopySwitches(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("NEW A")
	sh.Dispatch(`10 PRINT "AAA"`)
	sh.Dispatch("SAVE A")
	sh.Dispatch("NEW B")
	sh.Dispatch(`10 PRINT "BBB"`)
	sh.Dispatch("SAVE B")
	out.Reset()
	sh.Dispatch("PIP /AP A.BAS=B.BAS")
	if strings.Contains(out.String(), "?") {
		t.Fatalf("pip /ap: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("TYPE A.BAS")
	got := out.String()
	if !strings.Contains(got, "AAA") || !strings.Contains(got, "BBB") {
		t.Fatalf("pip append: %q", got)
	}
	out.Reset()
	sh.Dispatch("PIP /NE A.BAS=B.BAS")
	if !strings.Contains(out.String(), "now exists") {
		t.Fatalf("pip /ne: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("PIP /PROT:40 P.BAS=B.BAS")
	if strings.Contains(out.String(), "?") {
		t.Fatalf("pip /prot: %q", out.String())
	}
	prot, err := sh.Disk.Prot(mustSpec(t, "P.BAS"), 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if prot != 40 {
		t.Fatalf("prot: %d", prot)
	}
	out.Reset()
	sh.Dispatch("PIP /HE")
	if !strings.Contains(out.String(), "PIP dst=src") {
		t.Fatalf("pip /he: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("PIP /GO NOSUCH.BAS=MISSING.BAS")
	if !strings.Contains(out.String(), "Can't find") && !strings.Contains(out.String(), "?") {
		t.Fatalf("pip /go: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("PIP B.BAS/WI")
	if !strings.Contains(out.String(), "B") {
		t.Fatalf("pip /wi: %q", out.String())
	}
}

func TestDirAllocBriefHeader(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("SAVE HELLO")
	out.Reset()
	sh.Dispatch("DIR/A")
	if !strings.Contains(out.String(), "HELLO") || !strings.Contains(out.String(), "Alloc") {
		t.Fatalf("dir/a: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("DIR/C")
	if !strings.Contains(out.String(), "HELLO") || !strings.Contains(out.String(), "Clu") {
		t.Fatalf("dir/c: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("DIR/B")
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("dir/b: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("DIR/N")
	got := out.String()
	if !strings.Contains(got, "HELLO") {
		t.Fatalf("dir/n: %q", got)
	}
	if strings.Contains(got, "SY:[") {
		t.Fatalf("dir/n still has header: %q", got)
	}
}

func TestPleaseReply(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("PLEASE printer jammed")
	out.Reset()
	sh.Dispatch("PLEASE/RE")
	if !strings.Contains(out.String(), "PLEASE/RE") {
		t.Fatalf("please/re usage: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("PLEASE/RE 1 acknowledged")
	if !strings.Contains(out.String(), "acknowledged") {
		t.Fatalf("please/re: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("PLEASE/LI")
	if !strings.Contains(out.String(), "acknowledged") {
		t.Fatalf("please list reply: %q", out.String())
	}
	j2, err := sh.sys.Attach("T")
	if err != nil {
		t.Fatal(err)
	}
	var gout strings.Builder
	g := sh.sys.newSession(j2, &gout, nil)
	g.Login("GUEST", "GUEST")
	gout.Reset()
	g.Dispatch("PLEASE/LI")
	if !strings.Contains(gout.String(), "Protection violation") {
		t.Fatalf("guest please/li: %q", gout.String())
	}
	gout.Reset()
	g.Dispatch("PLEASE/RE 1 later")
	if !strings.Contains(gout.String(), "Protection violation") {
		t.Fatalf("guest please/re: %q", gout.String())
	}
}

func TestPleaseMatch(t *testing.T) {
	m := pleaseMsg{ID: 3, Job: 2, KB: "KB0:"}
	if !pleaseMatch(m, "3") || !pleaseMatch(m, "2") || !pleaseMatch(m, "KB0") || !pleaseMatch(m, "KB0:") {
		t.Fatal("pleaseMatch should accept id, job, and KB")
	}
	if pleaseMatch(m, "") || pleaseMatch(m, "9") {
		t.Fatal("pleaseMatch should reject empty and other ids")
	}
}

func TestOpenModeTentative(t *testing.T) {
	sh, _ := guestShell(t)
	sh.Dispatch("NEW T")
	sh.Dispatch(`10 OPEN "TENT.DAT" FOR OUTPUT AS FILE 1, MODE 64`)
	sh.Dispatch(`20 PRINT #1, "HI"`)
	sh.Dispatch(`30 END`)
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	spec := mustSpec(t, "TENT.DAT")
	if sh.Disk.Exists(spec, 100, 100, false) {
		t.Fatal("MODE 64 file should vanish at program end without CLOSE")
	}
	sh.Dispatch(`10 OPEN "KEEP.DAT" FOR OUTPUT AS FILE 1, MODE 64`)
	sh.Dispatch(`20 PRINT #1, "HI"`)
	sh.Dispatch(`30 CLOSE 1`)
	sh.Dispatch(`40 END`)
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !sh.Disk.Exists(mustSpec(t, "KEEP.DAT"), 100, 100, false) {
		t.Fatal("CLOSE should commit a MODE 64 file")
	}
}

func TestOpenModeReadRegardless(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("NEW T")
	sh.Dispatch(`10 OPEN "HID.DAT" FOR OUTPUT AS FILE 1`)
	sh.Dispatch(`20 PRINT #1, "X"`)
	sh.Dispatch(`30 CLOSE 1`)
	sh.Dispatch(`40 END`)
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdName("HID.DAT AS HID.DAT<63>"); err != nil {
		t.Fatal(err)
	}
	sh.Dispatch("NEW T")
	sh.Dispatch("10 ON ERROR GOTO 50")
	sh.Dispatch(`20 OPEN "HID.DAT" FOR INPUT AS FILE 1`)
	sh.Dispatch(`30 PRINT "NO"`)
	sh.Dispatch("40 END")
	sh.Dispatch("50 PRINT ERR")
	sh.Dispatch("60 END")
	out.Reset()
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "10") {
		t.Fatalf("prot deny: %q", out.String())
	}
	sh.Dispatch("NEW T")
	sh.Dispatch(`10 OPEN "HID.DAT" FOR INPUT AS FILE 1, MODE 256`)
	sh.Dispatch("20 INPUT #1, A$")
	sh.Dispatch("30 PRINT A$")
	sh.Dispatch("40 CLOSE 1")
	sh.Dispatch("50 END")
	out.Reset()
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "X") {
		t.Fatalf("mode 256: %q", out.String())
	}
}

func TestFIPDisableLogins(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("SYSTEM", "SYSTEM")
	var out strings.Builder
	sh.out = &out
	sh.Dispatch(`A$=SYS(CHR$(6%)+CHR$(-14%))`)
	j2, err := sh.sys.Attach("T")
	if err != nil {
		t.Fatal(err)
	}
	var gout strings.Builder
	g := sh.sys.newSession(j2, &gout, nil)
	g.Login("GUEST", "GUEST")
	if !strings.Contains(gout.String(), "Logins are disabled") {
		t.Fatalf("no logins: %q", gout.String())
	}
	if g.Account != nil {
		t.Fatal("guest should not be logged in")
	}
	out.Reset()
	sh.Dispatch(`A$=SYS(CHR$(6%)+CHR$(-15%))`)
	gout.Reset()
	g.Login("GUEST", "GUEST")
	if g.Account == nil {
		t.Fatalf("logins re-enabled: %q", gout.String())
	}
}
