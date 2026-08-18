package rsts

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Host images for the character devices a V7 program OPENed besides
// disk, keyboard, printer and null. Each unit is a file under the disk
// root: MT0, PP0, PR0, CR0, DX0, DT0. Magtape and DECtape/floppy are
// 512-byte records; paper tape and cards are sequential text.

func deviceImagePath(root, dev string, unit int) string {
	if unit < 0 {
		unit = 0
	}
	return filepath.Join(root, fmt.Sprintf("%s%d", strings.ToUpper(dev), unit))
}

func (s *Shell) openImageDevice(m *Machine, channel int, dev string, unit int, rest, mode string) error {
	root := s.DiskRoot
	if root == "" && s.Disk != nil {
		root = s.Disk.Root
	}
	path := deviceImagePath(root, dev, unit)
	switch dev {
	case "PP":
		if mode == "INPUT" {
			return basicErr("Not a valid device")
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		m.Files[channel] = &chanFile{file: f, path: path, mode: "APPEND", class: devPrinter}
		return nil
	case "PR", "CR":
		if mode == "OUTPUT" || mode == "APPEND" {
			return basicErr("Not a valid device")
		}
		f, err := os.Open(path)
		if err != nil {
			f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
		}
		if err != nil {
			return basicErr("Device hung or write locked")
		}
		cf := &chanFile{file: f, path: path, mode: "INPUT", class: devTape, r: bufio.NewReader(f)}
		if dev == "CR" {
			cf.dev = &cardDev{r: cf.r}
		}
		m.Files[channel] = cf
		return nil
	default:
		flag := os.O_RDWR | os.O_CREATE
		use := mode
		if mode == "INPUT" {
			flag = os.O_RDONLY
			if _, err := os.Stat(path); err != nil {
				return basicErr("Device hung or write locked")
			}
		} else if mode == "OUTPUT" {
			flag = os.O_RDWR | os.O_CREATE | os.O_TRUNC
			use = "OUTPUT"
		} else if mode == "APPEND" {
			flag = os.O_RDWR | os.O_CREATE
			use = "APPEND"
		} else {
			use = "RANDOM"
		}
		f, err := os.OpenFile(path, flag, 0o644)
		if err != nil {
			return err
		}
		if use == "APPEND" {
			_, _ = f.Seek(0, io.SeekEnd)
		}
		class := devTape
		if dev == "DX" {
			class = devDisk
		}
		m.Files[channel] = &chanFile{
			file:    f,
			path:    path,
			mode:    use,
			class:   class,
			recSize: blockSize,
			buf:     make([]byte, blockSize),
		}
		return nil
	}
}

// cardDev reads 80-column card images, padding or clipping each line.
type cardDev struct {
	r *bufio.Reader
}

func (d *cardDev) devWrite(string) error { return basicErr("Not a valid device") }

func (d *cardDev) devReadLine() (string, error) {
	line, err := d.r.ReadString('\n')
	if err != nil && !(err == io.EOF && line != "") {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) > 80 {
		line = line[:80]
	} else if len(line) < 80 {
		line += strings.Repeat(" ", 80-len(line))
	}
	return line, nil
}

func (d *cardDev) devClose() {}

func (m *Machine) specPercent(ch, fn int) (int, error) {
	f := m.Files[ch]
	if f == nil {
		return 0, m.errCode("I/O channel not open", 9)
	}
	if f.class == devTape || (f.recSize == blockSize && f.file != nil) {
		return m.tapeSpec(f, fn)
	}
	if f.file != nil && fn == 0 {
		st, err := f.file.Stat()
		if err != nil {
			return 0, m.err("I/O error")
		}
		return fileBlocks(st.Size(), 1, 0), nil
	}
	return 0, nil
}

func (m *Machine) tapeSpec(f *chanFile, fn int) (int, error) {
	if f.file == nil {
		return 0, m.err("I/O error")
	}
	switch fn {
	case 0: // rewind
		if _, err := f.file.Seek(0, io.SeekStart); err != nil {
			return 0, m.err("I/O error")
		}
		f.recNo = 0
		f.r = nil
		f.eof = false
		return 0, nil
	case 1: // write tape mark
		mark := make([]byte, blockSize)
		if _, err := f.file.Write(mark); err != nil {
			return 0, m.err("I/O error")
		}
		f.recNo++
		return 0, nil
	case 2: // skip record forward
		if _, err := f.file.Seek(int64(blockSize), io.SeekCurrent); err != nil {
			return 0, m.err("Magtape record length error")
		}
		f.recNo++
		return 0, nil
	case 3: // skip record reverse
		off, _ := f.file.Seek(0, io.SeekCurrent)
		if off < int64(blockSize) {
			_, _ = f.file.Seek(0, io.SeekStart)
			f.recNo = 0
			return 0, nil
		}
		if _, err := f.file.Seek(-int64(blockSize), io.SeekCurrent); err != nil {
			return 0, m.err("Magtape record length error")
		}
		if f.recNo > 0 {
			f.recNo--
		}
		return 0, nil
	case 4: // skip file forward (to next tape mark)
		buf := make([]byte, blockSize)
		for {
			n, err := io.ReadFull(f.file, buf)
			if err != nil {
				f.eof = true
				return 0, m.err("End of file on device")
			}
			f.recNo++
			if n == blockSize && allZero(buf) {
				return 0, nil
			}
		}
	case 5:
		return m.tapeSpec(f, 0)
	default:
		return 0, m.err("Illegal number")
	}
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func (s *Shell) cmdBackup(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	sw, arg := parseCmdSwitches(rest)
	if switchOn(sw, "RE", "RESTORE") {
		return s.restoreBackup(arg, acct)
	}
	src, dst := splitBackupArgs(arg)
	if dst == "" {
		dst = "MT0:"
	}
	if src == "" {
		src = "*.*"
	}
	dev, unit, _, _, ok := parseCharDevice(dst)
	if !ok || (dev != "MT" && dev != "DT" && dev != "DX") {
		return fsErr("Not a valid device")
	}
	root := s.DiskRoot
	if root == "" && s.Disk != nil {
		root = s.Disk.Root
	}
	tape := deviceImagePath(root, dev, unit)
	spec, err := s.parseSpec(src, "*")
	if err != nil {
		return err
	}
	if spec.Name == "*" && spec.Ext == "" {
		spec.Ext = "*"
	}
	_, infos, err := s.Disk.ListDir(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return err
	}
	f, err := os.Create(tape)
	if err != nil {
		return err
	}
	defer f.Close()
	nfile := 0
	for _, info := range infos {
		body, err := os.ReadFile(info.Path)
		if err != nil {
			continue
		}
		if err := writeBckFile(f, info.Name, body); err != nil {
			return err
		}
		nfile++
	}
	_ = writeBckFile(f, "", nil)
	fmt.Fprintf(s.out, "%d files copied to %s%d:\n", nfile, dev, unit)
	return nil
}

func splitBackupArgs(arg string) (src, dst string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", ""
	}
	if i := strings.IndexByte(arg, '='); i >= 0 {
		return strings.TrimSpace(arg[i+1:]), strings.TrimSpace(arg[:i])
	}
	parts := strings.Fields(arg)
	if len(parts) == 1 {
		u := strings.ToUpper(parts[0])
		if strings.HasPrefix(u, "MT") || strings.HasPrefix(u, "DT") || strings.HasPrefix(u, "DX") {
			return "", parts[0]
		}
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func writeBckFile(w io.Writer, name string, body []byte) error {
	rec := make([]byte, blockSize)
	copy(rec[:4], "BCK1")
	n := strings.ToUpper(name)
	if len(n) > 16 {
		n = n[:16]
	}
	copy(rec[4:20], n)
	binary.LittleEndian.PutUint32(rec[20:24], uint32(len(body)))
	if _, err := w.Write(rec); err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	for off := 0; off < len(body); off += blockSize {
		chunk := make([]byte, blockSize)
		copy(chunk, body[off:])
		if _, err := w.Write(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (s *Shell) restoreBackup(arg string, acct *Account) error {
	src := strings.TrimSpace(arg)
	if src == "" {
		src = "MT0:"
	}
	dev, unit, _, _, ok := parseCharDevice(src)
	if !ok {
		dev, unit = "MT", 0
	}
	root := s.DiskRoot
	if root == "" && s.Disk != nil {
		root = s.Disk.Root
	}
	tape := deviceImagePath(root, dev, unit)
	f, err := os.Open(tape)
	if err != nil {
		return fsErr("Device hung or write locked")
	}
	defer f.Close()
	nfile := 0
	for {
		rec := make([]byte, blockSize)
		if _, err := io.ReadFull(f, rec); err != nil {
			break
		}
		if string(rec[:4]) != "BCK1" {
			return fsErr("Magtape record length error")
		}
		name := strings.TrimRight(string(rec[4:20]), "\x00 ")
		size := binary.LittleEndian.Uint32(rec[20:24])
		if name == "" {
			break
		}
		body := make([]byte, size)
		left := int(size)
		for left > 0 {
			chunk := make([]byte, blockSize)
			if _, err := io.ReadFull(f, chunk); err != nil {
				return fsErr("Magtape record length error")
			}
			n := blockSize
			if n > left {
				n = left
			}
			copy(body[int(size)-left:], chunk[:n])
			left -= n
		}
		spec, err := s.parseSpec(name, "")
		if err != nil {
			return err
		}
		if err := s.Disk.WriteText(spec, acct.Proj, acct.Prog, s.priv(), string(body), defaultProt); err != nil {
			return err
		}
		nfile++
	}
	fmt.Fprintf(s.out, "%d files restored from %s%d:\n", nfile, dev, unit)
	return nil
}
