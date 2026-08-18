package rsts

import (
	"errors"
	"io"
	"strings"
)

func (m *Machine) trap(err error) (jump, bool) {
	if err == nil || errors.Is(err, ErrInterrupt) || m.onErrorLine == 0 || m.inHandler {
		return jump{}, false
	}
	m.errNum = errorCode(err)
	m.errLine = m.CurrentLine
	if be, ok := err.(*BasicError); ok && be.Line != 0 {
		m.errLine = be.Line
	}
	m.resumeLine = m.CurrentLine
	m.inHandler = true
	return jump{kind: jumpGoto, line: m.onErrorLine}, true
}

func (m *Machine) doResume(s stmt) (jump, error) {
	if !m.inHandler && m.errNum == 0 {
		return jump{}, m.err("RESUME without error")
	}
	if s.resumeNxt {
		return jump{kind: jumpResume, line: -1}, nil
	}
	if s.expr != nil {
		n, err := m.evalNum(s.expr)
		if err != nil {
			return jump{}, err
		}
		if int(n) == 0 {
			return jump{kind: jumpResume, line: 0}, nil
		}
		return jump{kind: jumpResume, line: int(n)}, nil
	}
	return jump{kind: jumpResume, line: 0}, nil
}

func (m *Machine) execMods(s stmt, mi int) (jump, error) {
	if mi < 0 {
		return m.execCore(s)
	}
	mod := s.mods[mi]
	switch mod.kind {
	case "IF":
		cond, err := m.eval(mod.cond)
		if err != nil {
			return jump{}, err
		}
		if m.truth(cond) {
			return m.execMods(s, mi-1)
		}
		return jump{}, nil
	case "UNLESS":
		cond, err := m.eval(mod.cond)
		if err != nil {
			return jump{}, err
		}
		if !m.truth(cond) {
			return m.execMods(s, mi-1)
		}
		return jump{}, nil
	case "WHILE":
		for n := 0; n < 1000000; n++ {
			cond, err := m.eval(mod.cond)
			if err != nil {
				return jump{}, err
			}
			if !m.truth(cond) {
				return jump{}, nil
			}
			j, err := m.execMods(s, mi-1)
			if err != nil || j.kind != jumpNone {
				return j, err
			}
		}
		return jump{}, m.err("Too many iterations")
	case "UNTIL":
		for n := 0; n < 1000000; n++ {
			cond, err := m.eval(mod.cond)
			if err != nil {
				return jump{}, err
			}
			if m.truth(cond) {
				return jump{}, nil
			}
			j, err := m.execMods(s, mi-1)
			if err != nil || j.kind != jumpNone {
				return j, err
			}
		}
		return jump{}, m.err("Too many iterations")
	case "FOR":
		start, err := m.evalNum(mod.start)
		if err != nil {
			return jump{}, err
		}
		end, err := m.evalNum(mod.end)
		if err != nil {
			return jump{}, err
		}
		step := 1.0
		if mod.step != nil {
			step, err = m.evalNum(mod.step)
			if err != nil {
				return jump{}, err
			}
		}
		m.setVar(mod.forVar, numValue(start))
		for n := 0; n < 1000000; n++ {
			cur, err := m.numVal(m.getVar(mod.forVar))
			if err != nil {
				return jump{}, err
			}
			if step >= 0 && cur > end {
				return jump{}, nil
			}
			if step < 0 && cur < end {
				return jump{}, nil
			}
			j, err := m.execMods(s, mi-1)
			if err != nil || j.kind != jumpNone {
				return j, err
			}
			m.setVar(mod.forVar, numValue(cur+step))
		}
		return jump{}, m.err("Too many iterations")
	default:
		return m.execCore(s)
	}
}

func (m *Machine) doPrintUsing(s stmt) error {
	fmtv, err := m.eval(s.using)
	if err != nil {
		return err
	}
	var vals []value
	for _, item := range s.items {
		if item.sep != "" {
			continue
		}
		v, err := m.eval(item.expr)
		if err != nil {
			return err
		}
		vals = append(vals, v)
	}
	text, err := formatUsing(m.strVal(fmtv), vals)
	if err != nil {
		return err
	}
	newline := s.trailing != "NONE"
	if s.hasChan {
		ch, err := m.evalNum(s.channel)
		if err != nil {
			return err
		}
		if newline {
			text += "\n"
		}
		return m.fileWrite(int(ch), text)
	}
	if m.IO.Write != nil {
		m.IO.Write(text, newline)
	}
	if newline {
		m.col = 0
	} else {
		m.col += len(text)
	}
	return nil
}

func (m *Machine) doChange(s stmt) error {
	toStr := strings.HasSuffix(s.toName, "$")
	if toStr {
		name := s.fromName
		if name == "" {
			return m.err("Syntax error")
		}
		nval, err := m.getArray(name, []int{0})
		if err != nil {
			return err
		}
		n, err := m.numVal(nval)
		if err != nil {
			return err
		}
		count := int(n)
		if count < 0 {
			count = 0
		}
		var b strings.Builder
		for i := 1; i <= count; i++ {
			cv, err := m.getArray(name, []int{i})
			if err != nil {
				return err
			}
			code, err := m.numVal(cv)
			if err != nil {
				return err
			}
			b.WriteByte(byte(int(code) & 255))
		}
		m.setVar(s.toName, strValue(b.String()))
		return nil
	}
	var src string
	if s.fromExpr != nil {
		v, err := m.eval(s.fromExpr)
		if err != nil {
			return err
		}
		src = m.strVal(v)
	} else {
		src = m.strVal(m.getVar(s.fromName))
	}
	if err := m.ensureArray(s.toName, []int{len(src)}); err != nil {
		return err
	}
	info := m.arrays[s.toName]
	need := len(src)
	if len(info.dims) == 0 || info.dims[0] < need {
		if err := m.dimArray(s.toName, []int{need}); err != nil {
			return err
		}
	}
	if err := m.setArray(s.toName, []int{0}, numValue(float64(len(src)))); err != nil {
		return err
	}
	for i := 0; i < len(src); i++ {
		if err := m.setArray(s.toName, []int{i + 1}, numValue(float64(src[i]))); err != nil {
			return err
		}
	}
	return nil
}

func (m *Machine) channel(s stmt) (*chanFile, int, error) {
	ch, err := m.evalNum(s.channel)
	if err != nil {
		return nil, 0, err
	}
	n := int(ch)
	f := m.Files[n]
	if f == nil {
		return nil, 0, m.err("I/O error")
	}
	return f, n, nil
}

func (m *Machine) ensureRec(f *chanFile) {
	if area := m.fileMap(f); area != nil {
		f.recSize = len(area.buf)
		f.buf = area.buf
		return
	}
	if f.recSize < 1 {
		f.recSize = 512
	}
	if len(f.buf) != f.recSize {
		buf := make([]byte, f.recSize)
		copy(buf, f.buf)
		f.buf = buf
	}
}

func (m *Machine) doField(s stmt) error {
	f, _, err := m.channel(s)
	if err != nil {
		return err
	}
	m.ensureRec(f)
	off := 0
	f.fields = nil
	for _, item := range s.fields {
		n, err := m.evalNum(item.length)
		if err != nil {
			return err
		}
		ln := int(n)
		if ln < 0 {
			ln = 0
		}
		if off+ln > f.recSize {
			m.ensureRec(f)
			if off+ln > f.recSize {
				need := off + ln
				nb := make([]byte, need)
				copy(nb, f.buf)
				f.buf = nb
				f.recSize = need
			}
		}
		f.fields = append(f.fields, fieldSlot{name: item.name, start: off, length: ln})
		spaces := strings.Repeat(" ", ln)
		m.setVar(item.name, strValue(spaces))
		copy(f.buf[off:off+ln], spaces)
		off += ln
	}
	return nil
}

func (m *Machine) unpackFields(f *chanFile) {
	for _, slot := range f.fields {
		end := slot.start + slot.length
		if end > len(f.buf) {
			end = len(f.buf)
		}
		if slot.start >= len(f.buf) {
			m.setVar(slot.name, strValue(strings.Repeat(" ", slot.length)))
			continue
		}
		chunk := f.buf[slot.start:end]
		if len(chunk) < slot.length {
			chunk = append(chunk, bytesOf(' ', slot.length-len(chunk))...)
		}
		m.setVar(slot.name, strValue(string(chunk)))
	}
}

func (m *Machine) packFields(f *chanFile) {
	for _, slot := range f.fields {
		s := m.strVal(m.getVar(slot.name))
		m.placeField(f, slot, s, false)
	}
}

func bytesOf(ch byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = ch
	}
	return b
}

func (m *Machine) placeField(f *chanFile, slot fieldSlot, s string, right bool) {
	buf := make([]byte, slot.length)
	for i := range buf {
		buf[i] = ' '
	}
	if len(s) > slot.length {
		s = s[:slot.length]
	}
	if right {
		copy(buf[slot.length-len(s):], s)
	} else {
		copy(buf, s)
	}
	end := slot.start + slot.length
	if end > len(f.buf) {
		nb := make([]byte, end)
		copy(nb, f.buf)
		f.buf = nb
	}
	copy(f.buf[slot.start:end], buf)
	m.setVar(slot.name, strValue(string(buf)))
}

func (m *Machine) fieldSlot(name string) (*chanFile, *fieldSlot) {
	for _, f := range m.Files {
		if f == nil {
			continue
		}
		for i := range f.fields {
			if f.fields[i].name == name {
				return f, &f.fields[i]
			}
		}
	}
	return nil, nil
}

func (m *Machine) doLRset(s stmt, right bool) error {
	v, err := m.eval(s.expr)
	if err != nil {
		return err
	}
	text := m.strVal(v)
	name := s.target.name
	if f, slot := m.fieldSlot(name); f != nil && slot != nil {
		m.placeField(f, *slot, text, right)
		return nil
	}
	cur := m.strVal(m.getVar(name))
	width := len(cur)
	if width == 0 {
		width = len(text)
	}
	buf := make([]byte, width)
	for i := range buf {
		buf[i] = ' '
	}
	if len(text) > width {
		text = text[:width]
	}
	if right {
		copy(buf[width-len(text):], text)
	} else {
		copy(buf, text)
	}
	m.setVar(name, strValue(string(buf)))
	return nil
}

func (m *Machine) doGet(s stmt) error {
	f, _, err := m.channel(s)
	if err != nil {
		return err
	}
	if f.file == nil {
		return m.err("I/O error")
	}
	m.ensureRec(f)
	rec := f.recNo + 1
	if s.hasRec {
		n, err := m.evalNum(s.record)
		if err != nil {
			return err
		}
		rec = int(n)
	}
	if rec < 1 {
		return m.err("Illegal record number")
	}
	off := int64(rec-1) * int64(f.recSize)
	if _, err := f.file.Seek(off, io.SeekStart); err != nil {
		return m.err("I/O error")
	}
	for i := range f.buf {
		f.buf[i] = 0
	}
	n, err := io.ReadFull(f.file, f.buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		if n == 0 {
			return m.err("End of file on device")
		}
	} else if err != nil {
		return m.err("I/O error")
	}
	f.recNo = rec
	m.recount = n
	if area := m.fileMap(f); area != nil {
		return m.unpackMap(area)
	}
	m.unpackFields(f)
	return nil
}

func (m *Machine) doPut(s stmt) error {
	f, _, err := m.channel(s)
	if err != nil {
		return err
	}
	if f.file == nil {
		return m.err("I/O error")
	}
	m.ensureRec(f)
	if area := m.fileMap(f); area != nil {
		if err := m.packMap(area); err != nil {
			return err
		}
		f.buf = area.buf
		f.recSize = len(area.buf)
	} else {
		m.packFields(f)
	}
	rec := f.recNo
	if rec < 1 {
		rec = 1
	}
	if s.hasRec {
		n, err := m.evalNum(s.record)
		if err != nil {
			return err
		}
		rec = int(n)
	}
	if rec < 1 {
		return m.err("Illegal record number")
	}
	off := int64(rec-1) * int64(f.recSize)
	if _, err := f.file.Seek(off, io.SeekStart); err != nil {
		return m.err("I/O error")
	}
	if _, err := f.file.Write(f.buf); err != nil {
		return m.err("I/O error")
	}
	f.recNo = rec
	return nil
}
