package rsts

import (
	"strings"
	"testing"
)

func TestParseDeviceSpec(t *testing.T) {
	spec, err := ParseFileSpec("DB1:FOO.BAS", "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Device != "DB" || spec.Unit != 1 || !spec.UnitSet || spec.Name != "FOO" || spec.Ext != "BAS" {
		t.Fatalf("db1: %+v", spec)
	}
	spec, err = ParseFileSpec("PAYROL:BAR.TXT", "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Device != "PAYROL" || spec.Name != "BAR" {
		t.Fatalf("pack id: %+v", spec)
	}
}

func TestMountDismount(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	if err := sh.cmdDir("DB1:"); err == nil {
		t.Fatal("unmounted DB1: should fail")
	}
	if err := sh.cmdMount("DB1: PAYROL"); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdDir("DB1:"); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdDir("PAYROL:"); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdType("DB1:README.TXT"); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdMount("DB1: PAYROL /PUBLIC"); err == nil {
		t.Fatal("already mounted / guest public")
	}
	if err := sh.cmdDismount("DB1:"); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdType("DB1:README.TXT"); err == nil {
		t.Fatal("dismounted read")
	}

	sh.cmdBye("")
	sh.Login("SYSTEM", "SYSTEM")
	if err := sh.cmdDismount("SY0:"); err == nil {
		t.Fatal("dismount system")
	}
	if err := sh.cmdDismount("DB0:"); err == nil {
		t.Fatal("dismount DB0")
	}
	if err := sh.cmdDskint("DL0: WORK"); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdMount("DL0: WORK /PUBLIC"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	sh.out = &out
	sh.cmdDisks()
	got := out.String()
	if !strings.Contains(got, "SYSDSK") || !strings.Contains(got, "PAYROL") || !strings.Contains(got, "WORK") {
		t.Fatalf("systat/d: %q", got)
	}
	if err := sh.cmdDismount("DL0: WORK"); err != nil {
		t.Fatal(err)
	}
}

func TestSplitVerbAttachedSwitch(t *testing.T) {
	verb, rest := splitVerb("SYSTAT/D")
	if verb != "SYSTAT" || rest != "/D" {
		t.Fatalf("SYSTAT/D -> %q %q", verb, rest)
	}
	verb, rest = splitVerb("DISMOU DB1:")
	if verb != "DISMOUNT" || rest != "DB1:" {
		t.Fatalf("DISMOU -> %q %q", verb, rest)
	}
	verb, rest = splitVerb("SYSTAT/F/N")
	if verb != "SYSTAT" || rest != "/F/N" {
		t.Fatalf("SYSTAT/F/N -> %q %q", verb, rest)
	}
}

func TestSystatSlashDAndHelpDisk(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")

	var out strings.Builder
	sh.out = &out
	sh.Dispatch("SYSTAT/D")
	got := out.String()
	if strings.Contains(got, "Syntax") {
		t.Fatalf("SYSTAT/D syntax: %q", got)
	}
	if !strings.Contains(got, "SYSDSK") || !strings.Contains(got, "Disk") {
		t.Fatalf("SYSTAT/D disks: %q", got)
	}

	out.Reset()
	sh.Dispatch("HELP DISK")
	got = out.String()
	if strings.Contains(got, "No help") {
		t.Fatalf("HELP DISK: %q", got)
	}
	if !strings.Contains(got, "MOUNT") {
		t.Fatalf("HELP DISK text: %q", got)
	}

	out.Reset()
	sh.Dispatch("HELP DISKS")
	if strings.Contains(out.String(), "No help") {
		t.Fatalf("HELP DISKS: %q", out.String())
	}

	out.Reset()
	sh.Dispatch("HELP MOUNT")
	if strings.Contains(out.String(), "No help") {
		t.Fatalf("HELP MOUNT: %q", out.String())
	}

	out.Reset()
	sh.Dispatch("SHOW DISKS")
	if !strings.Contains(out.String(), "SYSDSK") {
		t.Fatalf("SHOW DISKS: %q", out.String())
	}

	out.Reset()
	sh.Dispatch("SYSTAT/M")
	if strings.Contains(out.String(), "Syntax") || !strings.Contains(out.String(), "Memory") {
		t.Fatalf("SYSTAT/M: %q", out.String())
	}

	out.Reset()
	sh.Dispatch("SHOW MEMORY")
	if !strings.Contains(out.String(), "Memory") {
		t.Fatalf("SHOW MEMORY: %q", out.String())
	}

	out.Reset()
	sh.Dispatch("HELP SYSTAT")
	if !strings.Contains(out.String(), "SYSTAT/D") {
		t.Fatalf("HELP SYSTAT: %q", out.String())
	}

	out.Reset()
	sh.Dispatch("HELP SHOW")
	if !strings.Contains(out.String(), "SHOW DISKS") {
		t.Fatalf("HELP SHOW: %q", out.String())
	}
}
