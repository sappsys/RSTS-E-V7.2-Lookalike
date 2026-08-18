package rsts

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseCharDevice(t *testing.T) {
	cases := []struct {
		in    string
		dev   string
		unit  int
		set   bool
		rest  string
		valid bool
	}{
		{"KB:", "KB", 0, false, "", true},
		{"kb3:", "KB", 3, true, "", true},
		{"TT:", "TT", 0, false, "", true},
		{"NL:", "NL", 0, false, "", true},
		{"LP:", "LP", 0, false, "", true},
		{"LP1:REPORT.LST", "LP", 1, true, "REPORT.LST", true},
		{"KB:FOO.BAS", "", 0, false, "", false},
		{"DB1:FOO.BAS", "", 0, false, "", false},
		{"FOO.BAS", "", 0, false, "", false},
		{"PK:", "", 0, false, "", false},
	}
	for _, c := range cases {
		dev, unit, set, rest, ok := parseCharDevice(c.in)
		if ok != c.valid {
			t.Errorf("%s: recognised = %v, want %v", c.in, ok, c.valid)
			continue
		}
		if !ok {
			continue
		}
		if dev != c.dev || unit != c.unit || set != c.set || rest != c.rest {
			t.Errorf("%s gave %s unit %d set %v rest %q", c.in, dev, unit, set, rest)
		}
	}
}

func TestOpenKeyboardChannel(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out

	if err := sh.Basic.LoadSource(`10 OPEN "KB:" AS FILE 1
20 PRINT #1, "HELLO TERMINAL"
30 CLOSE 1
40 END
`, "KBT"); err != nil {
		t.Fatal(err)
	}
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "HELLO TERMINAL") {
		t.Fatalf("KB: did not reach the terminal: %q", out.String())
	}
}

func TestNullDeviceSwallowsAndEnds(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out

	if err := sh.Basic.LoadSource(`10 OPEN "NL:" AS FILE 1
20 PRINT #1, "NOBODY SEES THIS"
30 ON ERROR GOTO 60
40 LINE INPUT #1, A$
50 PRINT "SHOULD NOT GET HERE"
60 CLOSE 1
70 PRINT "END OF FILE AT ONCE"
80 END
`, "NLT"); err != nil {
		t.Fatal(err)
	}
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "NOBODY SEES THIS") {
		t.Fatalf("the null device printed: %q", got)
	}
	if strings.Contains(got, "SHOULD NOT GET HERE") {
		t.Fatalf("the null device returned data: %q", got)
	}
	if !strings.Contains(got, "END OF FILE AT ONCE") {
		t.Fatalf("reading the null device should be at end of file: %q", got)
	}
}

func TestPrinterSpoolsToAccount(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out

	if err := sh.Basic.LoadSource(`10 OPEN "LP:" AS FILE 1
20 PRINT #1, "PAYROLL RUN"
30 CLOSE 1
40 END
`, "LPT"); err != nil {
		t.Fatal(err)
	}
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	text, err := sh.Disk.ReadText(mustSpec(t, "LP0.LST"), 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "PAYROLL RUN") {
		t.Fatalf("spool file holds %q", text)
	}

	// A named spool file is allowed after the colon.
	if err := sh.Basic.LoadSource(`10 OPEN "LP:REPORT.LST" AS FILE 1
20 PRINT #1, "NAMED"
30 CLOSE 1
40 END
`, "LPT2"); err != nil {
		t.Fatal(err)
	}
	if err := sh.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	text, err = sh.Disk.ReadText(mustSpec(t, "REPORT.LST"), 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "NAMED") {
		t.Fatalf("named spool file holds %q", text)
	}
}

// Writing to another job's terminal is the same privilege as FORCE.
func TestKeyboardChannelToAnotherJob(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), Config{MaxUsers: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 0)
	defer sys.Close()

	jobA, err := sys.Attach("A")
	if err != nil {
		t.Fatal(err)
	}
	var outA bytes.Buffer
	shA := sys.newSession(jobA, &outA, &quietTerm{})
	shA.Login("GUEST", "GUEST")

	jobB, err := sys.Attach("B")
	if err != nil {
		t.Fatal(err)
	}
	var outB bytes.Buffer
	shB := sys.newSession(jobB, &outB, &quietTerm{})
	shB.Login("GUEST", "GUEST")

	src := `10 OPEN "` + shA.KB + `" AS FILE 1
20 PRINT #1, "MESSAGE FROM JOB B"
30 CLOSE 1
40 END
`
	// A guest may not write to someone else's keyboard.
	if err := shB.Basic.LoadSource(src, "KBX"); err != nil {
		t.Fatal(err)
	}
	if err := shB.Basic.RunProgram(); err == nil {
		t.Fatal("a guest should not reach another terminal")
	}

	// The system account may.
	shB.Account = nil
	shB.Login("SYSTEM", "SYSTEM")
	if err := shB.Basic.LoadSource(src, "KBX"); err != nil {
		t.Fatal(err)
	}
	if err := shB.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outA.String(), "MESSAGE FROM JOB B") {
		t.Fatalf("job A never saw it: %q", outA.String())
	}
}
