package rsts

import (
	"errors"
	"io"
	"time"
)

// GET/PUT of a keyboard or PK: is one-byte record I/O, the V7.2 way
// a program reads a terminal without line editing.

func (m *Machine) charWait() time.Duration {
	if m.inputWait < 0 {
		return -1
	}
	return time.Duration(m.inputWait * float64(time.Second))
}

func (m *Machine) charRecordLen(f *chanFile) int {
	n := 0
	for _, sl := range f.fields {
		if end := sl.start + sl.length; end > n {
			n = end
		}
	}
	if n < 1 {
		if f.recSize > 0 && f.recSize != 512 {
			n = f.recSize
		} else {
			n = 1
		}
	}
	if len(f.buf) < n {
		buf := make([]byte, n)
		copy(buf, f.buf)
		f.buf = buf
	}
	f.recSize = n
	return n
}

func (m *Machine) doGetChar(f *chanFile) error {
	start := time.Now()
	defer m.noteWait(start)
	n := m.charRecordLen(f)
	wait := m.charWait()
	got := 0
	for got < n {
		if m.Interrupted() {
			return m.interruptErr()
		}
		b, err := m.readCharByte(f, wait)
		if err != nil {
			if errors.Is(err, errWaitTimeout) {
				if got == 0 {
					return m.errCode("Keyboard wait exhausted", 15)
				}
				break
			}
			if errors.Is(err, errForced) || errors.Is(err, ErrInterrupt) {
				return m.interruptErr()
			}
			if err == io.EOF {
				if got == 0 {
					f.eof = true
					return m.err("End of file on device")
				}
				break
			}
			return m.err("I/O error")
		}
		f.buf[got] = b
		got++
	}
	for i := got; i < len(f.buf); i++ {
		f.buf[i] = 0
	}
	m.recount = got
	m.unpackFields(f)
	return nil
}

func (m *Machine) readCharByte(f *chanFile, wait time.Duration) (byte, error) {
	if f.pk != nil {
		return f.pk.ctrlGetByte(wait, m.Interrupted)
	}
	if g, ok := f.dev.(interface {
		getByte(time.Duration) (byte, error)
	}); ok {
		return g.getByte(wait)
	}
	return 0, io.EOF
}

func (m *Machine) doPutChar(f *chanFile) error {
	n := m.charRecordLen(f)
	if area := m.fileMap(f); area != nil {
		if err := m.packMap(area); err != nil {
			return err
		}
		f.buf = area.buf
		n = len(area.buf)
	} else {
		m.packFields(f)
	}
	if n > len(f.buf) {
		n = len(f.buf)
	}
	buf := string(f.buf[:n])
	if f.pk != nil {
		return f.pk.ctrlWriteBytes([]byte(buf))
	}
	if f.dev != nil {
		return f.dev.devWrite(buf)
	}
	return m.err("I/O error")
}

func (d *kbDev) getByte(wait time.Duration) (byte, error) {
	var t terminal
	if d.peer != nil {
		t = d.peer.term
	} else if d.self != nil {
		t = d.self.term
	}
	if g, ok := t.(interface {
		GetByte(time.Duration) (byte, error)
	}); ok {
		return g.GetByte(wait)
	}
	if wait >= 0 {
		return 0, errWaitTimeout
	}
	return 0, io.EOF
}

func (nullDev) getByte(time.Duration) (byte, error) { return 0, io.EOF }
