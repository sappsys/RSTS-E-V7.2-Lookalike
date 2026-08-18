package rsts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type spoolJob struct {
	ID   int       `json:"id"`
	Who  string    `json:"who"`
	Job  int       `json:"job"`
	File string    `json:"file"`
	At   time.Time `json:"at"`
}

type spoolFile struct {
	Next int        `json:"next"`
	Jobs []spoolJob `json:"jobs"`
}

func (sys *System) queuePath() string {
	return filepath.Join(sys.Disk.Root, "queue.json")
}

func (sys *System) loadQueue() spoolFile {
	var q spoolFile
	data, err := os.ReadFile(sys.queuePath())
	if err != nil {
		return spoolFile{Next: 1}
	}
	if json.Unmarshal(data, &q) != nil {
		return spoolFile{Next: 1}
	}
	if q.Next < 1 {
		q.Next = 1
	}
	return q
}

func (sys *System) saveQueue(q spoolFile) error {
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sys.queuePath(), append(data, '\n'), 0o644)
}

func (sys *System) enqueuePrint(who string, job int, file string) int {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	q := sys.loadQueue()
	id := q.Next
	if id < 1 {
		id = 1
	}
	q.Next = id + 1
	q.Jobs = append(q.Jobs, spoolJob{
		ID:   id,
		Who:  who,
		Job:  job,
		File: file,
		At:   time.Now(),
	})
	_ = sys.saveQueue(q)
	return id
}

func (sys *System) listQueue() []spoolJob {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	return sys.loadQueue().Jobs
}

func (sys *System) deleteQueue(id int, who string, priv bool) bool {
	sys.mu.Lock()
	defer sys.mu.Unlock()
	q := sys.loadQueue()
	kept := q.Jobs[:0]
	found := false
	for _, j := range q.Jobs {
		if j.ID == id && (priv || j.Who == who) {
			found = true
			continue
		}
		kept = append(kept, j)
	}
	q.Jobs = kept
	_ = sys.saveQueue(q)
	return found
}

func (s *Shell) cmdQue(rest string) error {
	if s.sys == nil {
		fmt.Fprintln(s.out, "?QUE")
		return nil
	}
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	sw, arg := parseCmdSwitches(rest)
	if switchOn(sw, "DE", "DELETE") {
		id := 0
		fmt.Sscanf(arg, "%d", &id)
		if id < 1 {
			fmt.Fprintln(s.out, "?QUE/DE n")
			return nil
		}
		who := ""
		if acct != nil {
			who = acct.Display()
		}
		if !s.sys.deleteQueue(id, who, s.priv()) {
			fmt.Fprintln(s.out, "?Can't find file or account")
		}
		return nil
	}
	if arg != "" && !switchOn(sw, "LI", "LIST") {
		spec, err := s.parseSpec(arg, "")
		if err != nil {
			return err
		}
		who := ""
		if acct != nil {
			who = acct.Display()
		}
		id := s.sys.enqueuePrint(who, s.Job, spec.Filename())
		fmt.Fprintf(s.out, "Job %d queued to LP:\n", id)
		return nil
	}
	jobs := s.sys.listQueue()
	fmt.Fprintln(s.out, " LP:  print queue")
	if len(jobs) == 0 {
		fmt.Fprintln(s.out, "%Queue empty")
		return nil
	}
	fmt.Fprintf(s.out, "%-5s  %-10s  %s\n", "Job", "Who", "File")
	for _, j := range jobs {
		fmt.Fprintf(s.out, "%-5d  %-10s  %s\n", j.ID, j.Who, j.File)
	}
	return nil
}

func (s *Shell) enqueueClosedPrinter(path string) {
	if s.sys == nil || path == "" {
		return
	}
	who := ""
	if s.Account != nil {
		who = s.Account.Display()
	}
	s.sys.enqueuePrint(who, s.Job, filepath.Base(path))
}

// startSpooler is the QUMRUN stand-in: a background drain of QUE onto
// the host printer file LP0 under the disk root.
func (sys *System) startSpooler() {
	if sys == nil {
		return
	}
	go sys.runSpooler()
}

func (sys *System) runSpooler() {
	if sys.sleepOrStop(300 * time.Millisecond) {
		return
	}
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-sys.shutdown:
			return
		case <-t.C:
			sys.drainOne()
		}
	}
}

func (sys *System) drainOne() {
	sys.mu.Lock()
	q := sys.loadQueue()
	if len(q.Jobs) == 0 {
		sys.mu.Unlock()
		return
	}
	job := q.Jobs[0]
	q.Jobs = q.Jobs[1:]
	_ = sys.saveQueue(q)
	sys.mu.Unlock()
	sys.printJob(job)
}

func (sys *System) printJob(job spoolJob) {
	var data []byte
	if proj, prog, err := ParsePPN(job.Who); err == nil && sys.Disk != nil {
		if folder, err := sys.Disk.AccountDir(proj, prog); err == nil {
			data, _ = os.ReadFile(filepath.Join(folder, job.File))
		}
	}
	if sys.Disk == nil {
		return
	}
	out := filepath.Join(sys.Disk.Root, "LP0")
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n\f*** Job %d  %s  %s ***\n", job.ID, job.Who, job.File)
	if len(data) > 0 {
		_, _ = f.Write(data)
		if data[len(data)-1] != '\n' {
			_, _ = f.Write([]byte("\n"))
		}
	}
}

func (s *Shell) cmdQumrun(rest string) error {
	if s.sys == nil {
		fmt.Fprintln(s.out, "?QUMRUN")
		return nil
	}
	fmt.Fprintln(s.out, "QUMRUN  LP0:")
	return s.cmdQue(rest)
}
