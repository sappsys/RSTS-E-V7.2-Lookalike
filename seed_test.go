package rsts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	wantAccount(t, sys.Accounts, "GUEST", 100, 100)
	wantAccount(t, sys.Accounts, "DEMO", 200, 200)

	for _, path := range []string{
		"accounts.json",
		"packs.json",
		filepath.Join("SY", "1,2", "NOTICE.TXT"),
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
}

func TestSeedsNestedMissingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "no", "such", "place", "disk")
	sys := newSystemIn(t, root)
	wantAccount(t, sys.Accounts, "SYSTEM", 1, 2)
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
	wantAccount(t, sys.Accounts, "DEMO", 200, 200)
	if a := sys.Accounts.Find("SYSTEM"); a == nil || !a.Privileged {
		t.Fatal("restored [1,2] should be privileged")
	}
	if a := sys.Accounts.Find("GUEST"); a != nil {
		t.Fatal("a deliberately deleted GUEST should stay deleted")
	}

	// The restored account must survive on disk, not just in memory.
	again := newSystemIn(t, root)
	wantAccount(t, again.Accounts, "SYSTEM", 1, 2)
}

// An old disk must pick up a newer exerciser, notice or CUSP.
func TestRefreshesStaleSystemSample(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk")
	newSystemIn(t, root)

	path := filepath.Join(root, "SY", "1,2", "COMP.BAS")
	if err := os.WriteFile(path, []byte("10 PRINT \"OLD RELEASE\"\n20 END\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != samples["1,2"]["COMP.BAS"] {
		t.Fatalf("stale system sample was not refreshed:\n%s", got)
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
	if err := os.RemoveAll(filepath.Join(root, "DB1")); err != nil {
		t.Fatal(err)
	}

	newSystemIn(t, root)
	for _, path := range []string{
		filepath.Join("SY", "1,2", "NOTICE.TXT"),
		filepath.Join("SY", "1,2", "WHOAMI.BAC"),
		"DB1",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("not repaired: %s (%v)", path, err)
		}
	}
}
