package rsts

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestOpenKeyboardAtBye(t *testing.T) {
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
	termA := &quietTerm{in: []string{"FROM SERIAL"}}
	shA := sys.newSession(jobA, &outA, termA)
	if shA.Account != nil {
		t.Fatal("KB A should be at Bye")
	}

	jobB, err := sys.Attach("B")
	if err != nil {
		t.Fatal(err)
	}
	var outB bytes.Buffer
	shB := sys.newSession(jobB, &outB, &quietTerm{})
	shB.Login("GUEST", "GUEST")

	src := `10 OPEN "` + shA.KB + `" AS FILE 1
20 LINE INPUT #1, A$
30 PRINT A$
40 PRINT #1, "TO SERIAL"
50 CLOSE 1
60 END
`
	if err := shB.Basic.LoadSource(src, "KBF"); err != nil {
		t.Fatal(err)
	}
	if err := shB.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outB.String(), "FROM SERIAL") {
		t.Fatalf("guest did not read the free KB: %q", outB.String())
	}
	if !strings.Contains(outA.String(), "TO SERIAL") {
		t.Fatalf("guest did not write the free KB: %q", outA.String())
	}
	if sys.kbAssigned(shA.KB) {
		t.Fatal("CLOSE should release the keyboard")
	}
}

func TestOpenKeyboardInUse(t *testing.T) {
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
	shA := sys.newSession(jobA, &bytes.Buffer{}, &quietTerm{})

	jobB, err := sys.Attach("B")
	if err != nil {
		t.Fatal(err)
	}
	shB := sys.newSession(jobB, &bytes.Buffer{}, &quietTerm{})
	shB.Login("GUEST", "GUEST")

	jobC, err := sys.Attach("C")
	if err != nil {
		t.Fatal(err)
	}
	var outC bytes.Buffer
	shC := sys.newSession(jobC, &outC, &quietTerm{})
	shC.Login("GUEST", "GUEST")

	if err := shB.Basic.ExecImmediate(`OPEN "` + shA.KB + `" AS FILE 1`); err != nil {
		t.Fatalf("first OPEN of a free KB: %v", err)
	}
	err = shC.Basic.ExecImmediate(`OPEN "` + shA.KB + `" AS FILE 1`)
	if err == nil {
		t.Fatal("second OPEN should fail while assigned")
	}
	if !strings.Contains(err.Error(), "in use") {
		t.Fatalf("want device in use, got %v", err)
	}
}

func TestKeyboardGetPutChar(t *testing.T) {
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
	termA := &quietTerm{bytes: []byte{'Q'}}
	shA := sys.newSession(jobA, &outA, termA)
	if shA.Account != nil {
		t.Fatal("KB A should be at Bye")
	}

	jobB, err := sys.Attach("B")
	if err != nil {
		t.Fatal(err)
	}
	var outB bytes.Buffer
	shB := sys.newSession(jobB, &outB, &quietTerm{})
	shB.Login("GUEST", "GUEST")

	src := `10 OPEN "` + shA.KB + `" AS FILE 1, RECORDSIZE 1
20 FIELD #1, 1 AS A$
30 GET #1
40 PRINT ASC(A$)
50 LSET A$="Z"
60 PUT #1
70 CLOSE 1
80 END
`
	if err := shB.Basic.LoadSource(src, "KBG"); err != nil {
		t.Fatal(err)
	}
	if err := shB.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outB.String(), "81") { // ASC("Q")
		t.Fatalf("GET char: %q", outB.String())
	}
	if !strings.Contains(outA.String(), "Z") {
		t.Fatalf("PUT char: %q", outA.String())
	}
}

func TestKeyboardGetWaitPoll(t *testing.T) {
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
	shA := sys.newSession(jobA, &bytes.Buffer{}, &quietTerm{})

	jobB, err := sys.Attach("B")
	if err != nil {
		t.Fatal(err)
	}
	var outB bytes.Buffer
	shB := sys.newSession(jobB, &outB, &quietTerm{})
	shB.Login("GUEST", "GUEST")

	src := `10 ON ERROR GOTO 40
20 OPEN "` + shA.KB + `" AS FILE 1, RECORDSIZE 1
30 WAIT 0
35 GET #1
36 PRINT "NO"
40 PRINT ERR
50 CLOSE 1
60 END
`
	if err := shB.Basic.LoadSource(src, "W0"); err != nil {
		t.Fatal(err)
	}
	if err := shB.Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outB.String(), "15") {
		t.Fatalf("WAIT 0 GET: %q", outB.String())
	}
}

func TestPKGetWait0TimesOut(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), Config{MaxUsers: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 1)
	defer sys.Close()

	job, err := sys.Attach("B")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	sh := sys.newSession(job, &out, &quietTerm{})
	sh.Login("GUEST", "GUEST")

	src := `10 ON ERROR GOTO 100
20 OPEN "PK:" AS FILE 2, RECORDSIZE 1
30 FIELD #2, 1 AS B$
40 WAIT 0
50 GET #2
60 GOTO 50
100 PRINT "ERR";ERR;"ERL";ERL
110 CLOSE 2
120 END
`
	if err := sh.Basic.LoadSource(src, "WPK"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- sh.Basic.RunProgram() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		sh.Basic.Interrupt()
		t.Fatalf("WAIT 0 GET #PK hung; out=%q", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "ERR 15") && !strings.Contains(got, "ERR15") {
		t.Fatalf("want ERR 15 after draining PK, got %q", got)
	}
}

func loadMITM(t *testing.T, kb string) string {
	t.Helper()
	b, err := os.ReadFile("samples/100,100/MITM.BAS")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if kb != "" && kb != "KB0:" {
		src = strings.Replace(src, `"KB0:"`, `"`+kb+`"`, 1)
	}
	return src
}

func TestMITMForwardsBannerAndStops(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), Config{MaxUsers: 6})
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 2)
	defer sys.Close()

	jobA, err := sys.Attach("A")
	if err != nil {
		t.Fatal(err)
	}
	var outA bytes.Buffer
	termA := &injectTerm{}
	shA := sys.newSession(jobA, &outA, termA)

	jobB, err := sys.Attach("B")
	if err != nil {
		t.Fatal(err)
	}
	var outB bytes.Buffer
	shB := sys.newSession(jobB, &outB, &quietTerm{})
	shB.Login("GUEST", "GUEST")
	outB.Reset()
	outA.Reset()

	src := loadMITM(t, shA.KB)
	if err := shB.Basic.LoadSource(src, "MITM"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- shB.Basic.RunProgram() }()

	start := time.Now()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(outB.String(), "Bye") && strings.Contains(outA.String(), "Bye") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tap := outB.String()
	remote := outA.String()
	if !strings.Contains(tap, "Bye") {
		shB.Basic.Interrupt()
		<-done
		t.Fatalf("MITM never showed PK output after %v. tap=%q remote=%q", time.Since(start), tap, remote)
	}
	if time.Since(start) > 400*time.Millisecond {
		shB.Basic.Interrupt()
		<-done
		t.Fatalf("PK banner took %v; MITM is sleeping between bytes. tap=%q", time.Since(start), tap)
	}
	time.Sleep(30 * time.Millisecond)

	before := outB.String()
	termA.feed("X")
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(outB.String(), "X") > strings.Count(before, "X") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	gotX := strings.Count(outB.String(), "X") - strings.Count(before, "X")
	if gotX != 1 {
		shB.Basic.Interrupt()
		<-done
		t.Fatalf("typed X should print once, got %d: %q", gotX, outB.String()[len(before):])
	}

	shB.Basic.Interrupt()
	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "interrupt") && !strings.Contains(err.Error(), "Programmable") {
			t.Fatalf("MITM exit: %v (tap=%q)", err, outB.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MITM did not stop on interrupt")
	}
}

type injectTerm struct {
	mu sync.Mutex
	in []byte
}

func (t *injectTerm) ReadLine(string) (string, error) { return "", os.ErrClosed }
func (t *injectTerm) ReadPassword(string) (string, error) {
	return t.ReadLine("")
}
func (t *injectTerm) GetByte(wait time.Duration) (byte, error) {
	t.mu.Lock()
	if len(t.in) > 0 {
		b := t.in[0]
		t.in = t.in[1:]
		t.mu.Unlock()
		return b, nil
	}
	t.mu.Unlock()
	if wait >= 0 {
		return 0, errWaitTimeout
	}
	return 0, os.ErrClosed
}
func (t *injectTerm) feed(s string) {
	t.mu.Lock()
	t.in = append(t.in, s...)
	t.mu.Unlock()
}

// liveTerm is a stolen serial: ReadLine blocks at Bye until OPEN KBn:
// interrupts it, while GetByte is what MITM uses afterwards.
type liveTerm struct {
	injectTerm
	mu    sync.Mutex
	block chan struct{}
}

func (t *liveTerm) InterruptRead() {
	t.mu.Lock()
	if t.block != nil {
		select {
		case <-t.block:
		default:
			close(t.block)
		}
	}
	t.mu.Unlock()
}

func (t *liveTerm) ReadLine(string) (string, error) {
	t.mu.Lock()
	if t.block == nil {
		t.block = make(chan struct{})
	}
	ch := t.block
	t.mu.Unlock()
	<-ch
	t.mu.Lock()
	t.block = make(chan struct{})
	t.mu.Unlock()
	return "", errForced
}

func TestMITMRemoteLogin(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), Config{MaxUsers: 6})
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 2)
	defer sys.Close()

	jobA, err := sys.Attach("A")
	if err != nil {
		t.Fatal(err)
	}
	var outA bytes.Buffer
	termA := &liveTerm{}
	shA := sys.newSession(jobA, &outA, termA)
	go shA.Run()
	defer func() {
		shA.Running = false
		termA.InterruptRead()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(outA.String(), "Bye") {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(outA.String(), "Bye") {
		t.Fatalf("victim never reached Bye: %q", outA.String())
	}

	jobB, err := sys.Attach("B")
	if err != nil {
		t.Fatal(err)
	}
	var outB bytes.Buffer
	shB := sys.newSession(jobB, &outB, &quietTerm{})
	shB.Login("GUEST", "GUEST")
	outB.Reset()

	if err := shB.Basic.LoadSource(loadMITM(t, shA.KB), "MITM"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- shB.Basic.RunProgram() }()
	defer func() {
		shB.Basic.Interrupt()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()

	alive := func(stage, want string) {
		t.Helper()
		select {
		case err := <-done:
			t.Fatalf("MITM died during %s waiting for %q: %v\ntap=%q\nremote=%q", stage, want, err, outB.String(), outA.String())
		default:
		}
	}
	waitRemote := func(stage, want string, d time.Duration) {
		t.Helper()
		until := time.Now().Add(d)
		for time.Now().Before(until) {
			alive(stage, want)
			if strings.Contains(outA.String(), want) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("remote never saw %q during %s\ntap=%q\nremote=%q", want, stage, outB.String(), outA.String())
	}

	waitRemote("banner", "RSTS", time.Second)

	typeLine := func(s string) {
		for i := 0; i < len(s); i++ {
			termA.feed(s[i : i+1])
			time.Sleep(15 * time.Millisecond)
		}
		termA.feed("\r\n")
	}

	typeLine("")
	time.Sleep(200 * time.Millisecond)
	alive("enter", "Bye")

	typeLine("hello 100,100")
	waitRemote("hello", "Password:", 2*time.Second)

	typeLine("GUEST")
	waitRemote("password", "Ready", 3*time.Second)
	if strings.Contains(outA.String(), "Invalid") {
		t.Fatalf("login failed\nremote=%q\ntap=%q", outA.String(), outB.String())
	}

	before := outA.String()
	typeLine("dir")
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		alive("dir", "listing")
		got := outA.String()[len(before):]
		if strings.Contains(got, "NIM") || strings.Contains(got, ".BAS") || strings.Contains(got, "Name") {
			if !strings.Contains(outB.String(), "GUEST") {
				t.Fatalf("tap should show the password GUEST even though PK did not echo it; tap=%q", outB.String())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("DIR did not list files\nnew remote=%q\ntap=%q", outA.String()[len(before):], outB.String())
}
