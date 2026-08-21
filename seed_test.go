package rsts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newSystemIn(t *testing.T, root string) *System {
	t.Helper()
	sys, err := NewSystem(root, DefaultConfig())
	if err != nil {
		t.Fatalf("NewSystem(%s): %v", root, err)
	}
	t.Cleanup(sys.Close)
	return sys
}

func wantAccount(t *testing.T, db *AccountDB, token string, proj, prog int) {
	t.Helper()
	a := db.Find(token)
	if a == nil {
		t.Fatalf("account %s missing", token)
	}
	if a.Proj != proj || a.Prog != prog {
		t.Fatalf("account %s is [%d,%d], want [%d,%d]", token, a.Proj, a.Prog, proj, prog)
	}
}

func TestSeedsEmptyDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	sys := newSystemIn(t, root)

	wantAccount(t, sys.Accounts, "SYSTEM", 1, 2)
	wantAccount(t, sys.Accounts, "LIBRARY", 1, 9)
	wantAccount(t, sys.Accounts, "GUEST", 100, 100)
	wantAccount(t, sys.Accounts, "DEMO", 200, 200)

	for _, path := range []string{
		"accounts.json",
		"packs.json",
		filepath.Join("SY", "1,2", "NOTICE.TXT"),
		filepath.Join("SY", "1,2", "PIP.BAC"),
		filepath.Join("SY", "1,9", "COMP.BAS"),
		filepath.Join("SY", "1,9", "WHOAMI.BAC"),
		filepath.Join("SY", "100,100", "HELLO.BAS"),
		filepath.Join("SY", "200,200", "SIEVE.BAS"),
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("missing after seed: %s (%v)", path, err)
		}
	}
	if len(sys.Disk.Packs()) == 0 {
		t.Error("no packs after seed")
	}
	if _, err := os.Stat(filepath.Join(root, "SY", "1,2", "COMP.BAS")); err == nil {
		t.Error("COMP.BAS should not be seeded in [1,2]")
	}
	if _, err := os.Stat(filepath.Join(root, "SY", "1,9", "COMP.BAC")); err == nil {
		t.Error("COMP.BAS should not be compiled")
	}
}

func TestSeedsNestedMissingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "no", "such", "place", "disk")
	sys := newSystemIn(t, root)
	wantAccount(t, sys.Accounts, "SYSTEM", 1, 2)
	wantAccount(t, sys.Accounts, "LIBRARY", 1, 9)
}

func TestRebuildsDamagedAccountsFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "accounts.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	sys := newSystemIn(t, root)
	wantAccount(t, sys.Accounts, "SYSTEM", 1, 2)
	wantAccount(t, sys.Accounts, "GUEST", 100, 100)
	if _, err := os.Stat(path + ".bad"); err != nil {
		t.Errorf("damaged file not kept: %v", err)
	}
}

func TestRebuildsDamagedPacksFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "packs.json")
	if err := os.WriteFile(path, []byte("nonsense["), 0o644); err != nil {
		t.Fatal(err)
	}

	sys := newSystemIn(t, root)
	if len(sys.Disk.Packs()) == 0 {
		t.Fatal("packs not rebuilt")
	}
	if sys.Disk.systemPack() == nil {
		t.Fatal("no system pack after rebuild")
	}
	if _, err := os.Stat(path + ".bad"); err != nil {
		t.Errorf("damaged file not kept: %v", err)
	}
}

func TestRestoresMissingSystemAccount(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "accounts.json")
	keep := accountFile{Accounts: []Account{{Proj: 200, Prog: 200, Name: "DEMO", Password: "DEMO"}}}
	data, err := json.Marshal(keep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	sys := newSystemIn(t, root)
	wantAccount(t, sys.Accounts, "SYSTEM", 1, 2)
	wantAccount(t, sys.Accounts, "LIBRARY", 1, 9)
	wantAccount(t, sys.Accounts, "DEMO", 200, 200)
	if a := sys.Accounts.Find("SYSTEM"); a == nil || !a.Privileged {
		t.Fatal("restored [1,2] should be privileged")
	}
	if a := sys.Accounts.Find("LIBRARY"); a == nil || !a.Privileged {
		t.Fatal("restored [1,9] should be privileged")
	}
	if a := sys.Accounts.Find("GUEST"); a != nil {
		t.Fatal("a deliberately deleted GUEST should stay deleted")
	}

	// The restored account must survive on disk, not just in memory.
	again := newSystemIn(t, root)
	wantAccount(t, again.Accounts, "SYSTEM", 1, 2)
	wantAccount(t, again.Accounts, "LIBRARY", 1, 9)
}

// An old disk must pick up a newer exerciser, notice or CUSP.
func TestRefreshesStaleSystemSample(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "SY", "1,9", "COMP.BAS")
	if err := os.WriteFile(path, []byte("10 PRINT \"OLD RELEASE\"\n20 END\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != samples["1,9"]["COMP.BAS"] {
		t.Fatalf("stale system sample was not refreshed:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "SY", "1,2", "COMP.BAS")); err == nil {
		t.Fatal("COMP.BAS should not remain in [1,2]")
	}
}

// A program the user has changed is theirs, and survives an upgrade.
func TestKeepsEditedUserSample(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "SY", "100,100", "HELLO.BAS")
	mine := "10 PRINT \"MY OWN HELLO\"\n20 END\n"
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != mine {
		t.Fatalf("edited sample was overwritten:\n%s", got)
	}
}

// An untouched sample tracks the release even outside [1,2].
func TestRefreshesUntouchedUserSample(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "SY", "100,100", "HELLO.BAS")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	next := "10 PRINT \"HELLO FROM A LATER RELEASE\"\n20 END\n"
	restore := samples["100,100"]["HELLO.BAS"]
	samples["100,100"]["HELLO.BAS"] = next
	defer func() { samples["100,100"]["HELLO.BAS"] = restore }()

	newSystemIn(t, root)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == string(before) {
		t.Fatal("untouched sample was not refreshed")
	}
	if string(got) != next {
		t.Fatalf("unexpected content:\n%s", got)
	}
}

func TestRestoresMissingGuestSample(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "SY", "100,100", "HELLO.PAS")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing guest sample was not restored: %v", err)
	}
	if string(got) != samples["100,100"]["HELLO.PAS"] {
		t.Fatalf("restored HELLO.PAS does not match the sample")
	}
}

func TestRepairsDeletedAccountDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	if err := os.RemoveAll(filepath.Join(root, "SY", "1,2")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "SY", "1,9")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "DB1")); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	for _, path := range []string{
		filepath.Join("SY", "1,2", "NOTICE.TXT"),
		filepath.Join("SY", "1,2", "PIP.BAC"),
		filepath.Join("SY", "1,9", "WHOAMI.BAC"),
		filepath.Join("SY", "1,9", "COMP.BAS"),
		"DB1",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("not repaired: %s (%v)", path, err)
		}
	}
}

// An old disk that still has COMP/DATA/WHOAMI in [1,2] loses those
// copies once they seed onto [1,9].
func TestRetiresMovedLibraryFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	old := filepath.Join(root, "SY", "1,2", "COMP.BAS")
	if err := os.WriteFile(old, []byte("10 PRINT \"STALE\"\n20 END\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	if _, err := os.Stat(old); err == nil {
		t.Fatal("COMP.BAS should have left [1,2]")
	}
	got, err := os.ReadFile(filepath.Join(root, "SY", "1,9", "COMP.BAS"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != samples["1,9"]["COMP.BAS"] {
		t.Fatal("COMP.BAS was not seeded onto [1,9]")
	}
	if _, err := os.Stat(filepath.Join(root, "SY", "1,2", "PIP.BAC")); err != nil {
		t.Fatal("PIP.BAC should stay in [1,2]")
	}
}

func TestProjectOnePrivilegeCannotBeCleared(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "accounts.json")
	keep := accountFile{Accounts: []Account{
		{Proj: 1, Prog: 2, Name: "SYSTEM", Password: "SYSTEM"},
		{Proj: 1, Prog: 4, Name: "OPR", Password: "SECRET"},
		{Proj: 1, Prog: 9, Name: "LIBRARY", Password: "LIBRARY"},
	}}
	data, err := json.Marshal(keep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	sys := newSystemIn(t, root)
	for _, ppn := range [][2]int{{1, 2}, {1, 4}, {1, 9}} {
		a := sys.Accounts.FindPPN(ppn[0], ppn[1])
		if a == nil || !a.HasPrivilege() || !a.Privileged {
			t.Fatalf("[%d,%d] must be privileged after load", ppn[0], ppn[1])
		}
	}
}

func TestRefreshesStaleCUSP(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	old := "10 PRINT \"OLD CUSP\"\n20 END\n"
	bas := filepath.Join(root, "SY", "1,2", "PIP.BAS")
	bac := filepath.Join(root, "SY", "1,2", "PIP.BAC")
	if err := os.WriteFile(bas, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := compileSourceText(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bac, []byte(wrapPcode(img)), 0o644); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	got, err := os.ReadFile(bas)
	if err != nil {
		t.Fatal(err)
	}
	want := libraryCUSPFiles()["PIP.BAS"]
	if string(got) != want {
		t.Fatal("stale CUSP .BAS was not re-seeded")
	}
	got, err = os.ReadFile(bac)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := compileSourceText(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != wrapPcode(fresh) {
		t.Fatal("stale CUSP .BAC was not re-seeded")
	}
}

func TestCompilesCUSPWhenBasNewer(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	bas := filepath.Join(root, "SY", "1,2", "PIP.BAS")
	bac := filepath.Join(root, "SY", "1,2", "PIP.BAC")
	now := time.Now()
	if err := os.Chtimes(bac, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(bas, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	info, err := os.Stat(bac)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().After(now.Add(-90 * time.Minute)) {
		t.Fatalf("PIP.BAC was not recompiled from newer PIP.BAS; mtime %s", info.ModTime())
	}
}

func TestDoesNotCompileLibraryDemos(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)
	if _, err := os.Stat(filepath.Join(root, "SY", "1,9", "COMP.BAC")); err == nil {
		t.Fatal("COMP.BAS should not be compiled")
	}
	if _, err := os.Stat(filepath.Join(root, "SY", "1,9", "DATA.BAC")); err == nil {
		t.Fatal("DATA.BAS should not be compiled")
	}
	if _, err := os.Stat(filepath.Join(root, "SY", "1,9", "WHOAMI.BAC")); err != nil {
		t.Fatal("WHOAMI.BAC should be compiled")
	}
}
