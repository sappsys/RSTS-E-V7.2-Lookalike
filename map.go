package rsts

import (
	"encoding/binary"
	"math"
	"strings"
)

type mapArea struct {
	name   string
	buf    []byte
	fields []mapSlot
}

type mapSlot struct {
	typ    string
	name   string
	start  int
	length int
}

func mapTypeSize(typ string) int {
	switch typ {
	case "BYTE":
		return 1
	case "WORD", "INTEGER":
		return 2
	case "LONG", "SINGLE", "FLOAT":
		return 4
	case "DOUBLE":
		return 8
	default:
		return 0
	}
}

func (m *Machine) doMap(s stmt) error {
	if s.mapName == "" || len(s.mapFields) == 0 {
		return m.err("Syntax error")
	}
	off := 0
	var slots []mapSlot
	for _, f := range s.mapFields {
		typ := strings.ToUpper(f.typ)
		ln := mapTypeSize(typ)
		if f.size != nil {
			n, err := m.evalNum(f.size)
			if err != nil {
				return err
			}
			ln = int(n)
		}
		if typ == "STRING" && ln < 1 {
			ln = 16
		}
		if ln < 1 {
			return m.err("Illegal number")
		}
		slots = append(slots, mapSlot{typ: typ, name: f.name, start: off, length: ln})
		off += ln
	}
	area := &mapArea{name: s.mapName, buf: make([]byte, off), fields: slots}
	if m.maps == nil {
		m.maps = map[string]*mapArea{}
	}
	m.maps[s.mapName] = area
	m.currentMap = s.mapName
	for _, slot := range slots {
		if slot.typ == "STRING" || strings.HasSuffix(slot.name, "$") {
			m.setVar(slot.name, strValue(strings.Repeat(" ", slot.length)))
		} else {
			m.setVar(slot.name, numValue(0))
		}
	}
	return nil
}

func (m *Machine) fileMap(f *chanFile) *mapArea {
	if f == nil {
		return nil
	}
	name := f.mapName
	if name == "" {
		name = m.currentMap
	}
	if name == "" || m.maps == nil {
		return nil
	}
	return m.maps[name]
}

func (m *Machine) packMap(area *mapArea) error {
	for _, slot := range area.fields {
		end := slot.start + slot.length
		if end > len(area.buf) {
			nb := make([]byte, end)
			copy(nb, area.buf)
			area.buf = nb
		}
		v := m.getVar(slot.name)
		switch slot.typ {
		case "STRING":
			s := m.strVal(v)
			buf := bytesOf(' ', slot.length)
			if len(s) > slot.length {
				s = s[:slot.length]
			}
			copy(buf, s)
			copy(area.buf[slot.start:end], buf)
		case "BYTE":
			n, err := m.numVal(v)
			if err != nil {
				return err
			}
			area.buf[slot.start] = byte(int(n))
		case "WORD", "INTEGER":
			n, err := m.numVal(v)
			if err != nil {
				return err
			}
			binary.LittleEndian.PutUint16(area.buf[slot.start:end], uint16(int16(n)))
		case "LONG":
			n, err := m.numVal(v)
			if err != nil {
				return err
			}
			binary.LittleEndian.PutUint32(area.buf[slot.start:end], uint32(int32(n)))
		case "SINGLE", "FLOAT":
			n, err := m.numVal(v)
			if err != nil {
				return err
			}
			binary.LittleEndian.PutUint32(area.buf[slot.start:end], math.Float32bits(float32(n)))
		case "DOUBLE":
			n, err := m.numVal(v)
			if err != nil {
				return err
			}
			binary.LittleEndian.PutUint64(area.buf[slot.start:end], math.Float64bits(n))
		default:
			return m.err("Syntax error")
		}
	}
	return nil
}

func (m *Machine) unpackMap(area *mapArea) error {
	for _, slot := range area.fields {
		end := slot.start + slot.length
		if slot.start >= len(area.buf) {
			continue
		}
		if end > len(area.buf) {
			end = len(area.buf)
		}
		chunk := area.buf[slot.start:end]
		switch slot.typ {
		case "STRING":
			s := string(chunk)
			if len(s) < slot.length {
				s += strings.Repeat(" ", slot.length-len(s))
			}
			m.setVar(slot.name, strValue(s))
		case "BYTE":
			m.setVar(slot.name, numValue(float64(chunk[0])))
		case "WORD", "INTEGER":
			var n uint16
			if len(chunk) >= 2 {
				n = binary.LittleEndian.Uint16(chunk)
			}
			m.setVar(slot.name, numValue(float64(int16(n))))
		case "LONG":
			var n uint32
			if len(chunk) >= 4 {
				n = binary.LittleEndian.Uint32(chunk)
			}
			m.setVar(slot.name, numValue(float64(int32(n))))
		case "SINGLE", "FLOAT":
			var n uint32
			if len(chunk) >= 4 {
				n = binary.LittleEndian.Uint32(chunk)
			}
			m.setVar(slot.name, numValue(float64(math.Float32frombits(n))))
		case "DOUBLE":
			var n uint64
			if len(chunk) >= 8 {
				n = binary.LittleEndian.Uint64(chunk)
			}
			m.setVar(slot.name, numValue(math.Float64frombits(n)))
		default:
			return m.err("Syntax error")
		}
	}
	return nil
}
