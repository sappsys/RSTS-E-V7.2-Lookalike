package rsts

import (
	"encoding/binary"
	"math"
	"sort"
	"strings"
)

type loopKind int

const (
	loopFor loopKind = iota
	loopWhile
	loopUntil
)

type compileLoop struct {
	kind    loopKind
	name    string
	beginIP int
	failAt  int
}

type pendingFn struct {
	name   string
	params []string
	body   expr
	patch  int
}

// A multi-line DEF is compiled where it stands, with a jump around the
// body so that falling off the line above does not run it.
type openDef struct {
	name string
	skip int
}

type compiler struct {
	img       *pcodeImage
	strMap    map[string]uint16
	loops     []compileLoop
	fns       []pendingFn
	defs      []openDef
	immediate bool
}

func newCompiler() *compiler {
	return &compiler{
		img:    &pcodeImage{},
		strMap: map[string]uint16{},
	}
}

func compileProgram(prog map[int]string) (*pcodeImage, error) {
	c := newCompiler()
	keys := make([]int, 0, len(prog))
	for k := range prog {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, num := range keys {
		stmts, err := parseSourceLine(prog[num])
		if err != nil {
			return nil, attachLine(err, num)
		}
		c.emit(opLine)
		c.emitU32(uint32(num))
		c.img.Lines = append(c.img.Lines, pcodeLine{Num: num, IP: len(c.img.Code) - 5})
		for _, s := range stmts {
			if err := c.stmt(s); err != nil {
				return nil, attachLine(err, num)
			}
		}
	}
	if len(c.loops) > 0 {
		return nil, basicErr("FOR or WHILE without NEXT")
	}
	if len(c.defs) > 0 {
		return nil, basicErr("DEF without FNEND")
	}
	c.emit(opHalt)
	c.img.HaltIP = len(c.img.Code) - 1
	if err := c.emitFuncs(); err != nil {
		return nil, err
	}
	return c.img, nil
}

func compileImmediate(text string) (*pcodeImage, error) {
	stmts, err := parseSourceLine(text)
	if err != nil {
		return nil, err
	}
	c := newCompiler()
	c.immediate = true
	for _, s := range stmts {
		if err := c.stmt(s); err != nil {
			return nil, err
		}
	}
	if len(c.loops) > 0 {
		return nil, basicErr("Illegal in immediate mode")
	}
	c.emit(opHalt)
	c.img.HaltIP = len(c.img.Code) - 1
	if err := c.emitFuncs(); err != nil {
		return nil, err
	}
	return c.img, nil
}

func compileSourceText(text string) (*pcodeImage, error) {
	m := NewMachine(IO{})
	if err := m.LoadSource(text, "COMPILE"); err != nil {
		return nil, err
	}
	return compileProgram(m.Program)
}

func (c *compiler) intern(s string) uint16 {
	if i, ok := c.strMap[s]; ok {
		return i
	}
	if len(c.img.Strings) >= 0xFFFE {
		return 0
	}
	i := uint16(len(c.img.Strings))
	c.img.Strings = append(c.img.Strings, s)
	c.strMap[s] = i
	return i
}

func (c *compiler) internNum(n float64) uint16 {
	bits := math.Float64bits(n)
	for i, x := range c.img.Nums {
		if math.Float64bits(x) == bits {
			return uint16(i)
		}
	}
	i := uint16(len(c.img.Nums))
	c.img.Nums = append(c.img.Nums, n)
	return i
}

func (c *compiler) emit(op byte) { c.img.Code = append(c.img.Code, op) }

func (c *compiler) emitU8(v byte) { c.img.Code = append(c.img.Code, v) }

func (c *compiler) emitU16(v uint16) {
	c.img.Code = append(c.img.Code, byte(v), byte(v>>8))
}

func (c *compiler) emitU32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	c.img.Code = append(c.img.Code, b[:]...)
}

func (c *compiler) holeU32() int {
	at := len(c.img.Code)
	c.emitU32(0)
	return at
}

func (c *compiler) patchU32(at int, v uint32) {
	binary.LittleEndian.PutUint32(c.img.Code[at:at+4], v)
}

func (c *compiler) emitJump(op byte) int {
	c.emit(op)
	return c.holeU32()
}

func (c *compiler) stmt(s stmt) error {
	if len(s.mods) > 0 {
		return c.mods(s, len(s.mods)-1)
	}
	return c.core(s)
}

func (c *compiler) mods(s stmt, mi int) error {
	if mi < 0 {
		return c.core(s)
	}
	mod := s.mods[mi]
	switch mod.kind {
	case "IF":
		if err := c.expr(mod.cond); err != nil {
			return err
		}
		skip := c.emitJump(opJumpFalse)
		if err := c.mods(s, mi-1); err != nil {
			return err
		}
		c.patchU32(skip, uint32(len(c.img.Code)))
		return nil
	case "UNLESS":
		if err := c.expr(mod.cond); err != nil {
			return err
		}
		skip := c.emitJump(opJumpTrue)
		if err := c.mods(s, mi-1); err != nil {
			return err
		}
		c.patchU32(skip, uint32(len(c.img.Code)))
		return nil
	case "WHILE":
		begin := len(c.img.Code)
		if err := c.expr(mod.cond); err != nil {
			return err
		}
		done := c.emitJump(opJumpFalse)
		if err := c.mods(s, mi-1); err != nil {
			return err
		}
		c.emit(opJump)
		c.emitU32(uint32(begin))
		c.patchU32(done, uint32(len(c.img.Code)))
		return nil
	case "UNTIL":
		begin := len(c.img.Code)
		if err := c.expr(mod.cond); err != nil {
			return err
		}
		done := c.emitJump(opJumpTrue)
		if err := c.mods(s, mi-1); err != nil {
			return err
		}
		c.emit(opJump)
		c.emitU32(uint32(begin))
		c.patchU32(done, uint32(len(c.img.Code)))
		return nil
	case "FOR":
		if err := c.expr(mod.start); err != nil {
			return err
		}
		if err := c.expr(mod.end); err != nil {
			return err
		}
		if mod.step != nil {
			if err := c.expr(mod.step); err != nil {
				return err
			}
		} else {
			c.emit(opPush1)
		}
		c.emit(opForBegin)
		c.emitU16(c.intern(mod.forVar))
		fail := c.holeU32()
		if err := c.mods(s, mi-1); err != nil {
			return err
		}
		c.emit(opForNext)
		c.emitU16(c.intern(mod.forVar))
		c.patchU32(fail, uint32(len(c.img.Code)))
		return nil
	default:
		return c.core(s)
	}
}

func (c *compiler) core(s stmt) error {
	switch s.kind {
	case stRem:
		return nil
	case stData:
		for _, v := range s.data {
			if v.isStr {
				c.intern(v.str)
			}
			c.img.Data = append(c.img.Data, v)
		}
		return nil
	case stLet:
		if err := c.expr(s.expr); err != nil {
			return err
		}
		return c.store(s.target)
	case stPrint:
		return c.print(s)
	case stInput:
		return c.input(s)
	case stLineInput:
		if s.hasChan {
			if err := c.expr(s.channel); err != nil {
				return err
			}
			c.emit(opInputChan)
		}
		for _, ix := range s.target.indices {
			if err := c.expr(ix); err != nil {
				return err
			}
		}
		c.emit(opLineInput)
		c.emitU16(c.intern(s.target.name))
		c.emitU8(byte(len(s.target.indices)))
		return nil
	case stGoto:
		if err := c.expr(s.expr); err != nil {
			return err
		}
		c.emit(opGoto)
		return nil
	case stGosub:
		if err := c.expr(s.expr); err != nil {
			return err
		}
		c.emit(opGosub)
		return nil
	case stReturn:
		c.emit(opReturn)
		return nil
	case stIf:
		return c.ifStmt(s)
	case stFor:
		if err := c.expr(s.start); err != nil {
			return err
		}
		if err := c.expr(s.end); err != nil {
			return err
		}
		if s.step != nil {
			if err := c.expr(s.step); err != nil {
				return err
			}
		} else {
			c.emit(opPush1)
		}
		c.emit(opForBegin)
		c.emitU16(c.intern(s.forVar))
		fail := c.holeU32()
		c.loops = append(c.loops, compileLoop{kind: loopFor, name: s.forVar, failAt: fail})
		return nil
	case stNext:
		return c.next(s.nextVar)
	case stWhile:
		begin := len(c.img.Code)
		if err := c.expr(s.cond); err != nil {
			return err
		}
		fail := c.emitJump(opJumpFalse)
		c.loops = append(c.loops, compileLoop{kind: loopWhile, beginIP: begin, failAt: fail})
		return nil
	case stUntil:
		begin := len(c.img.Code)
		if err := c.expr(s.cond); err != nil {
			return err
		}
		fail := c.emitJump(opJumpTrue)
		c.loops = append(c.loops, compileLoop{kind: loopUntil, beginIP: begin, failAt: fail})
		return nil
	case stDim:
		for _, a := range s.arrays {
			if a.channel != nil {
				if err := c.expr(a.channel); err != nil {
					return err
				}
			}
			for _, b := range a.bounds {
				if err := c.expr(b); err != nil {
					return err
				}
			}
			if a.strLen != nil {
				if err := c.expr(a.strLen); err != nil {
					return err
				}
			}
			if a.channel != nil {
				c.emit(opDimVirt)
				c.emitU16(c.intern(a.name))
				c.emitU8(byte(len(a.bounds)))
				flags := byte(1)
				if a.strLen != nil {
					flags |= 2
				}
				c.emitU8(flags)
			} else {
				c.emit(opDim)
				c.emitU16(c.intern(a.name))
				c.emitU8(byte(len(a.bounds)))
			}
		}
		return nil
	case stRead:
		for _, t := range s.targets {
			for _, ix := range t.indices {
				if err := c.expr(ix); err != nil {
					return err
				}
			}
			c.emit(opRead)
			c.emitU16(c.intern(t.name))
			c.emitU8(byte(len(t.indices)))
		}
		return nil
	case stRestore:
		c.emit(opRestore)
		return nil
	case stEnd:
		c.emit(opEnd)
		return nil
	case stStop:
		c.emit(opStop)
		return nil
	case stOpen:
		if err := c.expr(s.path); err != nil {
			return err
		}
		if err := c.expr(s.channel); err != nil {
			return err
		}
		flags := byte(0)
		if s.recSize != nil {
			if err := c.expr(s.recSize); err != nil {
				return err
			}
			flags |= openRecSize
		}
		if s.org == "VIRTUAL" {
			flags |= openVirtual
		}
		if s.mapName != "" {
			flags |= openMap
		}
		c.emit(opOpen)
		c.emitU16(c.intern(s.mode))
		c.emitU8(flags)
		if s.mapName != "" {
			c.emitU16(c.intern(s.mapName))
		}
		return nil
	case stClose:
		if s.hasChan {
			if err := c.expr(s.channel); err != nil {
				return err
			}
			c.emit(opClose)
			c.emitU8(1)
		} else {
			c.emit(opClose)
			c.emitU8(0)
		}
		return nil
	case stRandomize:
		if s.hasSeed {
			if err := c.expr(s.seed); err != nil {
				return err
			}
			c.emit(opRandomize)
			c.emitU8(1)
		} else {
			c.emit(opRandomize)
			c.emitU8(0)
		}
		return nil
	case stOnGoto, stOnGosub:
		if err := c.expr(s.expr); err != nil {
			return err
		}
		for _, e := range s.lines {
			if err := c.expr(e); err != nil {
				return err
			}
		}
		c.emit(opOnJump)
		c.emitU8(byte(len(s.lines)))
		if s.kind == stOnGosub {
			c.emitU8(1)
		} else {
			c.emitU8(0)
		}
		return nil
	case stDef:
		c.emit(opDefFn)
		c.emitU16(c.intern(s.fnName))
		c.emitU8(byte(len(s.params)))
		for _, p := range s.params {
			c.emitU16(c.intern(p))
		}
		patch := c.holeU32()
		c.fns = append(c.fns, pendingFn{name: s.fnName, params: s.params, body: s.fnExpr, patch: patch})
		return nil
	case stDefMulti:
		if c.immediate {
			return basicErr("Illegal in immediate mode")
		}
		if len(c.defs) > 0 {
			return basicErr("DEF within DEF")
		}
		c.emit(opDefFn)
		c.emitU16(c.intern(s.fnName))
		c.emitU8(byte(len(s.params)))
		for _, p := range s.params {
			c.emitU16(c.intern(p))
		}
		body := c.holeU32()
		skip := c.emitJump(opJump)
		c.patchU32(body, uint32(len(c.img.Code)))
		c.defs = append(c.defs, openDef{name: s.fnName, skip: skip})
		return nil
	case stFnExit:
		if len(c.defs) == 0 {
			return basicErr("FNEXIT without DEF")
		}
		c.fnResult(c.defs[len(c.defs)-1].name)
		return nil
	case stFnEnd:
		if len(c.defs) == 0 {
			return basicErr("FNEND without DEF")
		}
		open := c.defs[len(c.defs)-1]
		c.defs = c.defs[:len(c.defs)-1]
		c.fnResult(open.name)
		c.patchU32(open.skip, uint32(len(c.img.Code)))
		return nil
	case stOnError:
		if err := c.expr(s.expr); err != nil {
			return err
		}
		c.emit(opOnError)
		return nil
	case stResume:
		if s.resumeNxt {
			c.emit(opResumeNext)
			return nil
		}
		if s.expr != nil {
			if err := c.expr(s.expr); err != nil {
				return err
			}
			c.emit(opResumeLine)
			return nil
		}
		c.emit(opResumeRetry)
		return nil
	case stChange:
		if strings.HasSuffix(s.toName, "$") {
			c.emit(opChangeStr)
			c.emitU16(c.intern(s.fromName))
			c.emitU16(c.intern(s.toName))
			return nil
		}
		if s.fromExpr != nil {
			if err := c.expr(s.fromExpr); err != nil {
				return err
			}
		} else {
			c.emit(opLoadVar)
			c.emitU16(c.intern(s.fromName))
		}
		c.emit(opChangeArr)
		c.emitU16(c.intern(s.toName))
		return nil
	case stGet, stPut:
		if err := c.expr(s.channel); err != nil {
			return err
		}
		flags := byte(0)
		if s.hasRec {
			if err := c.expr(s.record); err != nil {
				return err
			}
			flags = flagRec
		}
		if s.kind == stGet {
			c.emit(opGet)
		} else {
			c.emit(opPut)
		}
		c.emitU8(flags)
		return nil
	case stField:
		if err := c.expr(s.channel); err != nil {
			return err
		}
		for _, f := range s.fields {
			if err := c.expr(f.length); err != nil {
				return err
			}
		}
		c.emit(opField)
		c.emitU8(byte(len(s.fields)))
		for _, f := range s.fields {
			c.emitU16(c.intern(f.name))
		}
		return nil
	case stLset, stRset:
		if err := c.expr(s.expr); err != nil {
			return err
		}
		if s.kind == stLset {
			c.emit(opLset)
		} else {
			c.emit(opRset)
		}
		c.emitU16(c.intern(s.target.name))
		return nil
	case stMat:
		return c.mat(s)
	case stMap:
		return c.mapStmt(s)
	case stChain:
		if err := c.expr(s.path); err != nil {
			return err
		}
		flags := byte(0)
		if s.expr != nil {
			if err := c.expr(s.expr); err != nil {
				return err
			}
			flags = 1
		}
		c.emit(opChain)
		c.emitU8(flags)
		return nil
	case stSleep:
		if err := c.expr(s.expr); err != nil {
			return err
		}
		c.emit(opSleep)
		return nil
	case stKill:
		if err := c.expr(s.path); err != nil {
			return err
		}
		c.emit(opKill)
		return nil
	case stName:
		if err := c.expr(s.fromExpr); err != nil {
			return err
		}
		if err := c.expr(s.expr); err != nil {
			return err
		}
		c.emit(opName)
		return nil
	default:
		return basicErr("Syntax error")
	}
}

func (c *compiler) next(name string) error {
	if len(c.loops) == 0 {
		return basicErr("NEXT without FOR")
	}
	var popped []compileLoop
	if name != "" {
		for len(c.loops) > 0 {
			top := c.loops[len(c.loops)-1]
			c.loops = c.loops[:len(c.loops)-1]
			popped = append(popped, top)
			if top.kind == loopFor && top.name == name {
				break
			}
			if len(c.loops) == 0 && (top.kind != loopFor || top.name != name) {
				return basicErr("NEXT without FOR")
			}
		}
	} else {
		popped = append(popped, c.loops[len(c.loops)-1])
		c.loops = c.loops[:len(c.loops)-1]
	}
	match := popped[len(popped)-1]
	c.closeLoop(match, name)
	after := uint32(len(c.img.Code))
	for _, inner := range popped[:len(popped)-1] {
		c.patchU32(inner.failAt, after)
	}
	return nil
}

func (c *compiler) closeLoop(loop compileLoop, nextName string) {
	switch loop.kind {
	case loopFor:
		c.emit(opForNext)
		if nextName != "" {
			c.emitU16(c.intern(nextName))
		} else if loop.name != "" {
			c.emitU16(c.intern(loop.name))
		} else {
			c.emitU16(pcodeNoName)
		}
		c.patchU32(loop.failAt, uint32(len(c.img.Code)))
	case loopWhile, loopUntil:
		c.emit(opJump)
		c.emitU32(uint32(loop.beginIP))
		c.patchU32(loop.failAt, uint32(len(c.img.Code)))
	}
}

func (c *compiler) ifStmt(s stmt) error {
	if err := c.expr(s.cond); err != nil {
		return err
	}
	elseJump := c.emitJump(opJumpFalse)
	if err := c.branch(s.thenPart); err != nil {
		return err
	}
	if s.elsePart != nil {
		endJump := c.emitJump(opJump)
		c.patchU32(elseJump, uint32(len(c.img.Code)))
		if err := c.branch(s.elsePart); err != nil {
			return err
		}
		c.patchU32(endJump, uint32(len(c.img.Code)))
	} else {
		c.patchU32(elseJump, uint32(len(c.img.Code)))
	}
	return nil
}

func (c *compiler) branch(br *branch) error {
	if br == nil {
		return nil
	}
	if br.isLine {
		if err := c.expr(br.line); err != nil {
			return err
		}
		c.emit(opGoto)
		return nil
	}
	for _, s := range br.stmts {
		if err := c.stmt(s); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) print(s stmt) error {
	flags := byte(0)
	if s.hasChan {
		if err := c.expr(s.channel); err != nil {
			return err
		}
		flags |= flagChan
	}
	if s.trailing == "NONE" || s.trailing == "TAB" {
		flags |= flagNoNL
	}
	if s.hasUsing {
		if err := c.expr(s.using); err != nil {
			return err
		}
		n := 0
		for _, item := range s.items {
			if item.sep != "" {
				continue
			}
			if err := c.expr(item.expr); err != nil {
				return err
			}
			n++
		}
		c.emit(opPrintUsing)
		c.emitU8(byte(n))
		c.emitU8(flags)
		return nil
	}
	c.emit(opPrintStart)
	c.emitU8(flags)
	for _, item := range s.items {
		if item.sep == "TAB" {
			c.emit(opPrintComma)
			continue
		}
		if item.sep != "" {
			continue
		}
		if err := c.expr(item.expr); err != nil {
			return err
		}
		c.emit(opPrintItem)
	}
	c.emit(opPrintEnd)
	return nil
}

func (c *compiler) input(s stmt) error {
	if s.hasChan {
		if err := c.expr(s.channel); err != nil {
			return err
		}
		for _, t := range s.targets {
			for _, ix := range t.indices {
				if err := c.expr(ix); err != nil {
					return err
				}
			}
		}
		c.emit(opInputFile)
		c.emitU8(byte(len(s.targets)))
		for _, t := range s.targets {
			c.emitU16(c.intern(t.name))
			c.emitU8(byte(len(t.indices)))
		}
		return nil
	}
	prompt := uint16(0)
	flagsBase := byte(0)
	if s.hasPrompt && s.prompt != nil {
		prompt = c.intern(*s.prompt)
		flagsBase |= flagPrompt
	}
	for i, t := range s.targets {
		flags := flagsBase
		if i == 0 {
			flags |= flagFirst
		}
		for _, ix := range t.indices {
			if err := c.expr(ix); err != nil {
				return err
			}
		}
		c.emit(opInputOne)
		c.emitU16(c.intern(t.name))
		c.emitU8(byte(len(t.indices)))
		c.emitU8(flags)
		c.emitU16(prompt)
	}
	return nil
}

func (c *compiler) mat(s stmt) error {
	if s.matKind == "SCALE" {
		if err := c.expr(s.expr); err != nil {
			return err
		}
	}
	for _, b := range s.matBounds {
		if err := c.expr(b); err != nil {
			return err
		}
	}
	c.emit(opMat)
	switch s.matKind {
	case "READ":
		c.emitU8(matRead)
		c.emitMatNames(s.matNames)
	case "PRINT":
		c.emitU8(matPrint)
		tr := byte(0)
		if s.trailing == "TAB" {
			tr = 1
		} else if s.trailing == "NONE" {
			tr = 2
		}
		c.emitU8(tr)
		c.emitMatNames(s.matNames)
	case "INPUT":
		c.emitU8(matInput)
		c.emitMatNames(s.matNames)
	case "ZER":
		if len(s.matBounds) > 0 {
			c.emitU8(matZerRedim)
			c.emitU16(c.intern(s.matDest))
			c.emitU8(byte(len(s.matBounds)))
		} else {
			c.emitU8(matZer)
			c.emitU16(c.intern(s.matDest))
		}
	case "CON":
		if len(s.matBounds) > 0 {
			c.emitU8(matConRedim)
			c.emitU16(c.intern(s.matDest))
			c.emitU8(byte(len(s.matBounds)))
		} else {
			c.emitU8(matCon)
			c.emitU16(c.intern(s.matDest))
		}
	case "IDN":
		if len(s.matBounds) > 0 {
			c.emitU8(matIdnRedim)
			c.emitU16(c.intern(s.matDest))
			c.emitU8(byte(len(s.matBounds)))
		} else {
			c.emitU8(matIdn)
			c.emitU16(c.intern(s.matDest))
		}
	case "COPY":
		c.emitU8(matCopy)
		c.emitU16(c.intern(s.matDest))
		c.emitU16(c.intern(s.matLeft))
	case "ADD":
		c.emitU8(matAdd)
		c.emitU16(c.intern(s.matDest))
		c.emitU16(c.intern(s.matLeft))
		c.emitU16(c.intern(s.matRight))
	case "SUB":
		c.emitU8(matSub)
		c.emitU16(c.intern(s.matDest))
		c.emitU16(c.intern(s.matLeft))
		c.emitU16(c.intern(s.matRight))
	case "MUL":
		c.emitU8(matMul)
		c.emitU16(c.intern(s.matDest))
		c.emitU16(c.intern(s.matLeft))
		c.emitU16(c.intern(s.matRight))
	case "SCALE":
		c.emitU8(matScale)
		c.emitU16(c.intern(s.matDest))
		c.emitU16(c.intern(s.matLeft))
	case "TRN":
		c.emitU8(matTrn)
		c.emitU16(c.intern(s.matDest))
		c.emitU16(c.intern(s.matLeft))
	case "INV":
		c.emitU8(matInv)
		c.emitU16(c.intern(s.matDest))
		c.emitU16(c.intern(s.matLeft))
	default:
		return basicErr("Syntax error")
	}
	return nil
}

func (c *compiler) emitMatNames(names []string) {
	c.emitU8(byte(len(names)))
	for _, n := range names {
		c.emitU16(c.intern(n))
	}
}

func (c *compiler) mapStmt(s stmt) error {
	for _, f := range s.mapFields {
		if f.size != nil {
			if err := c.expr(f.size); err != nil {
				return err
			}
		}
	}
	c.emit(opMap)
	c.emitU16(c.intern(s.mapName))
	c.emitU8(byte(len(s.mapFields)))
	for _, f := range s.mapFields {
		c.emitU16(c.intern(f.typ))
		c.emitU16(c.intern(f.name))
		if f.size != nil {
			c.emitU8(1)
		} else {
			c.emitU8(0)
		}
	}
	return nil
}

func (c *compiler) store(t *varRef) error {
	if t == nil {
		c.emit(opPop)
		return nil
	}
	if len(t.indices) == 0 {
		c.emit(opStoreVar)
		c.emitU16(c.intern(t.name))
		return nil
	}
	for _, ix := range t.indices {
		if err := c.expr(ix); err != nil {
			return err
		}
	}
	c.emit(opStoreArr)
	c.emitU16(c.intern(t.name))
	c.emitU8(byte(len(t.indices)))
	return nil
}

func (c *compiler) expr(e expr) error {
	switch n := e.(type) {
	case numLit:
		if n.v == 1 {
			c.emit(opPush1)
			return nil
		}
		c.emit(opPushNum)
		c.emitU16(c.internNum(n.v))
		return nil
	case strLit:
		c.emit(opPushStr)
		c.emitU16(c.intern(n.v))
		return nil
	case varRef:
		if len(n.indices) == 0 {
			c.emit(opLoadVar)
			c.emitU16(c.intern(n.name))
			return nil
		}
		for _, ix := range n.indices {
			if err := c.expr(ix); err != nil {
				return err
			}
		}
		c.emit(opLoadArr)
		c.emitU16(c.intern(n.name))
		c.emitU8(byte(len(n.indices)))
		return nil
	case unaryExpr:
		if err := c.expr(n.x); err != nil {
			return err
		}
		switch n.op {
		case "-":
			c.emit(opNeg)
		case "+":
			c.emit(opPos)
		case "NOT":
			c.emit(opNot)
		default:
			return basicErr("Syntax error")
		}
		return nil
	case binExpr:
		if err := c.expr(n.l); err != nil {
			return err
		}
		if err := c.expr(n.r); err != nil {
			return err
		}
		op, ok := binOpCode(n.op)
		if !ok {
			return basicErr("Syntax error")
		}
		c.emit(op)
		return nil
	case callExpr:
		for _, a := range n.args {
			if err := c.expr(a); err != nil {
				return err
			}
		}
		c.emit(opCall)
		c.emitU16(c.intern(n.name))
		c.emitU8(byte(len(n.args)))
		return nil
	default:
		return basicErr("Syntax error")
	}
}

func binOpCode(op string) (byte, bool) {
	switch op {
	case "+":
		return opAdd, true
	case "-":
		return opSub, true
	case "*":
		return opMul, true
	case "/":
		return opDiv, true
	case `\`:
		return opIDiv, true
	case "MOD":
		return opMod, true
	case "^":
		return opPow, true
	case "=":
		return opEq, true
	case "<>":
		return opNe, true
	case "<":
		return opLt, true
	case "<=":
		return opLe, true
	case ">":
		return opGt, true
	case ">=":
		return opGe, true
	case "AND":
		return opAnd, true
	case "OR":
		return opOr, true
	}
	return 0, false
}

// fnResult returns from a multi-line function. Its value is whatever the
// body assigned to the function's own name, which is how BASIC-PLUS
// carries a result out of a DEF.
func (c *compiler) fnResult(name string) {
	c.emit(opLoadVar)
	c.emitU16(c.intern(name))
	c.emit(opFnReturn)
}

func (c *compiler) emitFuncs() error {
	for _, fn := range c.fns {
		c.patchU32(fn.patch, uint32(len(c.img.Code)))
		if err := c.expr(fn.body); err != nil {
			return err
		}
		c.emit(opFnReturn)
	}
	return nil
}
