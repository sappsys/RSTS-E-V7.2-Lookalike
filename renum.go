package rsts

import (
	"sort"
	"strconv"
)

// BASIC-PLUS line numbers run from 1 to 32767.
const maxLineNumber = 32767

// Renumber resequences the program and rewrites every reference to a line
// that moved. A reference to a line that does not exist is left alone and
// returned, sorted, so the caller can report it: renumbering is not the
// moment to silently repoint a broken GOTO somewhere new.
func (m *Machine) Renumber(start, step int) ([]int, error) {
	if m.Compiled {
		return nil, basicErr("Compiled file")
	}
	if start < 1 || step < 1 {
		return nil, basicErr("Illegal line number")
	}
	order := m.lineOrder()
	if len(order) == 0 {
		return nil, nil
	}
	if last := start + (len(order)-1)*step; last > maxLineNumber {
		return nil, basicErr("Illegal line number")
	}

	moved := make(map[int]int, len(order))
	for i, old := range order {
		moved[old] = start + i*step
	}

	renumbered := make(map[int]string, len(order))
	missing := map[int]bool{}
	for _, old := range order {
		text, err := rewriteRefs(m.Program[old], moved, missing)
		if err != nil {
			return nil, attachLine(err, old)
		}
		renumbered[moved[old]] = text
	}

	m.Program = renumbered
	m.editSeq++
	// A renumbered program is not the one that stopped, so CONT cannot
	// pick up where it left off.
	m.paused = nil

	out := make([]int, 0, len(missing))
	for n := range missing {
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

// rewriteRefs replaces the line numbers at the offsets the parser reported.
// It works from the end of the line backwards so that each replacement
// leaves the offsets of the ones before it untouched.
func rewriteRefs(text string, moved map[int]int, missing map[int]bool) (string, error) {
	offsets, err := lineRefOffsets(text)
	if err != nil {
		return "", err
	}
	for i := len(offsets) - 1; i >= 0; i-- {
		at := offsets[i]
		if at < 0 || at >= len(text) {
			continue
		}
		end := at
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
		}
		if end == at {
			continue
		}
		old, err := strconv.Atoi(text[at:end])
		if err != nil {
			continue
		}
		to, ok := moved[old]
		if !ok {
			// ON ERROR GOTO 0 and RESUME 0 mean something other than a
			// line, and a reference to a line that was never written is
			// the user's bug to fix, not ours to repoint.
			if old != 0 {
				missing[old] = true
			}
			continue
		}
		text = text[:at] + strconv.Itoa(to) + text[end:]
	}
	return text, nil
}
