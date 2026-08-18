package rsts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PLEASE is a message to the operator console (sys.console, KB0).
// The queue is disk/please.json. Privileged jobs and the console may
// list (/LI) and reply (/RE); anyone logged in may send.

type pleaseMsg struct {
	ID    int       `json:"id"`
	From  string    `json:"from"`
	Job   int       `json:"job"`
	KB    string    `json:"kb"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	Reply string    `json:"reply,omitempty"`
}

type pleaseFile struct {
	Next int         `json:"next"`
	Msgs []pleaseMsg `json:"msgs"`
}

func (sys *System) pleasePath() string {
	return filepath.Join(sys.Disk.Root, "please.json")
}

func (sys *System) loadPlease() pleaseFile {
	var q pleaseFile
	data, err := os.ReadFile(sys.pleasePath())
	if err != nil {
		return pleaseFile{Next: 1}
	}
	if json.Unmarshal(data, &q) != nil {
		return pleaseFile{Next: 1}
	}
	if q.Next < 1 {
		q.Next = 1
	}
	return q
}

func (sys *System) savePlease(q pleaseFile) error {
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sys.pleasePath(), append(data, '\n'), 0o644)
}

func (sys *System) sendPlease(from *Shell, text string) error {
	who := "?"
	job := 0
	kb := ""
	if from != nil {
		job = from.Job
		kb = from.KB
		if from.Account != nil {
			who = from.Account.Display()
		}
	}
	sys.mu.Lock()
	q := sys.loadPlease()
	id := q.Next
	if id < 1 {
		id = 1
	}
	q.Next = id + 1
	msg := pleaseMsg{
		ID:   id,
		From: who,
		Job:  job,
		KB:   kb,
		Text: text,
		At:   time.Now(),
	}
	q.Msgs = append(q.Msgs, msg)
	_ = sys.savePlease(q)
	console := sys.console
	sys.mu.Unlock()
	line := fmt.Sprintf("\nPLEASE %d from %s %s:\n  %s\n", id, who, kb, text)
	if console != nil && console.out != nil {
		fmt.Fprint(console.out, line)
	}
	return nil
}

func (s *Shell) cmdPlease(rest string) error {
	if _, err := s.needLogin(); err != nil {
		return err
	}
	sw, arg := parseCmdSwitches(rest)
	if switchOn(sw, "LI", "LIST") {
		if !s.priv() && !s.console {
			return fsErr("Protection violation")
		}
		return s.listPlease()
	}
	if switchOn(sw, "RE", "REPLY") {
		if !s.priv() && !s.console {
			return fsErr("Protection violation")
		}
		return s.replyPlease(arg)
	}
	if strings.TrimSpace(arg) == "" {
		var err error
		arg, err = s.readLine("Message-- ")
		if err != nil {
			return err
		}
		arg = strings.TrimSpace(arg)
	}
	if arg == "" {
		fmt.Fprintln(s.out, "?PLEASE message")
		return nil
	}
	if s.sys == nil {
		fmt.Fprintln(s.out, "Message logged (no operator console)")
		return nil
	}
	if err := s.sys.sendPlease(s, arg); err != nil {
		return err
	}
	fmt.Fprintln(s.out, "Message sent to operator")
	return nil
}

func (s *Shell) listPlease() error {
	if s.sys == nil {
		fmt.Fprintln(s.out, "%No PLEASE queue")
		return nil
	}
	s.sys.mu.Lock()
	q := s.sys.loadPlease()
	s.sys.mu.Unlock()
	fmt.Fprintln(s.out, " PLEASE  operator queue")
	if len(q.Msgs) == 0 {
		fmt.Fprintln(s.out, "%Queue empty")
		return nil
	}
	fmt.Fprintf(s.out, "%-5s  %-10s  %-6s  %s\n", "Id", "Who", "KB", "Text")
	for _, m := range q.Msgs {
		text := m.Text
		if m.Reply != "" {
			text = text + "  -> " + m.Reply
		}
		fmt.Fprintf(s.out, "%-5d  %-10s  %-6s  %s\n", m.ID, m.From, m.KB, text)
	}
	return nil
}

func (s *Shell) replyPlease(arg string) error {
	kb, text := splitWhere(arg)
	if text == "" {
		fmt.Fprintln(s.out, "?PLEASE/RE job message")
		return nil
	}
	if s.sys == nil {
		return fsErr("Can't find job")
	}
	s.sys.mu.Lock()
	q := s.sys.loadPlease()
	for i := range q.Msgs {
		m := &q.Msgs[i]
		if !pleaseMatch(*m, kb) {
			continue
		}
		m.Reply = text
		tok := strings.TrimSuffix(strings.TrimSpace(kb), ":")
		if kb == "" || strconv.Itoa(m.ID) == tok {
			kb = m.KB
		}
		break
	}
	_ = s.sys.savePlease(q)
	s.sys.mu.Unlock()
	return s.sys.Broadcast(kb, "OPR", text)
}

// pleaseMatch is true when tok names this queue entry: PLEASE id, job
// number, or keyboard (KB0 or KB0:).
func pleaseMatch(m pleaseMsg, tok string) bool {
	tok = strings.TrimSpace(strings.TrimSuffix(tok, ":"))
	if tok == "" {
		return false
	}
	if strconv.Itoa(m.ID) == tok || strconv.Itoa(m.Job) == tok {
		return true
	}
	return strings.EqualFold(strings.TrimSuffix(m.KB, ":"), tok)
}
