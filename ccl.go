package rsts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type cclEntry struct {
	Name string `json:"name"`
	Spec string `json:"spec"`
	Priv bool   `json:"priv,omitempty"`
}

type cclFile struct {
	Entries []cclEntry `json:"entries"`
}

func (sys *System) cclPath() string {
	return filepath.Join(sys.Disk.Root, "ccl.json")
}

func (sys *System) loadCCL() []cclEntry {
	data, err := os.ReadFile(sys.cclPath())
	if err != nil {
		return nil
	}
	var f cclFile
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	return f.Entries
}

func (sys *System) saveCCL(entries []cclEntry) error {
	data, err := json.MarshalIndent(cclFile{Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sys.cclPath(), append(data, '\n'), 0o644)
}

func (sys *System) lookupCCL(verb string) (cclEntry, bool) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	verb = strings.ToUpper(verb)
	var hit cclEntry
	n := 0
	for _, e := range sys.loadCCL() {
		name := strings.ToUpper(e.Name)
		if name == verb || strings.HasPrefix(name, verb) {
			hit = e
			n++
			if name == verb {
				return e, true
			}
		}
	}
	return hit, n == 1
}

func (s *Shell) cmdCCL(rest string) error {
	if s.sys == nil {
		fmt.Fprintln(s.out, "?CCL")
		return nil
	}
	if _, err := s.needLogin(); err != nil {
		return err
	}
	sw, arg := parseCmdSwitches(rest)
	if switchOn(sw, "DE", "DELETE") {
		if !s.priv() {
			return basicErr("Protection violation")
		}
		name := strings.ToUpper(strings.TrimSpace(arg))
		if name == "" {
			fmt.Fprintln(s.out, "?CCL/DE name")
			return nil
		}
		s.sys.mu.Lock()
		defer s.sys.mu.Unlock()
		entries := s.sys.loadCCL()
		kept := entries[:0]
		for _, e := range entries {
			if strings.ToUpper(e.Name) != name {
				kept = append(kept, e)
			}
		}
		return s.sys.saveCCL(kept)
	}
	if arg == "" {
		s.sys.mu.Lock()
		entries := s.sys.loadCCL()
		s.sys.mu.Unlock()
		if len(entries) == 0 {
			fmt.Fprintln(s.out, "%No CCLs installed")
			return nil
		}
		fmt.Fprintf(s.out, "%-10s  %s\n", "Name", "Program")
		for _, e := range entries {
			fmt.Fprintf(s.out, "%-10s  %s\n", e.Name, e.Spec)
		}
		return nil
	}
	if !s.priv() {
		return basicErr("Protection violation")
	}
	i := strings.IndexByte(arg, '=')
	if i < 0 {
		fmt.Fprintln(s.out, "?CCL name=filespec")
		return nil
	}
	name := strings.ToUpper(strings.TrimSpace(arg[:i]))
	spec := strings.TrimSpace(arg[i+1:])
	if name == "" || spec == "" {
		fmt.Fprintln(s.out, "?CCL name=filespec")
		return nil
	}
	s.sys.mu.Lock()
	defer s.sys.mu.Unlock()
	entries := s.sys.loadCCL()
	found := false
	for i := range entries {
		if strings.EqualFold(entries[i].Name, name) {
			entries[i] = cclEntry{Name: name, Spec: spec}
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, cclEntry{Name: name, Spec: spec})
	}
	return s.sys.saveCCL(entries)
}

func (s *Shell) runCCL(verb, rest string) bool {
	if s.sys == nil || s.Account == nil {
		return false
	}
	if s.jobRTS() == "RSX" {
		fmt.Fprintln(s.out, "?Wrong RTS")
		return true
	}
	e, ok := s.sys.lookupCCL(verb)
	if !ok {
		return false
	}
	s.cclArg = rest
	s.Basic.IO.CCLLine = rest
	s.cmdRun(e.Spec, false)
	return true
}
