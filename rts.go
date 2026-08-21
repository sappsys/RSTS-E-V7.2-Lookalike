package rsts

import (
	"fmt"
	"strings"
	"unicode"
)

func (s *Shell) jobRTS() string {
	if s != nil && strings.EqualFold(s.rts, "RSX") {
		return "RSX"
	}
	return "BASIC"
}

func (s *Shell) cmdSwitch(rest string) error {
	name := strings.ToUpper(strings.TrimSpace(rest))
	if i := strings.IndexByte(name, '/'); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}
	if name == "" {
		fmt.Fprintf(s.out, "RTS is %s\n", s.jobRTS())
		return nil
	}
	switch {
	case name == "BASIC" || strings.HasPrefix("BASIC", name) || name == "BASIC+" || name == "BAC":
		s.rts = "BASIC"
		s.syncJob()
		return nil
	case name == "RSX" || strings.HasPrefix("RSX", name):
		if _, err := s.needLogin(); err != nil {
			return err
		}
		s.rts = "RSX"
		s.syncJob()
		return nil
	case name == "RT11" || strings.HasPrefix("RT11", name) || name == "RT11M":
		return fsErr("Can't find RTS")
	default:
		return fsErr("Can't find RTS")
	}
}

const helpRSX = `RSX MCR. Commands at > :

  SWITCH [BASIC|RSX]    run-time system (SWITCH BASIC returns to Ready)
  RUN filespec          run a .TSK
  BYE                   log off
  HELLO                 log in as another user
  HELP [command]        this list, or SWITCH / RUN / BYE / HELLO

This system does not execute MACRO-11 tasks. RUN of a .TSK is
?Can't find file or account, or ?Not a task if the file exists.
BASIC CUSPs (and numbered BASIC lines) need SWITCH BASIC.
`

const helpRSXRun = `RUN filespec

Under RSX the default extension is .TSK. BASIC-PLUS .BAS / .BAC and
Pascal are Wrong RTS; SWITCH BASIC first.

This system does not execute MACRO-11. A missing file is
?Can't find file or account. A file named .TSK is ?Not a task.
`

const helpRSXBye = `HELLO [ppn]         log in (password prompt)
BYE                 log off
EXIT / QUIT         same as BYE here; at Bye they stop the emulator
                    on the console
`

func (s *Shell) cmdHelpRSX(rest string) {
	topic := strings.ToUpper(strings.TrimSpace(rest))
	if i := strings.IndexByte(topic, '/'); i >= 0 {
		topic = topic[:i]
	}
	name := matchRSXHelp(topic)
	if name == "" {
		t := strings.TrimSpace(rest)
		if t == "" {
			t = "that"
		}
		fmt.Fprintf(s.out, "?No help on %s\n", t)
		fmt.Fprintln(s.out, "Type HELP for a list of commands.")
		return
	}
	switch name {
	case "HELP":
		fmt.Fprintln(s.out, strings.TrimRight(helpRSX, "\n"))
	case "SWITCH":
		fmt.Fprintln(s.out, strings.TrimRight(helpText["SWITCH"], "\n"))
	case "RUN":
		fmt.Fprintln(s.out, strings.TrimRight(helpRSXRun, "\n"))
	case "BYE":
		fmt.Fprintln(s.out, strings.TrimRight(helpRSXBye, "\n"))
	}
}

func matchRSXHelp(topic string) string {
	if topic == "" {
		return "HELP"
	}
	opts := []struct{ name, canon string }{
		{"HELP", "HELP"}, {"HLP", "HELP"},
		{"SWITCH", "SWITCH"}, {"RSX", "SWITCH"}, {"RTS", "SWITCH"},
		{"RUN", "RUN"},
		{"BYE", "BYE"}, {"LOGOUT", "BYE"}, {"EXIT", "BYE"}, {"QUIT", "BYE"},
		{"HELLO", "BYE"}, {"LOGIN", "BYE"},
	}
	for _, o := range opts {
		if o.name == topic {
			return o.canon
		}
	}
	canon := ""
	for _, o := range opts {
		if !strings.HasPrefix(o.name, topic) {
			continue
		}
		if canon == "" {
			canon = o.canon
		} else if canon != o.canon {
			return ""
		}
	}
	return canon
}

func (s *Shell) rsxTurn() error {
	fmt.Fprint(s.out, ">")
	line, err := s.readLine("")
	if err != nil {
		return err
	}
	line = strings.TrimRight(line, "\n")
	if strings.TrimSpace(line) == "" {
		return nil
	}
	stripped := strings.TrimLeft(line, " \t")
	if stripped != "" && unicode.IsDigit(rune(stripped[0])) {
		fmt.Fprintln(s.out, "?Wrong RTS")
		return nil
	}
	verb, rest := splitVerb(stripped)
	switch verb {
	case "SWITCH":
		return s.cmdSwitch(rest)
	case "RUN":
		s.cmdRun(rest, false)
		return nil
	case "BYE", "LOGOUT", "EXIT", "QUIT":
		s.cmdBye(rest)
		return nil
	case "HELP", "HLP":
		s.cmdHelp(rest)
		return nil
	case "HELLO", "LOGIN":
		s.cmdHello(rest)
		return nil
	default:
		fmt.Fprintln(s.out, "?Invalid command")
		return nil
	}
}

func (s *Shell) runRSX(rest string) {
	name := strings.TrimSpace(rest)
	if name == "" {
		fmt.Fprintln(s.out, "?Can't find file or account")
		return
	}
	spec, err := s.parseSpec(name, "TSK")
	if err != nil {
		fmt.Fprintf(s.out, "?%s\n", strings.TrimPrefix(err.Error(), "?"))
		return
	}
	if spec.ExtGiven && spec.Ext != "TSK" {
		fmt.Fprintln(s.out, "?Wrong RTS")
		return
	}
	spec.Ext = "TSK"
	acct, err := s.needLogin()
	if err != nil {
		fmt.Fprintf(s.out, "?%s\n", strings.TrimPrefix(err.Error(), "?"))
		return
	}
	if s.Disk.Exists(spec, acct.Proj, acct.Prog, s.priv()) {
		fmt.Fprintln(s.out, "?Not a task")
		return
	}
	fmt.Fprintln(s.out, "?Can't find file or account")
}
