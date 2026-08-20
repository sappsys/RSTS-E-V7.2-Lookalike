package rsts

import (
	"fmt"
	"strings"
)

// Pascal is ISO 7185:1990 / ANSI/IEEE 770X3.97-1983 (Level 1: conformant
// arrays). It is parsed, type-checked and compiled to private .PAC
// bytecode here; it is not a PDP-11 .TSK. Packed is a representation
// hint except packed array of char, which is a string type. Extensions:
// OTHERWISE and 1..n case constants (ISO 10206), and shorter string
// constants are space-padded on assignment. UCSD USES/UNIT and packed
// bit-fields are not implemented.

const (
	pascalMaxInt  int64 = 2147483647
	pascalSetSpan       = 256
)

type PascalError struct {
	Msg  string
	Line int
	Col  int
}

func (e *PascalError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("?Pascal: %s at line %d", e.Msg, e.Line)
	}
	return "?Pascal: " + e.Msg
}

func pasErr(msg string, line, col int) error {
	return &PascalError{Msg: msg, Line: line, Col: col}
}

// CompilePascal type-checks a program. RunPascal compiles and executes it.
func CompilePascal(src string) (*pProgram, error) {
	prog, err := parsePascal(src)
	if err != nil {
		return nil, err
	}
	if err := checkPascal(prog); err != nil {
		return nil, err
	}
	return prog, nil
}

func RunPascal(src string, host PascalHost) error {
	prog, err := CompilePascal(src)
	if err != nil {
		return err
	}
	img, err := compilePascalImage(prog)
	if err != nil {
		return err
	}
	return runPascalImage(img, host)
}

func CompilePascalPAC(src string) (string, error) {
	prog, err := CompilePascal(src)
	if err != nil {
		return "", err
	}
	img, err := compilePascalImage(prog)
	if err != nil {
		return "", err
	}
	return wrapPAC(img), nil
}

type PascalHost struct {
	Write     func(s string)
	ReadLine  func() (string, error)
	OpenRead  func(name string) (string, error)
	OpenWrite func(name, body string) error
	PollStop  func() bool
}

func pascalHostFromIO(io IO) PascalHost {
	h := PascalHost{}
	if io.Write != nil {
		h.Write = func(s string) { io.Write(s, false) }
	}
	if io.Read != nil {
		h.ReadLine = func() (string, error) { return io.Read("") }
	}
	h.PollStop = io.PollInterrupt
	return h
}

func splitPascalLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}
