package rsts

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/term"
)

const (
	SystemName = "RSTS V7.2-10"
	Version    = "7.2-10"
)

var helpText = map[string]string{
	"": `RSTS/E V7.2  —  type HELP topic

Topics:
  LOGIN     HELLO / BYE
  FILES     DIR, TYPE, COPY, KILL, NAME
  BASIC     NEW, OLD, SAVE, COMPILE, LIST, RUN
  LANG      BASIC-PLUS statements
  FN        built-in functions
  COMMANDS  keyboard commands
  SYSTAT    jobs, disks, memory  (SYS, WHO)
  SHOW      SHOW DISKS / JOBS / CPU / ...
  DISKS     MOUNT, DISMOUNT, packs
  ACCOUNTS  default logins
  COMPILE   .BAC files and the privilege bit
  HARDWARE  PDP-11/70 configuration
  TELNET    multi-user Telnet / VT52
  JOBS      SYSTAT, ATTACH, PK:
  HELP      how to use HELP

Abbreviations work (HELP DISK = HELP DISKS).  HELP MOUNT, HELP SYSTAT,
HELP DIRECTORY, HELP HELLO, and HELP PIP are accepted.
`,
	"LOGIN": `HELLO [account]     log in  (account is PPN like 100,100 or a name)
BYE                 log out (returns to Bye)
PASSWORD            change your password
PASSWORD [p,pn]     (priv) set another account's password

Logged-out prompt is  Bye
Logged-in prompt is   Ready

At Bye:
  HELLO             log in
  EXIT / QUIT       stop the emulator (console)
  BYE               hang up a Telnet line; on the console, stay at Bye

Ctrl-C stops a running BASIC program and returns to Ready.
It does not exit the emulator.
`,
	"FILES": `DIR [filespec]              catalog of files
CAT / CATALOG               same as DIR
TYPE filespec               print a file
COPY src dst                copy a file
PIP dst=src                 copy (PIP syntax)
KILL filespec               delete a file
UNSAVE filespec             delete a file
NAME old AS new             rename
NAME old AS new<prot>       rename and/or set protection
SYSTAT / SYS / WHO          jobs (RSTS columns)
ATTACH / DETACH             reconnect a detached job
FORCE kb: command           (priv) type a line at another job
HANGUP job                  (priv, or your PK: child)
BROADCAST / SEND            message to a keyboard
SHOW CPU / HARDWARE         PDP-11/70 configuration
SYSTAT/D / SHOW DISKS       mounted disk packs
MOUNT / DISMOUNT / DSKINT   private and public packs
DATE / TIME                 clock

Filespecs:  NAME.EXT  [p,pn]NAME.EXT  SY:[p,pn]NAME.EXT  $NAME
            DB1:NAME.EXT   PAYROL:NAME.EXT   NAME.EXT<prot>
OLD/SAVE default extension is .BAS
COMPILE default extension is .BAC
Protection (V7.2): 60 default, 64 compiled, 128 privileged.
  <124>  compiled, owner-only
  <232>  privileged compiled, world-runnable
`,
	"BASIC": `NEW [name]          clear memory, optionally name the program
OLD name            load a .BAS file (or .BAC if you can read it)
SAVE [name]         write the current program as .BAS
REPLACE [name]      save, overwriting
COMPILE [name]      compile to .BAC  (default protection <124>)
COMPILE name<prot>  compile with an explicit protection code
LIST [n[-m]]        list program lines
RUN [name]          run (loads .BAC then .BAS if a name is given)
RUNNH / LISTNH      same, without the header
DELETE n[-m]        delete program lines
CLEAR               reset variables

A line that starts with a number is stored in the program:
  10 PRINT "HI"
  20 END
  RUN
`,
	"LANG": `Statements:
  LET  PRINT  INPUT  LINE INPUT  PRINT USING
  GOTO  GOSUB  RETURN  ON ... GOTO/GOSUB
  IF ... THEN ... ELSE
  FOR ... TO ... STEP / NEXT
  WHILE ... / NEXT   UNTIL ... / NEXT
  DIM  DATA  READ  RESTORE  CHANGE  MAT
  MAT READ/PRINT/INPUT  MAT C = A+B / A-B / A*B / (K)*A
  MAT C = ZER / CON / IDN / TRN(A) / INV(A)
  OPEN ... [FOR INPUT/OUTPUT/APPEND] AS FILE #n [, RECORDSIZE n]
  OPEN ... AS FILE #n, ORGANIZATION VIRTUAL
  OPEN "PK:" AS FILE n      spawn a job on a pseudo keyboard
  MAP (name) LONG X%, STRING A$ = n
  GET #n [, RECORD n]   PUT #n [, RECORD n]
  FIELD #n, n AS A$   LSET / RSET
  CLOSE #n  RANDOMIZE  DEF FNx = ...
  ON ERROR GOTO n / 0   RESUME [NEXT | n]
  END  STOP  REM  (or ! comment)

Statement modifiers (rightmost is outermost):
  statement IF cond
  statement UNLESS cond
  statement WHILE cond
  statement UNTIL cond
  statement FOR v=a TO b [STEP s]

Several statements on one line are separated by \
Integer divide is also \  (inside an expression)
Relational true is -1, false is 0
`,
	"FN": `Numeric: ABS INT FIX SGN SQR SIN COS TAN ATN LOG EXP RND PI ERR ERL
         PEEK SWAP% TIME DATE
String:  LEN LEFT$ RIGHT$ MID$ INSTR CHR$ ASC STR$ VAL NUM1$ NUM$
         SPACE$ STRING$ DATE$ TIME$ TAB SPC POS SYS
         CVT%$ CVT$% CVTF$ CVT$F CVT$$

DATE / DATE(0)   integer date  (year-1970)*1000 + yearday
TIME / TIME(0)   seconds since midnight (KW11-L 60 Hz clock)
TIME(1)          CPU seconds this job
DATE$ / TIME$    printable date and time
PEEK(addr)       16-bit word at even byte address (monitor / I/O page)
SWAP%(n)         swap bytes of a 16-bit word (T%(11%)+SWAP%(T%(12%)))
RIGHT$(s,n)      from character n to the end (BASIC-PLUS, not last-n)

SYS(CHR$(n)+...): 1=system, 2=PPN, 3=job, 4=program, 5=date,
  6=FIP  0/-21=binary PPN  -3=UU.TB1  -12=UU.TB2  9=ident
  7=time, 9=pack SY
ERR and ERL are the last trapped error number and line.
CVT%$ / CVT$% pack 16-bit integers.
CVTF$ / CVT$F pack IEEE float32 (the real 11/70 FPP was FP11-C).
`,
	"HARDWARE": `This is RSTS/E V7.2-10 on a PDP-11/70.

  CPU       PDP-11/70, 22-bit physical addressing
  Memory    1920 K-words usable (4 MW byte space; I/O page at the top)
  Cache     2K-byte bipolar cache
  FPP       FP11-C
  Clock     KW11-L line-frequency clock at 60 Hz
  Buses     MASSBUS (RH70) and UNIBUS
  Disk      SY0:/DB0: RP06 (SYSDSK); DB1: RP06; DL0:/DL1: RL02; DM0: RK07
  Console   KB0: DL11/KL11
  Jobs      63 maximum (V7.2)

Monitor words CUSPs often PEEK:

  PEEK(512%)     date integer
  PEEK(514%)     minutes to midnight
  PEEK(516%)     seconds / ticks
  PEEK(518%)     job number * 2 in the low byte
  PEEK(156%)     pointer to config table
  PEEK(-136%)    console switch register  (177570 octal)
  PEEK(-154%)    KW11-L CSR               (177546 octal)
  PEEK(-2%)      PSW                      (177776 octal)

SYS(CHR$(6%)+CHR$(-3%)) returns the monitor table: after CHANGE TO T%,
  T%(4%) is max jobs (63);
  T%(11%)+SWAP%(T%(12%)) is JOBTBL.

Type SHOW CPU  or  OLD CPU  then RUN.
`,
	"TELNET": `This system is multi-user. Each Telnet connection is a RSTS job
on its own KB: line (KB0: is the local console).

  config.toml
    max_users    = 25     simultaneous jobs (1..63)
    telnet_port  = 23     listener port (2323 if not root)
    telnet_bind  = "0.0.0.0"
    telnet       = true
    console      = true

Connect with any Telnet client. Terminal type VT52 is the baseline
(ESC A/B/C/D/H/J/K/Y/Z). ANSI/VT100 cursor keys are accepted too.

  telnet host 23

At the Bye prompt:  HELLO  then account and password.
BYE logs out. EXIT or QUIT at Bye stops the emulator on the console
(a Telnet EXIT hangs up that line only).
Ctrl-C interrupts a running program; Ctrl-U kills the input line.
`,
	"JOBS": `Job monitor commands (RSTS/E V7.2):

  SYSTAT [job] [/F] [/N]    all jobs (Where, What, Size, State, Run-Time)
  SYSTAT/D                  disk packs (device, pack ID, Pub/Pri)
  SYS                       same as SYSTAT
  WHO                       logged-in jobs only
  DETACH                    detach this job from the keyboard
  ATTACH n                  attach to a detached job you own
  FORCE kb: command         inject a command (privileged)
  HANGUP n                  hang up a job/line (privileged)
  BROADCAST ALL text        message every keyboard (privileged)
  SEND kb: text             message one job

States: KB wait, RN running, Det detached.
Where is KBn: or PKn: (pseudo keyboard).

OPEN "PK:" AS FILE n  assigns a PK unit and forks a new job at #.
PRINT #n sends keystrokes; INPUT #n / LINE INPUT #n reads the job's
output. CLOSE #n hangs up the spawned job.

  10 OPEN "PK:" AS FILE 1
  20 PRINT #1, "HELLO GUEST"
  30 PRINT #1, "GUEST"
`,
	"DISKS": `RSTS/E V7.2 disk packs (UMOUNT). A pack sits on a physical unit
and is logically mounted before you can store files on it.

Devices on this 11/70:

  SY:  SY0:  DB0:   RP06 system pack SYSDSK  (always mounted, public)
  DB1:              RP06
  DL0: DL1:         RL02
  DM0:              RK07

  MOUNT device: packid [/PRIVATE] [/PUBLIC] [/WRITE] [/RONLY]
  DISMOUNT device: [packid]
  DSKINT device: packid [/PUBLIC]     (priv) initialize a pack
  INITIALIZE device: packid [/PUBLIC]
  SYSTAT/D
  SHOW DISKS

Examples:
  MOUNT DB1: PAYROL
  DIR DB1:
  SAVE DB1:FOO
  DISMOUNT DB1:

/PUBLIC requires privilege (adds the pack to the public structure).
Ordinary users mount private packs. SY0:/DB0: cannot be dismounted.
Pack IDs are 1-6 letters or digits. Once mounted, PAYROL: is a
logical name for that unit.

A sample pack PAYROL is initialized on DB1: and left unmounted.
`,
	"SYSTAT": `SYSTAT is the V7.2 status CUSP (also typed as SYS). Switches may be
attached:  SYSTAT/D  is the same as  SYSTAT /D.

  SYSTAT              jobs (default, /J)
  SYSTAT/F            full job display with RTS
  SYSTAT/N            no header
  SYSTAT/U  WHO       logged-in jobs only
  SYSTAT/D            disks (packs, pack ID, Pub/Pri)
  SYSTAT/K  /T        keyboards / terminals
  SYSTAT/M            memory
  SYSTAT/R            run-time systems
  SYSTAT/S            system statistics
  SYSTAT/B            busy devices
  SYSTAT/H            hardware
  SYSTAT n            one job

WHO is SYSTAT/U. SHOW DISKS is SYSTAT/D. Type HELP DISKS for MOUNT.
`,
	"SHOW": `V7.2 used SYSTAT, not DCL SHOW. These SHOW words are accepted as
aliases for the same displays:

  SHOW JOBS      SYSTAT
  SHOW USERS     SYSTAT/U
  SHOW DISKS     SYSTAT/D
  SHOW MEMORY    SYSTAT/M
  SHOW TERMINALS SYSTAT/K
  SHOW RTS       SYSTAT/R
  SHOW STATUS    SYSTAT/S
  SHOW CPU       hardware
  SHOW ACCOUNT   this PPN
  SHOW ACCOUNTS  (priv) all PPNs
  SHOW DATE      DATE
  SHOW TIME      TIME
`,
	"COMMANDS": `Keyboard commands (V7.2 BASIC-PLUS CCL style). Unique prefixes
and attached switches work: SYSTAT/D, DISMOU DB1:, HLP DISK.

  HELLO  BYE  PASSWORD
  EXIT  QUIT              at Bye, stop the emulator
  DIR  CAT  TYPE  COPY  PIP  KILL  UNSAVE  NAME
  NEW  OLD  SAVE  REPLACE  COMPILE  LIST  LISTNH  RUN  RUNNH
  DELETE  CLEAR
  SYSTAT  SYS  WHO
  MOUNT  DISMOUNT  DSKINT  UMOUNT
  ATTACH  DETACH  FORCE  HANGUP  BROADCAST  SEND
  DATE  TIME  DAYTIME
  CREATE  DELETE/ACCOUNT  REACT
  SHOW  HELP
  CPU  HARDWARE

Type HELP topic. Topics: LOGIN FILES BASIC LANG FN COMMANDS SYSTAT
SHOW DISKS ACCOUNTS COMPILE HARDWARE TELNET JOBS
`,
	"HELP": `Help can be obtained on a topic by typing:

  HELP
  HELP topic

A topic is a command or subject name. Abbreviations match a unique
prefix. Attached switches are ignored (HELP SYSTAT/D = HELP SYSTAT).

Additional help is available on:
  LOGIN FILES BASIC LANG FN COMMANDS SYSTAT SHOW DISKS
  ACCOUNTS COMPILE HARDWARE TELNET JOBS HELP
`,
	"PLEASE": `PLEASE sent a message to the operator on V7.2. This system has no
operator console queue. Use SEND or BROADCAST.
`,
	"QUE": `QUE / QUMRUN batch and print queues are not configured on this system.
`,
	"TECO": `TECO and VTEDIT are not installed. Use OLD / LIST / SAVE to edit
BASIC-PLUS programs in memory, or TYPE / COPY for files.
`,
	"PDP": `Type HELP HARDWARE
`,
	"COMPILE": `COMPILE writes a compiled .BAC image of the program in memory.
On RSTS/E V7.2 this was BASIC-PLUS P-code. Here COMPILE emits a private
bytecode image (not Digital's P-code). RUN interprets that bytecode.
LIST and TYPE cannot recover the source from a .BAC.

  COMPILE                 write ProgramName.BAC <124>
  COMPILE PAYROL          write PAYROL.BAC <124>
  COMPILE PAYROL<232>     write PAYROL.BAC with protection 232

Protection bits (V7.2):
  64   compiled / executable
  128  privileged program (only with 64)

Only a privileged account ([1,*], or SYSTEM) may set bit 128.
Typical public privileged CUSP protection is <232>.

NAME old.BAC AS old.BAC<232>   set the privilege bit after COMPILE
PIP dest<232>=src              copy with a new protection

RUN of a .BAC with bits 64+128 gives the job temporary privilege
(JFSYS): same PPN, extra rights for the duration of the run.
Privilege is dropped and the image is destroyed on END, STOP, or
error, so a non-privileged user cannot LIST it.

.BAS source never confers privilege, even if bit 128 is set.
Non-privileged users may RUN a public <232> file but cannot OLD,
TYPE, or LIST it.

  RUN $WHOAMI     demo: guest runs a privileged CUSP in [1,2]
`,
	"ACCOUNTS": `Default accounts (name or PPN, then password):

  SYSTEM    1,2        SYSTEM     (privileged)
  GUEST     100,100    GUEST
  DEMO      200,200    DEMO

On V7.2, project [1,*] is privileged. Account work is done by a
privileged user (REACT on a real system):

  CREATE [p,pn] NAME n PASSWORD pw
  CREATE/ACCOUNT [p,pn] n pw
  DELETE/ACCOUNT [p,pn]
  REMOVE [p,pn]
  PASSWORD                  change your own (old + new)
  PASSWORD [p,pn] [new]     (priv) set that account's password
  SHOW ACCOUNTS             (priv) list PPNs and names
  REACT CREATE / DELETE / PASSWORD / LIST

CREATE of [1,*] is privileged. [1,2] cannot be deleted.
An account that is logged in cannot be deleted.

RUN of a <232> .BAC gives a normal user temporary privilege for
that run only (see HELP COMPILE).
`,
}

func FormatDir(dev, ppn string, infos []FileInfo) string {
	var b strings.Builder
	if dev == "" {
		dev = "SY:"
	}
	fmt.Fprintf(&b, "%s[%s]\n", dev, ppn)
	b.WriteString("Name .Typ    Size    Prot     Date        Time")
	blocks := 0
	for _, info := range infos {
		name := padRight(clip(info.NamePart(), 9), 9)
		ext := padRight(clip(info.ExtPart(), 3), 3)
		blocks += info.Blocks()
		fmt.Fprintf(&b, "\n%s.%s %6d    <%3d>  %s  %s",
			name, ext, info.Blocks(), info.Prot,
			info.Modified.Format("02-Jan-06"),
			strings.TrimLeft(info.Modified.Format("3:04 PM"), "0"))
	}
	if len(infos) == 0 {
		b.WriteString("\n%No files")
	} else {
		plural := "s"
		if len(infos) == 1 {
			plural = ""
		}
		fmt.Fprintf(&b, "\nTotal of %d file%s, %d blocks", len(infos), plural, blocks)
	}
	return b.String()
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

type terminal interface {
	ReadLine(prompt string) (string, error)
	ReadPassword(prompt string) (string, error)
}

type stdTerm struct {
	in  *bufio.Reader
	out io.Writer
}

func (t *stdTerm) ReadLine(prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(t.out, prompt)
	}
	line, err := t.in.ReadString('\n')
	if err != nil && !(err == io.EOF && line != "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (t *stdTerm) ReadPassword(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if t.out == os.Stdout && term.IsTerminal(fd) {
		fmt.Fprint(t.out, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(t.out)
		return string(b), err
	}
	return t.ReadLine(prompt)
}

type Shell struct {
	sys        *System
	term       terminal
	DiskRoot   string
	Accounts   *AccountDB
	Disk       *Disk
	Account    *Account
	Job        int
	KB         string
	Boot       time.Time
	Basic      *Machine
	AutoLogin  string
	Guest      bool
	Running    bool
	parked     bool
	skipBanner bool
	attachTo   int
	inProgram  bool
	tempPriv   bool
	forceCh    chan string
	in         *bufio.Reader
	out        io.Writer
	console    bool
}

func NewShell(diskRoot string, login string, guest bool) (*Shell, error) {
	sys, err := NewSystem(diskRoot, DefaultConfig())
	if err != nil {
		return nil, err
	}
	return sys.OpenConsole(login, guest)
}

func (sys *System) OpenConsole(login string, guest bool) (*Shell, error) {
	job, err := sys.Attach("CONSOLE")
	if err != nil {
		return nil, err
	}
	st := &stdTerm{in: bufio.NewReader(os.Stdin), out: os.Stdout}
	s := sys.newSession(job, os.Stdout, st)
	s.AutoLogin = login
	s.Guest = guest
	s.in = st.in
	s.console = true
	sys.setConsole(s)
	return s, nil
}

func (sys *System) newSession(job *Job, out io.Writer, term terminal) *Shell {
	s := &Shell{
		sys:      sys,
		term:     term,
		DiskRoot: sys.Disk.Root,
		Accounts: sys.Accounts,
		Disk:     sys.Disk,
		Job:      job.Num,
		KB:       job.KB,
		Boot:     sys.Boot,
		Running:  true,
		out:      out,
		forceCh:  make(chan string, 8),
	}
	s.Basic = NewMachine(IO{
		Write: s.write,
		Read:  s.read,
		Open:  s.openBasicFile,
		Job:   job.Num,
		PollInterrupt: func() bool {
			t, ok := s.term.(interface{ PollInterrupt() bool })
			return ok && t.PollInterrupt()
		},
	})
	s.Basic.cpuStart = sys.Boot
	sys.registerShell(s)
	s.syncJob()
	return s
}

func (s *Shell) seedSamples() error {
	for ppn, files := range samples {
		parts := strings.SplitN(ppn, ",", 2)
		proj, _ := strconv.Atoi(parts[0])
		prog, _ := strconv.Atoi(parts[1])
		folder, err := s.Disk.AccountDir(proj, prog)
		if err != nil {
			return err
		}
		for name, content := range files {
			path := filepath.Join(folder, name)
			spec, err := ParseFileSpec(fmt.Sprintf("[%d,%d]%s", proj, prog, name), "")
			if err != nil {
				return err
			}
			prot := defaultProt
			body := content
			switch {
			case strings.HasSuffix(strings.ToUpper(name), ".BAC"):
				img, err := compileSourceText(content)
				if err != nil {
					return err
				}
				body = wrapPcode(img)
				prot = privCompiledProt
			case strings.HasSuffix(strings.ToUpper(name), ".TXT") && proj == 1 && prog == 2:
				prot = 40
			}
			if _, err := os.Stat(path); err == nil {
				refresh := strings.EqualFold(name, "NOTICE.TXT") || strings.EqualFold(name, "LOGIN.TXT") || strings.HasSuffix(strings.ToUpper(name), ".BAC")
				if !refresh {
					if prot != defaultProt {
						_ = s.Disk.SetProt(spec, proj, prog, true, prot)
					}
					continue
				}
			}
			if err := s.Disk.WriteText(spec, proj, prog, true, body, prot); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Shell) write(text string, newline bool) {
	fmt.Fprint(s.out, text)
	if newline {
		fmt.Fprint(s.out, "\n")
	}
}

func (s *Shell) read(prompt string) (string, error) {
	line, err := s.readLine(prompt)
	if err != nil {
		return "", err
	}
	return line, nil
}

func (s *Shell) readLine(prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(s.out, prompt)
	}
	if line, ok := s.takeForce(); ok {
		fmt.Fprintln(s.out, line)
		return line, nil
	}
	if s.term != nil {
		line, err := s.term.ReadLine("")
		if line2, ok := s.takeForce(); ok {
			fmt.Fprintln(s.out, line2)
			return line2, nil
		}
		if err == errForced {
			if line2, ok := s.takeForce(); ok {
				return line2, nil
			}
		}
		if isInterruptErr(err) {
			return "", ErrInterrupt
		}
		return line, err
	}
	if s.in == nil {
		return "", io.EOF
	}
	line, err := s.in.ReadString('\n')
	if isInterruptErr(err) {
		return "", ErrInterrupt
	}
	if err != nil && !(err == io.EOF && line != "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func isInterruptErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrInterrupt) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "interrupted") || strings.Contains(msg, "EINTR")
}

func (s *Shell) takeForce() (string, bool) {
	if s.forceCh == nil {
		return "", false
	}
	select {
	case line := <-s.forceCh:
		return line, true
	default:
		return "", false
	}
}

func (s *Shell) readPassword(prompt string) (string, error) {
	if s.term != nil {
		return s.term.ReadPassword(prompt)
	}
	return s.readLine(prompt)
}

func (s *Shell) openBasicFile(m *Machine, channel int, path, mode string) error {
	if unit, ok := parsePKName(path); ok {
		return s.openPK(m, channel, unit)
	}
	if s.Account == nil {
		return basicErr("I/O error")
	}
	spec, err := ParseFileSpec(path, "DAT")
	if err != nil {
		return basicErr(err.Error())
	}
	folder, err := s.Disk.ResolveFolder(spec, s.Account.Proj, s.Account.Prog)
	if err != nil {
		return err
	}
	filename := spec.Filename()
	real := filepath.Join(folder, filename)
	if _, err := os.Stat(real); err != nil {
		entries, _ := os.ReadDir(folder)
		for _, ent := range entries {
			if !ent.IsDir() && strings.EqualFold(ent.Name(), filename) {
				real = filepath.Join(folder, ent.Name())
				break
			}
		}
	}
	var f *os.File
	switch mode {
	case "INPUT":
		f, err = os.Open(real)
		if err != nil {
			return basicErr("Can't find file or account")
		}
	case "OUTPUT":
		f, err = os.Create(real)
	case "APPEND":
		f, err = os.OpenFile(real, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	default:
		f, err = os.OpenFile(real, os.O_RDWR|os.O_CREATE, 0o644)
	}
	if err != nil {
		return err
	}
	cf := &chanFile{file: f, mode: mode}
	if mode == "INPUT" {
		cf.r = bufio.NewReader(f)
	}
	m.Files[channel] = cf
	return nil
}

func (s *Shell) banner() {
	fmt.Fprintf(s.out, "\n%s\n\n", SystemName)
}

func (s *Shell) Run() int {
	if !s.skipBanner {
		s.banner()
	}
	defer func() {
		if s.parked {
			if s.sys != nil {
				s.sys.parkShell(s)
			}
			return
		}
		s.Basic.CloseAllFiles()
		if s.sys != nil {
			s.sys.unregisterShell(s)
			s.sys.Detach(s.Job)
		}
	}()
	if s.Guest {
		s.Login("GUEST", "GUEST")
	} else if s.AutoLogin != "" {
		s.Login(s.AutoLogin, "")
	}
	for s.Running {
		if err := s.oneTurn(); err != nil {
			if err == io.EOF || errors.Is(err, net.ErrClosed) {
				fmt.Fprintln(s.out)
				s.Running = false
				break
			}
			if errors.Is(err, ErrInterrupt) || isInterruptErr(err) {
				fmt.Fprintf(s.out, "\n^C\n")
				if s.Account != nil {
					fmt.Fprintln(s.out)
					fmt.Fprintln(s.out, "Ready")
				}
				continue
			}
			fmt.Fprintf(s.out, "\n^C\n")
			if s.Account != nil {
				fmt.Fprintln(s.out)
				fmt.Fprintln(s.out, "Ready")
			}
		}
	}
	return 0
}

func (s *Shell) oneTurn() error {
	if s.Account == nil {
		return s.loggedOut()
	}
	return s.ready()
}

func (s *Shell) loggedOut() error {
	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "Bye")
	line, err := s.readLine("")
	if err != nil {
		return err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	verb, rest := splitVerb(line)
	switch verb {
	case "HELLO", "LOGIN", "LOG":
		s.cmdHello(rest)
	case "HELP", "HLP":
		s.cmdHelp(rest)
	case "EXIT", "QUIT":
		s.cmdHalt()
	case "BYE", "LOGOUT":
		if !s.console {
			s.Running = false
		}
	default:
		fmt.Fprintln(s.out, "?Please say HELLO")
	}
	return nil
}

func (s *Shell) ready() error {
	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "Ready")
	line, err := s.readLine("")
	if err != nil {
		return err
	}
	s.Dispatch(line)
	return nil
}

func (s *Shell) Dispatch(raw string) {
	line := strings.TrimRight(raw, "\n")
	if strings.TrimSpace(line) == "" {
		return
	}
	stripped := strings.TrimLeft(line, " \t")
	if stripped != "" && unicode.IsDigit(rune(stripped[0])) {
		s.storeProgramLine(stripped)
		return
	}
	verb, rest := splitVerb(stripped)
	if err := s.dispatchCmd(verb, rest); err == errNotCmd {
		if err := s.Basic.ExecImmediate(stripped); err != nil {
			fmt.Fprintln(s.out, err.Error())
		}
		return
	} else if err != nil {
		fmt.Fprintf(s.out, "?%s\n", strings.TrimPrefix(err.Error(), "?"))
	}
}

var errNotCmd = fmt.Errorf("not a command")

func (s *Shell) dispatchCmd(verb, rest string) error {
	switch verb {
	case "HELLO", "LOGIN":
		s.cmdHello(rest)
	case "BYE", "LOGOUT", "EXIT", "QUIT":
		s.cmdBye(rest)
	case "HELP", "HLP":
		s.cmdHelp(rest)
	case "DIR", "CAT", "CATALOG":
		return s.cmdDir(rest)
	case "TYPE":
		return s.cmdType(rest)
	case "COPY":
		return s.cmdCopy(rest)
	case "PIP":
		return s.cmdPip(rest)
	case "KILL", "UNSAVE":
		return s.cmdKill(rest)
	case "NAME":
		return s.cmdName(rest)
	case "RENAME":
		return s.cmdRename(rest)
	case "NEW":
		s.cmdNew(rest)
	case "OLD":
		return s.cmdOld(rest)
	case "SAVE":
		return s.cmdSave(rest, false)
	case "REPLACE":
		return s.cmdSave(rest, true)
	case "COMPILE", "COMPIL":
		return s.cmdCompile(rest)
	case "LIST":
		s.cmdList(rest, true)
	case "LISTNH":
		s.cmdList(rest, false)
	case "RUN":
		s.cmdRun(rest, true)
	case "RUNNH":
		s.cmdRun(rest, false)
	case "DELETE", "DEL":
		upperRest := strings.ToUpper(strings.TrimSpace(rest))
		if strings.HasPrefix(upperRest, "/ACCOUNT") {
			arg := strings.TrimSpace(rest)
			if i := strings.Index(upperRest, "/ACCOUNT"); i >= 0 {
				arg = strings.TrimSpace(rest[i+len("/ACCOUNT"):])
			}
			return s.cmdDeleteAccount(arg)
		}
		return s.cmdDelete(rest)
	case "CLEAR":
		s.Basic.resetRuntime()
	case "SYSTAT", "SYS":
		s.cmdSystat(rest)
	case "SYSTAT/D", "SYS/D":
		s.cmdDisks()
	case "WHO":
		s.cmdSystat("/U " + rest)
	case "MOUNT":
		return s.cmdMount(rest)
	case "DISMOUNT", "DISMOU":
		return s.cmdDismount(rest)
	case "DSKINT", "INITIALIZE":
		return s.cmdDskint(rest)
	case "UMOUNT":
		fmt.Fprintln(s.out, "MOUNT device: packid    DISMOUNT device:")
		return nil
	case "DETACH":
		s.cmdDetach()
	case "ATTACH":
		s.cmdAttach(rest)
	case "FORCE":
		return s.cmdForce(rest)
	case "HANGUP":
		return s.cmdHangup(rest)
	case "BROADCAST":
		return s.cmdBroadcast(rest)
	case "SEND", "TALK":
		return s.cmdBroadcast(rest)
	case "CPU", "HARDWARE":
		s.cmdHardware()
	case "DATE":
		fmt.Fprintln(s.out, NowDate())
	case "TIME":
		fmt.Fprintln(s.out, NowTime())
	case "DAYTIME":
		fmt.Fprintf(s.out, "%s  %s\n", NowDate(), NowTime())
	case "PASSWORD":
		return s.cmdPassword(rest)
	case "CREATE", "CREATE/ACCOUNT":
		if rest2, ok := stripLeadingSwitch(rest, "ACCOUNT"); ok {
			rest = rest2
		}
		return s.cmdCreate(rest)
	case "DELETE/ACCOUNT", "DEL/ACCOUNT", "REMOVE", "REMOVE/ACCOUNT", "KILL/ACCOUNT":
		return s.cmdDeleteAccount(rest)
	case "REACT":
		return s.cmdReact(rest)
	case "ACCOUNT":
		return s.cmdAccount()
	case "SHOW":
		s.cmdShow(rest)
	default:
		return errNotCmd
	}
	return nil
}

func splitVerb(line string) (string, string) {
	line = strings.TrimLeft(line, " \t")
	i := 0
	for i < len(line) {
		r := rune(line[i])
		if unicode.IsSpace(r) || line[i] == '/' {
			break
		}
		i++
	}
	verb := strings.ToUpper(line[:i])
	rest := strings.TrimSpace(line[i:])
	return matchCmd(verb), rest
}

func stripLeadingSwitch(rest, name string) (string, bool) {
	rest = strings.TrimSpace(rest)
	pref := "/" + strings.ToUpper(name)
	if strings.HasPrefix(strings.ToUpper(rest), pref) {
		return strings.TrimSpace(rest[len(pref):]), true
	}
	return rest, false
}

var keyboardCmds = []string{
	"HELLO", "LOGIN", "BYE", "LOGOUT", "EXIT", "QUIT",
	"HELP", "HLP",
	"DIR", "CAT", "CATALOG",
	"TYPE", "COPY", "PIP", "KILL", "UNSAVE", "NAME", "RENAME",
	"NEW", "OLD", "SAVE", "REPLACE", "COMPILE", "COMPIL",
	"LIST", "LISTNH", "RUN", "RUNNH", "DELETE", "DEL", "CLEAR",
	"SYSTAT", "SYS", "WHO",
	"MOUNT", "DISMOUNT", "DSKINT", "INITIALIZE", "UMOUNT",
	"DETACH", "ATTACH", "FORCE", "HANGUP", "BROADCAST", "SEND", "TALK",
	"CPU", "HARDWARE", "DATE", "TIME", "DAYTIME",
	"PASSWORD", "CREATE", "REACT", "ACCOUNT", "SHOW", "REMOVE",
}

func matchCmd(verb string) string {
	if verb == "" {
		return verb
	}
	for _, c := range keyboardCmds {
		if c == verb {
			return c
		}
	}
	var hits []string
	for _, c := range keyboardCmds {
		if strings.HasPrefix(c, verb) {
			hits = append(hits, c)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return verb
}

func switchTokens(rest string) []string {
	var out []string
	i := 0
	for i < len(rest) {
		if unicode.IsSpace(rune(rest[i])) {
			i++
			continue
		}
		if rest[i] == '/' {
			j := i + 1
			for j < len(rest) && rest[j] != '/' && !unicode.IsSpace(rune(rest[j])) {
				j++
			}
			out = append(out, rest[i:j])
			i = j
			continue
		}
		j := i
		for j < len(rest) && rest[j] != '/' && !unicode.IsSpace(rune(rest[j])) {
			j++
		}
		out = append(out, rest[i:j])
		i = j
	}
	return out
}

func (s *Shell) storeProgramLine(line string) {
	i := 0
	for i < len(line) && unicode.IsDigit(rune(line[i])) {
		i++
	}
	if i == 0 {
		fmt.Fprintln(s.out, "?Illegal line number")
		return
	}
	num, _ := strconv.Atoi(line[:i])
	text := strings.TrimRight(strings.TrimLeft(line[i:], " \t"), " \t")
	if err := s.Basic.StoreLine(num, text); err != nil {
		fmt.Fprintln(s.out, err.Error())
	}
}

func (s *Shell) cmdHello(rest string) {
	if s.Account != nil {
		fmt.Fprintln(s.out, "?Already logged in -- type BYE first")
		return
	}
	token := strings.TrimSpace(rest)
	if token == "" {
		var err error
		token, err = s.readLine("Account or Name: ")
		if err != nil {
			return
		}
		token = strings.TrimSpace(token)
	}
	pw, err := s.readPassword("Password: ")
	if err != nil {
		return
	}
	s.Login(token, pw)
}

func (s *Shell) Login(token, password string) {
	if password == "" {
		var err error
		password, err = s.readPassword("Password: ")
		if err != nil {
			return
		}
	}
	acct := s.Accounts.Authenticate(token, password)
	if acct == nil {
		fmt.Fprintln(s.out, "?Invalid entry -- try again")
		return
	}
	s.Account = acct
	s.tempPriv = false
	s.Basic.IO.PPN = acct.Display()
	s.Basic.IO.AccountName = acct.Name
	s.Basic.IO.Privileged = s.accountPriv()
	s.Basic.IO.Job = s.Job
	if s.sys != nil {
		s.sys.mu.Lock()
		if j := s.sys.jobs[s.Job]; j != nil {
			j.OwnerPPN = acct.Display()
		}
		s.sys.mu.Unlock()
	}
	s.syncJob()
	fmt.Fprintln(s.out)
	kb := strings.TrimSuffix(s.KB, ":")
	fmt.Fprintf(s.out, "%s  Job %d  %s  %s  %s\n", SystemName, s.Job, kb, NowDate(), NowTime())
	fmt.Fprintf(s.out, "User:  %s\n", acct.PPN())
	fmt.Fprintln(s.out)
}

func (s *Shell) syncJob() {
	if s.sys == nil {
		return
	}
	s.sys.mu.Lock()
	defer s.sys.mu.Unlock()
	j := s.sys.jobs[s.Job]
	if j == nil {
		return
	}
	who, what := "*****", "LOGINS"
	if s.Account != nil {
		who = s.Account.Display()
		what = s.Basic.ProgramName
		if what == "" || what == "NONAME" {
			what = "Ready"
		}
	}
	j.Who = who
	j.What = what
	j.SizeK = 8
	if s.Basic != nil {
		j.SizeK = 8 + len(s.Basic.Program)/5
		if j.SizeK > 31 {
			j.SizeK = 31
		}
	}
	if j.Detached {
		j.State = "Det"
		j.Where = "Det"
	} else if s.inProgram {
		j.State = "RN"
	} else {
		j.State = "KB"
	}
}

func (s *Shell) userLimit() int {
	if s.sys != nil && s.sys.Config.MaxUsers > 0 {
		return s.sys.Config.MaxUsers
	}
	return 25
}

func (s *Shell) cmdHalt() {
	s.Running = false
	if s.console && s.sys != nil {
		s.sys.Close()
	}
}

func (s *Shell) cmdBye(rest string) {
	if s.Account == nil {
		if s.console {
			return
		}
		s.Running = false
		return
	}
	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "Saved all disk files on SY:")
	fmt.Fprintln(s.out)
	fmt.Fprintf(s.out, "Job %d  User %s  logged off %s  at %s  %s\n\n",
		s.Job, s.Account.PPN(), s.KB, NowDate(), NowTime())
	s.Basic.ClearProgram("NONAME")
	s.tempPriv = false
	if s.Basic != nil {
		s.Basic.IO.Privileged = false
		s.Basic.IO.PPN = ""
		s.Basic.IO.AccountName = ""
	}
	s.Account = nil
	s.syncJob()
	if strings.EqualFold(rest, "/EXIT") || strings.EqualFold(rest, "EXIT") {
		s.cmdHalt()
	}
}

func (s *Shell) cmdHelp(rest string) {
	topic := strings.ToUpper(strings.TrimSpace(rest))
	if i := strings.IndexByte(topic, '/'); i >= 0 {
		topic = topic[:i]
	}
	if alias, ok := helpAlias[topic]; ok {
		topic = alias
	}
	text, ok := helpText[topic]
	if !ok && topic != "" {
		var hits []string
		for k := range helpText {
			if k != "" && strings.HasPrefix(k, topic) {
				hits = append(hits, k)
			}
		}
		for a, canon := range helpAlias {
			if strings.HasPrefix(a, topic) {
				hits = append(hits, canon)
			}
		}
		hits = uniqueStrings(hits)
		if len(hits) == 1 {
			topic, text, ok = hits[0], helpText[hits[0]], true
		} else if len(hits) > 1 {
			fmt.Fprintln(s.out, "Additional help is available on:")
			fmt.Fprintln(s.out, " ", strings.Join(hits, "  "))
			return
		}
	}
	if !ok {
		t := strings.TrimSpace(rest)
		if t == "" {
			t = "that"
		}
		fmt.Fprintf(s.out, "?No help on %s\n", t)
		fmt.Fprintln(s.out, "Type HELP for a list of topics.")
		return
	}
	fmt.Fprintln(s.out, strings.TrimRight(text, "\n"))
}

var helpAlias = map[string]string{
	"DISK": "DISKS", "DEVICE": "DISKS", "DEVICES": "DISKS",
	"PACK": "DISKS", "PACKS": "DISKS", "MOUNT": "DISKS", "DISMOUNT": "DISKS",
	"DSKINT": "DISKS", "UMOUNT": "DISKS", "INITIALIZE": "DISKS", "INIT": "DISKS",
	"ASSIGN": "DISKS", "DEASSIGN": "DISKS", "REASSIGN": "DISKS",
	"SYS": "SYSTAT", "WHO": "SYSTAT", "STATUS": "SYSTAT",
	"CCL": "COMMANDS", "KEYBOARD": "JOBS", "KEYBOARDS": "JOBS",
	"CMDS": "COMMANDS", "DCL": "COMMANDS",
	"CPU": "HARDWARE", "PDP": "HARDWARE", "PDP11": "HARDWARE", "SWITCH": "HARDWARE",
	"HLP":       "HELP",
	"DIRECTORY": "FILES", "DIR": "FILES", "CAT": "FILES", "CATALOG": "FILES",
	"TYPE": "FILES", "PIP": "FILES", "COPY": "FILES", "KILL": "FILES",
	"NAME": "FILES", "UNSAVE": "FILES", "FILENAMES": "FILES", "FIT": "FILES",
	"DIRECT": "FILES", "QUOLST": "DISKS", "QUOTA": "DISKS",
	"HELLO": "LOGIN", "BYE": "LOGIN", "LOGOUT": "LOGIN", "EXIT": "LOGIN",
	"PASSWORD": "ACCOUNTS", "REACT": "ACCOUNTS", "CREATE": "ACCOUNTS",
	"NEW": "BASIC", "OLD": "BASIC", "SAVE": "BASIC", "RUN": "BASIC",
	"LIST": "BASIC", "COMPILE": "COMPILE",
	"ATTACH": "JOBS", "DETACH": "JOBS", "FORCE": "JOBS", "HANGUP": "JOBS",
	"BROADCAST": "JOBS", "SEND": "JOBS", "TALK": "JOBS", "PK": "JOBS",
	"RT11": "SYSTAT", "RSX": "SYSTAT", "RTS": "SYSTAT",
	"DATE": "COMMANDS", "TIME": "COMMANDS", "DAYTIME": "COMMANDS",
	"ADVANCED": "LANG", "STATEMENTS": "LANG", "FUNCTIONS": "FN",
	"PLEASE": "PLEASE", "OPR": "PLEASE",
	"QUE": "QUE", "QUEUE": "QUE", "QUMRUN": "QUE",
	"TECO": "TECO", "VTEDIT": "TECO", "EDIT": "TECO",
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (s *Shell) accountPriv() bool {
	return s.Account != nil && (s.Account.Privileged || s.Account.Proj == 1)
}

func (s *Shell) priv() bool {
	return s.accountPriv() || s.tempPriv
}

func (s *Shell) syncPrivilege() {
	if s.Basic != nil {
		s.Basic.IO.Privileged = s.priv()
	}
}

func (s *Shell) dropTempPriv() {
	s.tempPriv = false
	s.syncPrivilege()
}

func (s *Shell) needLogin() (*Account, error) {
	if s.Account == nil {
		return nil, fsErr("Please say HELLO")
	}
	return s.Account, nil
}

func (s *Shell) cmdDir(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	arg := rest
	if arg == "" {
		arg = "*.*"
	}
	spec, err := ParseFileSpec(arg, "*")
	if err != nil {
		return err
	}
	if spec.Name == "*" && rest == "" {
		spec.Ext = "*"
	}
	ppn, infos, err := s.Disk.ListDir(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return err
	}
	fmt.Fprintln(s.out, FormatDir(spec.DevName(), ppn, infos))
	return nil
}

func (s *Shell) cmdType(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	if strings.TrimSpace(rest) == "" {
		rest, err = s.readLine("File name-- ")
		if err != nil {
			return err
		}
		rest = strings.TrimSpace(rest)
	}
	spec, err := ParseFileSpec(rest, "")
	if err != nil {
		return err
	}
	text, err := s.Disk.ReadText(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return err
	}
	fmt.Fprint(s.out, text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		fmt.Fprint(s.out, "\n")
	}
	return nil
}

func (s *Shell) cmdCopy(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	parts := strings.Fields(rest)
	if len(parts) != 2 {
		fmt.Fprintln(s.out, "?COPY src dst")
		return nil
	}
	src, err := ParseFileSpec(parts[0], "")
	if err != nil {
		return err
	}
	dst, err := ParseFileSpec(parts[1], src.Ext)
	if err != nil {
		return err
	}
	return s.Disk.Copy(src, dst, acct.Proj, acct.Prog, s.priv())
}

func (s *Shell) cmdPip(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	i := strings.IndexByte(rest, '=')
	if i < 0 {
		fmt.Fprintln(s.out, "?PIP dst=src")
		return nil
	}
	dst, err := ParseFileSpec(strings.TrimSpace(rest[:i]), "")
	if err != nil {
		return err
	}
	src, err := ParseFileSpec(strings.TrimSpace(rest[i+1:]), "")
	if err != nil {
		return err
	}
	if dst.Ext == "" {
		dst.Ext = src.Ext
	}
	return s.Disk.Copy(src, dst, acct.Proj, acct.Prog, s.priv())
}

func (s *Shell) cmdKill(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		name = s.Basic.ProgramName
	}
	spec, err := ParseFileSpec(name, "BAS")
	if err != nil {
		return err
	}
	return s.Disk.Delete(spec, acct.Proj, acct.Prog, s.priv())
}

func (s *Shell) cmdName(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	upper := strings.ToUpper(rest)
	idx := strings.Index(upper, " AS ")
	if idx < 0 {
		fmt.Fprintln(s.out, "?NAME old AS new")
		return nil
	}
	old, err := ParseFileSpec(rest[:idx], "BAS")
	if err != nil {
		return err
	}
	new, err := ParseFileSpec(rest[idx+4:], "BAS")
	if err != nil {
		return err
	}
	if err := s.Disk.Rename(old, new, acct.Proj, acct.Prog, s.accountPriv()); err != nil {
		return err
	}
	if old.Filename() == s.Basic.ProgramName+".BAS" {
		s.Basic.ProgramName = new.Name
	}
	return nil
}

func (s *Shell) cmdRename(rest string) error {
	parts := strings.Fields(rest)
	if len(parts) != 2 {
		fmt.Fprintln(s.out, "?RENAME old new")
		return nil
	}
	return s.cmdName(parts[0] + " AS " + parts[1])
}

func (s *Shell) cmdNew(rest string) {
	name := strings.TrimSpace(rest)
	if name == "" {
		var err error
		name, err = s.readLine("New file name-- ")
		if err != nil {
			return
		}
		name = strings.TrimSpace(name)
	}
	name = strings.ToUpper(name)
	if name == "" {
		name = "NONAME"
	}
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	s.Basic.ClearProgram(name)
	s.syncJob()
}

func (s *Shell) cmdOld(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		name, err = s.readLine("Old file name-- ")
		if err != nil {
			return err
		}
		name = strings.TrimSpace(name)
	}
	spec, err := ParseFileSpec(name, "BAS")
	if err != nil {
		return err
	}
	text, err := s.Disk.ReadText(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return err
	}
	if spec.Ext == "BAC" || strings.HasPrefix(text, bacMagic) {
		if err := s.Basic.LoadCompiled(text, spec.Name, isPrivCompiled(s.fileProt(spec))); err != nil {
			return err
		}
		s.syncJob()
		return nil
	}
	if err := s.Basic.LoadSource(text, spec.Name); err != nil {
		return err
	}
	s.syncJob()
	return nil
}

func (s *Shell) fileProt(spec FileSpec) int {
	acct := s.Account
	if acct == nil {
		return defaultProt
	}
	prot, err := s.Disk.Prot(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return defaultProt
	}
	return prot
}

func (s *Shell) cmdSave(rest string, replace bool) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		name = s.Basic.ProgramName
	}
	if name == "NONAME" {
		name, err = s.readLine("File name-- ")
		if err != nil {
			return err
		}
		if strings.TrimSpace(name) == "" {
			name = "NONAME"
		}
	}
	spec, err := ParseFileSpec(name, "BAS")
	if err != nil {
		return err
	}
	if !replace && s.Disk.Exists(spec, acct.Proj, acct.Prog, s.priv()) {
		fmt.Fprintln(s.out, "?File exists -- use REPLACE")
		return nil
	}
	s.Basic.ProgramName = spec.Name
	if s.Basic.Compiled {
		return fsErr("Compiled file")
	}
	return s.Disk.WriteText(spec, acct.Proj, acct.Prog, s.priv(), s.Basic.SourceText(), defaultProt)
}

func (s *Shell) cmdList(rest string, heading bool) {
	start, end, hasStart, hasEnd := parseLineRange(rest)
	if heading {
		fmt.Fprintf(s.out, "%s   %s    %s\n\n", s.Basic.ProgramName, NowTime(), NowDate())
	}
	if s.Basic.Compiled {
		fmt.Fprintln(s.out, "?Compiled file")
		return
	}
	for _, line := range s.Basic.Listing(start, end, hasStart, hasEnd) {
		fmt.Fprintln(s.out, line)
	}
}

func (s *Shell) cmdCompile(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	if len(s.Basic.Program) == 0 {
		return fsErr("No program")
	}
	name := strings.TrimSpace(rest)
	if name == "" || strings.HasPrefix(name, "<") {
		base := s.Basic.ProgramName
		if base == "" {
			base = "NONAME"
		}
		name = base + name
	}
	spec, err := ParseFileSpec(name, "BAC")
	if err != nil {
		return err
	}
	if !spec.ExtGiven {
		spec.Ext = "BAC"
	}
	prot := compiledProt
	if spec.ProtSet {
		prot = spec.Prot | protExecutable
		spec.ProtSet = false
	}
	if err := checkPrivProt(prot, s.accountPriv()); err != nil {
		return err
	}
	s.Basic.ProgramName = spec.Name
	img, err := compileProgram(s.Basic.Program)
	if err != nil {
		return err
	}
	return s.Disk.WriteText(spec, acct.Proj, acct.Prog, s.accountPriv(), wrapPcode(img), prot)
}

func (s *Shell) cmdRun(rest string, heading bool) {
	if strings.TrimSpace(rest) != "" {
		if err := s.loadForRun(rest); err != nil {
			fmt.Fprintf(s.out, "?%s\n", strings.TrimPrefix(err.Error(), "?"))
			return
		}
	}
	if heading {
		fmt.Fprintf(s.out, "%s   %s    %s\n\n", s.Basic.ProgramName, NowTime(), NowDate())
	}
	privImage := s.Basic.PrivImage
	if privImage {
		s.tempPriv = true
		s.syncPrivilege()
	}
	s.inProgram = true
	s.syncJob()
	err := s.Basic.RunProgram()
	s.inProgram = false
	if err != nil {
		if errors.Is(err, ErrInterrupt) {
			fmt.Fprint(s.out, "\n^C\n")
		} else {
			fmt.Fprintln(s.out, err.Error())
		}
	}
	stopped := s.Basic.Stopped
	line := s.Basic.CurrentLine
	s.dropTempPriv()
	if privImage {
		s.Basic.ClearProgram("NONAME")
	}
	s.syncJob()
	if stopped {
		fmt.Fprintf(s.out, "Stop at line %d\n", line)
	}
}

func (s *Shell) loadForRun(name string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	spec, err := ParseFileSpec(name, "")
	if err != nil {
		return err
	}
	tryBAC := !spec.ExtGiven || spec.Ext == "BAC"
	tryBAS := !spec.ExtGiven || spec.Ext == "BAS"
	if spec.ExtGiven && spec.Ext != "BAC" && spec.Ext != "BAS" {
		return s.loadSourceSpec(spec)
	}
	if tryBAC {
		bac := spec
		bac.Ext = "BAC"
		if s.Disk.Exists(bac, acct.Proj, acct.Prog, s.priv()) {
			return s.loadCompiledSpec(bac)
		}
		if spec.ExtGiven && spec.Ext == "BAC" {
			return fsErr("Can't find file or account")
		}
	}
	if tryBAS {
		bas := spec
		bas.Ext = "BAS"
		return s.loadSourceSpec(bas)
	}
	return fsErr("Can't find file or account")
}

func (s *Shell) loadSourceSpec(spec FileSpec) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	text, err := s.Disk.ReadText(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return err
	}
	if err := s.Basic.LoadSource(text, spec.Name); err != nil {
		return err
	}
	s.syncJob()
	return nil
}

func (s *Shell) loadCompiledSpec(spec FileSpec) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	text, prot, err := s.Disk.ReadExecute(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return err
	}
	if err := s.Basic.LoadCompiled(text, spec.Name, isPrivCompiled(prot)); err != nil {
		return err
	}
	s.syncJob()
	return nil
}

func (s *Shell) cmdDelete(rest string) error {
	token := strings.TrimSpace(rest)
	if token == "" {
		fmt.Fprintln(s.out, "?DELETE n[-m]  or  DELETE filespec")
		return nil
	}
	if unicode.IsDigit(rune(token[0])) {
		start, end, hasStart, hasEnd := parseLineRange(token)
		if !hasStart {
			fmt.Fprintln(s.out, "?Illegal line number")
			return nil
		}
		if !hasEnd {
			end = start
		}
		for num := range s.Basic.Program {
			if num >= start && num <= end {
				delete(s.Basic.Program, num)
			}
		}
		return nil
	}
	return s.cmdKill(token)
}

func (s *Shell) needPriv() error {
	if _, err := s.needLogin(); err != nil {
		return err
	}
	if !s.accountPriv() {
		return fsErr("Protection violation")
	}
	return nil
}

func (s *Shell) cmdPassword(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	arg := strings.TrimSpace(rest)
	target := acct
	newPass := ""
	if arg != "" {
		fields := strings.Fields(arg)
		token := fields[0]
		if _, _, err := ParsePPN(token); err == nil || s.Accounts.Find(token) != nil {
			if !s.accountPriv() {
				return fsErr("Protection violation")
			}
			found := s.Accounts.Find(token)
			if found == nil {
				if proj, prog, err := ParsePPN(token); err == nil {
					found = s.Accounts.FindPPN(proj, prog)
				}
			}
			if found == nil {
				return fsErr("Can't find file or account")
			}
			target = found
			if len(fields) > 1 {
				newPass = fields[1]
			}
		} else if s.accountPriv() {
			newPass = token
		} else {
			return fsErr("Protection violation")
		}
	}
	if target.Proj == acct.Proj && target.Prog == acct.Prog && !s.accountPriv() {
		old, err := s.readPassword("Old password: ")
		if err != nil {
			return err
		}
		if strings.ToUpper(strings.TrimSpace(old)) != acct.Password {
			fmt.Fprintln(s.out, "?Invalid entry -- try again")
			return nil
		}
	}
	if newPass == "" {
		newPass, err = s.readPassword("New password: ")
		if err != nil {
			return err
		}
		again, err := s.readPassword("Confirm: ")
		if err != nil {
			return err
		}
		if strings.ToUpper(strings.TrimSpace(newPass)) != strings.ToUpper(strings.TrimSpace(again)) {
			fmt.Fprintln(s.out, "?Passwords don't match")
			return nil
		}
	}
	if err := s.Accounts.SetPassword(target, newPass); err != nil {
		return err
	}
	fmt.Fprintln(s.out, "Password changed")
	return nil
}

func (s *Shell) cmdCreate(rest string) error {
	if err := s.needPriv(); err != nil {
		return err
	}
	proj, prog, name, password, err := s.parseCreateArgs(rest)
	if err != nil {
		return err
	}
	if name == "" {
		name, err = s.readLine("Account name-- ")
		if err != nil {
			return err
		}
	}
	if password == "" {
		password, err = s.readPassword("Password: ")
		if err != nil {
			return err
		}
	}
	privileged := proj == 1
	created, err := s.Accounts.Create(proj, prog, name, password, privileged)
	if err != nil {
		return err
	}
	if _, err := s.Disk.AccountDir(created.Proj, created.Prog); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "Created %s %s\n", created.Display(), created.Name)
	return nil
}

func (s *Shell) parseCreateArgs(rest string) (proj, prog int, name, password string, err error) {
	tokens := strings.Fields(rest)
	if len(tokens) < 1 {
		var line string
		line, err = s.readLine("PPN-- ")
		if err != nil {
			return
		}
		tokens = strings.Fields(line)
	}
	if len(tokens) < 1 {
		return 0, 0, "", "", fsErr("Illegal PPN")
	}
	proj, prog, err = ParsePPN(tokens[0])
	if err != nil {
		return
	}
	for i := 1; i < len(tokens); i++ {
		key := strings.ToUpper(tokens[i])
		if (key == "NAME" || key == "PASSWORD") && i+1 < len(tokens) {
			if key == "NAME" {
				name = tokens[i+1]
			} else {
				password = tokens[i+1]
			}
			i++
			continue
		}
		if name == "" {
			name = tokens[i]
			continue
		}
		if password == "" {
			password = tokens[i]
		}
	}
	return
}

func (s *Shell) cmdDeleteAccount(rest string) error {
	if err := s.needPriv(); err != nil {
		return err
	}
	token := strings.TrimSpace(rest)
	if t, ok := stripLeadingSwitch(token, "ACCOUNT"); ok {
		token = t
	}
	if token == "" {
		var err error
		token, err = s.readLine("Account to delete-- ")
		if err != nil {
			return err
		}
		token = strings.TrimSpace(token)
	}
	if token == "" {
		fmt.Fprintln(s.out, "?DELETE/ACCOUNT [p,pn]")
		return nil
	}
	target := s.Accounts.Find(token)
	if target == nil {
		if proj, prog, err := ParsePPN(token); err == nil {
			target = s.Accounts.FindPPN(proj, prog)
		}
	}
	if target == nil {
		return fsErr("Can't find file or account")
	}
	if target.Proj == 1 && target.Prog == 2 {
		return fsErr("Protection violation")
	}
	if s.Account != nil && s.Account.Proj == target.Proj && s.Account.Prog == target.Prog {
		return fsErr("Account in use")
	}
	if s.sys != nil {
		if jobs := s.sys.jobsForPPN(target.Proj, target.Prog); len(jobs) > 0 {
			return fsErr("Account in use")
		}
	}
	if err := s.Accounts.Delete(target.Proj, target.Prog); err != nil {
		return err
	}
	_ = s.Disk.RemoveAccount(target.Proj, target.Prog)
	fmt.Fprintf(s.out, "Deleted %s %s\n", target.Display(), target.Name)
	return nil
}

func (s *Shell) cmdReact(rest string) error {
	if err := s.needPriv(); err != nil {
		return err
	}
	verb, arg := splitVerb(rest)
	switch verb {
	case "CREATE", "ADD":
		return s.cmdCreate(arg)
	case "DELETE", "REMOVE":
		return s.cmdDeleteAccount(arg)
	case "PASSWORD":
		return s.cmdPassword(arg)
	case "LIST", "DIR", "":
		return s.cmdShowAccounts()
	default:
		fmt.Fprintln(s.out, "?REACT CREATE, DELETE, PASSWORD, or LIST")
		return nil
	}
}

func (s *Shell) cmdShowAccounts() error {
	if err := s.needPriv(); err != nil {
		return err
	}
	accts := s.Accounts.List()
	fmt.Fprintln(s.out, "  PPN       Name      Priv")
	for _, a := range accts {
		priv := ""
		if a.Privileged || a.Proj == 1 {
			priv = "Priv"
		}
		fmt.Fprintf(s.out, "[%3d,%3d]  %-9s %s\n", a.Proj, a.Prog, a.Name, priv)
	}
	return nil
}

func (s *Shell) cmdAccount() error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	fmt.Fprintf(s.out, "%s  %s\n", acct.Display(), acct.Name)
	if acct.Privileged {
		fmt.Fprintln(s.out, "Privileged")
	}
	return nil
}

func (s *Shell) cmdShow(rest string) {
	toks := switchTokens(rest)
	verb := ""
	systatRest := strings.TrimSpace(rest)
	if len(toks) > 0 && !strings.HasPrefix(toks[0], "/") {
		verb = strings.ToUpper(toks[0])
		systatRest = strings.TrimSpace(rest[len(toks[0]):])
	}
	switch verb {
	case "USERS", "USER", "WHO":
		s.cmdSystat("/U " + systatRest)
	case "JOBS", "JOB", "SYSTEM", "":
		s.cmdSystat(systatRest)
	case "ACCOUNT":
		_ = s.cmdAccount()
	case "ACCOUNTS":
		_ = s.cmdShowAccounts()
	case "DISKS", "DISK", "DEVICES", "DEVICE", "PACKS", "PACK":
		s.cmdSystat("/D " + systatRest)
	case "MEMORY", "MEM":
		s.cmdSystat("/M " + systatRest)
	case "TERMINALS", "TERMINAL", "KEYBOARDS", "KEYBOARD", "KB":
		s.cmdSystat("/K " + systatRest)
	case "RTS", "RUNTIME", "RUN-TIME":
		s.cmdSystat("/R " + systatRest)
	case "STATUS", "STATS", "STATISTICS":
		s.cmdSystat("/S " + systatRest)
	case "BUSY":
		s.cmdSystat("/B " + systatRest)
	case "CPU", "HARDWARE", "CONFIG":
		s.cmdHardware()
	case "DATE":
		fmt.Fprintln(s.out, NowDate())
	case "TIME":
		fmt.Fprintln(s.out, NowTime())
	default:
		fmt.Fprintln(s.out, "?SHOW JOBS, USERS, DISKS, MEMORY, TERMINALS, RTS, STATUS, CPU, ACCOUNT")
	}
}

func (s *Shell) cmdDisks() {
	fmt.Fprintf(s.out, "Status of %s  at  %s  %s\n\n", SystemName, NowDate(), NowTime())
	s.printDisks(true)
}

func (s *Shell) printDisks(withHdr bool) {
	if s.Disk == nil {
		fmt.Fprintln(s.out, "?Can't find file or account")
		return
	}
	if withHdr {
		fmt.Fprintln(s.out, "Disk    Pack    Open   Size    Free   Clu  Name    Comments")
	}
	for _, p := range s.Disk.Packs() {
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
		} else {
			if state == "" {
				state = "Dsm"
			}
		}
		size := 40000
		switch p.Media {
		case "RP06":
			size = 340670
		case "RL02":
			size = 20480
		case "RK07":
			size = 53790
		}
		free := size / 4
		if !p.Init {
			free = size
		}
		open := 0
		if p.Mounted {
			open = 1
		}
		fmt.Fprintf(s.out, "%-6s  %-6s %4d  %6d  %6d    4  %-6s  %s\n",
			p.Designator(), p.Media, open, size, free, name, state)
	}
	fmt.Fprintln(s.out)
}

func parseDiskCmd(rest string) (dev string, unit int, packID string, sw map[string]bool, err error) {
	sw = map[string]bool{}
	norm := strings.ReplaceAll(rest, "/", " /")
	var args []string
	for _, tok := range strings.Fields(norm) {
		u := strings.ToUpper(tok)
		if strings.HasPrefix(u, "/") {
			sw[strings.TrimPrefix(u, "/")] = true
			continue
		}
		args = append(args, tok)
	}
	if len(args) == 0 {
		return "", 0, "", sw, fsErr("Not a valid device")
	}
	devTok, extra := args[0], ""
	if i := strings.IndexByte(devTok, ':'); i >= 0 {
		extra = devTok[i+1:]
		devTok = devTok[:i]
	}
	name, unit, unitSet := splitDeviceToken(devTok)
	kind, ok := canonicalDiskDev(name)
	if !ok {
		return "", 0, "", sw, fsErr("Not a valid device")
	}
	if !unitSet {
		unit = 0
	}
	packID = extra
	if packID == "" && len(args) > 1 {
		packID = args[1]
	}
	packID = strings.ToUpper(strings.Trim(packID, ":"))
	return kind.Name, unit, packID, sw, nil
}

func (s *Shell) cmdMount(rest string) error {
	if _, err := s.needLogin(); err != nil {
		return err
	}
	dev, unit, packID, sw, err := parseDiskCmd(rest)
	if err != nil {
		fmt.Fprintln(s.out, "?MOUNT device: packid [/PRIVATE] [/PUBLIC] [/RONLY]")
		return nil
	}
	if packID == "" {
		fmt.Fprintln(s.out, "?MOUNT device: packid")
		return nil
	}
	public := sw["PUBLIC"] || sw["PUB"]
	readOnly := sw["RONLY"] || sw["RO"] || sw["NOWRITE"] || sw["READ"]
	if err := s.Disk.Mount(dev, unit, packID, public, readOnly, s.accountPriv()); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "%s%d:  %s mounted\n", dev, unit, packID)
	return nil
}

func (s *Shell) cmdDismount(rest string) error {
	if _, err := s.needLogin(); err != nil {
		return err
	}
	dev, unit, packID, _, err := parseDiskCmd(rest)
	if err != nil {
		fmt.Fprintln(s.out, "?DISMOUNT device: [packid]")
		return nil
	}
	if err := s.Disk.Dismount(dev, unit, packID, s.accountPriv()); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "%s%d:  dismounted\n", dev, unit)
	return nil
}

func (s *Shell) cmdDskint(rest string) error {
	if err := s.needPriv(); err != nil {
		return err
	}
	dev, unit, packID, sw, err := parseDiskCmd(rest)
	if err != nil || packID == "" {
		fmt.Fprintln(s.out, "?DSKINT device: packid [/PUBLIC]")
		return nil
	}
	public := sw["PUBLIC"] || sw["PUB"]
	if err := s.Disk.Initialize(dev, unit, packID, public, true); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "%s%d:  pack %s initialized\n", dev, unit, packID)
	return nil
}

func (s *Shell) cmdHardware() {
	fmt.Fprintf(s.out, "CPU      %s  (22-bit addressing)\n", CPUName)
	fmt.Fprintf(s.out, "Memory   %d K-words usable  (%d KW 22-bit space)\n", MemoryKW, MemoryMaxKW)
	fmt.Fprintf(s.out, "Cache    %dK-byte bipolar\n", CacheKB)
	fmt.Fprintf(s.out, "FPP      %s\n", FPPName)
	fmt.Fprintf(s.out, "Clock    KW11-L  %d Hz\n", ClockHz)
	fmt.Fprintf(s.out, "Buses    MASSBUS %s, %s\n", MassbusName, UnibusName)
	fmt.Fprintf(s.out, "Disk     SY0:/DB0: %s (SYSDSK)  DB1: RP06  DL0:/DL1: RL02  DM0: RK07\n", SystemDisk)
	fmt.Fprintf(s.out, "Console  %s %s\n", s.KB, ConsoleName)
	fmt.Fprintf(s.out, "Jobs     %d configured (V7.2 allows %d)\n", s.userLimit(), MaxJobs)
}

func parseLineRange(text string) (start, end int, hasStart, hasEnd bool) {
	token := strings.ReplaceAll(strings.TrimSpace(text), " ", "")
	if token == "" {
		return 0, 0, false, false
	}
	if i := strings.IndexByte(token, '-'); i >= 0 {
		a, err1 := strconv.Atoi(token[:i])
		b, err2 := strconv.Atoi(token[i+1:])
		if err1 != nil || err2 != nil {
			return 0, 0, false, false
		}
		return a, b, true, true
	}
	n, err := strconv.Atoi(token)
	if err != nil {
		return 0, 0, false, false
	}
	return n, 0, true, false
}

func Main(args []string) int {
	disk := os.Getenv("RSTS_DISK")
	if disk == "" {
		wd, _ := os.Getwd()
		disk = filepath.Join(wd, "disk")
	}
	configPath := ""
	login := ""
	guest := false
	portOverride := -1
	noConsole := false
	noTelnet := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--disk":
			if i+1 < len(args) {
				i++
				disk = args[i]
			}
		case "--config":
			if i+1 < len(args) {
				i++
				configPath = args[i]
			}
		case "--port":
			if i+1 < len(args) {
				i++
				n, err := strconv.Atoi(args[i])
				if err == nil {
					portOverride = n
				}
			}
		case "--login":
			if i+1 < len(args) {
				i++
				login = args[i]
			}
		case "--guest":
			guest = true
		case "--no-console":
			noConsole = true
		case "--no-telnet":
			noTelnet = true
		case "--version":
			fmt.Printf("%s  (%s)\n", SystemName, CPUName)
			return 0
		case "-h", "--help":
			fmt.Print(`RSTS/E V7.2-10  PDP-11/70  BASIC-PLUS

Usage: rsts [options]

  --disk DIR      virtual disk directory (default: ./disk)
  --config FILE   config.toml (default: ./config.toml)
  --port N        override telnet_port
  --guest         log in the console as GUEST
  --login NAME    prompt for that account's password
  --no-console    Telnet only
  --no-telnet     local console only
  --version       print version

config.toml keys: max_users (25), telnet_port (23), telnet_bind,
                  telnet, console
`)
			return 0
		}
	}
	cfg, cfgPath, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if portOverride >= 0 {
		cfg.TelnetPort = portOverride
		cfg.Telnet = portOverride > 0
	}
	if noConsole {
		cfg.Console = false
	}
	if noTelnet {
		cfg.Telnet = false
	}
	if !cfg.Console && !cfg.Telnet {
		fmt.Fprintln(os.Stderr, "Nothing to run: console and telnet are both off")
		return 1
	}
	sys, err := NewSystem(disk, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = cfgPath
	telnetOK := false
	if cfg.Telnet {
		addr, err := sys.StartTelnet()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Telnet :%d: %v\n", cfg.TelnetPort, err)
			if cfg.TelnetPort == 23 {
				fmt.Fprintln(os.Stderr, "Hint: port 23 needs root; set telnet_port = 2323 in config.toml")
			}
			if !cfg.Console {
				return 1
			}
		} else if addr != "" {
			telnetOK = true
			fmt.Fprintf(os.Stderr, "Telnet %s  %d users  VT52  (%s)\n", addr, cfg.MaxUsers, cfgPath)
		}
	}
	if cfg.Console {
		job, err := sys.Attach("CONSOLE")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			sys.Close()
			return 1
		}
		st := &stdTerm{in: bufio.NewReader(os.Stdin), out: os.Stdout}
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		go func() {
			for range ch {
				if sys.InterruptConsole() {
					continue
				}
				sys.Close()
				return
			}
		}()
		sys.runOnTerm(job, os.Stdout, st, "CONSOLE", login, guest)
		signal.Stop(ch)
		if sys.Halted() || !telnetOK {
			sys.Close()
			return 0
		}
		fmt.Fprintln(os.Stderr, "Console detached. Telnet still up. Ctrl-C to stop.")
	}
	if !telnetOK {
		return 0
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		<-ch
		sys.Close()
	}()
	sys.Wait()
	return 0
}
