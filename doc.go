// Package rsts is a RSTS/E V7.2 lookalike: BASIC-PLUS, Pascal, a Bye/Ready CLI,
// PPN disk storage, and jobs on a console, Telnet, or serial line.
//
// It is a timesharing user environment, not a PDP-11 CPU emulator and not
// BASIC-PLUS-2. BASIC programs compile to a private bytecode (not Digital .BAC
// P-code). Pascal is ISO 7185 / ANSI X3.97 (Level 1 conformant arrays),
// compiled and run on a private bytecode VM here rather than as a PDP-11 .TSK. The portrayed
// system stays V7.2; the dash level in Version is this project's own release
// number.
package rsts
