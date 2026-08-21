package rsts

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func u16at(raw string, n int) int {
	if n < 0 {
		n = 0
	}
	if len(raw) < n+2 {
		return 0
	}
	return int(binary.LittleEndian.Uint16([]byte(raw[n : n+2])))
}

func splitSpecIndex(raw string) (spec string, idx int, indexed bool) {
	if i := strings.IndexByte(raw, 0); i >= 0 {
		spec = raw[:i]
		if len(raw) >= i+3 {
			return spec, u16at(raw, i+1), true
		}
		return spec, 0, false
	}
	return strings.TrimRight(raw, "\x00"), 0, false
}

func dumpJoin(n int, rec func(i int) string) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(rec(i))
	}
	return b.String()
}

func (s *Shell) fipExtra(sub int, raw string) (string, error) {
	switch sub {
	case -18:
		if s.cclArg != "" {
			return s.cclArg, nil
		}
		if s.Basic != nil {
			return s.Basic.IO.CCLLine, nil
		}
		return "", nil
	case -20:
		return s.fipDirScan(raw)
	case -22:
		if len(raw) == 0 {
			return s.fipJobDump()
		}
		return s.fipJobScan(u16at(raw, 0))
	case -23:
		if len(raw) == 0 {
			return s.fipPackDump()
		}
		return s.fipPackScan(u16at(raw, 0))
	case -24:
		if len(raw) == 0 {
			return s.fipQueDump()
		}
		return s.fipQueScan(u16at(raw, 0))
	case -25:
		return s.fipQueOp(raw)
	case -26:
		if len(raw) == 0 {
			return s.fipPleaseDump()
		}
		return s.fipPleaseScan(u16at(raw, 0))
	case -27:
		return s.fipPleaseOp(raw)
	case -28:
		if len(raw) == 0 {
			return s.fipCCLDump()
		}
		return s.fipCCLScan(u16at(raw, 0))
	case -29:
		return s.fipCCLOp(raw)
	case -30:
		return s.fipQuota(strings.TrimRight(raw, "\x00"))
	case -31:
		return s.fipTTY(strings.TrimRight(raw, "\x00"))
	case -32:
		return s.fipCapture(func() error { return s.cmdBackup(strings.TrimRight(raw, "\x00")) })
	case -33:
		return s.fipCapture(func() error { return s.cmdSubmit(strings.TrimRight(raw, "\x00")) })
	case -34:
		return s.fipCapture(func() error { return s.cmdUtility(strings.TrimRight(raw, "\x00")) })
	case -35:
		return s.fipMemStats(), nil
	case -36:
		return s.fipFileOp(raw)
	default:
		return fipZeros(), nil
	}
}

func (s *Shell) fipCapture(fn func() error) (string, error) {
	var buf strings.Builder
	old := s.out
	s.out = &buf
	err := fn()
	s.out = old
	return buf.String(), err
}

func (s *Shell) fipDirScan(raw string) (string, error) {
	if s.Disk == nil {
		return "", nil
	}
	specPart, idx, indexed := splitSpecIndex(raw)
	if strings.TrimSpace(specPart) == "" {
		specPart = "*.*"
	}
	spec, err := s.parseSpec(specPart, "*")
	if err != nil {
		return "", err
	}
	if spec.Name == "*" && (specPart == "*.*" || spec.Ext == "") {
		spec.Ext = "*"
	}
	proj, prog := 0, 0
	priv := false
	if s.Account != nil {
		proj, prog = s.Account.Proj, s.Account.Prog
		priv = s.priv()
	}
	ppn, infos, err := s.Disk.ListDir(spec, proj, prog, priv)
	if err != nil {
		return "", err
	}
	dev := spec.DevName()
	if dev == "" {
		dev = "SY:"
	}
	rec := func(i int) string {
		info := infos[i]
		return fmt.Sprintf("%s|%s|%d|%d|%s|%s|%d|%d|%s|%s",
			info.NamePart(), info.ExtPart(), info.Blocks(), info.Prot,
			info.Modified.Format("02-Jan-06"),
			strings.TrimLeft(info.Modified.Format("3:04 PM"), "0"),
			info.Cluster, info.Alloc, dev, ppn)
	}
	if !indexed {
		return dumpJoin(len(infos), rec), nil
	}
	if idx < 0 || idx >= len(infos) {
		return "", nil
	}
	return rec(idx), nil
}

func (s *Shell) jobSlice() []Job {
	s.syncJob()
	if s.sys != nil {
		return s.sys.JobList()
	}
	rts := s.jobRTS()
	who, what := "*****", "LOGINS"
	if s.Account != nil {
		who = s.Account.Display()
		what = "Ready"
	}
	return []Job{{Num: s.Job, KB: s.KB, Who: who, What: what, SizeK: 8, State: "KB", RTS: rts}}
}

func jobRec(j Job) string {
	who := strings.Trim(j.Who, "[]")
	rts := j.RTS
	if rts == "" {
		rts = "BASIC"
	}
	return fmt.Sprintf("%d|%s|%s|%s|%d|%s|%s|%s",
		j.Num, who, j.where(), j.What, j.sizeK(), j.state(), j.cpu(), rts)
}

func (s *Shell) fipJobScan(idx int) (string, error) {
	jobs := s.jobSlice()
	if idx < 0 || idx >= len(jobs) {
		return "", nil
	}
	return jobRec(jobs[idx]), nil
}

func (s *Shell) fipJobDump() (string, error) {
	jobs := s.jobSlice()
	return dumpJoin(len(jobs), func(i int) string { return jobRec(jobs[i]) }), nil
}

func (s *Shell) packRec(p *Pack) string {
	name := p.ID
	if name == "" {
		name = "******"
	}
	state := p.Flags()
	if !p.Init {
		state = "Uninit"
	} else if p.Mounted {
		if state == "" {
			state = "Mtd"
		}
	} else if state == "" {
		state = "Dsm"
	}
	size, used := s.Disk.PackUsage(p)
	open := 0
	if p.Mounted && s.sys != nil {
		open = s.sys.openOnPack(p)
	}
	mounted := 0
	if p.Mounted {
		mounted = 1
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d|%s|%s|%d",
		p.Designator(), p.Media, open, size, size-used, packCluster(p.Media), name, state, mounted)
}

func (s *Shell) fipPackScan(idx int) (string, error) {
	if s.Disk == nil {
		return "", nil
	}
	packs := s.Disk.Packs()
	if idx < 0 || idx >= len(packs) {
		return "", nil
	}
	return s.packRec(packs[idx]), nil
}

func (s *Shell) fipPackDump() (string, error) {
	if s.Disk == nil {
		return "", nil
	}
	packs := s.Disk.Packs()
	return dumpJoin(len(packs), func(i int) string { return s.packRec(packs[i]) }), nil
}

func (s *Shell) fipQueScan(idx int) (string, error) {
	if s.sys == nil {
		return "", nil
	}
	jobs := s.sys.listQueue()
	if idx < 0 || idx >= len(jobs) {
		return "", nil
	}
	j := jobs[idx]
	return fmt.Sprintf("%d|%s|%s", j.ID, j.Who, j.File), nil
}

func (s *Shell) fipQueDump() (string, error) {
	if s.sys == nil {
		return "", nil
	}
	jobs := s.sys.listQueue()
	return dumpJoin(len(jobs), func(i int) string {
		j := jobs[i]
		return fmt.Sprintf("%d|%s|%s", j.ID, j.Who, j.File)
	}), nil
}

func (s *Shell) fipQueOp(raw string) (string, error) {
	if s.sys == nil {
		return "", fsErr("QUE")
	}
	acct, err := s.needLogin()
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}
	op := raw[0]
	arg := strings.TrimRight(raw[1:], "\x00")
	switch op {
	case 'D', 'd':
		id, _ := strconv.Atoi(strings.TrimSpace(arg))
		if id < 1 {
			return "?QUE/DE n\n", nil
		}
		who := ""
		if acct != nil {
			who = acct.Display()
		}
		if !s.sys.deleteQueue(id, who, s.priv()) {
			return "", fsErr("Can't find file or account")
		}
		return "", nil
	case 'E', 'e':
		spec, err := s.parseSpec(arg, "")
		if err != nil {
			return "", err
		}
		who := ""
		if acct != nil {
			who = acct.Display()
		}
		id := s.sys.enqueuePrint(who, s.Job, spec.Filename())
		return fmt.Sprintf("Job %d queued to LP:\n", id), nil
	default:
		return "", nil
	}
}

func (s *Shell) fipPleaseScan(idx int) (string, error) {
	if s.sys == nil {
		return "", nil
	}
	s.sys.mu.Lock()
	q := s.sys.loadPlease()
	s.sys.mu.Unlock()
	if idx < 0 || idx >= len(q.Msgs) {
		return "", nil
	}
	m := q.Msgs[idx]
	text := strings.ReplaceAll(m.Text, "|", "/")
	reply := strings.ReplaceAll(m.Reply, "|", "/")
	return fmt.Sprintf("%d|%s|%d|%s|%s|%s", m.ID, m.From, m.Job, m.KB, text, reply), nil
}

func (s *Shell) fipPleaseDump() (string, error) {
	if s.sys == nil {
		return "", nil
	}
	s.sys.mu.Lock()
	q := s.sys.loadPlease()
	s.sys.mu.Unlock()
	return dumpJoin(len(q.Msgs), func(i int) string {
		m := q.Msgs[i]
		text := strings.ReplaceAll(m.Text, "|", "/")
		reply := strings.ReplaceAll(m.Reply, "|", "/")
		return fmt.Sprintf("%d|%s|%d|%s|%s|%s", m.ID, m.From, m.Job, m.KB, text, reply)
	}), nil
}

func (s *Shell) fipPleaseOp(raw string) (string, error) {
	if _, err := s.needLogin(); err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}
	op := raw[0]
	arg := strings.TrimRight(raw[1:], "\x00")
	switch op {
	case 'S', 's':
		if s.sys == nil {
			return "Message logged (no operator console)\n", nil
		}
		if err := s.sys.sendPlease(s, arg); err != nil {
			return "", err
		}
		return "Message sent to operator\n", nil
	case 'R', 'r':
		if !s.priv() && !s.console {
			return "", fsErr("Protection violation")
		}
		return s.fipCapture(func() error { return s.replyPlease(arg) })
	default:
		return "", nil
	}
}

func (s *Shell) fipCCLScan(idx int) (string, error) {
	if s.sys == nil {
		return "", nil
	}
	s.sys.mu.Lock()
	entries := s.sys.loadCCL()
	s.sys.mu.Unlock()
	if idx < 0 || idx >= len(entries) {
		return "", nil
	}
	e := entries[idx]
	return e.Name + "|" + e.Spec, nil
}

func (s *Shell) fipCCLDump() (string, error) {
	if s.sys == nil {
		return "", nil
	}
	s.sys.mu.Lock()
	entries := s.sys.loadCCL()
	s.sys.mu.Unlock()
	return dumpJoin(len(entries), func(i int) string {
		return entries[i].Name + "|" + entries[i].Spec
	}), nil
}

func (s *Shell) fipCCLOp(raw string) (string, error) {
	return s.fipCapture(func() error { return s.cmdCCL(strings.TrimRight(raw, "\x00")) })
}

func (s *Shell) fipQuota(arg string) (string, error) {
	acct, err := s.needLogin()
	if err != nil {
		return "", err
	}
	want := acct
	if strings.TrimSpace(arg) != "" {
		if !s.priv() {
			return "", fsErr("Protection violation")
		}
		want = s.Accounts.Find(arg)
		if want == nil {
			if proj, prog, perr := ParsePPN(arg); perr == nil {
				want = s.Accounts.FindPPN(proj, prog)
			}
		}
		if want == nil {
			return "", fsErr("Can't find file or account")
		}
	}
	folder, err := s.Disk.AccountDir(want.Proj, want.Prog)
	if err != nil {
		return "", err
	}
	used := s.Disk.folderBlocks(folder)
	jobs := 0
	if s.sys != nil {
		jobs = len(s.sys.jobsForPPN(want.Proj, want.Prog))
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d", want.Display(), want.Name, want.Quota, used, want.JobQuota, jobs), nil
}

func (s *Shell) fipTTY(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return s.fipCapture(func() error {
			s.showTTY()
			return nil
		})
	}
	return s.fipCapture(func() error { return s.cmdSet(arg) })
}

func (s *Shell) fipMemStats() string {
	s.syncJob()
	used, njobs, nlog := 0, 1, 0
	if s.Account != nil {
		nlog = 1
	}
	cpu := time.Duration(0)
	open := 0
	if s.sys != nil {
		jobs := s.sys.JobList()
		njobs = len(jobs)
		nlog = 0
		for _, j := range jobs {
			used += j.sizeK()
			cpu += j.CPU
			if j.Who != "" && j.Who != "*****" {
				nlog++
			}
		}
		open = s.sys.openChannels()
	} else if s.Basic != nil {
		used = s.Basic.SizeKW()
	}
	free := MemoryKW - MonitorKW - RTSKW - used
	if free < 0 {
		free = 0
	}
	up := time.Since(s.Boot)
	if up < 0 {
		up = 0
	}
	dsize, dfree := 0, 0
	if s.Disk != nil {
		for _, p := range s.Disk.Packs() {
			if !p.Mounted {
				continue
			}
			cap, u := s.Disk.PackUsage(p)
			dsize += cap
			dfree += cap - u
		}
	}
	return fmt.Sprintf("%d|%d|%d|%d|%d|%d|%s|%s|%d|%d|%d|%d|%s|%d|%d",
		MonitorKW, RTSKW, used, free, njobs, nlog, formatCPU(up), formatCPU(cpu),
		open, dsize, dfree, s.userLimit(), CPUName, ClockHz, MemoryKW)
}

func (s *Shell) fipFileOp(raw string) (string, error) {
	acct, err := s.needLogin()
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}
	op := raw[0]
	rest := raw[1:]
	switch op {
	case 'K', 'k':
		return "", s.pipDelete(strings.TrimRight(rest, "\x00"), acct)
	case 'C', 'c':
		parts := strings.Split(rest, "\x00")
		if len(parts) < 2 {
			return "", fsErr("Illegal file name")
		}
		dstArg, srcArg := parts[0], parts[1]
		flags := ""
		if len(parts) > 2 {
			flags = strings.ToUpper(parts[2])
		}
		dst, err := s.parseSpec(dstArg, "")
		if err != nil {
			return "", err
		}
		src, err := s.parseSpec(srcArg, "")
		if err != nil {
			return "", err
		}
		if dst.Ext == "" {
			dst.Ext = src.Ext
		}
		if i := strings.IndexByte(flags, 'P'); i >= 0 && i+1 < len(flags) {
			n, err := strconv.Atoi(strings.TrimSpace(flags[i+1:]))
			if err != nil || n < 0 || n > 255 {
				return "", fsErr("Illegal protection code")
			}
			dst.Prot, dst.ProtSet = n, true
		}
		appendTo := strings.Contains(flags, "A")
		noSuper := strings.Contains(flags, "N")
		return "", s.Disk.copyFile(src, dst, acct.Proj, acct.Prog, s.priv(), appendTo, noSuper)
	case 'R', 'r':
		parts := strings.Split(rest, "\x00")
		if len(parts) < 2 {
			return "?PIP/RE new=old\n", nil
		}
		return "", s.pipRename(parts[0]+"="+parts[1], acct)
	case 'X', 'x':
		parts := strings.SplitN(rest, "\x00", 2)
		if len(parts) < 2 {
			return "", fsErr("Illegal file name")
		}
		return "", s.pipConcat(parts[0], parts[1], acct)
	default:
		return "", nil
	}
}
