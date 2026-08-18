package rsts

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The character devices a program can OPEN alongside disk files.
//
//	KB:  TT:   this job's terminal, or KBn: for another one
//	LP:        the line printer, spooled to a file in the account
//	NL:        the null device
//
// RSTS reached all of these through the same OPEN as a file, which is why
// a program can be handed a channel and not care what is on the end of it.

// parseCharDevice recognises a device name with no file part, or a
// printer with a spool file named after the colon.
func parseCharDevice(path string) (dev string, unit int, unitSet bool, rest string, ok bool) {
	s := strings.ToUpper(strings.TrimSpace(path))
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", 0, false, "", false
	}
	name, rest := s[:i], strings.TrimSpace(s[i+1:])
	j := len(name)
	for j > 0 && name[j-1] >= '0' && name[j-1] <= '9' {
		j--
	}
	if j < len(name) {
		unit, _ = strconv.Atoi(name[j:])
		unitSet = true
	}
	dev = name[:j]
	switch dev {
	case "KB", "TT":
		// A terminal takes no file name.
		return dev, unit, unitSet, "", rest == ""
	case "LP":
		return dev, unit, unitSet, rest, true
	case "NL":
		return dev, unit, unitSet, "", rest == ""
	}
	return "", 0, false, "", false
}

// kbDev is a terminal on a channel. Writing to another job's keyboard is
// the same privilege as FORCE and BROADCAST, and only your own terminal
// can be read.
type kbDev struct {
	out  io.Writer
	self *Shell
}

func (d *kbDev) devWrite(text string) error {
	_, err := fmt.Fprint(d.out, text)
	return err
}

func (d *kbDev) devReadLine() (string, error) {
	if d.self == nil {
		return "", basicErr("I/O error")
	}
	return d.self.readLine("")
}

func (d *kbDev) devClose() {}

// nullDev swallows output and is at end of file straight away.
type nullDev struct{}

func (nullDev) devWrite(string) error        { return nil }
func (nullDev) devReadLine() (string, error) { return "", io.EOF }
func (nullDev) devClose()                    {}

// openCharDevice attaches one of the character devices to a channel.
func (s *Shell) openCharDevice(m *Machine, channel int, dev string, unit int, unitSet bool, rest, mode string) error {
	switch dev {
	case "KB", "TT":
		target := s
		if unitSet && s.sys != nil {
			want := fmt.Sprintf("KB%d:", unit)
			if !strings.EqualFold(want, s.KB) {
				other := s.sys.shellOnKB(want)
				if other == nil {
					return basicErr("Not a valid device")
				}
				if !s.priv() {
					return basicErr("Protection violation")
				}
				m.Files[channel] = &chanFile{
					mode:  mode,
					class: devKeyboard,
					dev:   &kbDev{out: other.out},
				}
				return nil
			}
		}
		m.Files[channel] = &chanFile{
			mode:  mode,
			class: devKeyboard,
			dev:   &kbDev{out: target.out, self: target},
		}
		return nil

	case "NL":
		m.Files[channel] = &chanFile{mode: mode, class: devNull, dev: nullDev{}}
		return nil

	case "LP":
		// No printer is attached to this 11/70, so the line printer
		// spools to a file in the account the way a spooled system did.
		name := rest
		if name == "" {
			name = fmt.Sprintf("LP%d.LST", unit)
		}
		spec, err := s.parseSpec(name, "LST")
		if err != nil {
			return basicErr(err.Error())
		}
		// A printer is write-only and sequential, so a plain OPEN with no
		// FOR clause appends rather than opening for update.
		if mode == "" || mode == "RANDOM" {
			mode = "APPEND"
		}
		if err := s.openDiskFile(m, channel, spec, mode); err != nil {
			return err
		}
		if f := m.Files[channel]; f != nil {
			f.class = devPrinter
		}
		return nil
	}
	return basicErr("Not a valid device")
}
