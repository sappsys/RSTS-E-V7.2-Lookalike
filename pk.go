package rsts

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

type pkLink struct {
	mu     sync.Mutex
	cv     *sync.Cond
	toJob  bytes.Buffer
	toCtrl bytes.Buffer
	dead   bool
	kicked bool
}

func newPKLink() *pkLink {
	p := &pkLink{}
	p.cv = sync.NewCond(&p.mu)
	return p
}

func (p *pkLink) Hangup() {
	p.mu.Lock()
	p.dead = true
	p.cv.Broadcast()
	p.mu.Unlock()
}

func (p *pkLink) ctrlWrite(s string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead {
		return io.EOF
	}
	s = strings.ReplaceAll(s, "\n", "\r")
	p.toJob.WriteString(s)
	p.cv.Broadcast()
	return nil
}

func (p *pkLink) ctrlReadLine() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		b := p.toCtrl.Bytes()
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line := make([]byte, i+1)
			_, _ = p.toCtrl.Read(line)
			return strings.TrimRight(string(line), "\r\n"), nil
		}
		if p.dead {
			if p.toCtrl.Len() > 0 {
				rest := p.toCtrl.String()
				p.toCtrl.Reset()
				return strings.TrimRight(rest, "\r\n"), nil
			}
			return "", io.EOF
		}
		p.cv.Wait()
	}
}

func (p *pkLink) jobWrite(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead {
		return 0, io.EOF
	}
	n, _ := p.toCtrl.Write(b)
	p.cv.Broadcast()
	return n, nil
}

func (p *pkLink) jobReadByte() (byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.toJob.Len() == 0 && !p.dead && !p.kicked {
		p.cv.Wait()
	}
	if p.kicked {
		p.kicked = false
		return 0, errForced
	}
	if p.toJob.Len() == 0 {
		return 0, io.EOF
	}
	b, err := p.toJob.ReadByte()
	return b, err
}

func (p *pkLink) kick() {
	p.mu.Lock()
	p.kicked = true
	p.cv.Broadcast()
	p.mu.Unlock()
}

type pkTerm struct {
	link *pkLink
}

func (t *pkTerm) Write(p []byte) (int, error) {
	var out []byte
	for i := 0; i < len(p); i++ {
		if p[i] == '\n' && (i == 0 || p[i-1] != '\r') {
			out = append(out, '\r', '\n')
			continue
		}
		out = append(out, p[i])
	}
	return t.link.jobWrite(out)
}

func (t *pkTerm) ReadLine(prompt string) (string, error) {
	return t.readEdit(prompt, true)
}

func (t *pkTerm) ReadPassword(prompt string) (string, error) {
	s, err := t.readEdit(prompt, false)
	if _, werr := t.Write([]byte("\r\n")); werr != nil && err == nil {
		err = werr
	}
	return s, err
}

func (t *pkTerm) InterruptRead() { t.link.kick() }

func (t *pkTerm) PollInterrupt() bool {
	return t.link.pollCtrlC()
}

func (p *pkLink) pollCtrlC() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.toJob.Bytes()
	i := bytes.IndexByte(b, 3)
	if i < 0 {
		return false
	}
	rest := append(append([]byte{}, b[:i]...), b[i+1:]...)
	p.toJob.Reset()
	_, _ = p.toJob.Write(rest)
	return true
}

func (t *pkTerm) readEdit(prompt string, echo bool) (string, error) {
	if prompt != "" {
		if _, err := t.Write([]byte(prompt)); err != nil {
			return "", err
		}
	}
	var buf []byte
	esc := 0
	for {
		b, err := t.link.jobReadByte()
		if err != nil {
			if err == errForced {
				return "", errForced
			}
			if len(buf) > 0 && err == io.EOF {
				return string(buf), nil
			}
			return "", err
		}
		if esc > 0 {
			if b == '[' || b == 'O' || b == 'Y' {
				esc++
				if esc > 3 {
					esc = 0
				}
				continue
			}
			esc = 0
			continue
		}
		switch b {
		case 0:
			continue
		case 27:
			esc = 1
		case '\r', '\n':
			if echo {
				_, _ = t.Write([]byte("\r\n"))
			}
			return string(buf), nil
		case 3:
			return "", ErrInterrupt
		case 4, 26:
			if len(buf) == 0 {
				return "", io.EOF
			}
		case 21, 24:
			buf = buf[:0]
			if echo {
				_, _ = t.Write([]byte("^U\r\n"))
				if prompt != "" {
					_, _ = t.Write([]byte(prompt))
				}
			}
		case 8, 127:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				if echo {
					_, _ = t.Write([]byte{8, ' ', 8})
				}
			}
		default:
			if b < 32 {
				continue
			}
			if len(buf) < 255 {
				buf = append(buf, b)
				if echo {
					_, _ = t.Write([]byte{b})
				}
			}
		}
	}
}

func parsePKName(path string) (unit int, ok bool) {
	s := strings.ToUpper(strings.TrimSpace(path))
	if !strings.Contains(s, ":") {
		return 0, false
	}
	if strings.HasPrefix(s, "PK:") {
		rest := strings.TrimPrefix(s, "PK:")
		if rest == "" || rest == "*" {
			return -1, true
		}
		return 0, false
	}
	if !strings.HasPrefix(s, "PK") {
		return 0, false
	}
	i := 2
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 2 || i >= len(s) || s[i] != ':' {
		return 0, false
	}
	if strings.TrimSpace(s[i+1:]) != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s[2:i])
	if err != nil || n < 0 || n > 63 {
		return 0, false
	}
	return n, true
}

func (s *Shell) openPK(m *Machine, channel, unit int) error {
	if s.sys == nil {
		return basicErr("Not a valid device")
	}
	if s.Account == nil {
		return basicErr("I/O error")
	}
	u, err := s.sys.allocPK(unit)
	if err != nil {
		return err
	}
	link := newPKLink()
	job, err := s.sys.Attach("PK" + strconv.Itoa(u))
	if err != nil {
		s.sys.freePK(u)
		return basicErr(err.Error())
	}
	job.Where = fmt.Sprintf("PK%d:", u)
	job.PK = u
	job.OwnerJob = s.Job
	if s.Account != nil {
		job.OwnerPPN = s.Account.Display()
	}
	job.PKLink = link
	s.sys.replaceJob(job)

	term := &pkTerm{link: link}
	child := s.sys.newSession(job, term, term)
	go func() {
		child.Run()
		link.Hangup()
		s.sys.freePK(u)
	}()

	if m.Files == nil {
		m.Files = map[int]*chanFile{}
	}
	m.Files[channel] = &chanFile{mode: "PK", pk: link, pkJob: job.Num, pkUnit: u}
	return nil
}

func (sys *System) allocPK(want int) (int, error) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if sys.pkInUse == nil {
		sys.pkInUse = map[int]bool{}
	}
	if want >= 0 {
		if sys.pkInUse[want] {
			return 0, basicErr("Device hung")
		}
		sys.pkInUse[want] = true
		return want, nil
	}
	for i := 0; i < sys.Config.MaxUsers; i++ {
		if !sys.pkInUse[i] {
			sys.pkInUse[i] = true
			return i, nil
		}
	}
	return 0, ErrBusy
}

func (sys *System) freePK(u int) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	delete(sys.pkInUse, u)
}

func (sys *System) replaceJob(j *Job) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if j != nil {
		sys.jobs[j.Num] = j
	}
}

func closeChanFile(f *chanFile) {
	if f == nil {
		return
	}
	if f.pk != nil {
		f.pk.Hangup()
	}
	if f.file != nil {
		_ = f.file.Close()
	}
}

func formatCPU(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	cs := int(d.Milliseconds()/10) % 100
	min := sec / 60
	sec = sec % 60
	return fmt.Sprintf("%d:%02d.%02d", min, sec, cs)
}
