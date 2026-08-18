package rsts

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// batchTerm feeds a detached SUBMIT/BATCH job from a command file.
type batchTerm struct {
	lines []string
	i     int
}

func (t *batchTerm) ReadLine(prompt string) (string, error) {
	if t.i >= len(t.lines) {
		return "", io.EOF
	}
	s := t.lines[t.i]
	t.i++
	return s, nil
}

func (t *batchTerm) ReadPassword(prompt string) (string, error) {
	return t.ReadLine(prompt)
}

func (s *Shell) cmdQuolst(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	sw, arg := parseCmdSwitches(rest)
	if switchOn(sw, "SET", "QUOTA") {
		return s.cmdSetQuota(arg, true, false)
	}
	if switchOn(sw, "JOB", "JQ", "JOBQUOTA") {
		return s.cmdSetQuota(arg, false, true)
	}
	want := acct
	if arg != "" {
		if !s.priv() {
			return fsErr("Protection violation")
		}
		want = s.Accounts.Find(arg)
		if want == nil {
			if proj, prog, perr := ParsePPN(arg); perr == nil {
				want = s.Accounts.FindPPN(proj, prog)
			}
		}
		if want == nil {
			return fsErr("Can't find file or account")
		}
	}
	folder, err := s.Disk.AccountDir(want.Proj, want.Prog)
	if err != nil {
		return err
	}
	used := s.Disk.folderBlocks(folder)
	q := want.Quota
	fmt.Fprintf(s.out, "%s  %s\n", want.Display(), want.Name)
	if q <= 0 {
		fmt.Fprintf(s.out, "Disk quota    unlimited    used %d\n", used)
	} else {
		free := q - used
		if free < 0 {
			free = 0
		}
		fmt.Fprintf(s.out, "Disk quota    %d    used %d    free %d\n", q, used, free)
	}
	jobs := 0
	if s.sys != nil {
		jobs = len(s.sys.jobsForPPN(want.Proj, want.Prog))
	}
	jq := want.JobQuota
	if jq <= 0 {
		fmt.Fprintf(s.out, "Job quota     unlimited    logged in %d\n", jobs)
	} else {
		fmt.Fprintf(s.out, "Job quota     %d    logged in %d\n", jq, jobs)
	}
	return nil
}

func (s *Shell) cmdSetQuota(arg string, setDisk, setJob bool) error {
	if err := s.needPriv(); err != nil {
		return err
	}
	if s.Accounts == nil {
		return fsErr("Can't find file or account")
	}
	who, n, err := parseQuotaArgs(arg)
	if err != nil {
		fmt.Fprintln(s.out, "?REACT QUOTA [p,pn] n")
		return nil
	}
	acct := s.Account
	if who != "" {
		acct = s.Accounts.Find(who)
		if acct == nil {
			if proj, prog, perr := ParsePPN(who); perr == nil {
				acct = s.Accounts.FindPPN(proj, prog)
			}
		}
	}
	if acct == nil {
		return fsErr("Can't find file or account")
	}
	quota, jobQuota := n, n
	if !setDisk {
		quota = acct.Quota
	}
	if !setJob {
		jobQuota = acct.JobQuota
	}
	updated, err := s.Accounts.SetQuota(acct.Proj, acct.Prog, quota, jobQuota, setDisk, setJob)
	if err != nil {
		return err
	}
	if s.sys != nil {
		s.sys.applyAccountQuota(updated)
	} else {
		acct.Quota = updated.Quota
		acct.JobQuota = updated.JobQuota
		if s.Account != nil && s.Account.Proj == updated.Proj && s.Account.Prog == updated.Prog {
			s.Account.Quota = updated.Quota
			s.Account.JobQuota = updated.JobQuota
			if s.Basic != nil {
				s.Basic.IO.Quota = updated.Quota
			}
		}
	}
	if setDisk {
		fmt.Fprintf(s.out, "Quota %s  %d\n", updated.Display(), updated.Quota)
	}
	if setJob {
		fmt.Fprintf(s.out, "Job quota %s  %d\n", updated.Display(), updated.JobQuota)
	}
	return nil
}

func parseQuotaArgs(arg string) (who string, n int, err error) {
	fields := strings.Fields(strings.TrimSpace(arg))
	if len(fields) == 0 {
		return "", 0, fmt.Errorf("Illegal number")
	}
	n, err = strconv.Atoi(fields[len(fields)-1])
	if err != nil || n < 0 {
		return "", 0, fmt.Errorf("Illegal number")
	}
	if len(fields) > 1 {
		who = strings.Join(fields[:len(fields)-1], "")
	}
	return who, n, nil
}

func (sys *System) applyAccountQuota(acct *Account) {
	if sys == nil || acct == nil {
		return
	}
	sys.mu.Lock()
	defer sys.mu.Unlock()
	for _, sh := range sys.shells {
		if sh == nil || sh.Account == nil {
			continue
		}
		if sh.Account.Proj == acct.Proj && sh.Account.Prog == acct.Prog {
			sh.Account.Quota = acct.Quota
			sh.Account.JobQuota = acct.JobQuota
			if sh.Basic != nil {
				sh.Basic.IO.Quota = acct.Quota
			}
		}
	}
}

func (s *Shell) cmdSubmit(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	if s.sys == nil {
		fmt.Fprintln(s.out, "?SUBMIT")
		return nil
	}
	arg := strings.TrimSpace(rest)
	if arg == "" {
		fmt.Fprintln(s.out, "?SUBMIT filespec")
		return nil
	}
	spec, err := s.parseSpec(arg, "")
	if err != nil {
		return err
	}
	text, err := s.Disk.ReadText(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return err
	}
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		fmt.Fprintln(s.out, "?Can't find file or account")
		return nil
	}
	job, err := s.sys.Attach("BATCH")
	if err != nil {
		return err
	}
	job.Detached = true
	job.State = "Det"
	job.Where = "Det"
	job.Who = acct.Display()
	job.OwnerPPN = acct.Display()
	job.What = spec.Filename()
	sh := s.sys.newSession(job, io.Discard, &batchTerm{lines: lines})
	sh.Account = acct
	sh.tempPriv = s.tempPriv
	sh.syncPrivilege()
	if sh.Basic != nil {
		sh.Basic.IO.PPN = acct.Display()
		sh.Basic.IO.AccountName = acct.Name
		sh.Basic.IO.Quota = acct.Quota
		sh.Basic.IO.Job = job.Num
	}
	sh.syncJob()
	go func() {
		sh.skipBanner = true
		sh.Run()
	}()
	fmt.Fprintf(s.out, "Job %d submitted\n", job.Num)
	return nil
}

func (s *Shell) cmdShutup(rest string) error {
	if err := s.needPriv(); err != nil {
		return err
	}
	if s.sys == nil {
		s.Running = false
		return nil
	}
	fmt.Fprintf(s.out, "%s shutting down\n", SystemName)
	s.sys.interruptAll()
	s.sys.Close()
	s.Running = false
	return nil
}

func (s *Shell) cmdUtility(rest string) error {
	if err := s.needPriv(); err != nil {
		return err
	}
	arg := strings.ToUpper(strings.TrimSpace(rest))
	if i := strings.IndexByte(arg, '/'); i >= 0 {
		arg = strings.TrimSpace(arg[:i] + " " + arg[i+1:])
	}
	switch {
	case arg == "" || arg == "LI" || strings.HasPrefix(arg, "LI "):
		fmt.Fprintln(s.out, "UTILITY")
		fmt.Fprintln(s.out, "  REACT     accounts / quotas")
		fmt.Fprintln(s.out, "  DSKINT    initialize a pack")
		fmt.Fprintln(s.out, "  CCL       install a keyboard command")
		fmt.Fprintln(s.out, "  SHUTUP    halt the system")
		return nil
	case strings.HasPrefix(arg, "SHUT"):
		return s.cmdShutup("")
	case strings.HasPrefix(arg, "CCL"):
		return s.cmdCCL(strings.TrimSpace(arg[3:]))
	case strings.HasPrefix(arg, "REACT"):
		return s.cmdReact(strings.TrimSpace(arg[5:]))
	case strings.HasPrefix(arg, "DSKINT"):
		return s.cmdDskint(strings.TrimSpace(arg[6:]))
	default:
		fmt.Fprintln(s.out, "REACT  DSKINT  CCL  SHUTUP")
		return nil
	}
}

func (sys *System) interruptAll() {
	if sys == nil {
		return
	}
	sys.mu.Lock()
	shells := make([]*Shell, 0, len(sys.shells)+len(sys.parked))
	for _, sh := range sys.shells {
		shells = append(shells, sh)
	}
	for _, sh := range sys.parked {
		shells = append(shells, sh)
	}
	sys.mu.Unlock()
	for _, sh := range shells {
		if sh == nil {
			continue
		}
		sh.Running = false
		if sh.Basic != nil {
			sh.Basic.Interrupt()
		}
		if t, ok := sh.term.(interface{ InterruptRead() }); ok {
			t.InterruptRead()
		}
	}
}
