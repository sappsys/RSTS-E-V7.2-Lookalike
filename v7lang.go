package rsts

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type commonSave struct {
	isArr bool
	val   value
	arr   *arrayInfo
}

func (m *Machine) interruptErr() error {
	if m != nil && m.trapCtrlC && m.onErrorLine != 0 && !m.inHandler {
		return m.errCode("Programmable ^C trap", 28)
	}
	return ErrInterrupt
}

func (m *Machine) packCommon() {
	if m == nil {
		return
	}
	saved := make([]commonSave, len(m.common))
	for i, name := range m.common {
		if a, ok := m.arrays[name]; ok && a != nil {
			saved[i] = commonSave{isArr: true, arr: cloneArray(a)}
			continue
		}
		saved[i] = commonSave{val: m.getVar(name)}
	}
	m.commonSaved = saved
}

func cloneArray(a *arrayInfo) *arrayInfo {
	if a == nil {
		return nil
	}
	c := *a
	c.dims = append([]int(nil), a.dims...)
	c.data = append([]value(nil), a.data...)
	return &c
}

func (m *Machine) doCommonItem(name string, bounds []int, strLen int) error {
	m.common = append(m.common, name)
	if len(bounds) > 0 {
		if err := m.dimArray(name, bounds); err != nil {
			return err
		}
		if strLen > 0 && strings.HasSuffix(name, "$") {
			if a := m.arrays[name]; a != nil {
				a.strLen = strLen
				a.isStr = true
			}
		}
	}
	idx := len(m.common) - 1
	if idx >= len(m.commonSaved) {
		return nil
	}
	slot := m.commonSaved[idx]
	if slot.isArr {
		if slot.arr != nil {
			m.arrays[name] = cloneArray(slot.arr)
			m.arrays[name].dims = append([]int(nil), m.arrays[name].dims...)
		}
		return nil
	}
	return m.assign(&varRef{name: name}, slot.val)
}

func (m *Machine) doMidSet(name string, idxs []int, start, length float64, hasLen bool, repl string) error {
	cur, err := m.midTarget(name, idxs)
	if err != nil {
		return err
	}
	i := int(start)
	if i < 1 {
		i = 1
	}
	s := cur
	if i > len(s)+1 {
		s += strings.Repeat(" ", i-len(s)-1)
	}
	pos := i - 1
	n := len(s) - pos
	if hasLen {
		n = int(length)
		if n < 0 {
			n = 0
		}
	}
	if n < 0 {
		n = 0
	}
	chunk := repl
	if len(chunk) < n {
		chunk += strings.Repeat(" ", n-len(chunk))
	}
	if len(chunk) > n {
		chunk = chunk[:n]
	}
	for pos > len(s) {
		s += " "
	}
	end := pos + n
	if end > len(s) {
		s += strings.Repeat(" ", end-len(s))
	}
	s = s[:pos] + chunk + s[end:]
	return m.putMidTarget(name, idxs, s)
}

func (m *Machine) midTarget(name string, idxs []int) (string, error) {
	if len(idxs) == 0 {
		return m.strVal(m.getVar(name)), nil
	}
	v, err := m.getArray(name, idxs)
	if err != nil {
		return "", err
	}
	return m.strVal(v), nil
}

func (m *Machine) putMidTarget(name string, idxs []int, s string) error {
	if len(idxs) == 0 {
		m.setVar(name, strValue(s))
		return nil
	}
	return m.setArray(name, idxs, strValue(s))
}

func xlateString(src, table string) string {
	tab := make([]byte, 256)
	for i := range tab {
		if i < len(table) {
			tab[i] = table[i]
		}
	}
	var b strings.Builder
	for i := 0; i < len(src); i++ {
		ch := tab[src[i]]
		if ch != 0 {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func (m *Machine) channelAtEnd(ch int) (bool, error) {
	f := m.Files[ch]
	if f == nil {
		return false, m.errCode("I/O channel not open", 9)
	}
	if f.eof {
		return true, nil
	}
	if f.dev != nil {
		_, isNull := f.dev.(nullDev)
		return isNull, nil
	}
	if f.pk != nil {
		return f.pk.atEnd(), nil
	}
	if f.r != nil {
		_, err := f.r.Peek(1)
		return err == io.EOF, nil
	}
	if f.file != nil {
		off, err := f.file.Seek(0, io.SeekCurrent)
		if err != nil {
			return false, m.err("I/O error")
		}
		st, err := f.file.Stat()
		if err != nil {
			return false, m.err("I/O error")
		}
		return off >= st.Size(), nil
	}
	return false, nil
}

func (m *Machine) applyFileAlloc(f *chanFile) error {
	if f == nil || f.file == nil {
		return nil
	}
	cluster := f.cluster
	if cluster < 1 {
		cluster = 1
	}
	alloc := f.alloc
	if alloc > 0 {
		want := int64(alloc) * int64(blockSize)
		st, err := f.file.Stat()
		if err != nil {
			return m.err("I/O error")
		}
		if st.Size() < want {
			if err := f.file.Truncate(want); err != nil {
				return m.err("I/O error")
			}
		} else if st.Size() > want {
			if err := f.file.Truncate(want); err != nil {
				return m.err("I/O error")
			}
		}
	}
	if m.IO.Disk != nil {
		if err := m.IO.Disk.SetFileAlloc(f.path, cluster, alloc); err != nil {
			return err
		}
		if m.IO.Quota > 0 && f.path != "" {
			used := m.IO.Disk.folderBlocks(filepath.Dir(f.path))
			if used > m.IO.Quota {
				return m.errCode("No room for user on device", 4)
			}
		}
	}
	return nil
}

func (m *Machine) lockRecord(f *chanFile, rec int) error {
	if f == nil || f.file == nil || rec < 1 {
		return nil
	}
	path := f.path
	if path == "" {
		path = f.file.Name()
	}
	if m.IO.Disk == nil {
		return nil
	}
	m.unlockChan(f)
	if err := m.IO.Disk.lockRecord(path, rec, m.IO.Job); err != nil {
		if f.modeN&modeWait != 0 {
			for {
				if m.Interrupted() {
					return m.interruptErr()
				}
				time.Sleep(50 * time.Millisecond)
				if err := m.IO.Disk.lockRecord(path, rec, m.IO.Job); err == nil {
					f.lockedRec = rec
					f.path = path
					return nil
				}
			}
		}
		return m.errCode("Disk block is interlocked", 19)
	}
	f.lockedRec = rec
	f.path = path
	return nil
}

func (m *Machine) unlockChan(f *chanFile) {
	if f == nil || f.lockedRec == 0 || m.IO.Disk == nil {
		if f != nil {
			f.lockedRec = 0
		}
		return
	}
	path := f.path
	if path == "" && f.file != nil {
		path = f.file.Name()
	}
	m.IO.Disk.unlockRecord(path, f.lockedRec, m.IO.Job)
	f.lockedRec = 0
}

func (m *Machine) lockSeqBlock(f *chanFile) error {
	if f == nil || f.file == nil || m.IO.Disk == nil {
		return nil
	}
	off, err := f.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil
	}
	if f.r != nil {
		off -= int64(f.r.Buffered())
	}
	if off < 0 {
		off = 0
	}
	rec := int(off/int64(blockSize)) + 1
	return m.lockRecord(f, rec)
}

func (m *Machine) checkWriteQuota(f *chanFile) error {
	if m == nil || f == nil || m.IO.Quota <= 0 || f.path == "" || m.IO.Disk == nil {
		return nil
	}
	used := m.IO.Disk.folderBlocks(filepath.Dir(f.path))
	if used > m.IO.Quota {
		return m.errCode("No room for user on device", 4)
	}
	return nil
}

func (m *Machine) doUnlock(ch int) error {
	f := m.Files[ch]
	if f == nil {
		return m.errCode("I/O channel not open", 9)
	}
	m.unlockChan(f)
	return nil
}

func fileBlocks(size int64, cluster, alloc int) int {
	if alloc > 0 {
		return alloc
	}
	if cluster < 1 {
		cluster = 1
	}
	n := int((size + blockSize - 1) / blockSize)
	if n == 0 {
		return 0
	}
	return ((n + cluster - 1) / cluster) * cluster
}

func (d *Disk) folderBlocks(folder string) int {
	if d == nil {
		return 0
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return 0
	}
	index := d.loadIndex(folder)
	total := 0
	for _, ent := range entries {
		if ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		meta := index[ent.Name()]
		if meta.Prot == 0 {
			meta = index[strings.ToUpper(ent.Name())]
		}
		n := fileBlocks(info.Size(), meta.Cluster, meta.Alloc)
		if n == 0 {
			n = 1
		}
		total += n
	}
	return total
}

func (d *Disk) enforceQuota(folder, path string, proj, prog int, newSize int64) error {
	if d == nil || d.quotaOf == nil {
		return nil
	}
	q := d.quotaOf(proj, prog)
	if q <= 0 {
		return nil
	}
	index := d.loadIndex(folder)
	base := filepath.Base(path)
	meta := index[base]
	if meta.Prot == 0 {
		meta = index[strings.ToUpper(base)]
	}
	oldBlocks := 0
	if info, err := os.Stat(path); err == nil {
		oldBlocks = fileBlocks(info.Size(), meta.Cluster, meta.Alloc)
		if oldBlocks == 0 {
			oldBlocks = 1
		}
	}
	newBlocks := fileBlocks(newSize, meta.Cluster, meta.Alloc)
	if newBlocks == 0 {
		newBlocks = 1
	}
	used := d.folderBlocks(folder)
	if used-oldBlocks+newBlocks > q {
		return fsErr("No room for user on device")
	}
	return nil
}
