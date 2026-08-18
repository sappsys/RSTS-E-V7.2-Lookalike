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
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/term"
)

// The system portrayed is RSTS/E V7.2 and does not change. DEC numbered
// the update level after the dash, and this project uses that number for
// its own releases, so a build reports the release it came from. Bump
// Version and everything else follows.
const (
	Version       = "7.2-11"
	SystemRelease = "V" + Version           // V7.2-11
	SystemName    = "RSTS " + SystemRelease // RSTS V7.2-11
	SystemLong    = "RSTS/E " + SystemRelease
)

var helpText = map[string]string{
	"": `RSTS/E V7.2  —  type HELP topic

Topics:
  LOGIN     HELLO / BYE
  FILES     DIR, TYPE, COPY, PIP, KILL, NAME
  BASIC     NEW, OLD, SAVE, COMPILE, LIST, RUN
  LANG      BASIC-PLUS statements
  EDIT      screen editor (VTEDIT style)
  FN        built-in functions
  COMMANDS  keyboard commands
  SET       TTYSET (WIDTH, ECHO, SCOPE, TAB, FORM, FILL, GAG, SPEED, TYPE)
  SYSTAT    jobs, disks, memory  (SYS, WHO)
  SHOW      SHOW DISKS / JOBS / CPU / ...
  DISKS     MOUNT, DISMOUNT, packs
  ACCOUNTS  default logins
  COMPILE   .BAC files and the privilege bit
  HARDWARE  PDP-11/70 configuration
  TELNET    multi-user Telnet / VT52
  SERIAL    terminals on serial lines
  JOBS      SYSTAT, ATTACH, PK:
  QUE       line-printer queue
  PLEASE    operator console
  CCL       installed keyboard commands
  QUOLST    disk and job quotas
  HELP      how to use HELP

Abbreviations work (HELP DISK = HELP DISKS).  HELP MOUNT, HELP SYSTAT,
HELP DIRECTORY, HELP HELLO, and HELP PIP are accepted.
`,
	"LOGIN": `HELLO [account]     log in  (account is PPN like 100,100 or a name)
HELLO/DETACH        log in and detach this job (keyboard returns to Bye)
BYE                 log out (returns to Bye)
PASSWORD            change your password
PASSWORD [p,pn]     (priv) set another account's password

Logged-out prompt is  Bye
Logged-in prompt is   Ready

After HELLO the system types [1,2]NOTICE.TXT, then runs LOGIN.BAS or
START.BAS in the account if one exists.

At Bye:
  HELLO             log in
  EXIT / QUIT       stop the emulator (console)
  BYE               hang up a Telnet line; on the console, stay at Bye

Ctrl-C stops a running BASIC program and returns to Ready.
It does not exit the emulator.
Ctrl-O discards output until the next Ctrl-O.
Ctrl-R redisplays the input line.
`,
	"FILES": `DIR [filespec]              catalog of files
CAT / CATALOG               same as DIR
TYPE filespec               print a file
COPY src dst                copy a file
PIP dst=src                 copy (PIP syntax)
PIP/DE filespec             delete
PIP/LI filespec             list (same as DIR)
PIP/RE new=old              rename
PIP dst=src1,src2           concatenate
PIP/AP /NE /PROT:n /GO /HE  append, no supersede, protection, continue, help
PIP/DI /WI /BR              list / wide / brief
DIR/W /S /P /F /N /B /A /C /SU /H
                            wide, size, prot, full, no header, brief,
                            allocation, cluster, summary, header
PLEASE message              send to the operator console (KB0:)
PLEASE/LI                   (priv/console) list the PLEASE queue
PLEASE/RE job text          (priv/console) reply to that job
QUE [filespec]              print queue; QUE/DE n  QUE/LI  QUMRUN
BACKUP [filespec] [MT0:]    copy files to a magtape image (also BCK)
BACKUP/RE [MT0:]            restore from that image
SUBMIT filespec             run a command file as a detached job
QUOLST                      disk and job quota for this account
CCL name=filespec           (priv) install a keyboard command
CCL /DE name                remove; CCL lists them
SHUTUP                      (priv) halt the system
UTILITY                     (priv) REACT, DSKINT, CCL, SHUTUP
KILL filespec               delete a file
UNSAVE filespec             delete a file
NAME old AS new             rename
ASSIGN device: logical      job logical name (ASSIGN DB1: WORK)
DEASSIGN [logical]          drop one name, or all if omitted
SET WIDTH n / ECHO / NOECHO / SCOPE / TAB / FORM / FILL n / GAG / SPEED n / TYPE name
                            terminal (TTYSET)
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
CONT                continue after STOP (not if the program was edited)
RENUM [start][,inc] resequence lines, default 10,10
DELETE n[-m]        delete program lines
CLEAR               reset variables

A line that starts with a number is stored in the program:
  10 PRINT "HI"
  20 END
  RUN

RENUM resequences the program and follows every reference with it:
GOTO, GOSUB, ON ... GOTO, ON ... GOSUB, THEN, ELSE, RESUME,
RESTORE, ON ERROR GOTO, and CHAIN LINE when the filespec is this
program.

  RENUM               start at 10, count by 10
  RENUM 100           start at 100, count by 10
  RENUM 100,20        start at 100, count by 20
  RENUM ,5            start at 10, count by 5
  RENUMBER            the same command

ON ERROR GOTO 0 and RESUME 0 are left alone: the 0 is not a line.
CHAIN "OTHER" LINE 100 is left alone. CHAIN "COMP" LINE 8000 is
rewritten when this program is COMP. A reference to a line that does
not exist is left as it was and reported, rather than being pointed at
some other line. The last line may not go past 32767. CONT will not
resume a program after RENUM.
`,
	"LANG": `Statements:
  LET  PRINT  INPUT  LINE INPUT  PRINT USING
  GOTO  GOSUB  RETURN  ON ... GOTO/GOSUB
  IF ... THEN ... ELSE
  FOR ... TO ... STEP / NEXT
  WHILE ... / NEXT   UNTIL ... / NEXT
  DEF FNx = expr        one line
  DEF FNx(a,b) ... FNEND    many lines, FNEXIT returns early
  DIM  DIM #n, A(m)[=len]  DATA  READ  RESTORE [n]  CHANGE  MAT
  MAT READ/PRINT/INPUT  MAT C = A+B / A-B / A*B / (K)*A
  MAT C = ZER / CON / IDN / TRN(A) / INV(A)
  MAT C = ZER(n,m) / CON(n,m) / IDN(n)   (optional redimension)
  OPEN ... [FOR INPUT/OUTPUT/APPEND] AS FILE #n [, RECORDSIZE n]
                            [, MODE n] [, CLUSTERSIZE n] [, FILESIZE n]
  OPEN ... AS FILE #n, ORGANIZATION VIRTUAL
  OPEN "PK:" AS FILE n      spawn a job on a pseudo keyboard
  OPEN "KB:" AS FILE n      this terminal, KBn: another one (priv)
  OPEN "LP:" AS FILE n      the printer, spooled to LPn.LST then QUE
  OPEN "NL:" AS FILE n      the null device
  OPEN "MT:" AS FILE n      magtape image (disk/MT0, 512-byte records)
  OPEN "PP:"/"PR:"/"CR:"    paper tape punch/reader, card reader
  OPEN "DX:"/"DT:"          floppy / DECtape images
  MAP (name) LONG X%, STRING A$ = n
  GET #n [, RECORD n]   PUT #n [, RECORD n]   UNLOCK #n
  FIELD #n, n AS A$   LSET / RSET
  CLOSE #n  RANDOMIZE  DEF FNx = ...
  ON ERROR GOTO n / 0   RESUME [NEXT | n]
  CHAIN filespec [LINE n]   COMMON A, B$(n)   SLEEP seconds
  WAIT seconds          timeout on the next INPUT (error 15)
  IF END #n THEN ...    true when the next read would be at EOF
  MID$(A$,i,n)=B$       replace n characters of A$ starting at i
  EXTEND / NOEXTEND     default NOEXTEND: names are 1 character
                            (A, A$, A%, FNA). EXTEND allows 29 characters.
  SCALE n               round floating results to n decimals (0-6)
  NAME old AS new   KILL filespec
  END  STOP  REM  (or ! comment)

OPEN MODE n bits (combinable): 1 update  2 append  8 wait  16 exclusive
  32 contiguous  64 tentative  128 no supersede  256 read regardless.
RESTORE n rereads DATA starting at that line (or the next DATA at or
after it). SPEC%(ch,fn) is magtape control: 0/5 rewind, 1 write mark,
2 skip forward, 3 skip reverse, 4 skip to tape mark.

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
         CVT%$ CVT$% CVTF$ CVT$F CVT$$ XLATE XLATE$
         RAD$ SPEC%

RECOUNT          characters the last INPUT or GET transferred
STATUS           the last OPEN: device class low, channel high
                 1 disk  2 keyboard  4 printer  8 tape  16 null
DET              determinant, set by MAT INV
NUM  NUM2        rows and columns, set by MAT INPUT

DATE / DATE(0)   integer date  (year-1970)*1000 + yearday
TIME / TIME(0)   seconds since midnight (KW11-L 60 Hz clock)
TIME(1)          CPU seconds this job has used (waits are not charged)
DATE$ / TIME$    printable date and time
PEEK(addr)       16-bit word at even byte address (monitor / I/O page)
SWAP%(n)         swap bytes of a 16-bit word (T%(11%)+SWAP%(T%(12%)))
RIGHT$(s,n)      from character n to the end (BASIC-PLUS, not last-n)

SYS(CHR$(n)+...): 1=system, 2=PPN, 3=job, 4=program, 5=date,
  6=FIP  0/-21=binary PPN  1=name  2=job  3=KB  4=program  5=date
     6=pack ID  7=minutes  8=priv  9=ident  10=SY  11=BASIC  12=BASIC+
     14=SY0:  -1=hangup  -2=echo  -5=assign  -6=deassign
     -8=KB unit  -9=date$  -3=UU.TB1  -12=UU.TB2
     -10=UU.TRM (width/echo)  -11=extra TRM (speed/type)
     -13=job CPU/size  -14=disable logins  -15=enable logins
     -16=send/broadcast  -17=directory lookup
     -7=Ctrl-C trap (CHR$(1%) enable, CHR$(0%) disable)
     other FIP subcodes return zeros
  7=time, 9=pack SY
ERR and ERL are the last trapped error number and line.
String arithmetic is exact to any length, which is how money was kept:
  SUM$(a$,b$)  DIF$(a$,b$)  PROD$(a$,b$)  QUO$(a$,b$)
  COMP%(a$,b$) -1, 0 or 1    PLACE$(a$,n) round at the nth place
  RAD$(n)      the three characters packed in a Radix-50 word

CVT%$ / CVT$% pack 16-bit integers.
CVTF$ / CVT$F pack IEEE float32 (the real 11/70 FPP was FP11-C).
SPEC%(ch%,fn%) magtape/device: 0 rewind, 1 write mark, 2 skip fwd,
  3 skip reverse, 4 skip to mark, 5 rewind. Other files: fn 0 is size
  in blocks.
`,
	"HARDWARE": `This is ` + SystemLong + ` on a PDP-11/70.

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
	"TELNET": `This system is multi-user. Each Telnet connection and each
serial line is a RSTS job on its own KB: line (KB0: is the console).

  config.toml
    # max_users    = 25     simultaneous jobs (1..63)
    # telnet_port  = 23     listener port (2323 if not root)
    # telnet_bind  = "0.0.0.0"
    # telnet       = true
    # console      = true
    # serial       = ""     "/dev/ttyUSB0,/dev/ttyS0"
    # disk         = "./disk"
    # guest        = false
    # login        = ""

Defaults and unused keys stay commented in the file. Uncomment to override.

Connect with any Telnet client. Terminal type VT52 is the baseline
(ESC A/B/C/D/H/J/K/Y/Z). ANSI/VT100 cursor keys are accepted too.

  telnet host 23

At the Bye prompt:  HELLO  then account and password.
BYE logs out. EXIT or QUIT at Bye stops the emulator on the console
(a Telnet EXIT hangs up that line only).
Ctrl-C interrupts a running program; Ctrl-U kills the input line.

Serial lines are listed in serial, separated by commas. Each is
answered at 9600 8N1 and behaves exactly like a Telnet line, and the
line is offered again when the user logs off. Real terminals and
USB-serial adapters can be hung off the emulator this way with no
network at all. Type HELP SERIAL.
`,
	"SERIAL": `Serial lines. Each device named in serial is answered at
9600 8N1 by default and is a RSTS job on its own KB: line, exactly
like a Telnet connection. SET SPEED n changes the baud on that line
(50 through 115200). When the user logs off the line is offered again.

  config.toml
    serial = "/dev/ttyUSB0,/dev/ttyS0"

The line is raw, with no flow control and no modem control lines, so a
three-wire cable works. Echo and line editing are done here, the way
RSTS did them. Ctrl-C interrupts a running program.

On Windows name the ports the usual way:

    serial = "COM1,COM3"

The \\.\ prefix that COM10 and above need is added for you.

A line that will not open is reported at startup and the rest of the
system still comes up. Linux, macOS and the BSDs use termios and Windows
uses a DCB; on any other platform a configured line reports that it
cannot be provided.

To try it without hardware on Unix, make a virtual pair:

  socat -d PTY,raw,echo=0,link=/tmp/tty1 PTY,raw,echo=0,link=/tmp/tty2

then set serial = "/tmp/tty1" and talk to /tmp/tty2.
`,
	"JOBS": `Job monitor commands (RSTS/E V7.2):

  SYSTAT [job] [/F] [/N]    all jobs (Where, What, Size, State, Run-Time)
  SYSTAT/D                  disk packs (device, pack ID, Pub/Pri)
  SYS                       same as SYSTAT
  WHO                       logged-in jobs only
  DETACH                    detach this job from the keyboard
  HELLO/DETACH              log in and detach (keyboard returns to Bye)
  ATTACH n                  attach to a detached job you own
                            (privileged: anyone's detached job)
  FORCE kb: command         inject a command (privileged)
  HANGUP n                  hang up a job/line (privileged)
  BROADCAST ALL text        message every keyboard (privileged)
  SEND kb: text             message one job
  SHUTUP                    (priv) halt every job and the listener
  UTILITY                   (priv) REACT, DSKINT, CCL, SHUTUP

SET GAG drops BROADCAST ALL on this keyboard.

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

A device name is two letters for the kind of drive, a unit number, and
a colon. The letters come from the PDP-11 handlers:

  DB:  RP04/RP05/RP06  MASSBUS RH70   340670 blocks on an RP06
  DL:  RL01/RL02       UNIBUS RL11     20480 blocks on an RL02
  DM:  RK06/RK07       UNIBUS RK611    53790 blocks on an RK07
  DK:  RK05            UNIBUS RK11      4800 blocks
  DP:  RP02/RP03       UNIBUS RP11     80000 blocks on an RP03
  DR:  RM02/RM03/RM05  MASSBUS        131680 blocks on an RM03
  DS:  RS03/RS04       MASSBUS          2048 blocks, fixed head
  DU:  RA60/RA80/RA81  MSCP UDA50     237212 blocks on an RA80

SY: is not a drive: it is the public structure, the system disk.
DSK: and LB: mean the same. Here SY:, SY0: and DB0: are one RP06.

Devices on this 11/70:

  SY:  SY0:  DB0:   RP06 system pack SYSDSK  (always mounted, public)
  DB1:              RP06, sample pack PAYROL, not mounted
  DL0: DL1:         RL02, empty
  DM0:              RK07, empty

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
  DSKINT DL0: WORK      put a new pack on an empty unit
  MOUNT DL0: WORK

/PUBLIC requires privilege (adds the pack to the public structure).
Ordinary users mount private packs. SY0:/DB0: cannot be dismounted.
Pack IDs are 1-6 letters or digits. Once mounted, PAYROL: is a
logical name for that unit.

Job logical names (in addition to pack IDs):

  ASSIGN DB1: WORK     DIR WORK:    SAVE WORK:FOO
  DEASSIGN WORK        DEASSIGN     (all)

A sample pack PAYROL is initialized on DB1: and left unmounted.

Character devices besides disk:

  KB: TT:   this job's terminal (KBn: another one)
  LP:       line printer, spooled then drained by QUMRUN to LP0
  NL:       null
  MT: MM: MS:  magtape image  disk/MT0  (512-byte records; BACKUP)
  PP: PR:   paper-tape punch / reader
  CR:       card reader (80 columns)
  DX: DT:   floppy / DECtape images
`,
	"SET": `Terminal settings (TTYSET). Stored on this job.

  SET                 show current settings
  SET WIDTH n
  SET ECHO / NOECHO
  SET SCOPE / NOSCOPE     CRT vs hardcopy; EDIT needs SCOPE
  SET TAB / NOTAB         NOTAB expands tabs to spaces
  SET FORM / NOFORM       NOFORM strips form feeds
  SET FILL n              NUL fill after CR (and after PRINT newline)
  SET GAG / NOGAG         GAG drops BROADCAST ALL
  SET SPEED n             serial baud (50..115200); Telnet is unchanged
  SET TYPE name           VT52, LA36, ... (what EDIT draws)
  SET TERMINAL WIDTH n
  TTYSET WIDTH n

Width is 0..255. SYS(CHR$(6%)+CHR$(-10%)) returns the width in the
first word of a 30-byte buffer (UU.TRM). Echo is UU.TRM as well.
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

The numbers are measured, not decoration:

  Size      the job's own storage in K-words (a word is two bytes):
            program text, variables, arrays, the string pool, and one
            buffer per open channel. A virtual array is charged to its
            file, not to the job.
  Run-Time  processor time the job has used. Waiting at Ready, at INPUT
            or in SLEEP is a wait state and is not charged.
  SYSTAT/M  Monitor and the BASIC-PLUS RTS are resident. The RTS is
            reentrant, so one 16K copy serves every job; each job pays
            only for its own data. Free is what is left of 1920K.
  SYSTAT/D  Size and Free are real blocks on the pack, counting whole
            clusters per file plus the MFD and one UFD per account.
            Open is the number of files open on that pack right now.
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
  PLEASE  QUE  QUMRUN  CCL  SUBMIT  QUOLST  BACKUP  BCK
  ASSIGN  DEASSIGN
  NEW  OLD  SAVE  REPLACE  COMPILE  LIST  LISTNH  RUN  RUNNH  CONT
  EDIT  VTEDIT  RENUM  RENUMBER  DELETE  CLEAR  SET  TTYSET
  SYSTAT  SYS  WHO
  MOUNT  DISMOUNT  DSKINT  UMOUNT
  ATTACH  DETACH  FORCE  HANGUP  BROADCAST  SEND
  SHUTUP  UTILITY
  DATE  TIME  DAYTIME
  CREATE  DELETE/ACCOUNT  REACT
  SHOW  HELP
  CPU  HARDWARE

Type HELP topic. Topics: LOGIN FILES BASIC LANG FN COMMANDS SYSTAT
SHOW DISKS ACCOUNTS COMPILE HARDWARE TELNET JOBS SET QUE CCL QUOLST
PLEASE
`,
	"HELP": `Help can be obtained on a topic by typing:

  HELP
  HELP topic

A topic is a command or subject name. Abbreviations match a unique
prefix. Attached switches are ignored (HELP SYSTAT/D = HELP SYSTAT).

Additional help is available on:
  LOGIN FILES BASIC LANG FN COMMANDS SYSTAT SHOW DISKS
  ACCOUNTS COMPILE HARDWARE TELNET JOBS SET QUE CCL QUOLST PLEASE HELP
`,
	"PLEASE": `PLEASE sends a message to the operator console (KB0:). Messages
are queued in please.json under the disk root even if no console is
attached.

  PLEASE text               send a message
  PLEASE                    prompt for the message
  PLEASE/LI                 list the queue (privileged or console)
  PLEASE/RE job text        reply to that job (privileged or console)
  OPR                       the same command

The operator sees:

  PLEASE n from [p,pn] KBn:
    text
`,
	"QUE": `QUE is the line-printer queue. OPEN "LP:" writes LPn.LST in the
account and, on CLOSE, enters that file in the queue. QUMRUN drains
the queue onto the host printer file LP0 under the disk root.

  QUE filespec     queue a file for LP:
  QUE  QUE/LI      list the queue
  QUE/DE n         delete entry n (your jobs, or all if privileged)
  QUMRUN           show the spooler and the queue

SUBMIT (also BATCH) runs a command file as a detached job: each line
is typed at Ready the way you would type it, and the keyboard is free.

  SUBMIT filespec
  BATCH filespec
`,
	"QUOLST": `QUOLST shows the disk and logged-in job quotas for this account.
Privileged users may give a PPN or name, and may set the limits.

  QUOLST
  QUOLST [p,pn]
  QUOLST/SET [p,pn] n     (priv) disk block quota (0 = unlimited)
  QUOLST/JOB [p,pn] n     (priv) logged-in job quota
  REACT QUOTA [p,pn] n
  REACT JOBQUOTA [p,pn] n

A Quota is the block limit for that PPN. Zero means no limit. Writing
a file that would go over it is error 4, ?No room for user on device,
including PRINT #. JobQuota is how many jobs that account may have
logged in at once (zero means no limit).
`,
	"CCL": `CCL installs a program as a keyboard command, the way UTILITY did.

  CCL name=filespec     (priv)  RUN that program when name is typed
  CCL/DE name           (priv)  remove it
  CCL                   list installed commands

Unique prefixes work. Built-in commands always win, so you cannot
replace DIR or SYSTAT. The program is RUN as if you had typed RUN filespec.
`,
	"EDIT": `EDIT is a screen editor in the spirit of VTEDIT, the macro package
RSTS sites layered on TECO for full-screen editing on a VT52.

  EDIT                edit the program in memory
  EDIT filespec       edit a file  (created on write if it is new)
  VTEDIT              the same command

  arrow keys          move
  ^A / ^E             start / end of line
  ^] / ^_             word forward / back
  ^G                  go to a line
  RETURN              split the line
  ^O                  open a line (split, stay put)
  DEL, ^H             rub out the character before the cursor
  ^D                  delete the character under the cursor
  ^K                  kill the whole line (copied for ^Y)
  ^U                  kill to the start of the line
  ^T                  transpose the last two characters
  ^S / ^R             find / reverse find  (empty repeats)
  ^\                  replace  (Y = this, N = skip, A = rest, ^G = stop)
  ^Y                  yank the last kill
  ^^                  set mark
  ^V / ^Q             copy / cut the region
  Insert              overwrite on or off
  ^W                  write
  ^X                  write and exit
  ^C                  exit  (twice, if there are unsaved changes)
  ^L                  redraw

Find, replace and go-to-line prompt on the status line. ^G or ^C
cancels a prompt. Find wraps around the file; replace runs from the
cursor to the end. The line at the foot of the screen shows the file,
where you are, and whether anything is unsaved.

EDIT with no file name edits the BASIC-PLUS program in memory, line
numbers and all. On write, every line is parsed first: if any line will
not compile, nothing is stored and the reason appears on the status
line, so a typo cannot lose the edit. EDIT of a file writes it back
through the normal file system, so protection codes are enforced and
kept. A compiled .BAC cannot be edited.

The real TECO is not installed.
`,
	"TECO": `TECO itself is not installed. Type EDIT for the screen editor,
which covers what VTEDIT was used for. OLD / LIST / SAVE still work for
BASIC-PLUS programs, and TYPE / COPY for files.
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
  REACT QUOTA [p,pn] n      disk block quota (0 = unlimited)
  REACT JOBQUOTA [p,pn] n   logged-in job quota
  QUOLST/SET [p,pn] n

CREATE of [1,*] is privileged. [1,2] cannot be deleted.
An account that is logged in cannot be deleted.

RUN of a <232> .BAC gives a normal user temporary privilege for
that run only (see HELP COMPILE).
`,
}

// One layout for the heading and every row of a directory listing:
// name and type left, size and protection right, date left, time right so
// that the AM and PM line up.
const dirRowFormat = "%-9s.%-3s  %6s  %5s  %-9s  %8s"

// FormatDir is the full DIR listing: name, type, size, protection, date, time.
func FormatDir(dev, ppn string, infos []FileInfo) string {
	var b strings.Builder
	if dev == "" {
		dev = "SY:"
	}
	fmt.Fprintf(&b, "%s[%s]\n", dev, ppn)
	// The header and the rows share one format, so the labels always sit
	// over their columns.
	fmt.Fprintf(&b, dirRowFormat, "Name", "Typ", "Size", "Prot", "Date", "Time")
	blocks := 0
	for _, info := range infos {
		blocks += info.Blocks()
		b.WriteString("\n")
		fmt.Fprintf(&b, dirRowFormat,
			clip(info.NamePart(), 9),
			clip(info.ExtPart(), 3),
			strconv.Itoa(info.Blocks()),
			fmt.Sprintf("<%3d>", info.Prot),
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

// parseCmdSwitches pulls /SWITCH and /SWITCH:value from anywhere in rest
// (DIR *.BAS/W, PIP FILE/DE). A bare "=" in PIP dst=src is not a switch.
func parseCmdSwitches(rest string) (map[string]string, string) {
	sw := map[string]string{}
	var arg strings.Builder
	i := 0
	for i < len(rest) {
		if rest[i] == '/' {
			i++
			start := i
			for i < len(rest) && rest[i] != '/' && rest[i] != ' ' && rest[i] != '\t' && rest[i] != '=' && rest[i] != ':' {
				i++
			}
			name := strings.ToUpper(rest[start:i])
			val := ""
			if i < len(rest) && (rest[i] == ':' || rest[i] == '=') {
				i++
				vstart := i
				for i < len(rest) && rest[i] != '/' && rest[i] != ' ' && rest[i] != '\t' {
					i++
				}
				val = rest[vstart:i]
			}
			if name != "" {
				sw[name] = val
			}
			continue
		}
		arg.WriteByte(rest[i])
		i++
	}
	return sw, strings.TrimSpace(arg.String())
}

func switchOn(sw map[string]string, names ...string) bool {
	for raw := range sw {
		for _, n := range names {
			n = strings.ToUpper(n)
			if raw == n || strings.HasPrefix(n, raw) {
				return true
			}
		}
	}
	return false
}

// switchMin is true when a typed switch is a unique prefix of canon of
// at least min letters. SIZE is "/S"; SUMMARY needs "/SU" so they do not
// collide.
func switchMin(sw map[string]string, canon string, min int) bool {
	canon = strings.ToUpper(canon)
	for raw := range sw {
		if strings.HasPrefix(canon, raw) && len(raw) >= min {
			return true
		}
	}
	return false
}

func switchValue(sw map[string]string, names ...string) string {
	for raw, v := range sw {
		for _, n := range names {
			n = strings.ToUpper(n)
			if raw == n || strings.HasPrefix(n, raw) {
				return v
			}
		}
	}
	return ""
}

// FormatDirWide is DIR/W and DIR/B: names in four columns, optional header.
func FormatDirWide(dev, ppn string, infos []FileInfo, header bool) string {
	var b strings.Builder
	if header {
		if dev == "" {
			dev = "SY:"
		}
		fmt.Fprintf(&b, "%s[%s]\n", dev, ppn)
	}
	if len(infos) == 0 {
		b.WriteString("%No files")
		return b.String()
	}
	for i, info := range infos {
		fmt.Fprintf(&b, "%-9s.%-3s", clip(info.NamePart(), 9), clip(info.ExtPart(), 3))
		if (i+1)%4 == 0 || i == len(infos)-1 {
			b.WriteByte('\n')
		} else {
			b.WriteString("    ")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatDirCols is DIR with selected columns. which is S, P, F, A, C, SU,
// FA, FC, or FAC (size, protection, full, allocation, cluster, summary).
func FormatDirCols(dev, ppn string, infos []FileInfo, header bool, which string) string {
	var b strings.Builder
	if header {
		if dev == "" {
			dev = "SY:"
		}
		fmt.Fprintf(&b, "%s[%s]\n", dev, ppn)
		switch which {
		case "S":
			fmt.Fprintf(&b, "%-9s.%-3s  %6s", "Name", "Typ", "Size")
		case "P":
			fmt.Fprintf(&b, "%-9s.%-3s  %5s", "Name", "Typ", "Prot")
		case "A":
			fmt.Fprintf(&b, "%-9s.%-3s  %6s", "Name", "Typ", "Alloc")
		case "C":
			fmt.Fprintf(&b, "%-9s.%-3s  %5s", "Name", "Typ", "Clu")
		case "FA":
			fmt.Fprintf(&b, dirRowFormat+"  %5s", "Name", "Typ", "Size", "Prot", "Date", "Time", "Alloc")
		case "FC":
			fmt.Fprintf(&b, dirRowFormat+"  %4s", "Name", "Typ", "Size", "Prot", "Date", "Time", "Clu")
		case "FAC":
			fmt.Fprintf(&b, dirRowFormat+"  %5s  %4s", "Name", "Typ", "Size", "Prot", "Date", "Time", "Alloc", "Clu")
		case "SU":
		default:
			fmt.Fprintf(&b, dirRowFormat, "Name", "Typ", "Size", "Prot", "Date", "Time")
		}
	}
	blocks := 0
	for _, info := range infos {
		blocks += info.Blocks()
		if which == "SU" {
			continue
		}
		b.WriteString("\n")
		switch which {
		case "S":
			fmt.Fprintf(&b, "%-9s.%-3s  %6s", clip(info.NamePart(), 9), clip(info.ExtPart(), 3), strconv.Itoa(info.Blocks()))
		case "P":
			fmt.Fprintf(&b, "%-9s.%-3s  %5s", clip(info.NamePart(), 9), clip(info.ExtPart(), 3), fmt.Sprintf("<%3d>", info.Prot))
		case "A":
			fmt.Fprintf(&b, "%-9s.%-3s  %6s", clip(info.NamePart(), 9), clip(info.ExtPart(), 3), strconv.Itoa(info.Alloc))
		case "C":
			fmt.Fprintf(&b, "%-9s.%-3s  %5s", clip(info.NamePart(), 9), clip(info.ExtPart(), 3), strconv.Itoa(info.Cluster))
		case "SU":
		case "FA":
			fmt.Fprintf(&b, dirRowFormat+"  %5s",
				clip(info.NamePart(), 9), clip(info.ExtPart(), 3),
				strconv.Itoa(info.Blocks()), fmt.Sprintf("<%3d>", info.Prot),
				info.Modified.Format("02-Jan-06"),
				strings.TrimLeft(info.Modified.Format("3:04 PM"), "0"),
				strconv.Itoa(info.Alloc))
		case "FC":
			fmt.Fprintf(&b, dirRowFormat+"  %4s",
				clip(info.NamePart(), 9), clip(info.ExtPart(), 3),
				strconv.Itoa(info.Blocks()), fmt.Sprintf("<%3d>", info.Prot),
				info.Modified.Format("02-Jan-06"),
				strings.TrimLeft(info.Modified.Format("3:04 PM"), "0"),
				strconv.Itoa(info.Cluster))
		case "FAC":
			fmt.Fprintf(&b, dirRowFormat+"  %5s  %4s",
				clip(info.NamePart(), 9), clip(info.ExtPart(), 3),
				strconv.Itoa(info.Blocks()), fmt.Sprintf("<%3d>", info.Prot),
				info.Modified.Format("02-Jan-06"),
				strings.TrimLeft(info.Modified.Format("3:04 PM"), "0"),
				strconv.Itoa(info.Alloc), strconv.Itoa(info.Cluster))
		default:
			fmt.Fprintf(&b, dirRowFormat,
				clip(info.NamePart(), 9),
				clip(info.ExtPart(), 3),
				strconv.Itoa(info.Blocks()),
				fmt.Sprintf("<%3d>", info.Prot),
				info.Modified.Format("02-Jan-06"),
				strings.TrimLeft(info.Modified.Format("3:04 PM"), "0"))
		}
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
	raw *term.State
}

func (t *stdTerm) Write(p []byte) (int, error) { return t.out.Write(p) }

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
	echo       bool
	width      int
	scope      bool
	tab        bool
	form       bool
	fill       int
	gag        bool
	speed      int
	ttype      string
	logicals   map[string]string
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
		echo:     true,
		width:    80,
		scope:    true,
		tab:      true,
		form:     true,
		speed:    9600,
		logicals: map[string]string{},
	}
	s.Basic = NewMachine(IO{
		Write: s.write,
		Read:  s.read,
		Open:  s.openBasicFile,
		Load: func(name string) error {
			if err := s.loadForRun(name); err != nil {
				return err
			}
			if s.Basic.PrivImage {
				s.tempPriv = true
			} else {
				s.tempPriv = false
			}
			s.syncPrivilege()
			return nil
		},
		Delete: s.basicKill,
		Rename: s.basicName,
		Disk:   sys.Disk,
		Job:    job.Num,
		KB:     job.KB,
		Width:  80,
		Echo:   true,
		Speed:  9600,
		PollInterrupt: func() bool {
			t, ok := s.term.(interface{ PollInterrupt() bool })
			return ok && t.PollInterrupt()
		},
		Hangup: func() error {
			if s.sys == nil {
				return nil
			}
			return s.sys.HangupJob(strconv.Itoa(s.Job))
		},
		Assign: func(dev, logical string) error {
			return s.cmdAssign(strings.TrimSpace(dev + " " + logical))
		},
		Deassign: func(name string) error {
			return s.cmdDeassign(name)
		},
		Broadcast: func(to, text string) error {
			if s.sys == nil {
				return fsErr("Can't find job")
			}
			from := "KB?"
			if s.Account != nil {
				from = s.Account.Display()
			}
			return s.sys.Broadcast(to, from+" "+s.KB, text)
		},
		SetLogins: func(off bool) error {
			if s.sys == nil {
				return nil
			}
			s.sys.mu.Lock()
			s.sys.noLogins = off
			s.sys.mu.Unlock()
			return nil
		},
	})
	s.Basic.cpuStart = sys.Boot
	sys.registerShell(s)
	s.syncJob()
	return s
}

func (s *Shell) seedSamples() error {
	seeds := loadSeeds(s.Disk.Root)
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
			if !seeds.replaces(path, body, proj, prog) {
				if prot != defaultProt {
					_ = s.Disk.SetProt(spec, proj, prog, true, prot)
				}
				continue
			}
			if err := s.Disk.WriteText(spec, proj, prog, true, body, prot); err != nil {
				return err
			}
			seeds.record(path, body)
		}
	}
	if err := seeds.save(s.Disk.Root); err != nil {
		return err
	}
	return nil
}

func (s *Shell) write(text string, newline bool) {
	fmt.Fprint(s.out, s.formatTTY(text))
	if newline {
		if s.fill > 0 {
			fmt.Fprint(s.out, strings.Repeat("\x00", s.fill))
		}
		fmt.Fprint(s.out, "\n")
	}
}

func (s *Shell) formatTTY(text string) string {
	if s == nil {
		return text
	}
	if !s.tab {
		text = expandTabs(text, 8)
	}
	if !s.form {
		text = strings.ReplaceAll(text, "\f", "")
	}
	if s.fill > 0 {
		pad := strings.Repeat("\x00", s.fill)
		text = strings.ReplaceAll(text, "\r", "\r"+pad)
	}
	return text
}

func expandTabs(s string, tabw int) string {
	if tabw < 1 {
		tabw = 8
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := tabw - (col % tabw)
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		if r == '\n' || r == '\r' {
			col = 0
		} else {
			col++
		}
	}
	return b.String()
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
	if dev, unit, unitSet, rest, ok := parseCharDevice(path); ok {
		return s.openCharDevice(m, channel, dev, unit, unitSet, rest, mode)
	}
	spec, err := s.parseSpec(path, "DAT")
	if err != nil {
		return basicErr(err.Error())
	}
	return s.openDiskFile(m, channel, spec, mode)
}

func (s *Shell) openDiskFile(m *Machine, channel int, spec FileSpec, mode string) error {
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
	if mode == "OUTPUT" && m.openModeN&modeNoSupersede != 0 {
		if _, err := os.Stat(real); err == nil {
			return basicErrCode("Name or account now exists", 16)
		}
	}
	// MODE 256 skips this protection check so a program can read a file
	// the account would otherwise be denied.
	if mode == "INPUT" && m.openModeN&modeReadAny == 0 {
		if _, err := os.Stat(real); err == nil {
			_, proj, prog, locErr := s.Disk.locate(spec, s.Account.Proj, s.Account.Prog, s.priv(), true)
			if locErr == nil {
				if err := s.Disk.checkAccess(real, proj, prog, s.Account.Proj, s.Account.Prog, s.priv(), accRead); err != nil {
					return err
				}
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
	cf := &chanFile{file: f, path: real, mode: mode, class: devDisk}
	if m.openModeN&modeTentative != 0 {
		cf.tentative = true
	}
	if mode == "INPUT" {
		cf.r = bufio.NewReader(f)
	}
	if s.sys != nil {
		if pack, err := s.Disk.resolvePack(spec); err == nil {
			dev := pack.Designator()
			s.sys.notePackOpen(dev, 1)
			cf.onClose = func() { s.sys.notePackOpen(dev, -1) }
		}
	}
	// Reusing a channel closes whatever was on it, so the file and its
	// place in the open count are both released.
	if old := m.Files[channel]; old != nil {
		closeChanFile(old)
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
		if s.runCCL(verb, rest) {
			return
		}
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
	case "PLEASE", "OPR":
		return s.cmdPlease(rest)
	case "QUE":
		return s.cmdQue(rest)
	case "QUMRUN":
		return s.cmdQumrun(rest)
	case "BACKUP", "BCK":
		return s.cmdBackup(rest)
	case "SUBMIT", "BATCH":
		return s.cmdSubmit(rest)
	case "QUOLST", "QUOTA":
		return s.cmdQuolst(rest)
	case "SHUTUP":
		return s.cmdShutup(rest)
	case "UTILITY":
		return s.cmdUtility(rest)
	case "CCL":
		return s.cmdCCL(rest)
	case "KILL", "UNSAVE":
		return s.cmdKill(rest)
	case "NAME":
		return s.cmdName(rest)
	case "RENAME":
		return s.cmdRename(rest)
	case "RENUM", "RENUMBER", "RESEQ", "RESEQUENCE":
		return s.cmdRenum(rest)
	case "EDIT", "VTEDIT":
		return s.cmdEdit(rest)
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
	case "CONT", "CONTINUE":
		s.cmdCont()
	case "SET", "TTYSET":
		return s.cmdSet(rest)
	case "ASSIGN":
		return s.cmdAssign(rest)
	case "DEASSIGN":
		return s.cmdDeassign(rest)
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
	"TYPE", "COPY", "PIP", "PLEASE", "OPR", "KILL", "UNSAVE", "NAME", "RENAME",
	"QUE", "QUMRUN", "CCL", "SUBMIT", "BATCH", "QUOLST", "QUOTA", "SHUTUP", "UTILITY",
	"NEW", "OLD", "SAVE", "REPLACE", "COMPILE", "COMPIL",
	"LIST", "LISTNH", "RUN", "RUNNH", "DELETE", "DEL", "CLEAR",
	"SYSTAT", "SYS", "WHO",
	"MOUNT", "DISMOUNT", "DSKINT", "INITIALIZE", "UMOUNT",
	"DETACH", "ATTACH", "FORCE", "HANGUP", "BROADCAST", "SEND", "TALK", "PLEASE", "OPR",
	"CPU", "HARDWARE", "DATE", "TIME", "DAYTIME",
	"PASSWORD", "CREATE", "REACT", "ACCOUNT", "SHOW", "REMOVE",
	"CONT", "SET", "TTYSET", "ASSIGN", "DEASSIGN",
	"BACKUP", "BCK",
	"RENUM", "RENUMBER", "RESEQ", "RESEQUENCE",
	"EDIT", "VTEDIT",
}

// Different spellings of one command, so that an abbreviation shared by
// only those spellings still resolves instead of looking ambiguous.
var cmdSynonym = map[string]string{
	"RENUMBER":   "RENUM",
	"RESEQ":      "RENUM",
	"RESEQUENCE": "RENUM",
	"LOGOUT":     "BYE",
	"CATALOG":    "CAT",
	"CONTINUE":   "CONT",
	"TTYSET":     "SET",
	"BATCH":      "SUBMIT",
	"QUOTA":      "QUOLST",
	"BCK":        "BACKUP",
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
	// RENU matches both RENUM and RENUMBER, which are the same command,
	// so it is not really ambiguous.
	canon := ""
	for _, h := range hits {
		c := h
		if alias, ok := cmdSynonym[h]; ok {
			c = alias
		}
		if canon == "" {
			canon = c
		} else if canon != c {
			return verb
		}
	}
	if canon != "" {
		return canon
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

func (s *Shell) parseSpec(text, defaultExt string) (FileSpec, error) {
	return ParseFileSpec(s.expandLogical(text), defaultExt)
}

func (s *Shell) expandLogical(text string) string {
	if s == nil || len(s.logicals) == 0 {
		return text
	}
	raw := strings.TrimSpace(text)
	body, prot, protSet, err := splitProt(raw)
	if err != nil {
		return text
	}
	i := strings.IndexByte(body, ':')
	if i <= 0 {
		return text
	}
	dev := strings.ToUpper(body[:i])
	alias, ok := s.logicals[dev]
	if !ok {
		return text
	}
	out := alias + body[i:]
	if protSet {
		return fmt.Sprintf("%s<%d>", out, prot)
	}
	return out
}

func specDevToken(spec FileSpec) string {
	if spec.UnitSet {
		return fmt.Sprintf("%s%d", spec.Device, spec.Unit)
	}
	return spec.Device
}

func (s *Shell) printNotice() {
	if s.Disk == nil {
		return
	}
	spec, err := ParseFileSpec("[1,2]NOTICE.TXT", "")
	if err != nil {
		return
	}
	text, err := s.Disk.ReadText(spec, 1, 2, true)
	if err != nil || strings.TrimSpace(text) == "" {
		return
	}
	fmt.Fprint(s.out, text)
	if !strings.HasSuffix(text, "\n") {
		fmt.Fprint(s.out, "\n")
	}
}

func (s *Shell) runLoginFile() {
	if s.Account == nil || s.Disk == nil {
		return
	}
	for _, name := range []string{"LOGIN", "START"} {
		spec, err := s.parseSpec(name, "BAS")
		if err != nil {
			continue
		}
		if s.Disk.Exists(spec, s.Account.Proj, s.Account.Prog, s.priv()) {
			s.cmdRun(name, false)
			return
		}
		bac := spec
		bac.Ext = "BAC"
		if s.Disk.Exists(bac, s.Account.Proj, s.Account.Prog, s.priv()) {
			s.cmdRun(name, false)
			return
		}
	}
}

func (s *Shell) basicKill(path string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	spec, err := s.parseSpec(path, "BAS")
	if err != nil {
		return err
	}
	return s.Disk.Delete(spec, acct.Proj, acct.Prog, s.priv())
}

func (s *Shell) basicName(old, new string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	ospec, err := s.parseSpec(old, "BAS")
	if err != nil {
		return err
	}
	nspec, err := s.parseSpec(new, "BAS")
	if err != nil {
		return err
	}
	if err := s.Disk.Rename(ospec, nspec, acct.Proj, acct.Prog, s.accountPriv()); err != nil {
		return err
	}
	if ospec.Filename() == s.Basic.ProgramName+".BAS" {
		s.Basic.ProgramName = nspec.Name
	}
	return nil
}

func (s *Shell) cmdAssign(rest string) error {
	if _, err := s.needLogin(); err != nil {
		return err
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		if len(s.logicals) == 0 {
			fmt.Fprintln(s.out, "No logical names")
			return nil
		}
		keys := make([]string, 0, len(s.logicals))
		for k := range s.logicals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(s.out, "%s:  =  %s:\n", k, s.logicals[k])
		}
		return nil
	}
	dev, logical := "", ""
	parts := strings.Fields(rest)
	if len(parts) >= 2 {
		dev, logical = parts[0], parts[1]
	} else {
		tok := strings.ToUpper(parts[0])
		i := strings.IndexByte(tok, ':')
		if i < 0 || i == len(tok)-1 {
			fmt.Fprintln(s.out, "?ASSIGN device: logical")
			return nil
		}
		dev, logical = tok[:i+1], tok[i+1:]
	}
	dev = strings.ToUpper(strings.TrimSpace(dev))
	if !strings.HasSuffix(dev, ":") {
		dev += ":"
	}
	logical = strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(logical), ":"))
	if !validPackID(logical) {
		return fsErr("Illegal file name")
	}
	spec, err := ParseFileSpec(dev, "")
	if err != nil {
		return err
	}
	if s.logicals == nil {
		s.logicals = map[string]string{}
	}
	s.logicals[logical] = specDevToken(spec)
	return nil
}

func (s *Shell) cmdDeassign(rest string) error {
	if _, err := s.needLogin(); err != nil {
		return err
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		s.logicals = map[string]string{}
		return nil
	}
	name := strings.ToUpper(strings.TrimSuffix(rest, ":"))
	delete(s.logicals, name)
	return nil
}

func (s *Shell) cmdSet(rest string) error {
	if _, err := s.needLogin(); err != nil {
		return err
	}
	fields := strings.Fields(strings.ToUpper(rest))
	if len(fields) == 0 {
		s.showTTY()
		return nil
	}
	i := 0
	if fields[0] == "TERMINAL" || fields[0] == "TT" {
		i = 1
		if i >= len(fields) {
			s.showTTY()
			return nil
		}
	}
	for i < len(fields) {
		sw := fields[i]
		i++
		switch sw {
		case "WIDTH":
			if i >= len(fields) {
				fmt.Fprintln(s.out, "?SET WIDTH n")
				return nil
			}
			n, err := strconv.Atoi(fields[i])
			i++
			if err != nil || n < 0 || n > 255 {
				return fsErr("Illegal number")
			}
			s.width = n
			if s.Basic != nil {
				s.Basic.IO.Width = n
			}
		case "ECHO":
			s.echo = true
			if s.Basic != nil {
				s.Basic.IO.Echo = true
			}
		case "NOECHO":
			s.echo = false
			if s.Basic != nil {
				s.Basic.IO.Echo = false
			}
		case "SCOPE":
			s.scope = true
		case "NOSCOPE":
			s.scope = false
		case "TAB":
			s.tab = true
		case "NOTAB":
			s.tab = false
		case "FORM":
			s.form = true
		case "NOFORM":
			s.form = false
		case "FILL":
			if i >= len(fields) {
				fmt.Fprintln(s.out, "?SET FILL n")
				return nil
			}
			n, err := strconv.Atoi(fields[i])
			i++
			if err != nil || n < 0 || n > 255 {
				return fsErr("Illegal number")
			}
			s.fill = n
		case "GAG":
			s.gag = true
		case "NOGAG":
			s.gag = false
		case "SPEED":
			if i >= len(fields) {
				fmt.Fprintln(s.out, "?SET SPEED n")
				return nil
			}
			n, err := strconv.Atoi(fields[i])
			i++
			if err != nil || n < 0 {
				return fsErr("Illegal number")
			}
			if _, ok := canonicalBaud(n); !ok && n != 0 {
				return fsErr("Illegal number")
			}
			s.speed = n
			if s.Basic != nil {
				s.Basic.IO.Speed = n
			}
		case "TYPE":
			if i >= len(fields) {
				fmt.Fprintln(s.out, "?SET TYPE name")
				return nil
			}
			s.ttype = fields[i]
			i++
			if s.Basic != nil {
				s.Basic.IO.TermType = s.ttype
			}
		default:
			fmt.Fprintln(s.out, "?SET WIDTH n  ECHO  SCOPE  TAB  FORM  FILL n  GAG  SPEED n  TYPE name")
			return nil
		}
	}
	s.applyTermSettings()
	return nil
}

func (s *Shell) showTTY() {
	echo := "NOECHO"
	if s.echo {
		echo = "ECHO"
	}
	scope := "NOSCOPE"
	if s.scope {
		scope = "SCOPE"
	}
	tab := "NOTAB"
	if s.tab {
		tab = "TAB"
	}
	form := "NOFORM"
	if s.form {
		form = "FORM"
	}
	gag := "NOGAG"
	if s.gag {
		gag = "GAG"
	}
	tt := s.ttype
	if tt == "" {
		tt = "UNKNOWN"
	}
	fmt.Fprintf(s.out, "WIDTH %d  %s  %s  %s  %s  FILL %d  %s  SPEED %d  TYPE %s\n",
		s.width, echo, scope, tab, form, s.fill, gag, s.speed, tt)
}

func (s *Shell) applyTermSettings() {
	if t, ok := s.term.(interface{ SetEcho(bool) }); ok {
		t.SetEcho(s.echo)
	}
	if t, ok := s.term.(interface{ SetWidth(int) }); ok && s.width > 0 {
		t.SetWidth(s.width)
	}
	if t, ok := s.term.(interface{ SetSpeed(int) error }); ok && s.speed > 0 {
		_ = t.SetSpeed(s.speed)
	}
	if t, ok := s.term.(interface{ SetTTY(bool, bool, int) }); ok {
		t.SetTTY(s.tab, s.form, s.fill)
	}
}

func (s *Shell) cmdHello(rest string) {
	if s.Account != nil {
		fmt.Fprintln(s.out, "?Already logged in -- type BYE first")
		return
	}
	sw, token := parseCmdSwitches(rest)
	detach := switchOn(sw, "DETACH")
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
	if detach && s.Account != nil {
		s.cmdDetach()
	}
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
	if s.sys != nil {
		s.sys.mu.Lock()
		blocked := s.sys.noLogins && !s.console
		s.sys.mu.Unlock()
		if blocked {
			fmt.Fprintln(s.out, "?Logins are disabled")
			return
		}
	}
	if s.sys != nil && acct.JobQuota > 0 {
		if n := len(s.sys.jobsForPPN(acct.Proj, acct.Prog)); n >= acct.JobQuota {
			fmt.Fprintln(s.out, "?Maximum users exceeded")
			return
		}
	}
	s.Account = acct
	s.tempPriv = false
	s.Basic.IO.PPN = acct.Display()
	s.Basic.IO.AccountName = acct.Name
	s.Basic.IO.Privileged = s.accountPriv()
	s.Basic.IO.Job = s.Job
	s.Basic.IO.Quota = acct.Quota
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
	s.printNotice()
	s.runLoginFile()
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
	j.SizeK = minJobKW
	if s.Basic != nil {
		j.SizeK = s.Basic.SizeKW()
		j.CPU = s.Basic.CPUTime()
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
	s.logicals = map[string]string{}
	s.echo = true
	s.width = 80
	s.scope = true
	s.tab = true
	s.form = true
	s.fill = 0
	s.gag = false
	s.speed = 9600
	s.ttype = ""
	if s.Basic != nil {
		s.Basic.IO.Echo = true
		s.Basic.IO.Width = 80
	}
	s.applyTermSettings()
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
	"CONT": "BASIC", "CONTINUE": "BASIC",
	"RENUM": "BASIC", "RENUMBER": "BASIC", "RESEQ": "BASIC",
	"SET": "SET", "TTYSET": "SET", "ECHO": "SET", "WIDTH": "SET",
	"SYS": "SYSTAT", "WHO": "SYSTAT", "STATUS": "SYSTAT",
	"TTY": "SERIAL", "RS232": "SERIAL", "MODEM": "SERIAL", "PORT": "SERIAL",
	"CCL": "CCL", "KEYBOARD": "JOBS", "KEYBOARDS": "JOBS",
	"CMDS": "COMMANDS", "DCL": "COMMANDS",
	"CPU": "HARDWARE", "PDP": "HARDWARE", "PDP11": "HARDWARE", "SWITCH": "HARDWARE",
	"HLP":       "HELP",
	"DIRECTORY": "FILES", "DIR": "FILES", "CAT": "FILES", "CATALOG": "FILES",
	"TYPE": "FILES", "PIP": "FILES", "COPY": "FILES", "KILL": "FILES",
	"NAME": "FILES", "UNSAVE": "FILES", "FILENAMES": "FILES", "FIT": "FILES",
	"DIRECT": "FILES", "QUOLST": "QUOLST", "QUOTA": "QUOLST",
	"BACKUP": "FILES", "BCK": "FILES", "RESTORE": "FILES",
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
	"QUE": "QUE", "QUEUE": "QUE", "QUMRUN": "QUE", "SUBMIT": "QUE", "BATCH": "QUE",
	"SHUTUP": "JOBS", "UTILITY": "JOBS",
	"TECO": "TECO", "VTEDIT": "EDIT", "EDITOR": "EDIT",
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
	sw, arg := parseCmdSwitches(rest)
	if arg == "" {
		arg = "*.*"
	}
	spec, err := s.parseSpec(arg, "*")
	if err != nil {
		return err
	}
	if spec.Name == "*" && arg == "*.*" {
		spec.Ext = "*"
	}
	ppn, infos, err := s.Disk.ListDir(spec, acct.Proj, acct.Prog, s.priv())
	if err != nil {
		return err
	}
	header := !switchMin(sw, "NOHEADER", 1)
	if switchMin(sw, "HEADER", 1) && !switchMin(sw, "NOHEADER", 1) {
		header = true
	}
	dev := spec.DevName()
	alloc := switchMin(sw, "ALLOCATION", 1)
	cluster := switchMin(sw, "CLUSTER", 1)
	switch {
	case switchMin(sw, "SUMMARY", 2):
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "SU"))
	case switchMin(sw, "WIDE", 1) || switchMin(sw, "BRIEF", 1):
		fmt.Fprintln(s.out, FormatDirWide(dev, ppn, infos, header))
	case switchMin(sw, "SIZE", 1) && !switchMin(sw, "SUMMARY", 2) && !alloc && !cluster:
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "S"))
	case switchMin(sw, "PROTECTION", 1) && !alloc && !cluster && !switchMin(sw, "FULL", 1):
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "P"))
	case alloc && !cluster && !switchMin(sw, "FULL", 1) && !switchMin(sw, "SIZE", 1):
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "A"))
	case cluster && !alloc && !switchMin(sw, "FULL", 1) && !switchMin(sw, "SIZE", 1):
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "C"))
	case alloc && cluster:
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "FAC"))
	case alloc:
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "FA"))
	case cluster:
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "FC"))
	default:
		fmt.Fprintln(s.out, FormatDirCols(dev, ppn, infos, header, "F"))
	}
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
	spec, err := s.parseSpec(rest, "")
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
	src, err := s.parseSpec(parts[0], "")
	if err != nil {
		return err
	}
	dst, err := s.parseSpec(parts[1], src.Ext)
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
	sw, arg := parseCmdSwitches(rest)
	if switchMin(sw, "HELP", 2) {
		fmt.Fprint(s.out, pipHelp)
		return nil
	}
	goOn := switchMin(sw, "GO", 2)
	run := func(err error) error {
		if err == nil {
			return nil
		}
		if goOn {
			fmt.Fprintln(s.out, err.Error())
			return nil
		}
		return err
	}
	switch {
	case switchOn(sw, "DE", "DELETE"):
		return run(s.pipDelete(arg, acct))
	case switchOn(sw, "LI", "LIST", "DI", "DIR", "WI", "WIDE", "BR", "BRIEF"):
		return s.cmdDir(rest)
	case switchOn(sw, "RE", "RENAME"):
		return run(s.pipRename(arg, acct))
	}
	i := strings.IndexByte(arg, '=')
	if i < 0 {
		fmt.Fprintln(s.out, "?PIP dst=src")
		return nil
	}
	dstArg := strings.TrimSpace(arg[:i])
	srcArg := strings.TrimSpace(arg[i+1:])
	if switchOn(sw, "CO", "CONCAT") || strings.Contains(srcArg, ",") {
		return run(s.pipConcat(dstArg, srcArg, acct))
	}
	dst, err := s.parseSpec(dstArg, "")
	if err != nil {
		return run(err)
	}
	src, err := s.parseSpec(srcArg, "")
	if err != nil {
		return run(err)
	}
	if dst.Ext == "" {
		dst.Ext = src.Ext
	}
	if v := switchValue(sw, "PROT", "PROTECTION"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 255 {
			return run(fsErr("Illegal protection code"))
		}
		dst.Prot, dst.ProtSet = n, true
	}
	appendTo := switchMin(sw, "APPEND", 2)
	// /NE is not a prefix of NOSUPERSEDE (that word starts with NO).
	noSuper := switchOn(sw, "NE", "NOSUPERSEDE")
	return run(s.Disk.copyFile(src, dst, acct.Proj, acct.Prog, s.priv(), appendTo, noSuper))
}

const pipHelp = `PIP dst=src [/AP] [/NE] [/PROT:n] [/GO]
PIP/DE filespec             delete
PIP/LI filespec             list (same as DIR)
PIP/DI filespec             same as /LI
PIP/WI  PIP/BR              wide / brief list
PIP/RE new=old              rename
PIP dst=src1,src2           concatenate
PIP/AP dst=src              append copy
PIP/NE dst=src              no supersede
PIP/PROT:n dst=src          set destination protection
PIP/GO                      continue after errors
PIP/RW  PIP/DEN             magtape (accepted)
`

func (s *Shell) pipDelete(arg string, acct *Account) error {
	if strings.TrimSpace(arg) == "" {
		fmt.Fprintln(s.out, "?PIP/DE filespec")
		return nil
	}
	spec, err := s.parseSpec(arg, "")
	if err != nil {
		return err
	}
	if spec.Wildcard || spec.Name == "*" || strings.ContainsAny(spec.Name, "*?") || strings.ContainsAny(spec.Ext, "*?") {
		ppn, infos, err := s.Disk.ListDir(spec, acct.Proj, acct.Prog, s.priv())
		_ = ppn
		if err != nil {
			return err
		}
		for _, info := range infos {
			one, err := s.parseSpec(info.Name, "")
			if err != nil {
				continue
			}
			one.Device = spec.Device
			one.Unit = spec.Unit
			one.UnitSet = spec.UnitSet
			one.Proj, one.Prog = spec.Proj, spec.Prog
			if err := s.Disk.Delete(one, acct.Proj, acct.Prog, s.priv()); err != nil {
				fmt.Fprintln(s.out, err.Error())
			}
		}
		return nil
	}
	return s.Disk.Delete(spec, acct.Proj, acct.Prog, s.priv())
}

func (s *Shell) pipRename(arg string, acct *Account) error {
	i := strings.IndexByte(arg, '=')
	if i < 0 {
		fmt.Fprintln(s.out, "?PIP/RE new=old")
		return nil
	}
	dst, err := s.parseSpec(strings.TrimSpace(arg[:i]), "BAS")
	if err != nil {
		return err
	}
	src, err := s.parseSpec(strings.TrimSpace(arg[i+1:]), "BAS")
	if err != nil {
		return err
	}
	return s.Disk.Rename(src, dst, acct.Proj, acct.Prog, s.priv())
}

func (s *Shell) pipConcat(dstArg, srcArg string, acct *Account) error {
	parts := strings.Split(srcArg, ",")
	var body strings.Builder
	var ext string
	for _, p := range parts {
		src, err := s.parseSpec(strings.TrimSpace(p), "")
		if err != nil {
			return err
		}
		if ext == "" {
			ext = src.Ext
		}
		text, err := s.Disk.ReadText(src, acct.Proj, acct.Prog, s.priv())
		if err != nil {
			return err
		}
		body.WriteString(text)
	}
	dst, err := s.parseSpec(dstArg, ext)
	if err != nil {
		return err
	}
	return s.Disk.WriteText(dst, acct.Proj, acct.Prog, s.priv(), body.String(), defaultProt)
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
	spec, err := s.parseSpec(name, "BAS")
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
	old, err := s.parseSpec(rest[:idx], "BAS")
	if err != nil {
		return err
	}
	new, err := s.parseSpec(rest[idx+4:], "BAS")
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

// cmdEdit runs the screen editor on a file, or on the program in memory
// when no file is named. Text is stored through the usual paths, so
// protection codes, pack state and program syntax are all still checked.
func (s *Shell) cmdEdit(rest string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	raw, ok := s.term.(rawTerm)
	if !ok {
		return fsErr("Not a terminal")
	}
	if !s.scope {
		return fsErr("Not a scope terminal")
	}
	if s.ttype != "" {
		raw = overrideTerm{rawTerm: raw, kind: s.ttype}
	}
	name := strings.TrimSpace(rest)

	var title, text string
	var save func(string) error

	if name == "" {
		if s.Basic.Compiled {
			return fsErr("Compiled file")
		}
		prog := s.Basic.ProgramName
		if prog == "" {
			prog = "NONAME"
		}
		title = prog + " (memory)"
		text = s.Basic.SourceText()
		save = func(body string) error { return s.editStoreProgram(prog, body) }
	} else {
		spec, err := s.parseSpec(name, "BAS")
		if err != nil {
			return err
		}
		prot := defaultProt
		if s.Disk.Exists(spec, acct.Proj, acct.Prog, s.priv()) {
			text, err = s.Disk.ReadText(spec, acct.Proj, acct.Prog, s.priv())
			if err != nil {
				return err
			}
			if strings.HasPrefix(text, bacMagic) {
				return fsErr("Compiled file")
			}
			prot = s.fileProt(spec)
		}
		title = spec.DevName() + spec.Filename()
		save = func(body string) error {
			return s.Disk.WriteText(spec, acct.Proj, acct.Prog, s.priv(), body, prot)
		}
	}

	was := s.inProgram
	s.inProgram = false
	if s.sys != nil {
		s.sys.SetJob(s.Job, s.Account.Display(), "VTEDIT")
	}
	_, err = newEditor(raw, title, text, save).Run()
	s.inProgram = was
	s.syncJob()
	if err != nil {
		if errors.Is(err, ErrInterrupt) {
			return nil
		}
		return err
	}
	return nil
}

// editStoreProgram replaces the program in memory, refusing the whole
// edit if a line will not parse so a typo cannot silently drop lines.
func (s *Shell) editStoreProgram(name, body string) error {
	scratch := NewMachine(IO{})
	if err := scratch.LoadSource(body, name); err != nil {
		return err
	}
	return s.Basic.LoadSource(body, name)
}

// cmdRenum implements RENUM [start][,increment], defaulting to 10,10.
func (s *Shell) cmdRenum(rest string) error {
	start, step := 10, 10
	fields := strings.FieldsFunc(rest, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(fields) > 2 {
		fmt.Fprintln(s.out, "?RENUM [start][,increment]")
		return nil
	}
	// A leading comma means "keep the default start", as in RENUM ,20.
	if strings.HasPrefix(strings.TrimSpace(rest), ",") && len(fields) == 1 {
		fields = []string{strconv.Itoa(start), fields[0]}
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return fsErr("Illegal line number")
		}
		if i == 0 {
			start = n
		} else {
			step = n
		}
	}
	if s.Basic.Compiled {
		return fsErr("Compiled file")
	}
	if len(s.Basic.Program) == 0 {
		return nil
	}
	missing, err := s.Basic.Renumber(start, step)
	if err != nil {
		return err
	}
	for _, n := range missing {
		fmt.Fprintf(s.out, "?Undefined line number %d\n", n)
	}
	s.syncJob()
	return nil
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
	spec, err := s.parseSpec(name, "BAS")
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
	spec, err := s.parseSpec(name, "BAS")
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
	spec, err := s.parseSpec(name, "BAC")
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
	if s.Basic.PrivImage {
		s.tempPriv = true
		s.syncPrivilege()
	}
	s.inProgram = true
	s.syncJob()
	err := s.Basic.RunProgram()
	s.finishRun(err)
}

func (s *Shell) cmdCont() {
	if s.Basic == nil || !s.Basic.Stopped || s.Basic.paused == nil {
		fmt.Fprintln(s.out, "?Can't continue")
		return
	}
	if s.Basic.PrivImage {
		s.tempPriv = true
		s.syncPrivilege()
	}
	s.inProgram = true
	s.syncJob()
	err := s.Basic.Continue()
	s.finishRun(err)
}

func (s *Shell) finishRun(err error) {
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
	if stopped {
		fmt.Fprintf(s.out, "Stop at line %d\n", line)
	} else if s.Basic.PrivImage {
		s.Basic.ClearProgram("NONAME")
	}
	s.syncJob()
}

func (s *Shell) loadForRun(name string) error {
	acct, err := s.needLogin()
	if err != nil {
		return err
	}
	spec, err := s.parseSpec(name, "")
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
		s.Basic.NoteEdit()
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
	case "QUOTA":
		return s.cmdSetQuota(arg, true, false)
	case "JOBQUOTA", "JOB":
		return s.cmdSetQuota(arg, false, true)
	default:
		fmt.Fprintln(s.out, "?REACT CREATE, DELETE, PASSWORD, LIST, QUOTA, or JOBQUOTA")
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
		size, used := s.Disk.PackUsage(p)
		free := size - used
		open := 0
		if p.Mounted && s.sys != nil {
			open = s.sys.openOnPack(p)
		}
		fmt.Fprintf(s.out, "%-6s  %-6s %4d  %6d  %6d  %3d  %-6s  %s\n",
			p.Designator(), p.Media, open, size, free, packCluster(p.Media), name, state)
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

// Main is the rsts command: flags, config.toml, then the console and
// optional Telnet/serial lines. Returns a process exit status.
func Main(args []string) int {
	envDisk := os.Getenv("RSTS_DISK")
	diskFlag := ""
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
				diskFlag = args[i]
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
			fmt.Print(SystemLong + `  PDP-11/70  BASIC-PLUS

Usage: rsts [options]

  --disk DIR      virtual disk directory (default: ./disk)
  --config FILE   config.toml (default: ./config.toml)
  --port N        override telnet_port
  --guest         log in the console as GUEST
  --login NAME    prompt for that account's password
  --no-console    Telnet only
  --no-telnet     local console only
  --version       print version

config.toml keys (defaults and unused stay commented in the file):
  max_users (25)  telnet_port (23)  telnet_bind  telnet  console
  serial  disk  guest  login
`)
			return 0
		}
	}
	cfg, cfgPath, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	disk := diskFlag
	if disk == "" {
		disk = envDisk
	}
	if disk == "" {
		disk = cfg.Disk
	}
	if disk == "" {
		wd, _ := os.Getwd()
		disk = filepath.Join(wd, "disk")
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
	if guest {
		cfg.Guest = true
	}
	if login == "" {
		login = cfg.Login
	}
	guest = cfg.Guest
	if !cfg.Console && !cfg.Telnet && len(cfg.Serial) == 0 {
		fmt.Fprintln(os.Stderr, "Nothing to run: console, telnet and serial are all off")
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
	serialOK := false
	if len(cfg.Serial) > 0 {
		up, errs := sys.StartSerial()
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "Serial %v\n", e)
		}
		if len(up) > 0 {
			serialOK = true
			fmt.Fprintf(os.Stderr, "Serial %s  %s\n", strings.Join(up, " "), serialSpeed)
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
		if sys.Halted() || !(telnetOK || serialOK) {
			sys.Close()
			return 0
		}
		lines := "Telnet"
		if !telnetOK {
			lines = "Serial"
		} else if serialOK {
			lines = "Telnet and serial"
		}
		fmt.Fprintf(os.Stderr, "Console detached. %s still up. Ctrl-C to stop.\n", lines)
	}
	if !telnetOK && !serialOK {
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
