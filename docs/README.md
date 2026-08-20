# RSTS/E documentation

## AA-2762F-TC RSTS/E V9.0 System Manager's Guide (June 1985)

Stored as `AA-2762F-TC_RSTS_E_V9.0_System_Managers_Guide_Jun85.pdf`.

Source: [bitsavers](https://bitsavers.trailing-edge.com/pdf/dec/pdp11/rsts_e/V09/2_System_Mangement/AA-2762F-TC_RSTS_E_V9.0_System_Managers_Guide_Jun85.pdf)

This system is **RSTS/E V7.2**. The V9.0 manager's guide is kept because it explains disk packs, public vs private structure, MOUNT/DISMOUNT, and device names in more detail than the V7.2 set. DCL verbs in that book (`SHOW DISK`, `INITIALIZE`) map to the V7.2 keyboard/UMOUNT forms used here:

| V9 DCL | V7.2 (this system) |
|--------|---------------------|
| `MOUNT device: packid` | `MOUNT device: packid` |
| `DISMOUNT device:` | `DISMOUNT device:` |
| `INITIALIZE device: packid` | `DSKINT device: packid` |
| `SHOW DISKS` | `SYSTAT/D` |

See `HELP DISKS`.

## AA-JP30B-TK BASIC-PLUS-2 Reference Manual (May 1991)

Stored as `AA-JP30B-TK_BASIC-PLUS-2_Reference_Manual_May91.pdf`.

Source: [dmv.net](https://www.dmv.net/dec/pdf/bp2v27rm.pdf)

This is **BASIC-PLUS-2 V2.7** for RSTS/E V9.7 or higher (also RSX-11M). This system is **BASIC-PLUS** on **RSTS/E V7.2**. The manual is a good statement/function reference where the languages overlap (PRINT, FOR, OPEN, SYS, and so on). Do not take BP2-only features as V7.2:

| In the BP2 manual | V7.2 BASIC-PLUS (this system) |
|-------------------|--------------------------------|
| Labels, compiler directives (`%IF`, `%INCLUDE`) | No |
| Explicit `DECLARE` / named constants | No; types are `$` / `%` suffixes |
| Environment `COMPILE` → object / `BUILD` | `COMPILE` writes a `.BAC` or `.PAC` bytecode image |
| Multi-statement `&` continuation | `\` on one line; no `&` |
| BP2 compiler switches, RMS libraries | Ignore |

`HELP LANG` and `HELP FN` describe what this interpreter actually runs.
