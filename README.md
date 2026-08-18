# RSTS/E V7.2-10

Go recreation of **RSTS/E V7.2-10** on a **PDP-11/70**: a `Bye` / `Ready` timesharing CLI, PPN file storage, disk packs, jobs, Telnet, and a **BASIC-PLUS** compiler/VM.

This is **not** a PDP-11 CPU emulator and **not** RSTS/E V9/V10 (no DCL as the default CLI). It is a user environment that talks and behaves like V7.2.

Identity strings are fixed: `RSTS V7.2-10`. `SYS(CHR$(1))` returns that name.

## Run

```bash
go run ./cmd/rsts
```

Or:

```bash
./build.sh          # writes bin/rsts
./bin/rsts
```

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
| `--no-console` | Telnet only |
| `--no-telnet` | Local console only |
| `--version` | Print `RSTS V7.2-10  (PDP-11/70)` |

`config.toml`:

```toml
max_users   = 25          # 1..63 jobs
telnet_port = 23          # use 2323 if not root
telnet_bind = "0.0.0.0"
telnet      = true
console     = true
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

RSTS V7.2-10  Job 1  KB0  17-Aug-26  7:23 PM
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

## Disk packs

V7.2 device names. A pack sits on a physical unit and must be mounted before you store files on it.

| Unit | Media | Pack |
|------|-------|------|
| `SY:` / `SY0:` / `DB0:` | RP06 | `SYSDSK` — public, always mounted |
| `DB1:` | RP06 | Sample `PAYROL` (initialized, left unmounted) |
| `DL0:` `DL1:` | RL02 | |
| `DM0:` | RK07 | |

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
| `[100,100]` | `HELLO`, `GUESS`, `FIB`, `STARS`, `TABLE`, `NOTE`, `WHILE`, `UNTIL`, `CHANGE`, `USING`, `ERRDEMO`, `MODS`, `CPU`, `PK`, `COMP`, `README.TXT` |
| `[200,200]` | `HELLO`, `SIEVE` |

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
| Telnet / VT52 | `telnet.go` |
| Pseudo keyboards | `pk.go` |
| Config | `config.go`, `config.toml` |
| Seeded programs | `samples.go` |
| Entry | `cmd/rsts/main.go` |

`disk/` is runtime and is not in git. `docs/` points at the V9 manager’s guide and BASIC-PLUS-2 manual (language overlap only). Those PDFs are not shipped in this repository; see `docs/README.md` for sources.

## License

[MIT](LICENSE). Fork, modify, and redistribute freely.

