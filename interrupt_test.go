package rsts

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCtrlCStopsProgram(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	if err := m.LoadSource("10 GOTO 10\n", "SPIN"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- m.RunProgram()
	}()
	time.Sleep(30 * time.Millisecond)
	m.Interrupt()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupt) {
			t.Fatalf("want interrupt, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not stop on interrupt")
	}
}

func TestCtrlCNotTrapped(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	src := "10 ON ERROR GOTO 50\n20 GOTO 20\n50 PRINT \"TRAPPED\"\n60 RESUME 20\n"
	if err := m.LoadSource(src, "SPIN"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- m.RunProgram()
	}()
	time.Sleep(30 * time.Millisecond)
	m.Interrupt()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupt) {
			t.Fatalf("want interrupt, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	if strings.Contains(c.out.String(), "TRAPPED") {
		t.Fatal("ON ERROR should not catch Ctrl-C")
	}
}

func TestLoggedOutExitHalts(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Running = true
	sh.cmdHalt()
	if sh.Running {
		t.Fatal("EXIT should end the console session")
	}
	if !sh.sys.Halted() {
		t.Fatal("EXIT at console should halt the emulator")
	}
}
