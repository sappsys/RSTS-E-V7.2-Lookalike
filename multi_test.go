package rsts

import (
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	cfg := DefaultConfig()
	src := `
max_users = 12 # jobs
telnet_port = 2323
telnet_bind = "127.0.0.1"
telnet = false
console = yes
disk = "./pack"
guest = true
login = "GUEST"
`
	if err := parseTOML(src, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.MaxUsers != 12 || cfg.TelnetPort != 2323 || cfg.TelnetBind != "127.0.0.1" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Telnet || !cfg.Console {
		t.Fatalf("bools %+v", cfg)
	}
	if cfg.Disk != "./pack" || !cfg.Guest || cfg.Login != "GUEST" {
		t.Fatalf("login %+v", cfg)
	}
}

func TestLoadConfigWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg, got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("path %s", got)
	}
	if cfg.MaxUsers != 25 || cfg.TelnetPort != 23 {
		t.Fatalf("%+v", cfg)
	}
}

func TestJobTable(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), Config{MaxUsers: 2, Telnet: false, Console: false})
	if err != nil {
		t.Fatal(err)
	}
	j1, err := sys.Attach("a")
	if err != nil || j1.Num != 1 || j1.KB != "KB0:" {
		t.Fatalf("j1 %+v %v", j1, err)
	}
	j2, err := sys.Attach("b")
	if err != nil || j2.Num != 2 || j2.KB != "KB1:" {
		t.Fatalf("j2 %+v %v", j2, err)
	}
	if _, err := sys.Attach("c"); err != ErrBusy {
		t.Fatalf("want busy, got %v", err)
	}
	sys.Detach(j1.Num)
	j3, err := sys.Attach("c")
	if err != nil || j3.Num != 1 {
		t.Fatalf("reuse %+v %v", j3, err)
	}
	if n := sys.JobCount(); n != 2 {
		t.Fatalf("count %d", n)
	}
}

// waitJobs lets spawned sessions finish writing to the disk before the
// test framework deletes the directory underneath them.
func waitJobs(sys *System, want int) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && sys.JobCount() > want {
		time.Sleep(10 * time.Millisecond)
	}
}

func waitNoJobs(sys *System) { waitJobs(sys, 0) }

func TestTelnetLogin(t *testing.T) {
	cfg := Config{MaxUsers: 3, Telnet: true, TelnetPort: 0, TelnetBind: "127.0.0.1", Console: false}
	sys, err := NewSystem(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer waitNoJobs(sys)
	defer sys.Close()
	addr, err := sys.StartTelnet()
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	time.Sleep(50 * time.Millisecond)
	_, _ = c.Write([]byte("HELLO GUEST\r\nGUEST\r\n"))
	deadline := time.Now().Add(2 * time.Second)
	var got strings.Builder
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := c.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
		}
		if strings.Contains(got.String(), "100,100") {
			return
		}
		if err != nil && !isTimeout(err) {
			break
		}
	}
	t.Fatalf("login output: %q", got.String())
}

func isTimeout(err error) bool {
	n, ok := err.(net.Error)
	return ok && n.Timeout()
}

func TestTelnetBusy(t *testing.T) {
	cfg := Config{MaxUsers: 1, Telnet: true, TelnetPort: 0, TelnetBind: "127.0.0.1", Console: false}
	sys, err := NewSystem(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer waitNoJobs(sys)
	defer sys.Close()
	addr, err := sys.StartTelnet()
	if err != nil {
		t.Fatal(err)
	}
	c1, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sys.JobCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	c2, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	_ = c2.SetDeadline(time.Now().Add(2 * time.Second))
	all, _ := io.ReadAll(c2)
	if !strings.Contains(string(all), "Maximum users") && !strings.Contains(string(all), "exceeded") {
		t.Fatalf("busy: %q", all)
	}
}

func TestSystatAndPK(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	// The PK: children below outlive the body of the test; only this
	// console job should still be there when the directory goes away.
	defer waitJobs(sh.sys, 1)
	sh.Login("GUEST", "GUEST")
	var out strings.Builder
	sh.out = &out
	sh.cmdSystat("")
	got := out.String()
	if !strings.Contains(got, "Job") || !strings.Contains(got, "State") || !strings.Contains(got, "KB0:") {
		t.Fatalf("systat: %q", got)
	}
	if !strings.Contains(got, SystemName) || strings.Contains(got, "V10") {
		t.Fatalf("systat version: %q", got)
	}

	if err := sh.openPK(sh.Basic, 1, -1); err != nil {
		t.Fatal(err)
	}
	f := sh.Basic.Files[1]
	if f == nil || f.pk == nil {
		t.Fatal("no pk channel")
	}
	deadline := time.Now().Add(2 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		gotCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			line, err := f.pk.ctrlReadLine(nil)
			if err != nil {
				errCh <- err
				return
			}
			gotCh <- line
		}()
		select {
		case line := <-gotCh:
			if strings.Contains(line, "RSTS") || strings.Contains(line, "PDP-11") {
				found = true
				goto done
			}
		case err := <-errCh:
			t.Fatal(err)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("no output from spawned PK: job")
		}
	}
done:
	if !found {
		t.Fatal("PK: job never printed a banner")
	}
	_ = f.pk.ctrlWrite("BYE\r")
	f.pk.Hangup()

	c := &capture{}
	sh.Basic.IO.Write = c.write
	if err := sh.Basic.LoadSource(samples["100,100"]["PK.BAS"], "PK"); err != nil {
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
		t.Fatal("PK.BAS timed out")
	}
}

func scoopConn(c net.Conn, b *strings.Builder, mu *sync.Mutex, stop <-chan struct{}) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-stop:
			return
		default:
		}
		_ = c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, err := c.Read(buf)
		if n > 0 {
			mu.Lock()
			b.Write(buf[:n])
			mu.Unlock()
		}
		if err != nil && !isTimeout(err) {
			return
		}
	}
}

func builderHas(mu *sync.Mutex, b *strings.Builder, want string) bool {
	mu.Lock()
	defer mu.Unlock()
	return strings.Contains(b.String(), want)
}

func builderStr(mu *sync.Mutex, b *strings.Builder) string {
	mu.Lock()
	defer mu.Unlock()
	return b.String()
}

func waitBuilder(mu *sync.Mutex, b *strings.Builder, want string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if builderHas(mu, b, want) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func telnetCollect(c net.Conn, d time.Duration) string {
	_ = c.SetReadDeadline(time.Now().Add(d))
	var got strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
		}
		if err != nil {
			return got.String()
		}
	}
}

func telnetWait(c net.Conn, want string, d time.Duration) string {
	deadline := time.Now().Add(d)
	var got strings.Builder
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := c.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
			if strings.Contains(got.String(), want) {
				return got.String()
			}
		}
		if err != nil && !isTimeout(err) {
			return got.String()
		}
	}
	return got.String()
}

func TestTelnetStolenGet(t *testing.T) {
	cfg := Config{MaxUsers: 6, Telnet: true, TelnetPort: 0, TelnetBind: "127.0.0.1", Console: false}
	sys, err := NewSystem(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 0)
	defer sys.Close()
	addr, err := sys.StartTelnet()
	if err != nil {
		t.Fatal(err)
	}

	kb0, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer kb0.Close()
	kb0out := telnetWait(kb0, "Bye", 2*time.Second)
	if !strings.Contains(kb0out, "Bye") {
		t.Fatalf("KB0 never reached Bye: %q", kb0out)
	}

	kb1, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer kb1.Close()
	_, _ = kb1.Write([]byte("HELLO GUEST\r\nGUEST\r\n"))
	kb1out := telnetWait(kb1, "Ready", 3*time.Second)
	if !strings.Contains(kb1out, "Ready") {
		t.Fatalf("KB1 did not log in: %q", kb1out)
	}

	src := `10 ON ERROR GOTO 100
20 OPEN "KB0:" AS FILE 1, RECORDSIZE 1
30 FIELD #1, 1 AS A$
40 WAIT 0
50 FOR I=1 TO 20
52 LSET A$="X"
54 PUT #1
56 NEXT I
60 GET #1
70 PRINT "GOT";ASC(A$)
80 GOTO 120
100 IF ERR=15 THEN SLEEP .05 \ RESUME 60
110 PRINT "ERR";ERR;" AT ";ERL
120 CLOSE 1
130 END
`
	// Type the program on KB1.
	for _, line := range strings.Split(strings.TrimRight(src, "\n"), "\n") {
		_, _ = kb1.Write([]byte(line + "\r\n"))
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = kb1.Write([]byte("RUN\r\n"))
	time.Sleep(300 * time.Millisecond)
	_, _ = kb0.Write([]byte("Q"))
	kb1out += telnetWait(kb1, "GOT", 3*time.Second)
	if !strings.Contains(kb1out, "GOT") {
		kb0out += telnetCollect(kb0, 200*time.Millisecond)
		t.Fatalf("WAIT 0 GET on stolen telnet KB0 failed\nKB1=%q\nKB0=%q", kb1out, kb0out)
	}
}

func TestTelnetMITM(t *testing.T) {
	cfg := Config{MaxUsers: 6, Telnet: true, TelnetPort: 0, TelnetBind: "127.0.0.1", Console: false}
	sys, err := NewSystem(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 0)
	defer sys.Close()
	addr, err := sys.StartTelnet()
	if err != nil {
		t.Fatal(err)
	}

	kb0, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer kb0.Close()
	if !strings.Contains(telnetWait(kb0, "Bye", 2*time.Second), "Bye") {
		t.Fatal("KB0 never reached Bye")
	}

	kb1, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer kb1.Close()
	_, _ = kb1.Write([]byte("HELLO GUEST\r\nGUEST\r\n"))
	if !strings.Contains(telnetWait(kb1, "Ready", 3*time.Second), "Ready") {
		t.Fatal("KB1 did not log in")
	}

	stop := make(chan struct{})
	defer close(stop)
	var tap, remote strings.Builder
	var mu sync.Mutex
	go scoopConn(kb0, &remote, &mu, stop)
	go scoopConn(kb1, &tap, &mu, stop)

	_, _ = kb1.Write([]byte("OLD MITM\r\nRUN\r\n"))
	if !waitBuilder(&mu, &tap, "Bye", 3*time.Second) {
		t.Fatalf("MITM tap never showed PK Bye: %q", builderStr(&mu, &tap))
	}
	if !waitBuilder(&mu, &remote, "Bye", 2*time.Second) && !waitBuilder(&mu, &remote, "RSTS", 2*time.Second) {
		t.Fatalf("stolen KB0 never saw PK banner: remote=%q tap=%q", builderStr(&mu, &remote), builderStr(&mu, &tap))
	}

	_, _ = kb0.Write([]byte("hello 100,100\r\n"))
	if !waitBuilder(&mu, &remote, "Password:", 3*time.Second) {
		t.Fatalf("typing on stolen KB0 did nothing; remote=%q tap=%q", builderStr(&mu, &remote), builderStr(&mu, &tap))
	}
	_, _ = kb0.Write([]byte("GUEST\r\n"))
	if !waitBuilder(&mu, &remote, "User:", 5*time.Second) && !waitBuilder(&mu, &remote, "Ready", 5*time.Second) {
		t.Fatalf("login through MITM failed; remote=%q tap=%q", builderStr(&mu, &remote), builderStr(&mu, &tap))
	}
	_, _ = kb0.Write([]byte("dir\r\n"))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := builderStr(&mu, &remote)
		if strings.Contains(got, ".BAS") || strings.Contains(got, "NIM") || strings.Contains(got, "Name") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("DIR through MITM failed; remote=%q tap=%q", builderStr(&mu, &remote), builderStr(&mu, &tap))
}

func TestTelnetMITMCtrlC(t *testing.T) {
	cfg := Config{MaxUsers: 6, Telnet: true, TelnetPort: 0, TelnetBind: "127.0.0.1", Console: false}
	sys, err := NewSystem(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 0)
	defer sys.Close()
	addr, err := sys.StartTelnet()
	if err != nil {
		t.Fatal(err)
	}

	kb0, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer kb0.Close()
	if !strings.Contains(telnetWait(kb0, "Bye", 2*time.Second), "Bye") {
		t.Fatal("KB0 never reached Bye")
	}

	kb1, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer kb1.Close()
	_, _ = kb1.Write([]byte("HELLO GUEST\r\nGUEST\r\n"))
	if !strings.Contains(telnetWait(kb1, "Ready", 3*time.Second), "Ready") {
		t.Fatal("KB1 did not log in")
	}

	stop := make(chan struct{})
	defer close(stop)
	var tap strings.Builder
	var mu sync.Mutex
	go scoopConn(kb1, &tap, &mu, stop)

	_, _ = kb1.Write([]byte("OLD MITM\r\nRUN\r\n"))
	if !waitBuilder(&mu, &tap, "Bye", 3*time.Second) {
		t.Fatalf("MITM did not start: %q", builderStr(&mu, &tap))
	}
	time.Sleep(200 * time.Millisecond)
	before := builderStr(&mu, &tap)
	_, _ = kb1.Write([]byte{3})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := builderStr(&mu, &tap)
		extra := ""
		if len(got) > len(before) {
			extra = got[len(before):]
		}
		if strings.Contains(extra, "Ready") || strings.Contains(extra, "^C") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = kb1.Write([]byte{255, 244}) // IAC IP
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := builderStr(&mu, &tap)
		extra := ""
		if len(got) > len(before) {
			extra = got[len(before):]
		}
		if strings.Contains(extra, "Ready") || strings.Contains(extra, "^C") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Ctrl-C did not stop MITM; tap=%q", builderStr(&mu, &tap))
}
