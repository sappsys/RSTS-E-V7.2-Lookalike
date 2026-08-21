package rsts

import (
	"embed"
	"strings"
)

//go:embed cusps/*.BAS
var cuspFS embed.FS

// Library CUSPs live in [1,2] as BASIC-PLUS .BAS / .BAC. The keyboard
// verb RUNs that image. LOGIN / HELLO / BYE stay keyboard monitors.
var libraryCUSPFile = map[string]string{
	"PIP":     "PIP",
	"DIR":     "DIRECT",
	"CAT":     "DIRECT",
	"CATALOG": "DIRECT",
	"SYSTAT":  "SYSTAT",
	"SYS":     "SYSTAT",
	"WHO":     "SYSTAT",
	"QUE":     "QUE",
	"QUMRUN":  "QUMRUN",
	"PLEASE":  "PLEASE",
	"OPR":     "PLEASE",
	"BACKUP":  "BACKUP",
	"BCK":     "BACKUP",
	"SUBMIT":  "SUBMIT",
	"BATCH":   "SUBMIT",
	"QUOLST":  "QUOLST",
	"QUOTA":   "QUOLST",
	"UTILITY": "UTILITY",
	"TTYSET":  "TTYSET",
	"CCL":     "CCL",
}

func isLibraryCUSP(name string) bool {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "PIP", "DIRECT", "SYSTAT", "QUE", "QUMRUN", "PLEASE", "BACKUP", "SUBMIT", "QUOLST", "UTILITY", "TTYSET", "CCL":
		return true
	}
	return false
}

func (s *Shell) runLibraryCUSP(verb, rest string) error {
	if s.jobRTS() == "RSX" {
		return fsErr("Wrong RTS")
	}
	if _, err := s.needLogin(); err != nil {
		return err
	}
	name := libraryCUSPFile[verb]
	if name == "" {
		name = verb
	}
	if verb == "WHO" {
		rest = strings.TrimSpace("/U " + rest)
	}
	specName := "$" + name + ".BAC"
	spec, err := s.parseSpec(specName, "BAC")
	if err != nil || s.Disk == nil || s.Account == nil {
		return s.execCUSPNamed(name, rest)
	}
	if !s.Disk.Exists(spec, s.Account.Proj, s.Account.Prog, s.priv()) {
		return s.execCUSPNamed(name, rest)
	}
	s.cclArg = rest
	s.Basic.IO.CCLLine = rest
	s.cmdRun(specName, false)
	return nil
}

func (s *Shell) execCUSPNamed(name, rest string) error {
	switch strings.ToUpper(name) {
	case "PIP":
		return s.cmdPip(rest)
	case "DIRECT":
		return s.cmdDir(rest)
	case "SYSTAT":
		s.cmdSystat(rest)
		return nil
	case "QUE":
		return s.cmdQue(rest)
	case "QUMRUN":
		return s.cmdQumrun(rest)
	case "PLEASE":
		return s.cmdPlease(rest)
	case "BACKUP":
		return s.cmdBackup(rest)
	case "SUBMIT":
		return s.cmdSubmit(rest)
	case "QUOLST":
		return s.cmdQuolst(rest)
	case "UTILITY":
		return s.cmdUtility(rest)
	case "TTYSET":
		return s.cmdSet(rest)
	case "CCL":
		return s.cmdCCL(rest)
	default:
		return fsErr("Can't find file or account")
	}
}

func libraryCUSPFiles() map[string]string {
	out := map[string]string{}
	ents, err := cuspFS.ReadDir("cusps")
	if err != nil {
		return out
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		data, err := cuspFS.ReadFile("cusps/" + e.Name())
		if err != nil {
			continue
		}
		name := strings.ToUpper(e.Name())
		body := string(data)
		out[name] = body
		if strings.HasSuffix(name, ".BAS") {
			out[strings.TrimSuffix(name, ".BAS")+".BAC"] = body
		}
	}
	return out
}

func (sys *System) seedDefaultCCL() error {
	if sys == nil {
		return nil
	}
	if len(sys.loadCCL()) > 0 {
		return nil
	}
	entries := []cclEntry{
		{Name: "PIP", Spec: "$PIP.BAC"},
		{Name: "DIR", Spec: "$DIRECT.BAC"},
		{Name: "CAT", Spec: "$DIRECT.BAC"},
		{Name: "SYSTAT", Spec: "$SYSTAT.BAC"},
		{Name: "QUE", Spec: "$QUE.BAC"},
		{Name: "PLEASE", Spec: "$PLEASE.BAC"},
		{Name: "BACKUP", Spec: "$BACKUP.BAC"},
		{Name: "SUBMIT", Spec: "$SUBMIT.BAC"},
		{Name: "QUOLST", Spec: "$QUOLST.BAC"},
		{Name: "UTILITY", Spec: "$UTILITY.BAC"},
		{Name: "TTYSET", Spec: "$TTYSET.BAC"},
		{Name: "CCL", Spec: "$CCL.BAC"},
	}
	return sys.saveCCL(entries)
}
