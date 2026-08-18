package rsts

import (
	"sort"
	"strconv"
	"strings"
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
		text, err := rewriteRefs(m.Program[old], moved, missing, m.ProgramName)
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
//
// CHAIN LINE n is a line in this program when the filespec is this
// program (CHAIN "COMP" LINE 8000 while the program is COMP). That n is
// marked by the parser like GOTO. CHAIN "OTHER" LINE 100 is left alone.
func rewriteRefs(text string, moved map[int]int, missing map[int]bool, self string) (string, error) {
	offsets, err := lineRefOffsets(text)
	if err != nil {
		return "", err
	}
	offsets = mergeOffsets(offsets, restoreRefOffsets(text))
	skip := map[int]bool{}
	for _, at := range foreignChainLineOffsets(text, self) {
		skip[at] = true
	}
	for i := len(offsets) - 1; i >= 0; i-- {
		at := offsets[i]
		if at < 0 || at >= len(text) || skip[at] {
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
			if old != 0 {
				missing[old] = true
			}
			continue
		}
		text = text[:at] + strconv.Itoa(to) + text[end:]
	}
	return text, nil
}

func mergeOffsets(a, b []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(a)+len(b))
	for _, v := range append(a, b...) {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

// restoreRefOffsets finds the digits after RESTORE so RENUM still follows
// RESTORE n if the parser ever treats that n as a plain expression.
func restoreRefOffsets(text string) []int {
	upper := strings.ToUpper(text)
	var out []int
	for i := 0; i < len(upper); {
		j := strings.Index(upper[i:], "RESTORE")
		if j < 0 {
			break
		}
		j += i
		k := j + len("RESTORE")
		if j > 0 {
			prev := upper[j-1]
			if prev >= 'A' && prev <= 'Z' || prev >= '0' && prev <= '9' || prev == '$' || prev == '%' {
				i = j + 1
				continue
			}
		}
		for k < len(text) && (text[k] == ' ' || text[k] == '\t') {
			k++
		}
		if k < len(text) && text[k] >= '0' && text[k] <= '9' {
			out = append(out, k)
		}
		i = k
	}
	return out
}

// foreignChainLineOffsets is CHAIN LINE n whose filespec is not this
// program. Those numbers belong to the other file and must not move.
func foreignChainLineOffsets(text, self string) []int {
	stmts, err := parseSourceLine(text, true)
	if err != nil {
		return nil
	}
	for _, s := range stmts {
		if s.kind != stChain || s.expr == nil {
			continue
		}
		if lit, ok := s.path.(strLit); ok && sameChainTarget(lit.v, self) {
			return nil
		}
		return lineKwNumberOffsets(text)
	}
	return nil
}

func sameChainTarget(spec, self string) bool {
	a, b := chainBaseName(spec), chainBaseName(self)
	return a != "" && a == b
}

func chainBaseName(spec string) string {
	spec = strings.ToUpper(strings.TrimSpace(spec))
	if i := strings.LastIndexAny(spec, ":]"); i >= 0 {
		spec = spec[i+1:]
	}
	spec = strings.TrimPrefix(spec, "$")
	if i := strings.IndexByte(spec, '.'); i >= 0 {
		spec = spec[:i]
	}
	return spec
}

func lineKwNumberOffsets(text string) []int {
	var out []int
	i := 0
	for i < len(text) {
		if text[i] == '"' {
			i++
			for i < len(text) {
				if text[i] == '"' {
					i++
					if i < len(text) && text[i] == '"' {
						i++
						continue
					}
					break
				}
				i++
			}
			continue
		}
		if i+4 <= len(text) && strings.EqualFold(text[i:i+4], "LINE") {
			prevOK := i == 0
			if i > 0 {
				p := text[i-1]
				prevOK = !((p >= 'A' && p <= 'Z') || (p >= 'a' && p <= 'z') || (p >= '0' && p <= '9') || p == '$' || p == '%')
			}
			next := byte(' ')
			if i+4 < len(text) {
				next = text[i+4]
			}
			nextOK := next == ' ' || next == '\t'
			if prevOK && nextOK {
				k := i + 4
				for k < len(text) && (text[k] == ' ' || text[k] == '\t') {
					k++
				}
				if k < len(text) && text[k] >= '0' && text[k] <= '9' {
					out = append(out, k)
					i = k
					continue
				}
			}
		}
		i++
	}
	return out
}
