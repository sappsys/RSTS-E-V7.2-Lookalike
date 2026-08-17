package rsts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrBusy      = errors.New("Maximum users exceeded")
	ErrInterrupt = errors.New("interrupt")
	errForced    = errors.New("forced")
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
	State    string
	Started  time.Time
	Detached bool
	PK       int // -1 if not a PK: job
	OwnerJob int
	OwnerPPN string
	PKLink   *pkLink
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
	pkInUse  map[int]bool
	listener interface{ Close() error }
	shutdown chan struct{}
	console  *Shell
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
	if err := sys.seedSamples(); err != nil {
		return nil, err
	}
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
	}
	sys.jobs[n] = j
	return j, nil
}

func (sys *System) Detach(num int) {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	delete(sys.jobs, num)
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
		if j := sys.jobs[i]; j != nil {
			out = append(out, *j)
		}
	}
	return out
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
