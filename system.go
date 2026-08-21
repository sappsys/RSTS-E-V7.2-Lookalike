package rsts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrBusy        = errors.New("Maximum users exceeded")
	ErrInterrupt   = errors.New("interrupt")
	errForced      = errors.New("forced")
	errWaitTimeout = errors.New("keyboard wait exhausted")
)

// Job is one attached terminal (console, Telnet, or PK: slave).
type Job struct {
	Num      int
	KB       string
	Where    string
	Remote   string
	Who      string
	What     string
	SizeK    int
	CPU      time.Duration
	State    string
	Started  time.Time
	Detached bool
	PK       int // -1 if not a PK: job
	OwnerJob int
	OwnerPPN string
	PKLink   *pkLink
	RTS      string
}

// System is the shared timesharing host: disk, accounts, job table.
type System struct {
	mu       sync.Mutex
	Config   Config
	Accounts *AccountDB
	Disk     *Disk
	Boot     time.Time
	jobs     map[int]*Job
	shells   map[int]*Shell
	parked   map[int]*Shell
	packOpen map[string]int
	pkInUse  map[int]bool
	listener interface{ Close() error }
	shutdown chan struct{}
	console  *Shell
	noLogins bool
	kbOwner  map[string]*kbAssign
}

// kbAssign is a KBn: taken by OPEN. Bye/Ready on that line waits until CLOSE.
type kbAssign struct {
	ownerJob int
	peer     *Shell
	once     sync.Once
	parkOnce sync.Once
	done     chan struct{}
	parked   chan struct{}
}

func (a *kbAssign) release() {
	if a == nil {
		return
	}
	a.once.Do(func() { close(a.done) })
}

func (a *kbAssign) markParked() {
	if a == nil {
		return
	}
	a.parkOnce.Do(func() { close(a.parked) })
}

func NewSystem(diskRoot string, cfg Config) (*System, error) {
	cfg.clamp()
	if err := os.MkdirAll(diskRoot, 0o755); err != nil {
		return nil, err
	}
	db, err := OpenAccountDB(filepath.Join(diskRoot, "accounts.json"))
	if err != nil {
		return nil, err
	}
	disk, err := OpenDisk(diskRoot)
	if err != nil {
		return nil, err
	}
	sys := &System{
		Config:   cfg,
		Accounts: db,
		Disk:     disk,
		Boot:     time.Now(),
		jobs:     map[int]*Job{},
		shells:   map[int]*Shell{},
		parked:   map[int]*Shell{},
		pkInUse:  map[int]bool{},
		shutdown: make(chan struct{}),
	}
	disk.quotaOf = func(proj, prog int) int {
		a := db.FindPPN(proj, prog)
		if a == nil {
			return 0
		}
		return a.Quota
	}
	if err := sys.seedSamples(); err != nil {
		return nil, err
	}
	if err := sys.seedDefaultCCL(); err != nil {
		return nil, err
	}
	applyInitState(sys)
	sys.startSpooler()
	return sys, nil
}

func (sys *System) seedSamples() error {
	s := &Shell{Disk: sys.Disk}
	return s.seedSamples()
}

func (sys *System) Attach(remote string) (*Job, error) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if len(sys.jobs) >= sys.Config.MaxUsers {
		return nil, ErrBusy
	}
	n := 0
	for i := 1; i <= sys.Config.MaxUsers; i++ {
		if sys.jobs[i] == nil {
			n = i
			break
		}
	}
	if n == 0 {
		return nil, ErrBusy
	}
	j := &Job{
		Num:     n,
		KB:      fmt.Sprintf("KB%d:", n-1),
		Where:   fmt.Sprintf("KB%d:", n-1),
		Remote:  remote,
		Who:     "*****",
		What:    "LOGINS",
		SizeK:   8,
		State:   "KB",
		Started: time.Now(),
		PK:      -1,
		RTS:     "BASIC",
	}
	sys.jobs[n] = j
	return j, nil
}

func (sys *System) Detach(num int) {
	sys.releaseKBForJob(num)
	sys.mu.Lock()
	delete(sys.jobs, num)
	sys.mu.Unlock()
}

func (sys *System) SetJob(num int, who, what string) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if j := sys.jobs[num]; j != nil {
		j.Who = who
		j.What = what
	}
}

func (sys *System) JobList() []Job {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	out := make([]Job, 0, len(sys.jobs))
	for i := 1; i <= sys.Config.MaxUsers; i++ {
		j := sys.jobs[i]
		if j == nil {
			continue
		}
		job := *j
		// CPU is charged as a program runs, so read it live rather than
		// waiting for the job to come back to Ready and resync.
		if sh := sys.shells[i]; sh != nil && sh.Basic != nil {
			job.CPU = sh.Basic.CPUTime()
		}
		out = append(out, job)
	}
	return out
}

// notePackOpen tracks how many files each pack has open, which is what the
// Open column of SYSTAT/D reports.
func (sys *System) notePackOpen(dev string, delta int) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	if sys.packOpen == nil {
		sys.packOpen = map[string]int{}
	}
	sys.packOpen[dev] += delta
	if sys.packOpen[dev] <= 0 {
		delete(sys.packOpen, dev)
	}
}

func (sys *System) openOnPack(p *Pack) int {
	if p == nil {
		return 0
	}
	sys.mu.Lock()
	defer sys.mu.Unlock()
	return sys.packOpen[p.Designator()]
}

func (sys *System) openChannels() int {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	n := 0
	for _, c := range sys.packOpen {
		n += c
	}
	return n
}

func (sys *System) JobCount() int {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	return len(sys.jobs)
}

func (sys *System) Close() {
	sys.mu.Lock()
	ln := sys.listener
	sys.listener = nil
	sys.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	select {
	case <-sys.shutdown:
	default:
		close(sys.shutdown)
	}
}

func (sys *System) Halted() bool {
	select {
	case <-sys.shutdown:
		return true
	default:
		return false
	}
}

// shellOnKB finds the session sitting on a keyboard, so that a program
// can open another terminal as a channel.
func (sys *System) shellOnKB(kb string) *Shell {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	for _, sh := range sys.shells {
		if sh != nil && strings.EqualFold(sh.KB, kb) {
			return sh
		}
	}
	return nil
}

func interruptShellTerm(sh *Shell) {
	if sh == nil || sh.term == nil {
		return
	}
	if t, ok := sh.term.(interface{ InterruptRead() }); ok {
		t.InterruptRead()
	}
}

// takeKB assigns a free (Bye) keyboard to owner. A logged-in line needs
// privilege. I/O on that KBn: then goes through the OPEN channel until CLOSE.
func (sys *System) takeKB(peer, owner *Shell) error {
	if sys == nil || peer == nil || owner == nil {
		return basicErr("Not a valid device")
	}
	sys.mu.Lock()
	if sys.kbOwner == nil {
		sys.kbOwner = map[string]*kbAssign{}
	}
	kb := peer.KB
	if sys.kbOwner[kb] != nil {
		sys.mu.Unlock()
		return basicErrCode("Account or device in use", 3)
	}
	if j := sys.jobs[peer.Job]; j != nil && j.PK >= 0 {
		sys.mu.Unlock()
		return basicErr("Not a valid device")
	}
	loggedIn := peer.Account != nil
	if j := sys.jobs[peer.Job]; j != nil && j.Who != "" && j.Who != "*****" {
		loggedIn = true
	}
	if loggedIn && !owner.priv() {
		sys.mu.Unlock()
		return basicErr("Protection violation")
	}
	a := &kbAssign{ownerJob: owner.Job, peer: peer, done: make(chan struct{}), parked: make(chan struct{})}
	sys.kbOwner[kb] = a
	sys.mu.Unlock()
	if t, ok := peer.term.(interface{ SetStolen(bool) }); ok {
		t.SetStolen(true)
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-a.parked:
		case <-timer.C:
		}
	} else {
		interruptShellTerm(peer)
	}
	return nil
}

func (sys *System) dropKB(kb string, ownerJob int) {
	if sys == nil {
		return
	}
	sys.mu.Lock()
	a := sys.kbOwner[kb]
	if a == nil {
		sys.mu.Unlock()
		return
	}
	if ownerJob != 0 && a.ownerJob != ownerJob {
		sys.mu.Unlock()
		return
	}
	delete(sys.kbOwner, kb)
	peer := a.peer
	sys.mu.Unlock()
	if peer != nil {
		if t, ok := peer.term.(interface{ SetStolen(bool) }); ok {
			t.SetStolen(false)
		}
	}
	a.release()
}

func (sys *System) kbAssigned(kb string) bool {
	if sys == nil {
		return false
	}
	sys.mu.Lock()
	defer sys.mu.Unlock()
	return sys.kbOwner[kb] != nil
}

func (sys *System) waitKBReleased(kb string) {
	for {
		sys.mu.Lock()
		a := sys.kbOwner[kb]
		sys.mu.Unlock()
		if a == nil {
			return
		}
		a.markParked()
		<-a.done
	}
}

func (sys *System) releaseKBForJob(num int) {
	if sys == nil {
		return
	}
	sys.mu.Lock()
	var peers []*Shell
	for kb, a := range sys.kbOwner {
		if a.ownerJob == num || (a.peer != nil && a.peer.Job == num) {
			delete(sys.kbOwner, kb)
			if a.peer != nil {
				peers = append(peers, a.peer)
			}
			a.release()
		}
	}
	sys.mu.Unlock()
	for _, peer := range peers {
		if t, ok := peer.term.(interface{ SetStolen(bool) }); ok {
			t.SetStolen(false)
		}
	}
}

func (sys *System) setConsole(s *Shell) {
	sys.mu.Lock()
	sys.console = s
	sys.mu.Unlock()
}

// InterruptConsole stops a running BASIC program on the local console.
// It returns false if there is no live console session (SIGINT may halt the emulator).
func (sys *System) InterruptConsole() bool {
	sys.mu.Lock()
	sh := sys.console
	live := sh != nil && sh.Running
	sys.mu.Unlock()
	if !live {
		return false
	}
	if sh.Basic != nil {
		sh.Basic.Interrupt()
	}
	if t, ok := sh.term.(interface{ InterruptRead() }); ok {
		t.InterruptRead()
	}
	return true
}
