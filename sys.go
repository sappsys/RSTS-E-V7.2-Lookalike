package rsts

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"time"
)

// The RSTS/E error numbers, as ERR reports them. A program that traps
// with ON ERROR branches on these, so they are the documented values
// rather than anything of our own devising.
var errorNumbers = []struct {
	text string
	code int
}{
	{"Bad directory for device", 1},
	{"Illegal file name", 2},
	{"Account or device in use", 3},
	{"No room for user on device", 4},
	{"Can't find file or account", 5},
	{"Not a valid device", 6},
	{"I/O channel already open", 7},
	{"Device not available", 8},
	{"I/O channel not open", 9},
	{"Protection violation", 10},
	{"End of file on device", 11},
	{"Fatal system I/O failure", 12},
	{"User data error on device", 13},
	{"Device hung or write locked", 14},
	{"Keyboard wait exhausted", 15},
	{"Name or account now exists", 16},
	{"Disk block is interlocked", 19},
	{"Disk pack is not mounted", 21},
	{"Disk not mounted", 21},
	{"Illegal cluster size", 23},
	{"Programmable ^C trap", 28},
	{"Device not file-structured", 30},
	{"Illegal byte count for I/O", 31},
	{"Magtape select error", 39},
	{"Magtape record length error", 40},
	{"Virtual buffer too large", 42},
	{"Virtual array not on disk", 43},
	{"Matrix or array too big", 44},
	{"Virtual array not yet open", 45},
	{"Illegal I/O channel", 46},
	{"Line too long", 47},
	{"Floating point error", 48},
	{"Data format error", 50},
	{"Integer error", 51},
	{"Illegal number", 52},
	{"Illegal argument in LOG", 53},
	{"Imaginary square roots", 54},
	{"Subscript out of range", 55},
	{"Can't invert matrix", 56},
	{"Out of data", 57},
	{"ON statement out of range", 58},
	{"Not enough data in record", 59},
	{"Integer overflow, FOR loop", 60},
	{"Division by 0", 61},
	{"FIELD overflows buffer", 63},
	{"Not a random access device", 64},
	{"RETURN without GOSUB", 72},
	{"FNEND without function call", 73},
	{"Arguments don't match", 88},
	{"Too many arguments", 89},
	{"Too few arguments", 97},
	{"RESUME and no error", 104},
	{"Redimensioned array", 105},
	{"PRINT-USING format error", 116},
	{"Maximum memory exceeded", 126},
	{"Illegal record number", 147},
	{"Bad RECORDSIZE value on OPEN", 148},
}

func errorCode(err error) int {
	if err == nil {
		return 0
	}
	if be, ok := err.(*BasicError); ok && be.Code != 0 {
		return be.Code
	}
	msg := err.Error()
	for _, e := range errorNumbers {
		if strings.Contains(msg, e.text) {
			return e.code
		}
	}
	// A few of our messages are worded differently from the manual's.
	switch {
	case strings.Contains(msg, "Division"):
		return 61
	case strings.Contains(msg, "Argument count"):
		return 88
	case strings.Contains(msg, "Type mismatch"):
		return 50
	case strings.Contains(msg, "Undefined function"):
		return 73
	case strings.Contains(msg, "RESUME without error"):
		return 104
	case strings.Contains(msg, "I/O"):
		return 12
	}
	return 1
}

func (m *Machine) sysCall(arg string) (string, error) {
	m.IO.ProgramName = m.ProgramName
	if len(arg) >= 2 && arg[0] == 6 && int8(arg[1]) == -7 {
		m.trapCtrlC = len(arg) < 3 || arg[2] != 0
		return "", nil
	}
	if m.IO.Sys != nil {
		return m.IO.Sys(arg)
	}
	return defaultSys(arg, m)
}

func defaultSys(arg string, m *Machine) (string, error) {
	io := m.IO
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
		// FIP: SYS(CHR$(6%)+CHR$(sub%)+payload). Unknown subcodes return
		// 30 NUL bytes, not a dummy PPN. This is not a complete Digital FIP.
		return fipCall(arg, m)
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
		return "", basicErr("Illegal SYS function")
	}
}

func fipPayload(arg string) string {
	if len(arg) < 3 {
		return ""
	}
	return strings.TrimRight(arg[2:], "\x00")
}

func extraRaw(arg string) string {
	if len(arg) < 3 {
		return ""
	}
	return arg[2:]
}

func fipZeros() string {
	return string(make([]byte, 30))
}

// fipCall implements SYS(CHR$(6%)+CHR$(sub%)+…). Subcodes this host can
// back are wired; anything else is 30 NULs. Not a complete Digital FIP.
func fipCall(arg string, m *Machine) (string, error) {
	io := m.IO
	sub := 0
	if len(arg) > 1 {
		sub = int(int8(arg[1]))
	}
	payload := fipPayload(arg)
	switch sub {
	case 2:
		if io.Job != 0 {
			return binU16(io.Job), nil
		}
		return binU16(1), nil
	case 3:
		if io.KB != "" {
			return io.KB, nil
		}
		return "KB0:", nil
	case 4:
		return io.ProgramName, nil
	case 5:
		return binU16(rstsDateInt(time.Now())), nil
	case 6:
		id := "SYSDSK"
		if io.Disk != nil {
			if p := io.Disk.systemPack(); p != nil && p.ID != "" {
				id = p.ID
			}
		}
		return id, nil
	case 7:
		return binU16(minutesToMidnight(time.Now())), nil
	case 8:
		if io.Privileged {
			return string([]byte{255, 255}), nil
		}
		return string([]byte{0, 0}), nil
	case 9:
		return sysIdentString(), nil
	case 10:
		return "SY", nil
	case 11:
		return "BASIC", nil
	case 12:
		return "BASIC+", nil
	case 14:
		return "SY0:", nil
	case -1:
		if io.Hangup != nil {
			return "", io.Hangup()
		}
		return fipZeros(), nil
	case -8:
		kb := io.KB
		u := 0
		if strings.HasPrefix(strings.ToUpper(kb), "KB") {
			n := strings.TrimSuffix(strings.TrimPrefix(strings.ToUpper(kb), "KB"), ":")
			u, _ = strconv.Atoi(n)
		}
		return binU16(u), nil
	case -2:
		if io.Echo {
			return string([]byte{255}), nil
		}
		return string([]byte{0}), nil
	case -9:
		return NowDate(), nil
	case -10:
		buf := make([]byte, 30)
		w := io.Width
		if w <= 0 {
			w = 80
		}
		binary.LittleEndian.PutUint16(buf[0:2], uint16(w))
		if io.Echo {
			buf[2] = 255
		}
		return string(buf), nil
	case -11:
		buf := make([]byte, 30)
		w := io.Width
		if w <= 0 {
			w = 80
		}
		binary.LittleEndian.PutUint16(buf[0:2], uint16(w))
		if io.Echo {
			buf[2] = 255
		}
		spd := io.Speed
		if spd <= 0 {
			spd = 9600
		}
		binary.LittleEndian.PutUint16(buf[4:6], uint16(spd))
		tt := io.TermType
		if tt == "" {
			tt = "VT52"
		}
		copy(buf[6:], tt)
		return string(buf), nil
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
	case -5:
		if io.Assign == nil {
			return fipZeros(), nil
		}
		dev, logical := splitAssignPayload(payload)
		if logical == "" {
			return fipZeros(), nil
		}
		return "", io.Assign(dev, logical)
	case -6:
		if io.Deassign == nil {
			return fipZeros(), nil
		}
		return "", io.Deassign(strings.TrimSpace(payload))
	case -13:
		cpu := int(m.CPUTime().Seconds())
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint16(buf[0:2], uint16(cpu))
		binary.LittleEndian.PutUint16(buf[2:4], 8)
		return string(buf), nil
	case -14:
		if !io.Privileged {
			return "", fsErr("Protection violation")
		}
		if io.SetLogins != nil {
			return "", io.SetLogins(true)
		}
		return fipZeros(), nil
	case -15:
		if !io.Privileged {
			return "", fsErr("Protection violation")
		}
		if io.SetLogins != nil {
			return "", io.SetLogins(false)
		}
		return fipZeros(), nil
	case -16:
		if io.Broadcast == nil {
			return fipZeros(), nil
		}
		to, text := splitWhere(payload)
		if text == "" {
			text = payload
			to = "ALL"
		}
		if (to == "ALL" || to == "") && !io.Privileged {
			return "", fsErr("Protection violation")
		}
		return "", io.Broadcast(to, text)
	case -17:
		return fipLookup(io, payload)
	case -18:
		if io.FipExtra != nil {
			return io.FipExtra(-18, extraRaw(arg))
		}
		return io.CCLLine, nil
	default:
		if io.FipExtra != nil && (sub <= -20 && sub != -21) {
			return io.FipExtra(sub, extraRaw(arg))
		}
		return fipZeros(), nil
	}
}

func splitAssignPayload(payload string) (dev, logical string) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", ""
	}
	if i := strings.IndexByte(payload, ':'); i >= 0 {
		dev = payload[:i+1]
		logical = strings.TrimSpace(payload[i+1:])
		return strings.ToUpper(dev), strings.ToUpper(strings.TrimSuffix(logical, ":"))
	}
	parts := strings.Fields(payload)
	if len(parts) >= 2 {
		return strings.ToUpper(parts[0]), strings.ToUpper(strings.TrimSuffix(parts[1], ":"))
	}
	return "", strings.ToUpper(payload)
}

// fipLookup is FIP -17: filespec in the payload, first directory match
// packed into 30 bytes (name, size, protection).
func fipLookup(io IO, payload string) (string, error) {
	buf := make([]byte, 30)
	if io.Disk == nil || strings.TrimSpace(payload) == "" {
		return string(buf), nil
	}
	spec, err := ParseFileSpec(payload, "")
	if err != nil {
		return "", err
	}
	proj, prog := parsePPN(io.PPN)
	_, infos, err := io.Disk.ListDir(spec, proj, prog, io.Privileged)
	if err != nil {
		return "", err
	}
	if len(infos) == 0 {
		return "", fsErr("Can't find file or account")
	}
	info := infos[0]
	copy(buf, info.Name)
	binary.LittleEndian.PutUint16(buf[16:18], uint16(info.Blocks()))
	buf[18] = byte(info.Prot)
	return string(buf), nil
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

func binU16(n int) string {
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, uint16(n))
	return string(buf)
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
