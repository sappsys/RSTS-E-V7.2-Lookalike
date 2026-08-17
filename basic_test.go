package rsts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type capture struct {
	out    strings.Builder
	inputs []string
}

func (c *capture) write(text string, newline bool) {
	c.out.WriteString(text)
	if newline {
		c.out.WriteByte('\n')
	}
}

func (c *capture) read(prompt string) (string, error) {
	c.out.WriteString(prompt)
	if len(c.inputs) == 0 {
		return "", os.ErrClosed
	}
	s := c.inputs[0]
	c.inputs = c.inputs[1:]
	return s, nil
}

func runProgram(t *testing.T, source string, inputs ...string) string {
	t.Helper()
	c := &capture{inputs: inputs}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	if err := m.LoadSource(source, "TEST"); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatalf("run: %v", err)
	}
	return c.out.String()
}

func immediate(t *testing.T, line string) string {
	t.Helper()
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	if err := m.ExecImmediate(line); err != nil {
		t.Fatalf("immediate %q: %v", line, err)
	}
	return c.out.String()
}

func TestExpressions(t *testing.T) {
	if got := strings.TrimSpace(immediate(t, "PRINT 1+2*3")); got != "7" {
		t.Fatalf("arith: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, `PRINT 7\2`)); got != "3" {
		t.Fatalf("idiv: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, "PRINT (2+3)^2")); got != "25" {
		t.Fatalf("pow: %q", got)
	}
	if !strings.Contains(immediate(t, `PRINT "AB"+"CD"`), "ABCD") {
		t.Fatal("concat")
	}
	if got := strings.TrimSpace(immediate(t, "PRINT 1=1")); got != "-1" {
		t.Fatalf("true: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, "PRINT 1=2")); got != "0" {
		t.Fatalf("false: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, "PRINT INT(3.9)")); got != "3" {
		t.Fatalf("int: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, `PRINT LEN("ABCD")`)); got != "4" {
		t.Fatalf("len: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, `PRINT LEFT$("ABCD",2)`)); got != "AB" {
		t.Fatalf("left: %q", got)
	}
	if got := strings.TrimSpace(immediate(t, `PRINT MID$("ABCD",2,2)`)); got != "BC" {
		t.Fatalf("mid: %q", got)
	}
}

func TestProgram(t *testing.T) {
	out := runProgram(t, "10 PRINT \"A\"\n20 GOTO 40\n30 PRINT \"B\"\n40 PRINT \"C\"\n50 END\n")
	if strings.ReplaceAll(out, " ", "") != "A\nC\n" {
		t.Fatalf("goto: %q", out)
	}

	out = runProgram(t, "10 FOR I = 1 TO 3\n20 PRINT I\n30 NEXT I\n40 END\n")
	if strings.TrimSpace(out) != "1\n 2\n 3" {
		t.Fatalf("for: %q", out)
	}

	out = runProgram(t, "10 X = 2\n20 IF X = 2 THEN PRINT \"YES\" ELSE PRINT \"NO\"\n30 END\n")
	if !strings.Contains(out, "YES") {
		t.Fatalf("if: %q", out)
	}

	out = runProgram(t, "10 GOSUB 40\n20 PRINT \"BACK\"\n30 END\n40 PRINT \"SUB\"\n50 RETURN\n")
	if !strings.Contains(out, "SUB") || !strings.Contains(out, "BACK") {
		t.Fatalf("gosub: %q", out)
	}

	out = runProgram(t, "10 I = 1\n20 WHILE I <= 3\n30 PRINT I\n40 I = I+1\n50 NEXT\n60 END\n")
	if strings.TrimSpace(out) != "1\n 2\n 3" {
		t.Fatalf("while: %q", out)
	}

	out = runProgram(t, samples["100,100"]["STARS.BAS"])
	if !strings.Contains(out, "*\n") || !strings.Contains(out, "********") {
		t.Fatalf("stars: %q", out)
	}

	out = runProgram(t, "10 DATA 10, 20, 30\n20 READ A, B, C\n30 PRINT A+B+C\n40 END\n")
	if strings.TrimSpace(out) != "60" {
		t.Fatalf("data: %q", out)
	}

	out = runProgram(t, "10 DEF FNSQ(X) = X*X\n20 PRINT FNSQ(6)\n30 END\n")
	if strings.TrimSpace(out) != "36" {
		t.Fatalf("fn: %q", out)
	}

	out = runProgram(t, "10 INPUT A\n20 PRINT A*2\n30 END\n", "5")
	if !strings.Contains(out, "10") {
		t.Fatalf("input: %q", out)
	}

	out = runProgram(t, "10 A=3 \\ PRINT A \\ PRINT A+1\n20 END\n")
	if !strings.Contains(out, "3") || !strings.Contains(out, "4") {
		t.Fatalf("multi: %q", out)
	}

	out = strings.TrimSpace(runProgram(t, "10 X%=2% \\ A$=\"HI\" \\ PRINT X%; A$\n20 END\n"))
	if !strings.Contains(out, "2") || !strings.Contains(out, "HI") {
		t.Fatalf("backslash assign: %q", out)
	}

	out = runProgram(t, "10 X = 2\n20 ON X GOTO 30, 50, 70\n30 PRINT \"ONE\"\n40 END\n50 PRINT \"TWO\"\n60 END\n70 PRINT \"THREE\"\n80 END\n")
	if !strings.Contains(out, "TWO") {
		t.Fatalf("on goto: %q", out)
	}

	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	if err := m.StoreLine(10, "PRNT"); err == nil {
		t.Fatal("expected syntax error")
	}

	out = runProgram(t, samples["100,100"]["FIB.BAS"], "5")
	if !strings.Contains(out, "1") || !strings.Contains(out, "5") {
		t.Fatalf("fib: %q", out)
	}

	out = runProgram(t, samples["200,200"]["SIEVE.BAS"])
	if !strings.Contains(out, "2") || !strings.Contains(out, "47") {
		t.Fatalf("sieve missing primes: %q", out)
	}
}

func TestFileSystem(t *testing.T) {
	tmp := t.TempDir()
	disk, err := OpenDisk(tmp)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseFileSpec("HELLO.BAS", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := disk.WriteText(spec, 100, 100, false, "10 PRINT\n", defaultProt); err != nil {
		t.Fatal(err)
	}
	text, err := disk.ReadText(spec, 100, 100, false)
	if err != nil || text != "10 PRINT\n" {
		t.Fatalf("round trip: %q %v", text, err)
	}
	ppn, infos, err := disk.ListDir(mustSpec(t, "*.*"), 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if ppn != "100,100" || len(infos) != 1 || infos[0].Name != "HELLO.BAS" {
		t.Fatalf("list: %s %#v", ppn, infos)
	}
	listing := FormatDir("SY:", ppn, infos)
	if !strings.Contains(listing, "HELLO") {
		t.Fatalf("format: %s", listing)
	}

	secret, err := ParseFileSpec("[1,2]SECRET.TXT", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := disk.WriteText(secret, 1, 2, true, "nope\n", defaultProt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := disk.ListDir(mustSpec(t, "[1,2]*.*"), 100, 100, false); err == nil {
		t.Fatal("expected protection violation")
	}
}

func TestAccounts(t *testing.T) {
	db, err := OpenAccountDB(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if db.Authenticate("GUEST", "GUEST") == nil {
		t.Fatal("guest")
	}
	if db.Authenticate("GUEST", "WRONG") != nil {
		t.Fatal("wrong pw")
	}
	if _, err := db.Create(150, 1, "BOB", "SECRET", false); err != nil {
		t.Fatal(err)
	}
	if db.Authenticate("150,1", "SECRET") == nil {
		t.Fatal("created")
	}
	bob := db.FindPPN(150, 1)
	if err := db.SetPassword(bob, "NEWPW"); err != nil {
		t.Fatal(err)
	}
	if db.Authenticate("BOB", "SECRET") != nil {
		t.Fatal("old password still worked")
	}
	if db.Authenticate("BOB", "NEWPW") == nil {
		t.Fatal("new password")
	}
	if err := db.Delete(150, 1); err != nil {
		t.Fatal(err)
	}
	if db.FindPPN(150, 1) != nil {
		t.Fatal("deleted account still present")
	}
	if err := db.Delete(1, 2); err == nil {
		t.Fatal("should not delete [1,2]")
	}
	if _, err := db.Create(150, 1, "BAD NAME", "X", false); err == nil {
		t.Fatal("illegal name")
	}
}

func TestAccountCommands(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	if err := sh.cmdCreate("150,1 NAME BOB PASSWORD SECRET"); err == nil {
		t.Fatal("guest CREATE")
	}
	if err := sh.cmdDeleteAccount("DEMO"); err == nil {
		t.Fatal("guest DELETE/ACCOUNT")
	}
	if err := sh.cmdPassword("DEMO NEWPW"); err == nil {
		t.Fatal("guest PASSWORD other")
	}

	sh.cmdBye("")
	sh.Login("SYSTEM", "SYSTEM")
	var out strings.Builder
	sh.out = &out
	if err := sh.cmdCreate("150,1 BOB SECRET"); err != nil {
		t.Fatal(err)
	}
	if sh.Accounts.Authenticate("BOB", "SECRET") == nil {
		t.Fatal("create login")
	}
	if err := sh.cmdCreate("[150,1] NAME BOB PASSWORD SECRET"); err == nil {
		t.Fatal("duplicate PPN")
	}
	if err := sh.cmdPassword("BOB NEWPW"); err != nil {
		t.Fatal(err)
	}
	if sh.Accounts.Authenticate("150,1", "NEWPW") == nil {
		t.Fatal("priv password reset")
	}
	out.Reset()
	if err := sh.cmdShowAccounts(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "BOB") || !strings.Contains(got, "SYSTEM") {
		t.Fatalf("show accounts: %q", got)
	}
	if err := sh.cmdDeleteAccount("[1,2]"); err == nil {
		t.Fatal("delete SYSTEM")
	}
	if err := sh.cmdDeleteAccount("1,2"); err == nil {
		t.Fatal("delete [1,2]")
	}
	if err := sh.cmdDeleteAccount("150,1"); err != nil {
		t.Fatal(err)
	}
	if sh.Accounts.Find("BOB") != nil {
		t.Fatal("bob still there")
	}
	if err := sh.dispatchCmd("CREATE/ACCOUNT", "170,3 DAVE SECRET"); err != nil {
		t.Fatal(err)
	}
	if sh.Accounts.Find("DAVE") == nil {
		t.Fatal("create/account")
	}
	if err := sh.dispatchCmd("REMOVE", "DAVE"); err != nil {
		t.Fatal(err)
	}
	if sh.Accounts.Find("DAVE") != nil {
		t.Fatal("remove")
	}
	if err := sh.cmdReact("CREATE 160,2 CAROL SECRET"); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdDeleteAccount("160,2"); err != nil {
		t.Fatal(err)
	}
	if sh.Accounts.FindPPN(160, 2) != nil {
		t.Fatal("react delete")
	}
}

func TestShellSeed(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	if sh.Account == nil {
		t.Fatal("login")
	}
	if err := sh.cmdOld("HELLO"); err != nil {
		t.Fatal(err)
	}
	listing := strings.Join(sh.Basic.Listing(0, 0, false, false), "\n")
	if !strings.Contains(listing, "HELLO FROM RSTS/E") {
		t.Fatalf("listing: %s", listing)
	}
}

func mustSpec(t *testing.T, s string) FileSpec {
	t.Helper()
	spec, err := ParseFileSpec(s, "")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
