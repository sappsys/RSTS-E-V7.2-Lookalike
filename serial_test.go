package rsts

import (
	"reflect"
	"strings"
	"testing"
)

func TestConfigSerialList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`serial = "/dev/ttyUSB0"`, []string{"/dev/ttyUSB0"}},
		{`serial = "/dev/ttyUSB0,/dev/ttyS0"`, []string{"/dev/ttyUSB0", "/dev/ttyS0"}},
		{`serial = "/dev/ttyUSB0, /dev/ttyS0 , /dev/ttyS1"`,
			[]string{"/dev/ttyUSB0", "/dev/ttyS0", "/dev/ttyS1"}},
		{`serial = ""`, nil},
		{`serial = " , "`, nil},
		{`serial = "/dev/ttyS0"  # one line`, []string{"/dev/ttyS0"}},
	}
	for _, c := range cases {
		cfg := DefaultConfig()
		if err := parseTOML(c.in, &cfg); err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		cfg.clamp()
		if !reflect.DeepEqual(cfg.Serial, c.want) {
			t.Errorf("%s gave %#v, want %#v", c.in, cfg.Serial, c.want)
		}
	}
}

func TestConfigSerialDefaultsToNone(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.Serial) != 0 {
		t.Fatalf("default config has serial lines: %#v", cfg.Serial)
	}
	// The file written on first run must parse back to the same thing.
	if err := parseTOML(defaultConfigTOML, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Serial) != 0 {
		t.Fatalf("the default config.toml turns on serial lines: %#v", cfg.Serial)
	}
	if !strings.Contains(defaultConfigTOML, "serial") {
		t.Fatal("the default config.toml should mention serial")
	}
}

func TestSerialTermLineEditing(t *testing.T) {
	f, peer, cleanup := serialPair(t)
	defer cleanup()

	term := newSerialTerm(f)
	go func() {
		// "HELLO" with a typo rubbed out, then Return.
		peer.WriteString("HELXO\b\bLO\r")
	}()
	line, err := term.ReadLine("")
	if err != nil {
		t.Fatal(err)
	}
	if line != "HELLO" {
		t.Fatalf("read %q, want HELLO", line)
	}

	go peer.WriteString("SECRET\r")
	pw, err := term.ReadPassword("Password: ")
	if err != nil {
		t.Fatal(err)
	}
	if pw != "SECRET" {
		t.Fatalf("password %q", pw)
	}
	if echoed := peer.ReadAvailable(); strings.Contains(echoed, "SECRET") {
		t.Fatalf("the password was echoed back: %q", echoed)
	}
}

func TestSerialTermTranslatesNewlines(t *testing.T) {
	f, peer, cleanup := serialPair(t)
	defer cleanup()

	term := newSerialTerm(f)
	if _, err := term.Write([]byte("one\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	got := peer.ReadAvailable()
	if got != "one\r\ntwo\r\n" {
		t.Fatalf("wrote %q, want CR LF endings", got)
	}
}

func TestSerialTermInterrupt(t *testing.T) {
	f, peer, cleanup := serialPair(t)
	defer cleanup()

	term := newSerialTerm(f)
	peer.WriteString("\x03")
	if !waitFor(func() bool { return term.PollInterrupt() }) {
		t.Fatal("Ctrl-C on the line was not seen")
	}

	// Other bytes are kept rather than eaten by the poll.
	peer.WriteString("AB\x03CD\r")
	if !waitFor(func() bool { return term.PollInterrupt() }) {
		t.Fatal("Ctrl-C in the middle of a line was not seen")
	}
	line, err := term.ReadLine("")
	if err != nil {
		t.Fatal(err)
	}
	if line != "ABCD" {
		t.Fatalf("kept %q, want ABCD", line)
	}
}

// A whole login over a real line, the same way a terminal on a serial
// port would drive it.
func TestSerialLineLogsIn(t *testing.T) {
	f, peer, cleanup := serialPair(t)
	defer cleanup()

	sys, err := NewSystem(t.TempDir(), Config{MaxUsers: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 0)
	defer sys.Close()

	job, err := sys.Attach("SERIAL test")
	if err != nil {
		t.Fatal(err)
	}
	term := newSerialTerm(f)
	go sys.runOnTerm(job, term, term, "SERIAL test", "", false)

	if !peer.WaitFor("Bye") {
		t.Fatalf("no Bye prompt on the line:\n%s", peer.Seen())
	}
	peer.WriteString("HELLO GUEST\r")
	if !peer.WaitFor("Password") {
		t.Fatalf("no password prompt:\n%s", peer.Seen())
	}
	peer.WriteString("GUEST\r")
	if !peer.WaitFor("100,100") {
		t.Fatalf("did not log in:\n%s", peer.Seen())
	}
	if !peer.WaitFor("Ready") {
		t.Fatalf("no Ready prompt:\n%s", peer.Seen())
	}

	peer.WriteString("PRINT 6*7\r")
	if !peer.WaitFor("42") {
		t.Fatalf("BASIC did not answer:\n%s", peer.Seen())
	}
	peer.WriteString("BYE\r")
	if !peer.WaitFor("logged off") {
		t.Fatalf("did not log off:\n%s", peer.Seen())
	}
}
