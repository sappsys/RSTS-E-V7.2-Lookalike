# RSTS/E V7.2

Go recreation of **RSTS/E V7.2** on a **PDP-11/70**: a `Bye` / `Ready` timesharing CLI, PPN file storage, disk packs, jobs, Telnet, and a **BASIC-PLUS** compiler/VM.

This is **not** a PDP-11 CPU emulator and **not** RSTS/E V9/V10 (no DCL as the default CLI). It is a user environment that talks and behaves like V7.2.

## Version

The system portrayed is **RSTS/E V7.2** on a PDP-11/70, and that does not change. DEC numbered the update level after the dash — `V7.2-10` was a real one — and this project uses that number for its own releases. `V7.2-10` is the first release, `V7.2-11` the next, and the emulator reports whichever it was built from: `SYS(CHR$(1))`, the login banner, and `--version` all agree.

Releases are on [GitHub](https://github.com/sappsys/RSTS-E-V7.2-Lookalike/releases); changes are in [CHANGELOG.txt](CHANGELOG.txt).

## Run

```bash
go run ./cmd/rsts
```

Or:

```bash
./build.sh          # writes bin/rsts
./bin/rsts
```

## Build

`build.sh` cross-compiles to any platform and architecture the Go toolchain knows. Binaries are built with CGO disabled and stripped, so each one is self-contained and needs no shared libraries at run time. (macOS is the exception the toolchain imposes: darwin binaries always link `libSystem.dylib`, which ships with every Mac.)

```bash
./build.sh                       # this machine
./build.sh --mac                 # Intel and Apple silicon
./build.sh --mac --amd64         # Intel Macs only
./build.sh --mac --arm64         # Apple silicon only
./build.sh --windows --linux     # both, common architectures
./build.sh linux/riscv64         # any GOOS/GOARCH pair
./build.sh --all                 # the common targets
./build.sh --everything          # every target Go supports
```

The default platform is the machine you are on.

| Platform | | Architecture | |
|----------|-|--------------|-|
| `--linux` | | `--amd64`, `--x64`, `--x86-64` | 64-bit Intel/AMD, **Intel Mac** |
| `--windows`, `--win` | | `--arm64`, `--aarch64` | 64-bit ARM, **Apple silicon** |
| `--mac`, `--macos`, `--darwin` | | `--x86`, `--386` | 32-bit Intel |
| `--freebsd`, `--openbsd`, `--netbsd` | | `--arm` | 32-bit ARM (ARMv7) |
| `--dragonfly`, `--solaris`, `--illumos` | | `--riscv64`, `--ppc64le`, `--ppc64` | |
| `--aix`, `--plan9`, `--android`, `--ios` | | `--s390x`, `--loong64`, `--wasm` | |
| `--js`, `--wasip1` | | `--mips`, `--mipsle`, `--mips64`, `--mips64le` | |
| `--os NAME` | any `GOOS` | `--arch NAME` | any `GOARCH` |

Anything without a flag of its own can be named directly as a `GOOS/GOARCH` pair, so every one of the 47 targets `go tool dist list` reports is reachable:

```bash
./build.sh linux/riscv64 openbsd/arm64 solaris/amd64 plan9/386
```

Platform and architecture flags accumulate into a matrix, so `./build.sh --linux --windows --amd64 --arm64` writes four binaries. With no platform at all you get this machine. With a platform but no architecture you get this machine's architecture when building for this machine, and the platform's usual architectures when cross compiling — which is why `--mac` alone gives you **both** an Intel and an Apple silicon binary.

`--everything` builds all 47 targets. The five that need cgo (Android on 386/amd64/arm, iOS) are reported and skipped rather than stopping the run; the other 42, `js/wasm` and `aix/ppc64` included, build clean.

Other flags: `--out DIR` (default `bin`), `--list` to see every supported pair, `--help`.

Binaries are named `rsts-PLATFORM-ARCHITECTURE`, with `.exe` on Windows and `.wasm` for the WebAssembly targets. A build for this machine is also copied to `bin/rsts`.

For a plain untuned build:

```bash
go build -o bin/rsts ./cmd/rsts
```

Options:

| Flag | Meaning |
|------|---------|
| `--disk DIR` | Virtual disk root (default `./disk`, or `$RSTS_DISK`) |
| `--config FILE` | `config.toml` path (default `./config.toml`; created if missing) |
| `--port N` | Telnet port (overrides config) |
| `--guest` | Log in as GUEST automatically |
| `--login NAME` | Prompt for that account's password |
| `--no-console` | Telnet and serial only |
| `--no-telnet` | Console and serial only |
| `--version` | Print the release and CPU, e.g. `RSTS V7.2-11  (PDP-11/70)` |

`config.toml`:

```toml
max_users   = 25          # 1..63 jobs
telnet_port = 23          # use 2323 if not root
telnet_bind = "0.0.0.0"
telnet      = true
console     = true
serial      = ""          # "/dev/ttyUSB0,/dev/ttyS0"
```

## Serial lines

`serial` takes a comma-separated list of devices. Each one is answered at **9600 8N1** and behaves exactly like a Telnet line: its own job, its own `KB:`, and the line offered again when the user logs off, the way a getty would.

```toml
serial = "/dev/ttyUSB0,/dev/ttyS0"
```

```text
$ rsts
Serial /dev/ttyUSB0 /dev/ttyS0  9600 8N1
```

That is enough to hang real terminals, or a USB-serial adapter, off the emulator for multi-user access with no network involved. Both lines above appear in `SYSTAT` as ordinary jobs:

```text
Job    Who       Where  What      Size  State   Run-Time
  1  100,100   KB0:   Ready       2K  KB      0:00.00
  2  1,2       KB1:   Ready       2K  KB      0:00.00
```

The line is opened raw with no flow control and no modem-control lines (`CLOCAL`), so a three-wire cable works. Echo and line editing are done by the emulator, as they were by RSTS. Ctrl-C interrupts a running program on a serial line just as it does anywhere else. A line that fails to open is reported and the rest of the system still starts.

Serial needs termios, so it works on Linux, macOS and the BSDs. Elsewhere the binary still builds and runs, and a configured serial line reports that the platform cannot provide one. You can test without hardware by making a virtual pair:

```bash
socat -d PTY,raw,echo=0,link=/tmp/tty1 PTY,raw,echo=0,link=/tmp/tty2
# serial = "/tmp/tty1", then talk to /tmp/tty2
```

Tests:

```bash
go test ./...
```

## Session

Logged-out prompt is **Bye**. Logged-in prompt is **Ready**.

```text
Bye
HELLO
Account or Name: GUEST
Password:

RSTS V7.2-11  Job 1  KB0  17-Aug-26  7:23 PM
User:  100,100

Ready
```

| At Bye | Effect |
|--------|--------|
| `HELLO` | Log in (name or `100,100`, then password) |
| `HELP` | Help |
| `EXIT` / `QUIT` | Stop the emulator (console). On Telnet, hang up that line only |
| `BYE` | Hang up a Telnet line; on the console, stay at Bye |

| At Ready | Effect |
|----------|--------|
| `BYE` / `LOGOUT` | Log out, return to Bye |
| `EXIT` / `QUIT` | Same as `BYE` (log out). Then `EXIT` at Bye to stop the emulator |
| `BYE EXIT` | Log out and stop the console session |

**Ctrl-C** stops a running BASIC program and returns to Ready. It does not exit the emulator. **Ctrl-U** kills the current input line.

Unique command prefixes and attached switches work the V7 CCL way: `SYSTAT/D`, `DISMOU DB1:`, `HLP DISK`.

## Accounts

| Name | PPN | Password | Notes |
|------|-----|----------|--------|
| SYSTEM | `[1,2]` | SYSTEM | Privileged (`[1,*]` is V7 JFSYS) |
| GUEST | `[100,100]` | GUEST | Sample programs |
| DEMO | `[200,200]` | DEMO | Sieve demo |

These three are seeded the first time the emulator runs. See [First run](#first-run).

Privilege is V7-style: project `[1,*]` is privileged. A compiled `.BAC` with protection bits 64+128 (typical public privileged `<232>`) grants **temporary privilege** for that `RUN` only; the image is dropped on exit. `.BAS` source never confers privilege.

Privileged account commands (REACT-style):

```text
CREATE [p,pn] NAME n PASSWORD pw
CREATE/ACCOUNT [p,pn] n pw
DELETE/ACCOUNT [p,pn]
REMOVE [p,pn]
PASSWORD                  change your own
PASSWORD [p,pn] [new]     set another account
SHOW ACCOUNTS
REACT CREATE / DELETE / PASSWORD / LIST
```

`[1,2]` cannot be deleted. An account that is logged in cannot be deleted. Creating `[1,*]` requires privilege.

## First run

The emulator needs nothing set up in advance. Run the binary in any directory and it builds what it needs:

```text
disk/
  accounts.json      SYSTEM [1,2], GUEST [100,100], DEMO [200,200]
  packs.json         SY0:/DB0: SYSDSK, DB1:, DL0:, DL1:, DM0:
  SY/1,2/            NOTICE.TXT, LOGIN.TXT, WHOAMI.BAS/.BAC, DATA.BAS, COMP.BAS
  SY/100,100/        HELLO, GUESS, FIB, STARS, ... (sample programs)
  SY/200,200/        HELLO, SIEVE
  DB1/               the sample PAYROL pack, initialized but unmounted
config.toml          created with defaults
```

The disk root is `./disk`, or `$RSTS_DISK`, or `--disk DIR`; missing parent directories are created. Everything is rechecked at each start, so deleting an account directory or a sample program restores it on the next run.

A damaged `accounts.json` or `packs.json` does not stop the system. The unreadable file is renamed to `NAME.json.bad` and rebuilt from defaults, with a note on stderr. `[1,2]` is structural — `DELETE/ACCOUNT` refuses to remove it, and it is restored if the file is edited to drop it, since otherwise nothing could administer the system. Deleting `GUEST` or `DEMO` is respected and they stay gone.

A `config.toml` with a bad setting *is* reported as an error rather than overwritten, so a typo cannot silently reset the system's configuration.

## Help

Type `HELP` or `HELP topic`. Abbreviations and CUSP names work (`HELP DISK` = `HELP DISKS`, `HELP MOUNT`, `HELP DIRECTORY`, `HELP PIP`, `HELP SYSTAT`).

| Topic | Contents |
|-------|----------|
| `LOGIN` | HELLO, BYE, EXIT, Ctrl-C, NOTICE / LOGIN.BAS |
| `FILES` | DIR, TYPE, COPY, PIP, KILL, NAME, ASSIGN, filespecs |
| `BASIC` | NEW, OLD, SAVE, COMPILE, LIST, RUN, CONT |
| `LANG` | BASIC-PLUS statements and modifiers |
| `FN` | Built-in functions and SYS |
| `COMMANDS` | Keyboard command list |
| `SET` | WIDTH / ECHO (TTYSET) |
| `SYSTAT` | Job/disk/memory switches |
| `SHOW` | SHOW aliases for SYSTAT |
| `DISKS` | MOUNT, DISMOUNT, DSKINT, packs |
| `ACCOUNTS` | Logins and REACT |
| `COMPILE` | `.BAC` bytecode and the privilege bit |
| `HARDWARE` | PDP-11/70 configuration and PEEK |
| `TELNET` | Multi-user Telnet / VT52 |
| `JOBS` | SYSTAT, ATTACH, PK: |
| `HELP` | How to use HELP |

## Keyboard commands

### Files

| Command | |
|---------|-|
| `DIR` / `CAT` / `CATALOG` `[filespec]` | Catalog |
| `TYPE filespec` | Print a file |
| `COPY src dst` | Copy |
| `PIP dst=src` | Copy (PIP syntax); `PIP dest<prot>=src` sets protection |
| `KILL` / `UNSAVE` filespec | Delete |
| `NAME old AS new` | Rename and/or set `<prot>` |
| `ASSIGN device: logical` | Job logical name (`ASSIGN DB1: WORK`) |
| `DEASSIGN [logical]` | Drop one name, or all |

### BASIC environment

| Command | |
|---------|-|
| `NEW [name]` | Clear memory |
| `OLD name` | Load `.BAS` (or `.BAC` if readable) |
| `SAVE` / `REPLACE` `[name]` | Write `.BAS` |
| `COMPILE [name][<prot>]` | Compile to `.BAC` (default `<124>`) |
| `LIST` / `LISTNH` `[n[-m]]` | List source |
| `RUN` / `RUNNH` `[name]` | Run (tries `.BAC` then `.BAS`) |
| `CONT` | Continue after `STOP` (fails if the program was edited) |
| `EDIT` / `VTEDIT` `[filespec]` | Screen editor: the program in memory, or a file |
| `RENUM` / `RENUMBER` `[start][,inc]` | Resequence lines, default `10,10` |
| `DELETE n[-m]` | Delete program lines |
| `CLEAR` | Reset variables |
| `SET WIDTH n` / `SET ECHO` / `SET NOECHO` | Terminal (also `TTYSET`) |

Numbered lines are stored in the program in memory:

```text
NEW DEMO
10 FOR I = 1 TO 5
20 PRINT I, I*I
30 NEXT I
40 END
RUN
SAVE
```

Immediate mode (no line number) is accepted at Ready: `PRINT 1+2`.

### Editing

RSTS itself had **TECO**, and sites layered the **VTEDIT** macro package on it to get full-screen editing on a VT52. TECO is not installed here; `EDIT` is a screen editor in that spirit.

```text
EDIT                edit the BASIC program in memory
EDIT NOTES.TXT      edit a file, created on write if new
VTEDIT              the same command
```

| Key | |
|-----|-|
| arrows | Move |
| `^A` / `^E` | Start / end of line |
| RETURN | Split the line |
| DEL, `^H` | Rub out the character before the cursor |
| `^D` | Delete the character under the cursor |
| `^K` | Delete the line |
| `^W` | Write |
| `^X` | Write and exit |
| `^C` | Exit (twice if there are unsaved changes) |
| `^L` | Redraw |

It draws VT52 sequences to a terminal that says it is a VT52 and ANSI to everything else, so it works both in a period-correct setup and in a modern telnet client. It is pure Go with no dependencies, and it cannot reach outside the emulator: there is no shell escape and no way to name a host path, because saving goes through the same file system calls as `SAVE` and `PIP`. Protection codes are enforced and preserved, and a compiled `.BAC` is refused.

`EDIT` with no file name edits the program in memory, line numbers included. On write every line is parsed first, and if any line will not compile then nothing is stored and the reason appears on the status line — a typo cannot quietly discard the edit.

`RENUM [start][,increment]` resequences the program, default `10,10`, and follows every reference with it: `GOTO`, `GOSUB`, `ON … GOTO`, `ON … GOSUB`, `THEN`, `ELSE`, `RESUME`, and `ON ERROR GOTO`.

```text
1 PRINT "START"          10 PRINT "START"
2 GOSUB 7                20 GOSUB 60
3 IF X=0 THEN 5 ELSE 6   30 IF X=0 THEN 40 ELSE 50
5 PRINT "ZERO"     -->   40 PRINT "ZERO"
6 GOTO 9                 50 GOTO 70
7 PRINT "SUB" \ RETURN   60 PRINT "SUB" \ RETURN
9 END                    70 END
```

The `0` in `ON ERROR GOTO 0` and `RESUME 0` is not a line number and is left alone, and so is the line number in `CHAIN "NEXT" LINE 100`, which belongs to the other program. A reference to a line that does not exist is left as it was and reported as `?Undefined line number n`, rather than being silently repointed at whatever now occupies that number. The last line may not pass 32767, and `CONT` will not resume a program once it has been renumbered.

### Status and devices

| Command | |
|---------|-|
| `SYSTAT` / `SYS` `[job] [/switches]` | System status |
| `WHO` | Logged-in jobs (`SYSTAT/U`) |
| `SHOW …` | Aliases (V7 had no DCL SHOW; accepted here) |
| `CPU` / `HARDWARE` | PDP-11/70 configuration |
| `DATE` / `TIME` / `DAYTIME` | Clock |
| `MOUNT device: packid [/PRIVATE] [/PUBLIC] [/RONLY]` | Mount a pack |
| `DISMOUNT device: [packid]` | Dismount |
| `DSKINT` / `INITIALIZE device: packid [/PUBLIC]` | (priv) Initialize a pack |
| `UMOUNT` | Reminder of MOUNT / DISMOUNT |

### Jobs

| Command | |
|---------|-|
| `DETACH` | Detach this job from the keyboard |
| `ATTACH n` | Attach to a detached job you own |
| `FORCE kb: command` | (priv) Inject a line at another job |
| `HANGUP n` | Hang up a job (priv, or your PK: child) |
| `BROADCAST ALL text` | (priv) Message every keyboard |
| `SEND` / `TALK kb: text` | Message one job |

## SYSTAT and SHOW

Attached switches: `SYSTAT/D` is the same as `SYSTAT /D`.

| Switch | Display |
|--------|---------|
| (default), `/J` | Jobs |
| `/F` | Full jobs + RTS column |
| `/N` | No header |
| `/U` `/W` | Logged-in jobs only |
| `/D` | Disk packs |
| `/K` `/T` | Keyboards |
| `/M` | Memory |
| `/R` | Run-time systems |
| `/S` | Statistics |
| `/B` | Busy devices |
| `/H` | Hardware |
| `/A` | All of the above |
| `n` | One job |

SHOW aliases: `JOBS`, `USERS`, `DISKS`, `MEMORY`, `TERMINALS`, `RTS`, `STATUS`, `BUSY`, `CPU`, `ACCOUNT`, `ACCOUNTS`, `DATE`, `TIME`.

### What the numbers mean

They are measured from the running system, in the units V7.2 used, rather than being fixed for show.

**Job size** is the job's own storage in K-words (a word is two bytes): tokenised program text, scalars at one word for `%` and two for floating, arrays at their real element count, the string pool at its actual bytes, and one 512-byte buffer per open channel — a `RECORDSIZE` or `MAP` buffer at its declared size. A virtual array is charged to its file, not to the job, because that is where it lives. So `DIM A(20000)` moves a job from 2K to 40K, and `DIM #1, A(20000)` leaves it at 2K.

**Run-Time** is processor time the job has actually spent executing BASIC. Sitting at `Ready`, waiting at `INPUT`, and `SLEEP` are wait states on a timesharing system and are not charged, so an idle job stays at `0:00.00` no matter how long it is logged in. `TIME(1)` returns the same figure.

**Memory** (`SYSTAT/M`) balances: Monitor and the BASIC-PLUS RTS are resident, User is the sum of the live job sizes, and Free is the rest of the 1920K. The RTS is reentrant, so its 16K is counted **once** however many jobs are running BASIC — each job pays only for its own data. That is how RSTS worked, and it is why ten users cost far less than ten times one user.

**Disk** (`SYSTAT/D`) Size and Free are real: every file on the pack counted as the whole clusters it occupies (4 on an RP06, 2 on an RK07), plus the MFD and one UFD block per account. `Open` is the number of files open on that pack at that moment.

The one number that cannot be measured is total memory itself: there is no PDP-11 address space to inspect, so 1920 K-words usable of a 2048 K-word 22-bit space is the configuration of the machine being portrayed, in `hardware.go`. Monitor at 96K and the RTS at 16K are likewise the sizes those components had.

## Disk packs

### Device names

A RSTS/E device name is **two letters for the controller, a unit number, and a colon**: `DB0:`, `DL1:`, `DM0:`. The two letters say what kind of drive it is, not which one — the unit number picks the drive. The letters are inherited from the PDP-11 device handlers, so they describe the hardware rather than the size:

| Device | Drives | Bus | Capacity | Notes |
|--------|--------|-----|----------|-------|
| `DB:` | RP04, RP05, **RP06** | MASSBUS (RH70) | 340,670 blocks (174 MB) on an RP06 | The big removable washing-machine packs. The system disk on this 11/70 |
| `DL:` | RL01, **RL02** | UNIBUS (RL11) | 20,480 blocks (10.4 MB) on an RL02 | Small removable cartridges, quick to swap |
| `DM:` | RK06, **RK07** | UNIBUS (RK611) | 53,790 blocks (27.5 MB) on an RK07 | Cartridge drives, between an RL and an RP |
| `DK:` | RK05 | UNIBUS (RK11) | 4,800 blocks (2.5 MB) | The old 2.5 MB cartridge |
| `DP:` | RP02, RP03 | UNIBUS (RP11) | 80,000 blocks on an RP03 | The pre-MASSBUS pack drive |
| `DR:` | RM02, RM03, RM05, RM80 | MASSBUS | 131,680 blocks on an RM03 | Later MASSBUS drives |
| `DS:` | RS03, RS04 | MASSBUS | 2,048 blocks on an RS04 | Fixed-head, tiny and fast; swap space |
| `DU:` | RA60, RA80, RA81 | MSCP (UDA50) | 237,212 blocks on an RA80 | Late additions, more common on later RSTS |

`SY:` is not a drive. It is the **public structure** — the system disk — and resolves to whichever pack holds the system. `DSK:` and `LB:` are accepted as the same thing. On this machine `SY:`, `SY0:`, and `DB0:` are all the same RP06.

The bold media in each row is what a unit of that type is created with here. A pack's real capacity and cluster size follow the drive, so `SYSTAT/D` shows an RL02 as 20,480 blocks and an RK07 as 53,790, and a file on an RP06 occupies whole 4-block clusters while one on an RL02 occupies single blocks.

### Configured on this system

A pack sits on a unit and must be mounted before you store files on it.

| Unit | Media | Pack |
|------|-------|------|
| `SY:` / `SY0:` / `DB0:` | RP06 | `SYSDSK` — public, always mounted |
| `DB1:` | RP06 | Sample `PAYROL` (initialized, left unmounted) |
| `DL0:` `DL1:` | RL02 | Empty, uninitialized |
| `DM0:` | RK07 | Empty, uninitialized |

An unused unit has no pack on it until you `DSKINT` one:

```text
DSKINT DL0: WORK
MOUNT DL0: WORK
SAVE DL0:FOO
```

Pack IDs are 1–6 letters or digits, so `WORK` is fine and `SCRATCH` is one character too long.

```text
MOUNT DB1: PAYROL
DIR DB1:
SAVE DB1:FOO
DISMOUNT DB1:
SYSTAT/D
```

`/PUBLIC` requires privilege. Ordinary users mount private packs. `SY0:` / `DB0:` cannot be dismounted. Pack IDs are 1–6 letters or digits; once mounted, `PAYROL:` is a logical name for that unit.

Host layout: `disk/SY/<proj>,<prog>/` for the system pack, plus `disk/DB1`, `disk/DL0`, … Pack state is `disk/packs.json`. The `disk/` tree is runtime (not source).

## Filespecs and protection

```text
NAME.EXT
[p,pn]NAME.EXT
SY:[p,pn]NAME.EXT
$NAME                 → [1,2]
DB1:NAME.EXT
PAYROL:NAME.EXT
NAME.EXT<prot>
```

OLD/SAVE default extension `.BAS`. COMPILE default `.BAC`.

Protection (V7.2): default **60**. Bit **64** = compiled/executable. Bit **128** = privileged (only with 64, and only `[1,*]` may set it).

| Code | Meaning |
|------|---------|
| `<60>` | Default source |
| `<124>` | Compiled, owner-only (60+64) |
| `<232>` | Privileged compiled, world-runnable |

Non-owners cannot `OLD`, `TYPE`, or `LIST` a compiled file. They may `RUN` a public `<232>` file.

## BASIC-PLUS

This is **BASIC-PLUS** (the V7 interpreter language), not BASIC-PLUS-2. `HELP LANG` and `HELP FN` are the dialect that actually runs.

Statements: `LET`, `PRINT`, `INPUT`, `LINE INPUT`, `PRINT USING`, `GOTO`, `GOSUB`, `RETURN`, `ON … GOTO/GOSUB`, `IF … THEN … ELSE`, `FOR/NEXT`, `WHILE/NEXT`, `UNTIL/NEXT`, `DIM`, `DIM #n` (virtual arrays), `DATA`, `READ`, `RESTORE`, `CHANGE`, `MAT` (including `ZER(n,m)` redim), `MAP`, `OPEN` / `PRINT#` / `INPUT#`, `GET` / `PUT`, `FIELD`, `LSET` / `RSET`, `CLOSE`, `RANDOMIZE`, `DEF FNx = …`, `ON ERROR GOTO`, `RESUME`, `CHAIN`, `SLEEP`, `NAME`, `KILL`, `END`, `STOP`, `REM` (or `!`).

Modifiers (rightmost is outermost): `IF`, `UNLESS`, `WHILE`, `UNTIL`, `FOR`. Several statements on one line are separated by `\`. Integer divide is also `\` inside an expression. Relational true is **-1**, false is **0**. Types: `$` string, `%` integer.

Functions: `ABS INT FIX SGN SQR SIN COS TAN ATN LOG EXP RND PI ERR ERL PEEK SWAP% TIME DATE LEN LEFT$ RIGHT$ MID$ INSTR CHR$ ASC STR$ VAL NUM1$ NUM$ SPACE$ STRING$ DATE$ TIME$ TAB SPC POS SYS CVT%$ CVT$% CVTF$ CVT$F CVT$$`.

`RIGHT$(s,n)` is from character *n* to the end (BASIC-PLUS, not last-*n*).

`SYS(CHR$(n)+…)`: 1=system name, 2=PPN, 3=job, 4=program, 5=date, 6=FIP (0/-21 binary PPN, 1=name, 2=job, 3=KB, 5=date, 9=ident, -3=UU.TB1, -12=UU.TB2, -10=UU.TRM), 7=time, 9=pack SY.

`CHAIN filespec [LINE n]` loads and runs another program. `SLEEP n` waits n seconds (Ctrl-C aborts). `CONT` at Ready resumes after `STOP` unless the program was edited.

Virtual arrays: `OPEN "FILE.DAT" AS FILE 1` then `DIM #1, A%(100)` or `DIM #1, A$(50)=20`. `%` elements are 2 bytes, floating 4, strings default 16. `MAT A = ZER(n,m)` redimensions.

`NAME "OLD" AS "NEW"` and `KILL "FILE"` are BASIC statements as well as keyboard commands.

`ASSIGN DB1: WORK` creates a job logical name; `DEASSIGN` removes it. Pack IDs still work when the pack is mounted.

After `HELLO`, `[1,2]NOTICE.TXT` is typed, then `LOGIN.BAS` or `START.BAS` in the account is `RUN` if present.

`OPEN "PK:" AS FILE n` assigns a pseudo keyboard and forks a job. `PRINT #n` sends keystrokes; `INPUT #n` / `LINE INPUT #n` reads output; `CLOSE #n` hangs up the child. Demo: `OLD PK` then `RUN` on GUEST.

## COMPILE and bytecode

On a real V7.2 system, `COMPILE` wrote BASIC-PLUS P-code into a `.BAC`. Here `COMPILE` emits a **private bytecode** image (not Digital’s P-code). `RUN`, `COMPILE`, and immediate mode all execute that VM. A real RSTS `.BAC` will not load; these files will not run on an 11/70.

`LIST` / `TYPE` cannot recover source from a `.BAC`. `COMPILE` leaves the source in memory; `OLD` of a `.BAC` loads bytecode only.

When extending BASIC, add an opcode in `pcode.go`, emission in `pcode_compile.go`, and a VM case in `pcode_vm.go`.

Privileged CUSP demo (from GUEST): `RUN $WHOAMI`.

## Jobs and Telnet

Each console or Telnet connection is a RSTS job on its own `KB:` line (`KB0:` is the local console). Up to 63 jobs (V7.2); `max_users` caps this emulator (default 25).

Telnet is a VT52-style NVT (ESC A/B/C/D/H/J/K/Y/Z). ANSI/VT100 cursor keys are accepted too.

```text
telnet host 23
```

`SYSTAT` shows jobs (Where, What, Size, State, Run-Time). States: `KB` wait, `RN` running, `Det` detached.

## Hardware (as V7.2 saw it)

PDP-11/70, 22-bit addressing, 1920 K-words usable, 2K-byte bipolar cache, FP11-C, KW11-L 60 Hz, MASSBUS RH70 + UNIBUS, console DL11.

CUSPs often `PEEK` monitor words: date at 512, minutes to midnight at 514, job number at 518, switch register `PEEK(-136%)` (177570 octal), KW11-L CSR `PEEK(-154%)`, PSW `PEEK(-2%)`.

`SHOW CPU` or `OLD CPU` then `RUN`.

## Sample programs

Seeded onto a new disk:

| Account | Files |
|---------|--------|
| `[1,2]` | `NOTICE.TXT`, `LOGIN.TXT`, `WHOAMI.BAS` / `WHOAMI.BAC<232>`, `DATA.BAS`, `COMP.BAS` |
| `[100,100]` | `HELLO`, `GUESS`, `FIB`, `STARS`, `TABLE`, `NOTE`, `WHILE`, `UNTIL`, `CHANGE`, `USING`, `ERRDEMO`, `MODS`, `CPU`, `PK`, `README.TXT` |
| `[200,200]` | `HELLO`, `SIEVE` |

`COMP.BAS` is the self-checking exerciser: it covers every statement, modifier, and function this system implements, prints `FAIL n` for any check that does not hold, and ends with `ALL PASSED: n`. It lives on `[1,2]` only, because its last test `CHAIN`s back to itself at line 8000 to print the totals and `KILL` its scratch files. Log in as SYSTEM and type `RUN COMP`. (`CONT` is a keyboard command, so it cannot be exercised from inside a program.)

## Design

Not a CPU emulator: the host is a timesharing **user environment**. BASIC is parsed to an AST, then compiled to private bytecode and interpreted.

| Area | Files |
|------|--------|
| CLI, HELP, login, COMPILE, DIR | `shell.go` |
| Jobs, SYSTAT, ATTACH, FORCE | `jobs.go` |
| Shared host, job table | `system.go` |
| Accounts | `accounts.go` |
| Filespecs, protection, `.BAC` wrap | `filesystem.go` |
| Packs, MOUNT/DSKINT | `packs.go` |
| BASIC lexer / parser / tree helpers | `token.go`, `parser.go`, `interp.go`, `plus.go`, `using.go`, `sys.go` |
| Bytecode ISA / compiler / VM | `pcode.go`, `pcode_compile.go`, `pcode_vm.go` |
| 11/70 constants, PEEK | `hardware.go` |
| Telnet / VT52, raw terminal | `telnet.go`, `rawterm.go` |
| Screen editor | `edit.go` |
| Job memory accounting | `memory.go` |
| RENUM | `renum.go` |
| Pseudo keyboards | `pk.go` |
| Config | `config.go`, `config.toml` |
| Seeded programs, seed manifest | `samples.go`, `seeds.go` |
| Entry | `cmd/rsts/main.go` |

Changes are recorded in [CHANGELOG.txt](CHANGELOG.txt).

`disk/` is runtime and is not in git. `docs/` points at the V9 manager’s guide and BASIC-PLUS-2 manual (language overlap only). Those PDFs are not shipped in this repository; see `docs/README.md` for sources.

## License

[MIT](LICENSE). Fork, modify, and redistribute freely.

