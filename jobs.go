package rsts

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

func (j Job) sizeK() int {
	if j.SizeK > 0 {
		return j.SizeK
	}
	return 8
}

func (j Job) state() string {
	if j.State != "" {
		return j.State
	}
	if j.Detached {
		return "Det"
	}
	return "KB"
}

func (j Job) where() string {
	if j.Detached {
		return "Det"
	}
	if j.Where != "" {
		return j.Where
	}
	return j.KB
}

func (j Job) cpu() string {
	return formatCPU(j.CPU)
}

func (sys *System) FindJob(token string) *Job {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	return sys.findJobLocked(token)
}

func (sys *System) findJobLocked(token string) *Job {
	s := strings.ToUpper(strings.TrimSpace(token))
	s = strings.TrimSuffix(s, ":")
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		if j := sys.jobs[n]; j != nil {
			cp := *j
			return &cp
		}
		return nil
	}
	if strings.HasPrefix(s, "KB") {
		want := s + ":"
		if s == "KB" {
			want = "KB0:"
		}
		for _, j := range sys.jobs {
			if j != nil && !j.Detached && strings.EqualFold(j.KB, want) {
				cp := *j
				return &cp
			}
		}
	}
	if strings.HasPrefix(s, "PK") {
		rest := strings.TrimPrefix(s, "PK")
		n := 0
		if rest != "" {
			var err error
			n, err = strconv.Atoi(rest)
			if err != nil {
				return nil
			}
		}
		for _, j := range sys.jobs {
			if j != nil && j.PK == n && j.PK >= 0 {
				cp := *j
				return &cp
			}
		}
	}
	return nil
}

func (sys *System) parkShell(s *Shell) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if sys.parked == nil {
		sys.parked = map[int]*Shell{}
	}
	if j := sys.jobs[s.Job]; j != nil {
		j.Detached = true
		j.State = "Det"
		j.Where = "Det"
	}
	sys.parked[s.Job] = s
	delete(sys.shells, s.Job)
}

func (sys *System) unpark(num int) *Shell {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	s := sys.parked[num]
	delete(sys.parked, num)
	if s != nil {
		if j := sys.jobs[num]; j != nil {
			j.Detached = false
			j.State = "KB"
			j.Where = s.KB
		}
		sys.shells[num] = s
	}
	return s
}

func (sys *System) registerShell(s *Shell) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if sys.shells == nil {
		sys.shells = map[int]*Shell{}
	}
	sys.shells[s.Job] = s
}

func (sys *System) unregisterShell(s *Shell) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if sys.shells[s.Job] == s {
		delete(sys.shells, s.Job)
	}
}

func (sys *System) Force(token, line string) error {
	j := sys.FindJob(token)
	if j == nil {
		return fsErr("Can't find job")
	}
	sys.mu.Lock()
	sh := sys.shells[j.Num]
	link := j.PKLink
	sys.mu.Unlock()
	if link != nil {
		return link.ctrlWrite(line + "\r")
	}
	if sh == nil {
		return fsErr("Job hung")
	}
	select {
	case sh.forceCh <- line:
	default:
		return fsErr("Job hung")
	}
	if t, ok := sh.term.(interface{ InterruptRead() }); ok {
		t.InterruptRead()
	}
	return nil
}

func (sys *System) jobsForPPN(proj, prog int) []int {
	want := fmt.Sprintf("[%d,%d]", proj, prog)
	sys.mu.Lock()
	defer sys.mu.Unlock()
	var nums []int
	seen := map[int]bool{}
	add := func(n int) {
		if n > 0 && !seen[n] {
			seen[n] = true
			nums = append(nums, n)
		}
	}
	for _, j := range sys.jobs {
		if j == nil {
			continue
		}
		if j.Who == want || j.OwnerPPN == want {
			add(j.Num)
		}
	}
	for n, sh := range sys.shells {
		if sh != nil && sh.Account != nil && sh.Account.Proj == proj && sh.Account.Prog == prog {
			add(n)
		}
	}
	for n, sh := range sys.parked {
		if sh != nil && sh.Account != nil && sh.Account.Proj == proj && sh.Account.Prog == prog {
			add(n)
		}
	}
	return nums
}

func (sys *System) HangupJob(token string) error {
	j := sys.FindJob(token)
	if j == nil {
		return fsErr("Can't find job")
	}
	if parked := sys.unpark(j.Num); parked != nil {
		parked.Basic.CloseAllFiles()
		sys.Detach(j.Num)
		return nil
	}
	sys.mu.Lock()
	sh := sys.shells[j.Num]
	link := (*pkLink)(nil)
	if jj := sys.jobs[j.Num]; jj != nil {
		link = jj.PKLink
	}
	sys.mu.Unlock()
	if link != nil {
		link.Hangup()
		return nil
	}
	if sh == nil {
		sys.Detach(j.Num)
		return nil
	}
	sh.Running = false
	if t, ok := sh.term.(interface{ InterruptRead() }); ok {
		t.InterruptRead()
	}
	select {
	case sh.forceCh <- "":
	default:
	}
	return nil
}

func (sys *System) Broadcast(token, from, text string) error {
	msg := fmt.Sprintf("\nFrom %s: %s\n", from, text)
	if strings.EqualFold(token, "ALL") || token == "" {
		sys.mu.Lock()
		shells := make([]*Shell, 0, len(sys.shells))
		for _, sh := range sys.shells {
			shells = append(shells, sh)
		}
		sys.mu.Unlock()
		for _, sh := range shells {
			fmt.Fprint(sh.out, msg)
		}
		return nil
	}
	j := sys.FindJob(token)
	if j == nil {
		return fsErr("Can't find job")
	}
	sys.mu.Lock()
	sh := sys.shells[j.Num]
	sys.mu.Unlock()
	if sh == nil {
		return fsErr("Job hung")
	}
	fmt.Fprint(sh.out, msg)
	return nil
}

func (sys *System) runOnTerm(job *Job, out io.Writer, term terminal, remote, login string, guest bool) {
	sh := sys.newSession(job, out, term)
	sh.AutoLogin = login
	sh.Guest = guest
	if remote == "CONSOLE" {
		sh.console = true
		sys.setConsole(sh)
		defer sys.setConsole(nil)
	}
	bind := func() {
		if remote == "CONSOLE" {
			sh.console = true
			sys.setConsole(sh)
		}
	}
	for {
		sh.Run()
		if sh.parked {
			nj, err := sys.Attach(remote)
			if err != nil {
				fmt.Fprintf(out, "?%s\n", err.Error())
				return
			}
			sh = sys.newSession(nj, out, term)
			bind()
			continue
		}
		if sh.attachTo != 0 {
			parked := sys.unpark(sh.attachTo)
			if parked == nil {
				fmt.Fprintf(out, "?Can't find job\n")
				nj, err := sys.Attach(remote)
				if err != nil {
					return
				}
				sh = sys.newSession(nj, out, term)
				bind()
				continue
			}
			parked.term = term
			parked.out = out
			parked.KB = sh.KB
			parked.Running = true
			parked.parked = false
			parked.skipBanner = true
			fmt.Fprintf(out, "Attached to job %d\n", parked.Job)
			sh = parked
			bind()
			continue
		}
		return
	}
}

func (s *Shell) cmdSystat(rest string) {
	s.syncJob()
	full, noHdr, loggedOnly := false, false, false
	want := ""
	sections := map[byte]bool{}
	for _, tok := range switchTokens(rest) {
		u := strings.ToUpper(tok)
		if !strings.HasPrefix(u, "/") {
			want = tok
			continue
		}
		sw := strings.TrimPrefix(u, "/")
		if sw == "" {
			continue
		}
		sw = sw[:1]
		switch sw {
		case "F":
			full = true
		case "N":
			noHdr = true
		case "U", "W", "L":
			loggedOnly = true
		case "A":
			for _, c := range "JDBKMRSH" {
				sections[byte(c)] = true
			}
		case "J":
			sections['J'] = true
		case "D":
			sections['D'] = true
		case "K", "T":
			sections['K'] = true
		case "M":
			sections['M'] = true
		case "R":
			sections['R'] = true
		case "S":
			sections['S'] = true
		case "B":
			sections['B'] = true
		case "H":
			sections['H'] = true
		case "O", "P":
			// /O output file and /P privileged-only were CUSP switches; ignored here
		default:
			fmt.Fprintf(s.out, "?SYSTAT/%s not implemented\n", sw)
			return
		}
	}
	if len(sections) == 0 {
		sections['J'] = true
	}
	if !noHdr {
		fmt.Fprintf(s.out, "Status of %s  at  %s  %s\n\n", SystemName, NowDate(), NowTime())
	}
	if sections['J'] {
		s.printSystatJobs(want, full, loggedOnly)
	}
	if sections['D'] {
		s.printDisks(true)
	}
	if sections['B'] {
		s.printSystatBusy()
	}
	if sections['K'] {
		s.printSystatKeyboards()
	}
	if sections['R'] {
		s.printSystatRTS()
	}
	if sections['M'] {
		s.printSystatMemory()
	}
	if sections['S'] {
		s.printSystatStats()
	}
	if sections['H'] {
		s.cmdHardware()
	}
}

func (s *Shell) printSystatJobs(want string, full, loggedOnly bool) {
	var jobs []Job
	if s.sys != nil {
		jobs = s.sys.JobList()
	} else {
		jobs = []Job{{Num: s.Job, KB: s.KB, Who: "*****", What: "LOGINS", Started: s.Boot, SizeK: 8, State: "KB"}}
	}
	if want != "" && s.sys != nil {
		if j := s.sys.FindJob(want); j != nil {
			jobs = []Job{*j}
		} else {
			fmt.Fprintln(s.out, "?Can't find job")
			return
		}
	}
	if loggedOnly {
		var live []Job
		for _, j := range jobs {
			if j.Who != "" && j.Who != "*****" {
				live = append(live, j)
			}
		}
		jobs = live
	}
	if full {
		fmt.Fprintln(s.out, "Job    Who       Where  What      Size  State   Run-Time   RTS")
	} else {
		fmt.Fprintln(s.out, "Job    Who       Where  What      Size  State   Run-Time")
	}
	for _, j := range jobs {
		who := strings.Trim(j.Who, "[]")
		line := fmt.Sprintf("%3d  %-9s %-6s %-9s %3dK  %-5s  %8s",
			j.Num, clip(who, 9), clip(j.where(), 6), clip(j.What, 9),
			j.sizeK(), j.state(), j.cpu())
		if full {
			line += "  BASIC"
		}
		fmt.Fprintln(s.out, line)
	}
	fmt.Fprintln(s.out)
}

func (s *Shell) printSystatKeyboards() {
	fmt.Fprintln(s.out, "Keyboards:")
	fmt.Fprintln(s.out, "KB     Job   Who       What      State")
	var jobs []Job
	if s.sys != nil {
		jobs = s.sys.JobList()
	} else {
		jobs = []Job{{Num: s.Job, KB: s.KB, Who: "*****", What: "LOGINS", State: "KB"}}
	}
	for _, j := range jobs {
		if j.Detached {
			continue
		}
		who := strings.Trim(j.Who, "[]")
		fmt.Fprintf(s.out, "%-6s %3d  %-9s %-9s %s\n",
			clip(j.KB, 6), j.Num, clip(who, 9), clip(j.What, 9), j.state())
	}
	fmt.Fprintln(s.out)
}

func (s *Shell) printSystatMemory() {
	used, njobs := 0, 1
	if s.sys != nil {
		jobs := s.sys.JobList()
		njobs = len(jobs)
		for _, j := range jobs {
			used += j.sizeK()
		}
	} else if s.Basic != nil {
		used = s.Basic.SizeKW()
	}
	free := MemoryKW - MonitorKW - RTSKW - used
	if free < 0 {
		free = 0
	}
	plural := "s"
	if njobs == 1 {
		plural = ""
	}
	fmt.Fprintln(s.out, "Memory:")
	fmt.Fprintf(s.out, "  Monitor   %5dK  resident\n", MonitorKW)
	fmt.Fprintf(s.out, "  BASIC     %5dK  RTS, reentrant, shared by %d job%s\n", RTSKW, njobs, plural)
	fmt.Fprintf(s.out, "  User      %5dK  %d job%s\n", used, njobs, plural)
	fmt.Fprintf(s.out, "  Free      %5dK\n", free)
	fmt.Fprintf(s.out, "  Cache     %5dK  bipolar\n", CacheKB)
	fmt.Fprintf(s.out, "  Total     %5dK  usable (%d KW 22-bit space)\n\n", MemoryKW, MemoryMaxKW)
	if s.sys != nil {
		fmt.Fprintln(s.out, "Job    Who       What      Size")
		for _, j := range s.sys.JobList() {
			who := strings.Trim(j.Who, "[]")
			fmt.Fprintf(s.out, "%3d  %-9s %-9s %4dK\n", j.Num, clip(who, 9), clip(j.What, 9), j.sizeK())
		}
		fmt.Fprintln(s.out)
	}
}

func (s *Shell) printSystatRTS() {
	njobs := 1
	if s.sys != nil {
		njobs = len(s.sys.JobList())
	}
	fmt.Fprintln(s.out, "Run-Time Systems:")
	fmt.Fprintln(s.out, " Name   Ext       Size  Users   Comments")
	fmt.Fprintf(s.out, " BASIC   BAC     16K    %3d    Perm, KBM, CSZ\n", njobs)
	fmt.Fprintln(s.out, " RT11    SAV      4K      0    Non-Res, KBM, CSZ")
	fmt.Fprintln(s.out, " RSX     TSK      3K      0    Non-Res, KBM")
	fmt.Fprintln(s.out)
}

func (s *Shell) printSystatStats() {
	njobs, nlog := 1, 0
	if s.Account != nil {
		nlog = 1
	}
	if s.sys != nil {
		jobs := s.sys.JobList()
		njobs = len(jobs)
		nlog = 0
		for _, j := range jobs {
			if j.Who != "" && j.Who != "*****" {
				nlog++
			}
		}
	}
	up := time.Since(s.Boot)
	if up < 0 {
		up = 0
	}
	used, cpu, open := 0, time.Duration(0), 0
	if s.sys != nil {
		for _, j := range s.sys.JobList() {
			used += j.sizeK()
			cpu += j.CPU
		}
		open = s.sys.openChannels()
	}
	free := MemoryKW - MonitorKW - RTSKW - used
	if free < 0 {
		free = 0
	}
	fmt.Fprintln(s.out, "Statistics:")
	fmt.Fprintf(s.out, "  Jobs      %d  (%d logged in, %d configured)\n", njobs, nlog, s.userLimit())
	fmt.Fprintf(s.out, "  Up        %s\n", formatCPU(up))
	fmt.Fprintf(s.out, "  CPU       %s  %d Hz  %s used\n", CPUName, ClockHz, formatCPU(cpu))
	fmt.Fprintf(s.out, "  Memory    %d K-words  (%dK user, %dK free)\n", MemoryKW, used, free)
	fmt.Fprintf(s.out, "  Files     %d open\n", open)
	if s.Disk != nil {
		var size, freeBlocks int
		for _, p := range s.Disk.Packs() {
			if !p.Mounted {
				continue
			}
			cap, used := s.Disk.PackUsage(p)
			size += cap
			freeBlocks += cap - used
		}
		fmt.Fprintf(s.out, "  Disk      %d blocks, %d free (mounted)\n", size, freeBlocks)
	}
	fmt.Fprintln(s.out)
}

func (s *Shell) printSystatBusy() {
	fmt.Fprintln(s.out, "Busy devices:")
	var jobs []Job
	if s.sys != nil {
		jobs = s.sys.JobList()
	} else {
		jobs = []Job{{Num: s.Job, KB: s.KB, Who: "*****", What: "LOGINS"}}
	}
	busy := false
	for _, j := range jobs {
		if j.Detached {
			continue
		}
		busy = true
		fmt.Fprintf(s.out, "  %-6s  Job %d  %s\n", j.KB, j.Num, j.What)
	}
	if s.Disk != nil {
		for _, p := range s.Disk.Packs() {
			if p.Mounted {
				busy = true
				fmt.Fprintf(s.out, "  %-6s  %s\n", p.Designator(), p.ID)
			}
		}
	}
	if !busy {
		fmt.Fprintln(s.out, "  None")
	}
	fmt.Fprintln(s.out)
}

func (s *Shell) cmdDetach() {
	if s.Account == nil {
		fmt.Fprintln(s.out, "?Please say HELLO")
		return
	}
	fmt.Fprintf(s.out, "Job %d detached from %s\n", s.Job, s.KB)
	s.parked = true
	s.Running = false
}

func (s *Shell) cmdAttach(rest string) {
	if s.sys == nil {
		fmt.Fprintln(s.out, "?Can't find job")
		return
	}
	tok := strings.TrimSpace(rest)
	if tok == "" {
		fmt.Fprintln(s.out, "?ATTACH job")
		return
	}
	j := s.sys.FindJob(tok)
	if j == nil {
		fmt.Fprintln(s.out, "?Can't find job")
		return
	}
	if !j.Detached {
		fmt.Fprintln(s.out, "?Job not detached")
		return
	}
	if !s.priv() && s.Account != nil && j.OwnerPPN != "" && j.OwnerPPN != s.Account.Display() && j.Who != s.Account.Display() {
		fmt.Fprintln(s.out, "?Protection violation")
		return
	}
	s.attachTo = j.Num
	s.Running = false
}

func (s *Shell) cmdForce(rest string) error {
	if !s.priv() {
		return fsErr("Protection violation")
	}
	kb, line := splitWhere(rest)
	if kb == "" || line == "" {
		fmt.Fprintln(s.out, "?FORCE kb: command")
		return nil
	}
	if s.sys == nil {
		return fsErr("Can't find job")
	}
	return s.sys.Force(kb, line)
}

func (s *Shell) cmdHangup(rest string) error {
	tok := strings.TrimSpace(rest)
	if tok == "" {
		fmt.Fprintln(s.out, "?HANGUP job")
		return nil
	}
	if s.sys == nil {
		return fsErr("Can't find job")
	}
	j := s.sys.FindJob(tok)
	if j == nil {
		return fsErr("Can't find job")
	}
	if !s.priv() && !(j.OwnerJob == s.Job) {
		return fsErr("Protection violation")
	}
	return s.sys.HangupJob(tok)
}

func (s *Shell) cmdBroadcast(rest string) error {
	kb, text := splitWhere(rest)
	if text == "" {
		fmt.Fprintln(s.out, "?BROADCAST [kb:] message")
		return nil
	}
	if s.sys == nil {
		return fsErr("Can't find job")
	}
	if kb == "" || strings.EqualFold(kb, "ALL") {
		if !s.priv() {
			return fsErr("Protection violation")
		}
		kb = "ALL"
	}
	from := "KB?"
	if s.Account != nil {
		from = s.Account.Display()
	}
	return s.sys.Broadcast(kb, from+" "+s.KB, text)
}

func splitWhere(rest string) (where, text string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", ""
	}
	upper := strings.ToUpper(rest)
	if strings.HasPrefix(upper, "ALL ") {
		return "ALL", strings.TrimSpace(rest[4:])
	}
	// FORCE KB3: SYSTAT   or  FORCE 3 SYSTAT
	if i := strings.IndexByte(rest, ':'); i >= 0 && i < 8 {
		return rest[:i+1], strings.TrimSpace(rest[i+1:])
	}
	parts := strings.Fields(rest)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.TrimSpace(rest[len(parts[0]):])
}
