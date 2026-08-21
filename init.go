package rsts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"
)

type initState struct {
	Started  bool `json:"started"`
	MaxUsers int  `json:"max_users,omitempty"`
}

func (sys *System) initStatePath() string {
	return filepath.Join(sys.Disk.Root, "init.json")
}

func (sys *System) loadInitState() initState {
	var st initState
	data, err := os.ReadFile(sys.initStatePath())
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

func (sys *System) saveInitState(st initState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sys.initStatePath(), append(data, '\n'), 0o644)
}

func (sys *System) markTimesharing() {
	st := sys.loadInitState()
	st.Started = true
	if st.MaxUsers == 0 {
		st.MaxUsers = sys.Config.MaxUsers
	}
	_ = sys.saveInitState(st)
}

func shouldRunINIT(sys *System, force, guest bool, login string, console bool) bool {
	if !console || guest || strings.TrimSpace(login) != "" {
		return false
	}
	if force {
		return true
	}
	if sys.loadInitState().Started {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func applyInitState(sys *System) {
	st := sys.loadInitState()
	if st.MaxUsers >= 1 && st.MaxUsers <= MaxJobs {
		sys.Config.MaxUsers = st.MaxUsers
	}
}

// runINIT is the console operator dialogue before timesharing (START).
// Returns false if the operator exits without START.
func (s *Shell) runINIT() bool {
	fmt.Fprintf(s.out, "\n%s INIT   %s\n\n", SystemLong, CPUName)
	fmt.Fprintln(s.out, "Option: START, DSKINT, DEFAULT, LIST, HELP, EXIT")
	for {
		line, err := s.readLine("Option: ")
		if err != nil {
			return false
		}
		verb, rest := splitVerb(strings.TrimSpace(line))
		if verb == "" {
			continue
		}
		opt := matchInitOption(verb)
		switch opt {
		case "START":
			if s.sys != nil {
				s.sys.markTimesharing()
			}
			fmt.Fprintln(s.out, "Starting timesharing")
			return true
		case "DSKINT":
			s.initDskint(rest)
		case "DEFAULT":
			s.initDefault()
		case "LIST", "HARDWR":
			s.cmdHardware()
		case "HELP":
			fmt.Fprintln(s.out, "START     begin timesharing (Bye on KB0:)")
			fmt.Fprintln(s.out, "DSKINT    initialize a pack   DSKINT device: packid [/PUBLIC]")
			fmt.Fprintln(s.out, "DEFAULT   maximum jobs")
			fmt.Fprintln(s.out, "LIST      hardware")
			fmt.Fprintln(s.out, "EXIT      halt without starting")
		case "EXIT", "QUIT":
			return false
		default:
			fmt.Fprintln(s.out, "?Invalid option")
		}
	}
}

func matchInitOption(verb string) string {
	opts := []string{"START", "DSKINT", "DEFAULT", "LIST", "HARDWR", "HELP", "EXIT", "QUIT"}
	if verb == "" {
		return ""
	}
	for _, o := range opts {
		if o == verb {
			return o
		}
	}
	var hits []string
	for _, o := range opts {
		if strings.HasPrefix(o, verb) {
			hits = append(hits, o)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return verb
}

func (s *Shell) initDskint(rest string) {
	dev, unit, packID, sw, err := parseDiskCmd(rest)
	if err != nil || packID == "" {
		fmt.Fprintln(s.out, "?DSKINT device: packid [/PUBLIC]")
		return
	}
	public := sw["PUBLIC"] || sw["PUB"]
	if err := s.Disk.Initialize(dev, unit, packID, public, true); err != nil {
		fmt.Fprintf(s.out, "?%s\n", strings.TrimPrefix(err.Error(), "?"))
		return
	}
	fmt.Fprintf(s.out, "%s%d:  pack %s initialized\n", dev, unit, packID)
}

func (s *Shell) initDefault() {
	cur := 25
	if s.sys != nil && s.sys.Config.MaxUsers > 0 {
		cur = s.sys.Config.MaxUsers
	}
	fmt.Fprintf(s.out, "Maximum jobs <%d>? ", cur)
	line, err := s.readLine("")
	if err != nil {
		return
	}
	line = strings.TrimSpace(line)
	n := cur
	if line != "" {
		v, err := strconv.Atoi(line)
		if err != nil || v < 1 || v > MaxJobs {
			fmt.Fprintf(s.out, "?Jobs are 1 through %d\n", MaxJobs)
			return
		}
		n = v
	}
	if s.sys != nil {
		s.sys.Config.MaxUsers = n
		st := s.sys.loadInitState()
		st.MaxUsers = n
		_ = s.sys.saveInitState(st)
	}
	fmt.Fprintf(s.out, "Maximum jobs = %d\n", n)
}
