package rsts

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func errorCode(err error) int {
	if err == nil {
		return 0
	}
	if be, ok := err.(*BasicError); ok && be.Code != 0 {
		return be.Code
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Division"):
		return 48
	case strings.Contains(msg, "Subscript"):
		return 55
	case strings.Contains(msg, "Can't find file"):
		return 5
	case strings.Contains(msg, "End of file"):
		return 11
	case strings.Contains(msg, "Protection"):
		return 10
	case strings.Contains(msg, "Out of data"):
		return 8
	case strings.Contains(msg, "Undefined line"):
		return 58
	case strings.Contains(msg, "Type mismatch"):
		return 51
	case strings.Contains(msg, "Illegal number"):
		return 52
	case strings.Contains(msg, "I/O"):
		return 50
	case strings.Contains(msg, "Syntax"):
		return 2
	default:
		return 1
	}
}

func (m *Machine) sysCall(arg string) (string, error) {
	m.IO.ProgramName = m.ProgramName
	if m.IO.Sys != nil {
		return m.IO.Sys(arg)
	}
	return defaultSys(arg, m.IO)
}

func defaultSys(arg string, io IO) (string, error) {
	f := 0
	if len(arg) > 0 {
		f = int(arg[0])
	}
	switch f {
	case 0:
		return "", nil
	case 1:
		return SystemName, nil
	case 2:
		if io.PPN != "" {
			return io.PPN, nil
		}
		return "[0,0]", nil
	case 3:
		return strconv.Itoa(io.Job), nil
	case 4:
		return io.ProgramName, nil
	case 5:
		return NowDate(), nil
	case 6:
		sub := 0
		if len(arg) > 1 {
			sub = int(int8(arg[1]))
		}
		switch sub {
		case 0, -21:
			proj, prog := parsePPN(io.PPN)
			buf := make([]byte, 4)
			binary.LittleEndian.PutUint16(buf[0:2], uint16(proj))
			binary.LittleEndian.PutUint16(buf[2:4], uint16(prog))
			return string(buf), nil
		case 1:
			if io.AccountName != "" {
				return io.AccountName, nil
			}
			return "GUEST", nil
		case -3:
			return sysTableTB1(), nil
		case -4, -12:
			return sysTableTB2(), nil
		case 9:
			return sysIdentString(), nil
		default:
			if io.PPN != "" {
				return io.PPN, nil
			}
			return "[0,0]", nil
		}
	case 7:
		return NowTime(), nil
	case 8:
		if io.Privileged {
			return string([]byte{255, 255}), nil
		}
		return string([]byte{0, 0}), nil
	case 9:
		return "SY", nil
	default:
		return "", fmt.Errorf("Illegal SYS function")
	}
}

func parsePPN(ppn string) (int, int) {
	s := strings.Trim(ppn, "[]")
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	a, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	b, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return a, b
}

func cvtPercentDollar(n float64) string {
	v := int16(int(math.Round(n)))
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, uint16(v))
	return string(buf)
}

func cvtDollarPercent(s string) float64 {
	if len(s) < 2 {
		s += "\x00\x00"
	}
	v := int16(binary.LittleEndian.Uint16([]byte(s[:2])))
	return float64(v)
}

func cvtFDollar(n float64) string {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, math.Float32bits(float32(n)))
	return string(buf)
}

func cvtDollarF(s string) float64 {
	if len(s) < 4 {
		s += "\x00\x00\x00\x00"
	}
	bits := binary.LittleEndian.Uint32([]byte(s[:4]))
	return float64(math.Float32frombits(bits))
}

func cvtDollarDollar(s string) string {
	b := []byte(s)
	for i := 0; i+1 < len(b); i += 2 {
		b[i], b[i+1] = b[i+1], b[i]
	}
	return string(b)
}
