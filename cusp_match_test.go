package rsts

import (
	"regexp"
	"strings"
	"testing"
)

func TestCUSPMatchesMonitor(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("SAVE HELLO")

	cmp := func(name string, cusp func(), builtin func() error) {
		t.Helper()
		out.Reset()
		cusp()
		got := strings.TrimRight(out.String(), "\n")
		out.Reset()
		if err := builtin(); err != nil {
			t.Fatalf("%s builtin: %v", name, err)
		}
		want := strings.TrimRight(out.String(), "\n")
		if strings.HasPrefix(name, "SYSTAT") || name == "WHO" {
			got = strings.ReplaceAll(got, "RN", "KB")
			clock := regexp.MustCompile(`\d+:\d{2}\.\d+`)
			got = clock.ReplaceAllString(got, "T")
			want = clock.ReplaceAllString(want, "T")
		}
		if got != want {
			t.Errorf("%s\nCUSP (%d):\n%s\nGo (%d):\n%s", name, len(got), got, len(want), want)
		}
	}

	cmp("DIR", func() { sh.Dispatch("DIR") }, func() error { return sh.cmdDir("") })
	cmp("DIR/S", func() { sh.Dispatch("DIR/S") }, func() error { return sh.cmdDir("/S") })
	cmp("DIR/P", func() { sh.Dispatch("DIR/P") }, func() error { return sh.cmdDir("/P") })
	cmp("DIR/A", func() { sh.Dispatch("DIR/A") }, func() error { return sh.cmdDir("/A") })
	cmp("DIR/C", func() { sh.Dispatch("DIR/C") }, func() error { return sh.cmdDir("/C") })
	cmp("DIR/W", func() { sh.Dispatch("DIR/W") }, func() error { return sh.cmdDir("/W") })
	cmp("DIR/B", func() { sh.Dispatch("DIR/B") }, func() error { return sh.cmdDir("/B") })
	cmp("DIR/SU", func() { sh.Dispatch("DIR/SU") }, func() error { return sh.cmdDir("/SU") })
	cmp("DIR/N", func() { sh.Dispatch("DIR/N") }, func() error { return sh.cmdDir("/N") })
	cmp("DIR HELLO.BAS", func() { sh.Dispatch("DIR HELLO.BAS") }, func() error { return sh.cmdDir("HELLO.BAS") })
	cmp("CAT", func() { sh.Dispatch("CAT") }, func() error { return sh.cmdDir("") })
	cmp("PIP/LI", func() { sh.Dispatch("PIP/LI") }, func() error { return sh.cmdDir("") })
	cmp("PIP/HE", func() { sh.Dispatch("PIP/HE") }, func() error { return sh.cmdPip("/HE") })

	cmp("SYSTAT", func() { sh.Dispatch("SYSTAT") }, func() error { sh.cmdSystat(""); return nil })
	cmp("SYSTAT/F", func() { sh.Dispatch("SYSTAT/F") }, func() error { sh.cmdSystat("/F"); return nil })
	cmp("SYSTAT/N", func() { sh.Dispatch("SYSTAT/N") }, func() error { sh.cmdSystat("/N"); return nil })
	cmp("SYSTAT/U", func() { sh.Dispatch("SYSTAT/U") }, func() error { sh.cmdSystat("/U"); return nil })
	cmp("WHO", func() { sh.Dispatch("WHO") }, func() error { sh.cmdSystat("/U "); return nil })
	cmp("SYSTAT/D", func() { sh.Dispatch("SYSTAT/D") }, func() error { sh.cmdSystat("/D"); return nil })
	cmp("SYSTAT/K", func() { sh.Dispatch("SYSTAT/K") }, func() error { sh.cmdSystat("/K"); return nil })
	cmp("SYSTAT/R", func() { sh.Dispatch("SYSTAT/R") }, func() error { sh.cmdSystat("/R"); return nil })
	cmp("SYSTAT/M", func() { sh.Dispatch("SYSTAT/M") }, func() error { sh.cmdSystat("/M"); return nil })
	cmp("SYSTAT/S", func() { sh.Dispatch("SYSTAT/S") }, func() error { sh.cmdSystat("/S"); return nil })
	cmp("SYSTAT/B", func() { sh.Dispatch("SYSTAT/B") }, func() error { sh.cmdSystat("/B"); return nil })
	cmp("SYSTAT/H", func() { sh.Dispatch("SYSTAT/H") }, func() error { sh.cmdSystat("/H"); return nil })

	cmp("QUE", func() { sh.Dispatch("QUE") }, func() error { return sh.cmdQue("") })
	cmp("QUOLST", func() { sh.Dispatch("QUOLST") }, func() error { return sh.cmdQuolst("") })
	cmp("TTYSET", func() { sh.Dispatch("TTYSET") }, func() error { return sh.cmdSet("") })

	out.Reset()
	sh.Dispatch("QUE HELLO.BAS")
	cmp("QUE list", func() { sh.Dispatch("QUE") }, func() error { return sh.cmdQue("") })
}

func TestSystemCUSPMatchesMonitor(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("SYSTEM", "SYSTEM")
	var out strings.Builder
	sh.out = &out

	cmp := func(name string, cusp func(), builtin func() error) {
		t.Helper()
		out.Reset()
		cusp()
		got := strings.TrimRight(out.String(), "\n")
		out.Reset()
		if err := builtin(); err != nil {
			t.Fatalf("%s builtin: %v", name, err)
		}
		want := strings.TrimRight(out.String(), "\n")
		if got != want {
			t.Errorf("%s\nCUSP (%d):\n%s\nGo (%d):\n%s", name, len(got), got, len(want), want)
		}
	}

	cmp("CCL", func() { sh.Dispatch("CCL") }, func() error { return sh.cmdCCL("") })
	cmp("UTILITY", func() { sh.Dispatch("UTILITY") }, func() error { return sh.cmdUtility("") })
	cmp("PLEASE/LI", func() { sh.Dispatch("PLEASE/LI") }, func() error { return sh.cmdPlease("/LI") })
	cmp("DIR $*.BAS", func() { sh.Dispatch("DIR $*.BAS") }, func() error { return sh.cmdDir("$*.BAS") })
}
