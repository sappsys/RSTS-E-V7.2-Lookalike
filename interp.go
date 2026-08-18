package rsts

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

type jumpKind int

const (
	jumpNone jumpKind = iota
	jumpGoto
	jumpGosub
	jumpReturn
	jumpEnd
	jumpStop
	jumpFor
	jumpNext
	jumpWhile
	jumpResume
)

type forFrame struct {
	kind      string // FOR or WHILE
	varName   string
	end       float64
	step      float64
	nextIndex int
	cond      expr
}

type jump struct {
	kind    jumpKind
	line    int
	frame   forFrame
	varName string
}

type chanFile struct {
	file       *os.File
	mode       string
	r          *bufio.Reader
	recSize    int
	recNo      int
	buf        []byte
	fields     []fieldSlot
	pk         *pkLink
	pkJob      int
	pkUnit     int
	orgVirtual bool
	mapName    string
	class      int
	dev        charDev
	onClose    func()
}

// charDev is a channel that is not a disk file: a terminal, the printer,
// the null device. RSTS put these behind the same OPEN as a file, so a
// program can PRINT #n to whichever it was given.
type charDev interface {
	devWrite(text string) error
	devReadLine() (string, error)
	devClose()
}

type fieldSlot struct {
	name   string
	start  int
	length int
}

type fnDef struct {
	params []string
	body   expr
}

type arrayInfo struct {
	dims     []int
	data     []value
	virtChan int
	virtBase int
	elemSize int
	strLen   int
	isStr    bool
	isInt    bool
}

type IO struct {
	Write         func(text string, newline bool)
	Read          func(prompt string) (string, error)
	Open          func(m *Machine, channel int, path, mode string) error
	Sys           func(arg string) (string, error)
	Load          func(name string) error
	Delete        func(path string) error
	Rename        func(old, new string) error
	PPN           string
	Job           int
	AccountName   string
	Privileged    bool
	ProgramName   string
	KB            string
	Width         int
	Echo          bool
	PollInterrupt func() bool
}

type Machine struct {
	IO          IO
	Program     map[int]string
	ProgramName string
	vars        map[string]value
	arrays      map[string]*arrayInfo
	functions   map[string]fnDef
	data        []value
	dataPtr     int
	gosub       []int
	forStack    []forFrame
	Files       map[int]*chanFile
	running     bool
	CurrentLine int
	col         int
	rng         *rand.Rand
	lastRnd     float64
	Stopped     bool
	Compiled    bool
	PrivImage   bool
	Image       *pcodeImage
	HasLine     bool
	onErrorLine int
	errNum      int
	errLine     int
	resumeLine  int
	inHandler   bool
	cpuStart    time.Time
	cpuNanos    atomic.Int64
	waitNanos   atomic.Int64
	intr        atomic.Bool
	maps        map[string]*mapArea
	currentMap  string
	paused      *pvm
	editSeq     int
	pauseSeq    int
	startLine   int
	chainTo     string
	chainLine   int
	virtNext    map[int]int
	// The variables a program reads after an operation: characters
	// transferred, the channel's state, the determinant left by MAT INV,
	// and the size MAT INPUT read.
	recount int
	status  int
	det     float64
	matNum  int
	matNum2 int
}

func NewMachine(io IO) *Machine {
	if io.Width <= 0 {
		io.Width = 80
	}
	if !io.Echo {
		io.Echo = true
	}
	m := &Machine{
		IO:          io,
		ProgramName: "NONAME",
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
		cpuStart:    time.Now(),
	}
	m.resetRuntime()
	m.Program = map[int]string{}
	return m
}

func (m *Machine) resetRuntime() {
	m.vars = map[string]value{}
	m.arrays = map[string]*arrayInfo{}
	m.functions = map[string]fnDef{}
	m.data = nil
	m.dataPtr = 0
	m.gosub = nil
	m.forStack = nil
	m.CloseAllFiles()
	m.col = 0
	m.Stopped = false
	m.HasLine = false
	m.CurrentLine = 0
	m.onErrorLine = 0
	m.errNum = 0
	m.errLine = 0
	m.resumeLine = 0
	m.inHandler = false
	m.maps = map[string]*mapArea{}
	m.currentMap = ""
	m.paused = nil
	m.virtNext = map[int]int{}
	m.recount = 0
	m.status = 0
	m.det = 0
	m.matNum = 0
	m.matNum2 = 0
}

// CPUTime is the time this job has spent executing BASIC, which is what
// the RSTS accounting meant by CPU time. Waiting at Ready, at INPUT, or in
// SLEEP is not charged: on a timesharing system those are wait states and
// the processor is someone else's.
func (m *Machine) CPUTime() time.Duration {
	if m == nil {
		return 0
	}
	return time.Duration(m.cpuNanos.Load())
}

func (m *Machine) startCPU() (time.Time, int64) {
	if m == nil {
		return time.Now(), 0
	}
	return time.Now(), m.waitNanos.Load()
}

func (m *Machine) chargeCPU(since time.Time, waitBase int64) {
	if m == nil {
		return
	}
	waited := time.Duration(m.waitNanos.Load() - waitBase)
	if d := time.Since(since) - waited; d > 0 {
		m.cpuNanos.Add(int64(d))
	}
}

// noteWait records time the job spent blocked rather than computing.
func (m *Machine) noteWait(since time.Time) {
	if m == nil {
		return
	}
	if d := time.Since(since); d > 0 {
		m.waitNanos.Add(int64(d))
	}
}

// readInput reads a line from the terminal and charges the delay to wait
// time instead of CPU.
func (m *Machine) readInput(prompt string) (string, error) {
	if m.IO.Read == nil {
		return "", m.err("I/O error")
	}
	start := time.Now()
	s, err := m.IO.Read(prompt)
	m.noteWait(start)
	return s, err
}

func (m *Machine) Interrupt() {
	if m != nil {
		m.intr.Store(true)
	}
}

func (m *Machine) Interrupted() bool {
	if m == nil {
		return false
	}
	if m.intr.Load() {
		return true
	}
	if m.IO.PollInterrupt != nil && m.IO.PollInterrupt() {
		m.intr.Store(true)
		return true
	}
	return false
}

func (m *Machine) clearInterrupt() {
	if m != nil {
		m.intr.Store(false)
	}
}

func (m *Machine) CloseAllFiles() {
	for _, f := range m.Files {
		closeChanFile(f)
	}
	m.Files = map[int]*chanFile{}
}

func (m *Machine) ClearProgram(name string) {
	if name == "" {
		name = "NONAME"
	}
	m.Program = map[int]string{}
	m.ProgramName = name
	m.Compiled = false
	m.PrivImage = false
	m.Image = nil
	m.editSeq++
	m.resetRuntime()
}

func (m *Machine) StoreLine(number int, text string) error {
	if m.Compiled {
		return basicErr("Compiled file")
	}
	if strings.TrimSpace(text) == "" {
		delete(m.Program, number)
		m.editSeq++
		return nil
	}
	if _, err := parseSourceLine(text); err != nil {
		return err
	}
	if m.Program == nil {
		m.Program = map[int]string{}
	}
	m.Program[number] = text
	m.editSeq++
	return nil
}

func (m *Machine) lineOrder() []int {
	keys := make([]int, 0, len(m.Program))
	for k := range m.Program {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func (m *Machine) Listing(start, end int, hasStart, hasEnd bool) []string {
	var lines []string
	for _, num := range m.lineOrder() {
		if hasStart && num < start {
			continue
		}
		if hasEnd && num > end {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d %s", num, m.Program[num]))
	}
	return lines
}

func (m *Machine) SourceText() string {
	var b strings.Builder
	for _, num := range m.lineOrder() {
		fmt.Fprintf(&b, "%d %s\n", num, m.Program[num])
	}
	return b.String()
}

func (m *Machine) LoadSource(text, name string) error {
	m.ClearProgram(name)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		i := 0
		for i < len(line) && unicode.IsSpace(rune(line[i])) {
			i++
		}
		j := i
		for j < len(line) && unicode.IsDigit(rune(line[j])) {
			j++
		}
		if j == i {
			return basicErr("Illegal line number")
		}
		num, _ := strconv.Atoi(line[i:j])
		rest := strings.TrimRight(strings.TrimLeft(line[j:], " \t"), " \t")
		if err := m.StoreLine(num, rest); err != nil {
			return err
		}
	}
	return nil
}

func (m *Machine) LoadCompiled(text, name string, privImage bool) error {
	payload, _ := unwrapBAC(text)
	if img, err := unmarshalPcode(payload); err == nil {
		m.ClearProgram(name)
		m.Image = img
		m.Compiled = true
		m.PrivImage = privImage
		return nil
	}
	if err := m.LoadSource(payload, name); err != nil {
		return err
	}
	img, err := compileProgram(m.Program)
	if err != nil {
		return err
	}
	m.Image = img
	m.Program = map[int]string{}
	m.Compiled = true
	m.PrivImage = privImage
	return nil
}

func (m *Machine) collectData() {
	m.data = nil
	m.dataPtr = 0
	for _, num := range m.lineOrder() {
		stmts, err := parseSourceLine(m.Program[num])
		if err != nil {
			continue
		}
		for _, s := range stmts {
			if s.kind == stData {
				m.data = append(m.data, s.data...)
			}
		}
	}
}

func (m *Machine) NoteEdit() {
	m.editSeq++
}

func (m *Machine) RunProgram() error {
	for {
		if m.Compiled && m.Image != nil {
			m.clearInterrupt()
			m.resetRuntime()
			err := m.runImage(m.Image, false)
			if m.chainTo != "" {
				if err := m.takeChain(); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if len(m.Program) == 0 {
			return basicErr("No program")
		}
		img, err := compileProgram(m.Program)
		if err != nil {
			return err
		}
		m.clearInterrupt()
		m.resetRuntime()
		err = m.runImage(img, false)
		if m.chainTo != "" {
			if err := m.takeChain(); err != nil {
				return err
			}
			continue
		}
		return err
	}
}

func (m *Machine) takeChain() error {
	name := strings.TrimSpace(m.chainTo)
	line := m.chainLine
	m.chainTo = ""
	m.chainLine = 0
	if name == "" {
		return basicErr("Illegal file name")
	}
	if m.IO.Load == nil {
		return basicErr("Can't find file or account")
	}
	if err := m.IO.Load(name); err != nil {
		return err
	}
	m.startLine = line
	return nil
}

func (m *Machine) Continue() error {
	if m.paused == nil || !m.Stopped {
		return basicErr("Can't continue")
	}
	if m.editSeq != m.pauseSeq {
		return basicErr("Can't continue")
	}
	vm := m.paused
	m.paused = nil
	m.Stopped = false
	m.running = true
	m.HasLine = true
	m.clearInterrupt()
	started, waitBase := m.startCPU()
	defer func() {
		m.chargeCPU(started, waitBase)
		m.running = false
		if m.Stopped {
			m.paused = vm
			m.pauseSeq = m.editSeq
			return
		}
		m.CloseAllFiles()
		m.HasLine = false
		m.paused = nil
	}()
	return vm.run()
}

func (m *Machine) findIndex(order []int, line int) (int, error) {
	for i, n := range order {
		if n == line {
			return i, nil
		}
	}
	return 0, basicErrAt("Undefined line number", m.CurrentLine)
}

func (m *Machine) skipBlock(index int, order []int) (int, error) {
	depth := 1
	for j := index + 1; j < len(order); j++ {
		stmts, err := parseSourceLine(m.Program[order[j]])
		if err != nil {
			continue
		}
		for _, s := range stmts {
			switch s.kind {
			case stFor, stWhile, stUntil:
				depth++
			case stNext:
				depth--
				if depth == 0 {
					return j + 1, nil
				}
			}
		}
	}
	return 0, basicErrAt("FOR or WHILE without NEXT", m.CurrentLine)
}

func (m *Machine) doNext(varName string, order []int) (int, error) {
	if len(m.forStack) == 0 {
		return 0, basicErrAt("NEXT without FOR", m.CurrentLine)
	}
	if varName != "" {
		for len(m.forStack) > 0 && m.forStack[len(m.forStack)-1].kind == "FOR" && m.forStack[len(m.forStack)-1].varName != varName {
			m.forStack = m.forStack[:len(m.forStack)-1]
		}
		if len(m.forStack) == 0 {
			return 0, basicErrAt("NEXT without FOR", m.CurrentLine)
		}
	}
	frame := m.forStack[len(m.forStack)-1]
	if frame.kind == "WHILE" || frame.kind == "UNTIL" {
		cond, err := m.eval(frame.cond)
		if err != nil {
			return 0, err
		}
		ok := m.truth(cond)
		if frame.kind == "UNTIL" {
			ok = !ok
		}
		if ok {
			return frame.nextIndex, nil
		}
		m.forStack = m.forStack[:len(m.forStack)-1]
		for i, n := range order {
			if n == m.CurrentLine {
				return i + 1, nil
			}
		}
		return 0, basicErrAt("NEXT without FOR", m.CurrentLine)
	}
	cur, err := m.numVal(m.getVar(frame.varName))
	if err != nil {
		return 0, err
	}
	cur += frame.step
	m.setVar(frame.varName, numValue(cur))
	cont := cur <= frame.end
	if frame.step < 0 {
		cont = cur >= frame.end
	}
	if cont {
		return frame.nextIndex, nil
	}
	m.forStack = m.forStack[:len(m.forStack)-1]
	for i, n := range order {
		if n == m.CurrentLine {
			return i + 1, nil
		}
	}
	return 0, basicErrAt("NEXT without FOR", m.CurrentLine)
}

func (m *Machine) ExecImmediate(text string) error {
	img, err := compileImmediate(text)
	if err != nil {
		return err
	}
	if err := m.runImage(img, true); err != nil {
		return err
	}
	if m.chainTo != "" {
		if err := m.takeChain(); err != nil {
			return err
		}
		return m.RunProgram()
	}
	return nil
}

func (m *Machine) execStmts(stmts []stmt) (jump, error) {
	for _, s := range stmts {
		j, err := m.execStmt(s)
		if err != nil {
			return jump{}, err
		}
		if j.kind != jumpNone {
			return j, nil
		}
	}
	return jump{}, nil
}

func (m *Machine) execStmt(s stmt) (jump, error) {
	if len(s.mods) > 0 {
		return m.execMods(s, len(s.mods)-1)
	}
	return m.execCore(s)
}

func (m *Machine) execCore(s stmt) (jump, error) {
	// Statement helpers below are used by the bytecode VM in pcode_vm.go.
	// Program and immediate execution compile to that VM; do not add
	// language features here without a matching opcode.
	switch s.kind {
	case stRem, stData:
		return jump{}, nil
	case stLet:
		v, err := m.eval(s.expr)
		if err != nil {
			return jump{}, err
		}
		return jump{}, m.assign(s.target, v)
	case stPrint:
		return jump{}, m.doPrint(s)
	case stInput:
		return jump{}, m.doInput(s)
	case stLineInput:
		return jump{}, m.doLineInput(s)
	case stGoto:
		n, err := m.evalNum(s.expr)
		if err != nil {
			return jump{}, err
		}
		return jump{kind: jumpGoto, line: int(n)}, nil
	case stGosub:
		n, err := m.evalNum(s.expr)
		if err != nil {
			return jump{}, err
		}
		return jump{kind: jumpGosub, line: int(n)}, nil
	case stReturn:
		return jump{kind: jumpReturn}, nil
	case stIf:
		return m.doIf(s)
	case stFor:
		start, err := m.evalNum(s.start)
		if err != nil {
			return jump{}, err
		}
		end, err := m.evalNum(s.end)
		if err != nil {
			return jump{}, err
		}
		step := 1.0
		if s.step != nil {
			step, err = m.evalNum(s.step)
			if err != nil {
				return jump{}, err
			}
		}
		m.setVar(s.forVar, numValue(start))
		return jump{kind: jumpFor, frame: forFrame{kind: "FOR", varName: s.forVar, end: end, step: step}}, nil
	case stNext:
		return jump{kind: jumpNext, varName: s.nextVar}, nil
	case stWhile:
		return jump{kind: jumpWhile, frame: forFrame{kind: "WHILE", cond: s.cond}}, nil
	case stUntil:
		return jump{kind: jumpWhile, frame: forFrame{kind: "UNTIL", cond: s.cond}}, nil
	case stDim:
		for _, a := range s.arrays {
			bounds := make([]int, len(a.bounds))
			for i, b := range a.bounds {
				n, err := m.evalNum(b)
				if err != nil {
					return jump{}, err
				}
				bounds[i] = int(n)
			}
			if err := m.dimArray(a.name, bounds); err != nil {
				return jump{}, err
			}
		}
		return jump{}, nil
	case stRead:
		for _, t := range s.targets {
			if m.dataPtr >= len(m.data) {
				return jump{}, m.err("Out of data")
			}
			v := m.data[m.dataPtr]
			m.dataPtr++
			cv, err := m.coerceInput(t, v)
			if err != nil {
				return jump{}, err
			}
			if err := m.assign(t, cv); err != nil {
				return jump{}, err
			}
		}
		return jump{}, nil
	case stRestore:
		m.dataPtr = 0
		return jump{}, nil
	case stEnd:
		return jump{kind: jumpEnd}, nil
	case stStop:
		return jump{kind: jumpStop}, nil
	case stOpen:
		return jump{}, m.doOpen(s)
	case stClose:
		if !s.hasChan {
			m.CloseAllFiles()
			return jump{}, nil
		}
		ch, err := m.evalNum(s.channel)
		if err != nil {
			return jump{}, err
		}
		n := int(ch)
		if f := m.Files[n]; f != nil {
			closeChanFile(f)
		}
		delete(m.Files, n)
		return jump{}, nil
	case stRandomize:
		if s.hasSeed {
			n, err := m.evalNum(s.seed)
			if err != nil {
				return jump{}, err
			}
			m.rng = rand.New(rand.NewSource(int64(n)))
		} else {
			m.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
		}
		return jump{}, nil
	case stOnGoto, stOnGosub:
		idx, err := m.evalNum(s.expr)
		if err != nil {
			return jump{}, err
		}
		i := int(idx)
		if i >= 1 && i <= len(s.lines) {
			line, err := m.evalNum(s.lines[i-1])
			if err != nil {
				return jump{}, err
			}
			k := jumpGoto
			if s.kind == stOnGosub {
				k = jumpGosub
			}
			return jump{kind: k, line: int(line)}, nil
		}
		return jump{}, nil
	case stDef:
		m.functions[s.fnName] = fnDef{params: s.params, body: s.fnExpr}
		return jump{}, nil
	case stOnError:
		n, err := m.evalNum(s.expr)
		if err != nil {
			return jump{}, err
		}
		m.onErrorLine = int(n)
		return jump{}, nil
	case stResume:
		return m.doResume(s)
	case stChange:
		return jump{}, m.doChange(s)
	case stGet:
		return jump{}, m.doGet(s)
	case stPut:
		return jump{}, m.doPut(s)
	case stField:
		return jump{}, m.doField(s)
	case stLset:
		return jump{}, m.doLRset(s, false)
	case stRset:
		return jump{}, m.doLRset(s, true)
	case stMat:
		return jump{}, m.doMat(s)
	case stMap:
		return jump{}, m.doMap(s)
	default:
		return jump{}, m.err("Syntax error")
	}
}

func (m *Machine) doIf(s stmt) (jump, error) {
	cond, err := m.eval(s.cond)
	if err != nil {
		return jump{}, err
	}
	br := s.thenPart
	if !m.truth(cond) {
		br = s.elsePart
	}
	if br == nil {
		return jump{}, nil
	}
	if br.isLine {
		n, err := m.evalNum(br.line)
		if err != nil {
			return jump{}, err
		}
		return jump{kind: jumpGoto, line: int(n)}, nil
	}
	return m.execStmts(br.stmts)
}

func (m *Machine) doPrint(s stmt) error {
	if s.hasUsing {
		return m.doPrintUsing(s)
	}
	var parts []string
	localCol := m.col
	for _, item := range s.items {
		if item.sep != "" {
			if item.sep == "TAB" {
				width := 14 - (localCol % 14)
				if width == 0 {
					width = 14
				}
				parts = append(parts, strings.Repeat(" ", width))
				localCol += width
			}
			continue
		}
		v, err := m.eval(item.expr)
		if err != nil {
			return err
		}
		var text string
		if c, ok := item.expr.(callExpr); ok && (c.name == "TAB" || c.name == "SPC") {
			text = m.strVal(v)
		} else if v.isStr {
			text = v.str
		} else {
			text = fmtNum(v.num)
			if !strings.HasPrefix(text, "-") && !strings.HasPrefix(text, " ") {
				text = " " + text
			}
		}
		parts = append(parts, text)
		localCol += len(text)
	}
	text := strings.Join(parts, "")
	newline := s.trailing == "NL"
	if !s.hasChan {
		if m.IO.Write != nil {
			m.IO.Write(text, newline)
		}
		if newline {
			m.col = 0
		} else {
			m.col = localCol
		}
		return nil
	}
	ch, err := m.evalNum(s.channel)
	if err != nil {
		return err
	}
	if newline {
		text += "\n"
	}
	return m.fileWrite(int(ch), text)
}

func (m *Machine) doInput(s stmt) error {
	if s.hasChan {
		ch, err := m.evalNum(s.channel)
		if err != nil {
			return err
		}
		raw, err := m.fileReadLine(int(ch))
		if err != nil {
			return err
		}
		m.recount = len(raw)
		values := splitInput(raw)
		for i, t := range s.targets {
			if i >= len(values) {
				return m.err("End of file on device")
			}
			cv, err := m.coerceInput(t, value{isStr: true, str: values[i]})
			if err != nil {
				return err
			}
			if err := m.assign(t, cv); err != nil {
				return err
			}
		}
		return nil
	}
	if s.hasPrompt && s.prompt != nil && m.IO.Write != nil {
		m.IO.Write(*s.prompt, false)
	}
	for i, t := range s.targets {
		suffix := "? "
		if i != 0 {
			suffix = "?? "
		}
		raw, err := m.readInput(suffix)
		if err != nil {
			return err
		}
		m.recount = len(raw)
		cv, err := m.coerceInput(t, value{isStr: true, str: raw})
		if err != nil {
			return err
		}
		if err := m.assign(t, cv); err != nil {
			return err
		}
	}
	return nil
}

func (m *Machine) doLineInput(s stmt) error {
	var raw string
	var err error
	if s.hasChan {
		ch, err2 := m.evalNum(s.channel)
		if err2 != nil {
			return err2
		}
		raw, err = m.fileReadLine(int(ch))
	} else {
		raw, err = m.readInput("")
	}
	if err != nil {
		return err
	}
	m.recount = len(raw)
	return m.assign(s.target, strValue(raw))
}

func (m *Machine) doOpen(s stmt) error {
	pathv, err := m.eval(s.path)
	if err != nil {
		return err
	}
	path := m.strVal(pathv)
	ch, err := m.evalNum(s.channel)
	if err != nil {
		return err
	}
	if m.IO.Open == nil {
		return m.err("I/O error")
	}
	if err := m.IO.Open(m, int(ch), path, s.mode); err != nil {
		return err
	}
	f := m.Files[int(ch)]
	if f == nil {
		return m.err("I/O error")
	}
	if s.recSize != nil {
		n, err := m.evalNum(s.recSize)
		if err != nil {
			return err
		}
		f.recSize = int(n)
		if f.recSize < 1 {
			f.recSize = 512
		}
		f.buf = make([]byte, f.recSize)
	}
	f.orgVirtual = s.org == "VIRTUAL"
	f.mapName = s.mapName
	m.status = f.statusWord(int(ch))
	return nil
}

// STATUS after an OPEN says what the channel is attached to: the device
// class in the low byte and the channel number in the high byte, the
// shape RSTS used. A program normally tests the class bits.
const (
	devDisk     = 1
	devKeyboard = 2
	devPrinter  = 4
	devTape     = 8
	devNull     = 16
)

func (f *chanFile) statusWord(channel int) int {
	class := f.class
	if class == 0 {
		class = devDisk
	}
	return class | channel<<8
}

func (m *Machine) eval(e expr) (value, error) {
	switch n := e.(type) {
	case numLit:
		return numValue(n.v), nil
	case strLit:
		return strValue(n.v), nil
	case varRef:
		if len(n.indices) > 0 {
			idxs, err := m.evalIndices(n.indices)
			if err != nil {
				return value{}, err
			}
			return m.getArray(n.name, idxs)
		}
		return m.getVar(n.name), nil
	case unaryExpr:
		v, err := m.eval(n.x)
		if err != nil {
			return value{}, err
		}
		switch n.op {
		case "-":
			x, err := m.numVal(v)
			return numValue(-x), err
		case "+":
			x, err := m.numVal(v)
			return numValue(x), err
		case "NOT":
			x, err := m.numVal(v)
			if err != nil {
				return value{}, err
			}
			return numValue(float64(^int(x))), nil
		default:
			return value{}, m.err("Syntax error")
		}
	case binExpr:
		l, err := m.eval(n.l)
		if err != nil {
			return value{}, err
		}
		r, err := m.eval(n.r)
		if err != nil {
			return value{}, err
		}
		return m.binOp(n.op, l, r)
	case callExpr:
		args := make([]value, len(n.args))
		for i, a := range n.args {
			v, err := m.eval(a)
			if err != nil {
				return value{}, err
			}
			args[i] = v
		}
		return m.call(n.name, args)
	default:
		return value{}, m.err("Syntax error")
	}
}

func (m *Machine) evalNum(e expr) (float64, error) {
	v, err := m.eval(e)
	if err != nil {
		return 0, err
	}
	return m.numVal(v)
}

func (m *Machine) evalIndices(es []expr) ([]int, error) {
	out := make([]int, len(es))
	for i, e := range es {
		n, err := m.evalNum(e)
		if err != nil {
			return nil, err
		}
		out[i] = int(n)
	}
	return out, nil
}

func (m *Machine) binOp(op string, left, right value) (value, error) {
	switch op {
	case "+":
		if left.isStr || right.isStr {
			return strValue(m.strVal(left) + m.strVal(right)), nil
		}
		a, err := m.numVal(left)
		if err != nil {
			return value{}, err
		}
		b, err := m.numVal(right)
		if err != nil {
			return value{}, err
		}
		return numValue(a + b), nil
	case "-":
		a, b, err := m.nums(left, right)
		return numValue(a - b), err
	case "*":
		a, b, err := m.nums(left, right)
		return numValue(a * b), err
	case "/":
		a, b, err := m.nums(left, right)
		if err != nil {
			return value{}, err
		}
		if b == 0 {
			return value{}, m.err("Division by 0")
		}
		return numValue(a / b), nil
	case `\`:
		a, b, err := m.nums(left, right)
		if err != nil {
			return value{}, err
		}
		if b == 0 {
			return value{}, m.err("Division by 0")
		}
		return numValue(float64(int(a / b))), nil
	case "MOD":
		a, b, err := m.nums(left, right)
		if err != nil {
			return value{}, err
		}
		if b == 0 {
			return value{}, m.err("Division by 0")
		}
		return numValue(float64(int(a) % int(b))), nil
	case "^":
		a, b, err := m.nums(left, right)
		return numValue(math.Pow(a, b)), err
	case "AND":
		a, b, err := m.nums(left, right)
		return numValue(float64(int(a) & int(b))), err
	case "OR":
		a, b, err := m.nums(left, right)
		return numValue(float64(int(a) | int(b))), err
	}
	var ok bool
	if left.isStr || right.isStr {
		ls, rs := m.strVal(left), m.strVal(right)
		switch op {
		case "=":
			ok = ls == rs
		case "<>":
			ok = ls != rs
		case "<":
			ok = ls < rs
		case ">":
			ok = ls > rs
		case "<=":
			ok = ls <= rs
		case ">=":
			ok = ls >= rs
		default:
			return value{}, m.err("Syntax error")
		}
	} else {
		a, b, err := m.nums(left, right)
		if err != nil {
			return value{}, err
		}
		switch op {
		case "=":
			ok = a == b
		case "<>":
			ok = a != b
		case "<":
			ok = a < b
		case ">":
			ok = a > b
		case "<=":
			ok = a <= b
		case ">=":
			ok = a >= b
		default:
			return value{}, m.err("Syntax error")
		}
	}
	if ok {
		return numValue(-1), nil
	}
	return numValue(0), nil
}

func (m *Machine) nums(l, r value) (float64, float64, error) {
	a, err := m.numVal(l)
	if err != nil {
		return 0, 0, err
	}
	b, err := m.numVal(r)
	return a, b, err
}

func (m *Machine) call(name string, args []value) (value, error) {
	if def, ok := m.functions[name]; ok {
		if len(args) != len(def.params) {
			return value{}, m.err("Argument count")
		}
		saved := map[string]value{}
		present := map[string]bool{}
		for _, p := range def.params {
			if v, ok := m.vars[p]; ok {
				saved[p] = v
				present[p] = true
			}
		}
		for i, p := range def.params {
			m.vars[p] = args[i]
		}
		defer func() {
			for _, p := range def.params {
				if present[p] {
					m.vars[p] = saved[p]
				} else {
					delete(m.vars, p)
				}
			}
		}()
		return m.eval(def.body)
	}
	if stringsHasPrefix(name, "FN") {
		return value{}, m.err("Undefined function")
	}
	argn := func(i int) (float64, error) {
		if i >= len(args) {
			return 0, m.err("Argument count")
		}
		return m.numVal(args[i])
	}
	args_ := func(i int) string {
		if i >= len(args) {
			return ""
		}
		return m.strVal(args[i])
	}
	switch name {
	case "ABS":
		n, err := argn(0)
		return numValue(math.Abs(n)), err
	case "INT":
		n, err := argn(0)
		return numValue(math.Floor(n)), err
	case "FIX":
		n, err := argn(0)
		return numValue(float64(int(n))), err
	case "SGN":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		s := 0.0
		if n > 0 {
			s = 1
		} else if n < 0 {
			s = -1
		}
		return numValue(s), nil
	case "SQR":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		if n < 0 {
			return value{}, m.err("Illegal function usage")
		}
		return numValue(math.Sqrt(n)), nil
	case "SIN":
		n, err := argn(0)
		return numValue(math.Sin(n)), err
	case "COS":
		n, err := argn(0)
		return numValue(math.Cos(n)), err
	case "TAN":
		n, err := argn(0)
		return numValue(math.Tan(n)), err
	case "ATN":
		n, err := argn(0)
		return numValue(math.Atan(n)), err
	case "LOG":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		if n <= 0 {
			return value{}, m.err("Illegal function usage")
		}
		return numValue(math.Log(n)), nil
	case "LOG10":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		if n <= 0 {
			return value{}, m.err("Illegal function usage")
		}
		return numValue(math.Log10(n)), nil
	case "EXP":
		n, err := argn(0)
		return numValue(math.Exp(n)), err
	case "PI":
		return numValue(math.Pi), nil
	case "LEN":
		return numValue(float64(len(args_(0)))), nil
	case "ASC":
		s := args_(0)
		if s == "" {
			return numValue(0), nil
		}
		return numValue(float64(s[0])), nil
	case "VAL":
		s := strings.TrimSpace(args_(0))
		if s == "" {
			return numValue(0), nil
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return value{}, m.err("Illegal number")
		}
		return numValue(n), nil
	case "STR$":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		return strValue(fmtNum(n)), nil
	case "NUM1$":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		return strValue(fmtNum(n)), nil
	case "NUM$":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		s := fmtNum(n)
		if n >= 0 && !strings.HasPrefix(s, " ") && !strings.HasPrefix(s, "-") {
			s = " " + s
		}
		return strValue(s), nil
	case "CHR$":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		return strValue(string([]byte{byte(int(n) & 255)})), nil
	case "POS":
		return numValue(float64(m.col + 1)), nil
	case "DATE$":
		return strValue(NowDate()), nil
	case "TIME$":
		return strValue(NowTime()), nil
	case "DATE":
		return numValue(float64(rstsDateInt(time.Now()))), nil
	case "TIME":
		which := 0.0
		if len(args) > 0 {
			var err error
			which, err = m.numVal(args[0])
			if err != nil {
				return value{}, err
			}
		}
		switch int(which) {
		case 1:
			return numValue(m.CPUTime().Seconds()), nil
		default:
			return numValue(secondsSinceMidnight()), nil
		}
	case "PEEK":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		w := peekWord(int(n), m.IO.Job) & 0xFFFF
		return numValue(float64(int16(uint16(w)))), nil
	case "SWAP%":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		return numValue(swapPercent(n)), nil
	case "RND":
		n := 1.0
		if len(args) > 0 {
			var err error
			n, err = m.numVal(args[0])
			if err != nil {
				return value{}, err
			}
		}
		if n < 0 {
			m.rng = rand.New(rand.NewSource(int64(-n)))
		}
		if n == 0 && m.lastRnd != 0 {
			return numValue(m.lastRnd), nil
		}
		m.lastRnd = m.rng.Float64()
		return numValue(m.lastRnd), nil
	case "LEFT$":
		s := args_(0)
		n, err := argn(1)
		if err != nil {
			return value{}, err
		}
		i := int(n)
		if i < 0 {
			i = 0
		}
		if i > len(s) {
			i = len(s)
		}
		return strValue(s[:i]), nil
	case "RIGHT$":
		s := args_(0)
		n, err := argn(1)
		if err != nil {
			return value{}, err
		}
		// BASIC-PLUS: from character n through the end (not "last n chars").
		i := int(n) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			return strValue(""), nil
		}
		return strValue(s[i:]), nil
	case "MID$":
		s := args_(0)
		start, err := argn(1)
		if err != nil {
			return value{}, err
		}
		st := int(start) - 1
		if st < 0 {
			st = 0
		}
		if st > len(s) {
			st = len(s)
		}
		if len(args) > 2 {
			ln, err := argn(2)
			if err != nil {
				return value{}, err
			}
			end := st + int(ln)
			if end > len(s) {
				end = len(s)
			}
			if end < st {
				end = st
			}
			return strValue(s[st:end]), nil
		}
		return strValue(s[st:]), nil
	case "INSTR":
		if len(args) == 2 {
			return numValue(float64(strings.Index(args_(0), args_(1)) + 1)), nil
		}
		start, err := argn(0)
		if err != nil {
			return value{}, err
		}
		st := int(start) - 1
		if st < 0 {
			st = 0
		}
		s := args_(1)
		sub := args_(2)
		if st > len(s) {
			return numValue(0), nil
		}
		idx := strings.Index(s[st:], sub)
		if idx < 0 {
			return numValue(0), nil
		}
		return numValue(float64(st + idx + 1)), nil
	case "SPACE$":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		i := int(n)
		if i < 0 {
			i = 0
		}
		return strValue(strings.Repeat(" ", i)), nil
	case "STRING$":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		i := int(n)
		if i < 0 {
			i = 0
		}
		var ch string
		if len(args) > 1 && args[1].isStr {
			if args[1].str == "" {
				ch = ""
			} else {
				ch = args[1].str[:1]
			}
		} else {
			c, err := argn(1)
			if err != nil {
				return value{}, err
			}
			ch = string(rune(int(c) & 255))
		}
		return strValue(strings.Repeat(ch, i)), nil
	case "TAB":
		col, err := argn(0)
		if err != nil {
			return value{}, err
		}
		pad := int(col) - 1 - m.col
		if pad < 0 {
			pad = 0
		}
		return strValue(strings.Repeat(" ", pad)), nil
	case "SPC":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		i := int(n)
		if i < 0 {
			i = 0
		}
		return strValue(strings.Repeat(" ", i)), nil
	case "SYS":
		if len(args) < 1 {
			return value{}, m.err("Argument count")
		}
		out, err := m.sysCall(m.strVal(args[0]))
		if err != nil {
			return value{}, m.err(err.Error())
		}
		return strValue(out), nil
	case "ERR":
		return numValue(float64(m.errNum)), nil
	case "ERL":
		return numValue(float64(m.errLine)), nil
	case "RECOUNT":
		return numValue(float64(m.recount)), nil
	case "STATUS":
		return numValue(float64(m.status)), nil
	case "DET":
		return numValue(m.det), nil
	case "NUM":
		return numValue(float64(m.matNum)), nil
	case "NUM2":
		return numValue(float64(m.matNum2)), nil
	case "CVT%$":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		return strValue(cvtPercentDollar(n)), nil
	case "CVT$%":
		return numValue(cvtDollarPercent(args_(0))), nil
	case "CVTF$":
		n, err := argn(0)
		if err != nil {
			return value{}, err
		}
		return strValue(cvtFDollar(n)), nil
	case "CVT$F":
		return numValue(cvtDollarF(args_(0))), nil
	case "CVT$$":
		return strValue(cvtDollarDollar(args_(0))), nil
	default:
		return value{}, m.err("Undefined function")
	}
}

func (m *Machine) assign(target *varRef, v value) error {
	cv, err := m.coerceVar(target.name, v)
	if err != nil {
		return err
	}
	if len(target.indices) > 0 {
		idxs, err := m.evalIndices(target.indices)
		if err != nil {
			return err
		}
		return m.setArray(target.name, idxs, cv)
	}
	m.setVar(target.name, cv)
	return nil
}

func (m *Machine) coerceVar(name string, v value) (value, error) {
	if strings.HasSuffix(name, "$") {
		return strValue(m.strVal(v)), nil
	}
	if strings.HasSuffix(name, "%") {
		n, err := m.numVal(v)
		if err != nil {
			return value{}, err
		}
		return numValue(float64(int(math.Round(n)))), nil
	}
	if v.isStr {
		return value{}, m.err("Type mismatch")
	}
	return v, nil
}

func (m *Machine) coerceInput(target *varRef, v value) (value, error) {
	if strings.HasSuffix(target.name, "$") {
		return strValue(strings.TrimRight(m.strVal(v), "\n")), nil
	}
	s := strings.TrimSpace(m.strVal(v))
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return value{}, m.err("Illegal number")
	}
	return numValue(n), nil
}

func (m *Machine) getVar(name string) value {
	if v, ok := m.vars[name]; ok {
		return v
	}
	if strings.HasSuffix(name, "$") {
		return strValue("")
	}
	return numValue(0)
}

func (m *Machine) setVar(name string, v value) {
	if m.vars == nil {
		m.vars = map[string]value{}
	}
	m.vars[name] = v
}

func (m *Machine) ensureArray(name string, idxs []int) error {
	if _, ok := m.arrays[name]; ok {
		return nil
	}
	n := len(idxs)
	if n < 1 {
		n = 1
	}
	bounds := make([]int, n)
	for i := range bounds {
		bounds[i] = 10
	}
	return m.dimArray(name, bounds)
}

func (m *Machine) dimArray(name string, bounds []int) error {
	size := 1
	dims := make([]int, len(bounds))
	for i, b := range bounds {
		if b < 0 {
			return m.err("Subscript out of range")
		}
		dims[i] = b
		size *= b + 1
	}
	fill := numValue(0)
	if strings.HasSuffix(name, "$") {
		fill = strValue("")
	}
	data := make([]value, size)
	for i := range data {
		data[i] = fill
	}
	if m.arrays == nil {
		m.arrays = map[string]*arrayInfo{}
	}
	m.arrays[name] = &arrayInfo{dims: dims, data: data}
	return nil
}

func (m *Machine) dimVirtArray(name string, bounds []int, channel, strLen int) error {
	if channel < 1 {
		return m.err("I/O error")
	}
	f := m.Files[channel]
	if f == nil || f.file == nil {
		return m.err("I/O error")
	}
	for _, b := range bounds {
		if b < 0 {
			return m.err("Subscript out of range")
		}
	}
	elem := 4
	isStr, isInt := false, false
	switch {
	case strings.HasSuffix(name, "%"):
		elem = 2
		isInt = true
	case strings.HasSuffix(name, "$"):
		isStr = true
		if strLen <= 0 {
			strLen = 16
		}
		if strLen < 1 {
			return m.err("Illegal number")
		}
		elem = strLen
	}
	if m.arrays == nil {
		m.arrays = map[string]*arrayInfo{}
	}
	nelt := 1
	for _, b := range bounds {
		nelt *= b + 1
	}
	nbytes := nelt * elem
	base := 0
	if old, ok := m.arrays[name]; ok && old.virtChan == channel {
		base = old.virtBase
	} else {
		if m.virtNext == nil {
			m.virtNext = map[int]int{}
		}
		base = m.virtNext[channel]
	}
	if m.virtNext == nil {
		m.virtNext = map[int]int{}
	}
	if end := base + nbytes; m.virtNext[channel] < end {
		m.virtNext[channel] = end
	}
	m.arrays[name] = &arrayInfo{
		dims:     append([]int(nil), bounds...),
		virtChan: channel,
		virtBase: base,
		elemSize: elem,
		strLen:   strLen,
		isStr:    isStr,
		isInt:    isInt,
	}
	return nil
}

func (m *Machine) offset(name string, idxs []int) (int, error) {
	if err := m.ensureArray(name, idxs); err != nil {
		return 0, err
	}
	info := m.arrays[name]
	if len(idxs) != len(info.dims) {
		return 0, m.err("Subscript out of range")
	}
	off := 0
	stride := 1
	for i, d := range info.dims {
		if idxs[i] < 0 || idxs[i] > d {
			return 0, m.err("Subscript out of range")
		}
		off += idxs[i] * stride
		stride *= d + 1
	}
	return off, nil
}

func (m *Machine) getArray(name string, idxs []int) (value, error) {
	off, err := m.offset(name, idxs)
	if err != nil {
		return value{}, err
	}
	info := m.arrays[name]
	if info.virtChan != 0 {
		return m.virtGet(info, off)
	}
	return info.data[off], nil
}

func (m *Machine) setArray(name string, idxs []int, v value) error {
	off, err := m.offset(name, idxs)
	if err != nil {
		return err
	}
	info := m.arrays[name]
	if info.virtChan != 0 {
		return m.virtSet(info, off, v)
	}
	info.data[off] = v
	return nil
}

func (m *Machine) virtGet(info *arrayInfo, off int) (value, error) {
	f := m.Files[info.virtChan]
	if f == nil || f.file == nil {
		return value{}, m.err("I/O error")
	}
	buf := make([]byte, info.elemSize)
	n, err := f.file.ReadAt(buf, int64(info.virtBase+off*info.elemSize))
	if n < info.elemSize {
		for i := n; i < info.elemSize; i++ {
			buf[i] = 0
		}
	}
	if err != nil && err != io.EOF && n == 0 {
		return value{}, m.err("I/O error")
	}
	switch {
	case info.isStr:
		return strValue(string(buf)), nil
	case info.isInt:
		return numValue(cvtDollarPercent(string(buf))), nil
	default:
		return numValue(cvtDollarF(string(buf))), nil
	}
}

func (m *Machine) virtSet(info *arrayInfo, off int, v value) error {
	f := m.Files[info.virtChan]
	if f == nil || f.file == nil {
		return m.err("I/O error")
	}
	var buf []byte
	switch {
	case info.isStr:
		s := m.strVal(v)
		if len(s) > info.elemSize {
			s = s[:info.elemSize]
		}
		b := []byte(s)
		if len(b) < info.elemSize {
			pad := make([]byte, info.elemSize)
			copy(pad, b)
			for i := len(b); i < info.elemSize; i++ {
				pad[i] = ' '
			}
			b = pad
		}
		buf = b
	case info.isInt:
		n, err := m.numVal(v)
		if err != nil {
			return err
		}
		buf = []byte(cvtPercentDollar(n))
	default:
		n, err := m.numVal(v)
		if err != nil {
			return err
		}
		buf = []byte(cvtFDollar(n))
	}
	_, err := f.file.WriteAt(buf, int64(info.virtBase+off*info.elemSize))
	if err != nil {
		return m.err("I/O error")
	}
	return nil
}

func (m *Machine) doKillPath(path string) error {
	if m.IO.Delete == nil {
		return m.err("I/O error")
	}
	if err := m.IO.Delete(path); err != nil {
		return m.err(strings.TrimPrefix(err.Error(), "?"))
	}
	return nil
}

func (m *Machine) doNamePath(old, new string) error {
	if m.IO.Rename == nil {
		return m.err("I/O error")
	}
	if err := m.IO.Rename(old, new); err != nil {
		return m.err(strings.TrimPrefix(err.Error(), "?"))
	}
	return nil
}

func (m *Machine) fileWrite(ch int, text string) error {
	f := m.Files[ch]
	if f == nil {
		return m.err("I/O error")
	}
	if f.pk != nil {
		return f.pk.ctrlWrite(text)
	}
	if f.dev != nil {
		return f.dev.devWrite(text)
	}
	if f.mode != "OUTPUT" && f.mode != "APPEND" {
		return m.err("I/O error")
	}
	_, err := f.file.WriteString(text)
	return err
}

func (m *Machine) fileReadLine(ch int) (string, error) {
	f := m.Files[ch]
	if f == nil {
		return "", m.err("I/O error")
	}
	if f.pk != nil {
		line, err := f.pk.ctrlReadLine()
		if err == io.EOF {
			return "", m.err("End of file on device")
		}
		return line, err
	}
	if f.dev != nil {
		line, err := f.dev.devReadLine()
		if err == io.EOF {
			return "", m.err("End of file on device")
		}
		return line, err
	}
	if f.mode != "INPUT" {
		return "", m.err("I/O error")
	}
	line, err := f.r.ReadString('\n')
	if err != nil && !(err == io.EOF && line != "") {
		if err == io.EOF {
			return "", m.err("End of file on device")
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func splitInput(raw string) []string {
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func fmtNum(v float64) string {
	if math.Abs(v-math.Round(v)) < 1e-10 {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}

func numValue(n float64) value { return value{num: n} }
func strValue(s string) value  { return value{isStr: true, str: s} }

func (m *Machine) numVal(v value) (float64, error) {
	if v.isStr {
		return 0, m.err("Type mismatch")
	}
	return v.num, nil
}

func (m *Machine) strVal(v value) string {
	if v.isStr {
		return v.str
	}
	return fmtNum(v.num)
}

func (m *Machine) truth(v value) bool {
	if v.isStr {
		return len(v.str) > 0
	}
	return v.num != 0
}

func (m *Machine) err(msg string) error {
	if m.HasLine {
		return basicErrAt(msg, m.CurrentLine)
	}
	return basicErr(msg)
}

func NowDate() string { return time.Now().Format("02-Jan-06") }

func NowTime() string {
	s := time.Now().Format("3:04 PM")
	return strings.TrimLeft(s, "0")
}
