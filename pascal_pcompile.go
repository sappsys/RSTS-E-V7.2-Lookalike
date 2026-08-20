package rsts

type pacC struct {
	img     *pacImage
	code    []pacInst
	typMap  map[*pType]int32
	procMap map[*pProcDecl]int32
	strMap  map[string]int32
	procs   []*pProcDecl
	labels  map[int64]pacLab
	fixups  []pacFix
	withN   int
	withs   []*pType
	nslot   int
	level   int
	err     error
	prog    *pProgram
}

type pacLab struct {
	ip, with int
}

type pacFix struct {
	at, with int
	lab      int64
}

func compilePascalImage(prog *pProgram) (*pacImage, error) {
	c := &pacC{
		img:     &pacImage{},
		typMap:  map[*pType]int32{},
		procMap: map[*pProcDecl]int32{},
		strMap:  map[string]int32{},
		prog:    prog,
	}
	c.img.IntType = c.ty(prog.intType)
	c.img.RealType = c.ty(prog.realType)
	c.img.BoolType = c.ty(prog.boolType)
	c.img.CharType = c.ty(prog.charType)
	c.img.TextType = c.ty(prog.textType)
	c.img.NilType = c.ty(prog.nilType)
	c.registerProcs(prog.block)
	c.emitAllProcs()
	c.labels = map[int64]pacLab{}
	c.fixups = nil
	c.withN = 0
	c.withs = nil
	c.nslot = prog.block.nslot
	c.level = 1
	c.img.Entry = len(c.code)
	c.emitStmt(prog.block.body)
	c.patch()
	c.emitOp(poHalt, 0, 0, 0, 0)
	c.img.Code = c.code
	c.img.MainNSlot = c.nslot
	c.freezeTypes()
	if c.err != nil {
		return nil, c.err
	}
	return c.img, nil
}

func (c *pacC) fail(msg string, line, col int) {
	if c.err == nil {
		c.err = pasErr(msg, line, col)
	}
}

func (c *pacC) intern(s string) int32 {
	if i, ok := c.strMap[s]; ok {
		return i
	}
	i := int32(len(c.img.Strings))
	c.img.Strings = append(c.img.Strings, s)
	c.strMap[s] = i
	return i
}

func (c *pacC) ty(t *pType) int32 {
	if t == nil {
		return 0
	}
	if id, ok := c.typMap[t]; ok {
		return id
	}
	id := int32(len(c.typMap) + 1)
	c.typMap[t] = id
	c.ty(t.elem)
	c.ty(t.index)
	c.ty(t.ptrTo)
	c.ty(t.base)
	c.ty(t.tagType)
	for _, f := range t.fields {
		c.ty(f.typ)
	}
	return id
}

func (c *pacC) freezeTypes() {
	n := len(c.typMap)
	info := make([]pacTypeInfo, n)
	types := make([]*pType, n+1)
	for t, id := range c.typMap {
		types[id] = t
		ti := pacTypeInfo{
			Kind: int32(t.kind), Lo: t.lo, Hi: t.hi, Name: t.name,
			Packed: t.packed, Conf: t.conf, Enums: t.enums,
			TagName: t.tagName, PtrName: t.ptrName,
			Elem: c.typMap[t.elem], Index: c.typMap[t.index],
			PtrTo: c.typMap[t.ptrTo], Base: c.typMap[t.base], TagType: c.typMap[t.tagType],
		}
		for _, f := range t.fields {
			ti.Fields = append(ti.Fields, pacFieldInfo{Name: f.name, Typ: c.typMap[f.typ]})
		}
		info[id-1] = ti
	}
	c.img.TypeInfo = info
	c.img.Types = types
}

func (c *pacC) registerProcs(b *pBlock) {
	if b == nil {
		return
	}
	for _, pr := range b.decl.procs {
		if pr.fwd || pr.block == nil {
			continue
		}
		if _, ok := c.procMap[pr]; !ok {
			c.procMap[pr] = int32(len(c.procs))
			c.procs = append(c.procs, pr)
		}
		c.registerProcs(pr.block)
	}
}

func (c *pacC) emitAllProcs() {
	c.img.Procs = make([]pacProcInfo, len(c.procs))
	for i, pr := range c.procs {
		c.img.Procs[i] = c.procInfo(pr)
	}
	for i, pr := range c.procs {
		c.labels = map[int64]pacLab{}
		c.fixups = nil
		c.withN = 0
		c.withs = nil
		c.nslot = pr.nslot
		c.level = 1
		if pr.sym != nil {
			c.level = pr.sym.level + 1
		}
		c.img.Procs[i].IP = len(c.code)
		if pr.block != nil {
			c.emitStmt(pr.block.body)
		}
		c.patch()
		c.emitOp(poRet, 0, 0, 0, 0)
		c.img.Procs[i].NSlot = int32(c.nslot)
	}
}

func (c *pacC) procInfo(pr *pProcDecl) pacProcInfo {
	info := pacProcInfo{Name: pr.name, NSlot: int32(pr.nslot), RetOff: int32(pr.retOff), RetType: -1}
	if pr.sym != nil {
		info.Level = int32(pr.sym.level)
		info.IsFunc = pr.sym.kind == skFunc
		info.RetType = c.ty(pr.sym.ret)
		for _, ps := range pr.sym.params {
			pm := pacParamInfo{Off: int32(ps.offset), ByRef: ps.byRef, Proc: ps.procParam || ps.kind == skProc || ps.kind == skFunc, Typ: c.ty(ps.typ)}
			for _, b := range ps.confB {
				pm.Conf = append(pm.Conf, int32(b.offset))
			}
			info.Params = append(info.Params, pm)
		}
	}
	return info
}

func (c *pacC) emitOp(op byte, a, b int32, i int64, f float64) int {
	idx := len(c.code)
	c.code = append(c.code, pacInst{Op: op, A: a, B: b, I: i, F: f})
	return idx
}

func (c *pacC) patch() {
	for _, f := range c.fixups {
		lab, ok := c.labels[f.lab]
		if !ok {
			c.fail("undefined label", 0, 0)
			continue
		}
		pops := int32(f.with - lab.with)
		if pops < 0 {
			pops = 0
		}
		c.code[f.at].A = int32(lab.ip)
		c.code[f.at].B = pops
	}
}

func (c *pacC) emitStmt(s *pStmt) {
	if s == nil || c.err != nil {
		return
	}
	switch s.kind {
	case pstEmpty:
	case pstCompound:
		for _, x := range s.list {
			c.emitStmt(x)
		}
	case pstLabel:
		c.labels[s.lab] = pacLab{ip: len(c.code), with: c.withN}
		c.emitStmt(s.body)
	case pstGoto:
		at := c.emitOp(poJmp, 0, 0, 0, 0)
		c.fixups = append(c.fixups, pacFix{at: at, lab: s.lab, with: c.withN})
	case pstAssign:
		c.emitAddr(s.lhs)
		c.emitExpr(s.rhs)
		c.emitOp(poStore, 0, 0, int64(s.line), 0)
	case pstCall:
		c.emitExpr(s.lhs)
		c.emitOp(poPop, 0, 0, 0, 0)
	case pstIf:
		c.emitExpr(s.cond)
		jz := c.emitOp(poJz, 0, 0, 0, 0)
		c.emitStmt(s.body)
		if s.els != nil {
			jmp := c.emitOp(poJmp, 0, 0, 0, 0)
			c.code[jz].A = int32(len(c.code))
			c.emitStmt(s.els)
			c.code[jmp].A = int32(len(c.code))
		} else {
			c.code[jz].A = int32(len(c.code))
		}
	case pstWhile:
		loop := len(c.code)
		c.emitExpr(s.cond)
		jz := c.emitOp(poJz, 0, 0, 0, 0)
		c.emitStmt(s.body)
		c.emitOp(poJmp, int32(loop), 0, 0, 0)
		c.code[jz].A = int32(len(c.code))
	case pstRepeat:
		loop := len(c.code)
		for _, x := range s.list {
			c.emitStmt(x)
		}
		c.emitExpr(s.cond)
		jz := c.emitOp(poJz, 0, 0, 0, 0)
		c.code[jz].A = int32(loop)
	case pstFor:
		c.emitFor(s)
	case pstCase:
		c.emitCase(s)
	case pstWith:
		n := 0
		for _, w := range s.list {
			if w != nil && w.lhs != nil {
				c.emitAddr(w.lhs)
				c.emitOp(poWithPush, 0, 0, 0, 0)
				c.withs = append(c.withs, unwrapType(w.lhs.typ))
				c.withN++
				n++
			}
		}
		c.emitStmt(s.body)
		for i := 0; i < n; i++ {
			c.emitOp(poWithPop, 0, 0, 0, 0)
			c.withN--
			if len(c.withs) > 0 {
				c.withs = c.withs[:len(c.withs)-1]
			}
		}
	}
}

func (c *pacC) emitFor(s *pStmt) {
	lim := c.nslot
	c.nslot++
	c.emitAddr(s.lhs)
	c.emitExpr(s.rhs)
	c.emitOp(poStore, 0, 0, int64(s.line), 0)
	c.emitExpr(s.cond)
	c.emitOp(poAddrVar, int32(c.level), int32(lim), int64(c.img.IntType), 0)
	c.emitOp(poStore, 0, 0, 0, 0)
	step := int64(1)
	if s.downto {
		step = -1
	}
	loop := len(c.code)
	c.emitAddr(s.lhs)
	c.emitOp(poLoad, 0, 0, 0, 0)
	c.emitOp(poAddrVar, int32(c.level), int32(lim), int64(c.img.IntType), 0)
	c.emitOp(poLoad, 0, 0, 0, 0)
	if s.downto {
		c.emitOp(poBin, int32(tkGe), 0, 0, 0)
	} else {
		c.emitOp(poBin, int32(tkLe), 0, 0, 0)
	}
	jz := c.emitOp(poJz, 0, 0, 0, 0)
	c.emitStmt(s.body)
	c.emitAddr(s.lhs)
	c.emitOp(poDupAddr, 0, 0, 0, 0)
	c.emitOp(poLoad, 0, 0, 0, 0)
	c.emitOp(poPushI, 0, 0, step, 0)
	c.emitOp(poBin, int32(tkPlus), 0, 0, 0)
	c.emitOp(poStore, 0, 1, 0, 0)
	c.emitOp(poJmp, int32(loop), 0, 0, 0)
	c.code[jz].A = int32(len(c.code))
}

func (c *pacC) emitCase(s *pStmt) {
	c.emitExpr(s.cond)
	var ends []int
	for _, arm := range s.arms {
		matched := -1
		for _, k := range arm.consts {
			c.emitOp(poDup, 0, 0, 0, 0)
			if k.op == exRange {
				c.emitExpr(k.x)
				c.emitOp(poBin, int32(tkGe), 0, 0, 0)
				loFail := c.emitOp(poJz, 0, 0, 0, 0)
				c.emitOp(poDup, 0, 0, 0, 0)
				c.emitExpr(k.y)
				c.emitOp(poBin, int32(tkLe), 0, 0, 0)
				hit := c.emitOp(poJz, 0, 0, 0, 0)
				if matched < 0 {
					matched = c.emitOp(poJmp, 0, 0, 0, 0)
				} else {
					c.emitOp(poJmp, int32(matched), 0, 0, 0)
				}
				c.code[loFail].A = int32(len(c.code))
				c.code[hit].A = int32(len(c.code))
			} else {
				c.emitExpr(k)
				c.emitOp(poBin, int32(tkEq), 0, 0, 0)
				miss := c.emitOp(poJz, 0, 0, 0, 0)
				if matched < 0 {
					matched = c.emitOp(poJmp, 0, 0, 0, 0)
				} else {
					c.emitOp(poJmp, int32(matched), 0, 0, 0)
				}
				c.code[miss].A = int32(len(c.code))
			}
		}
		nextArm := c.emitOp(poJmp, 0, 0, 0, 0)
		if matched >= 0 {
			c.code[matched].A = int32(len(c.code))
		}
		c.emitOp(poPop, 0, 0, 0, 0)
		c.emitStmt(arm.body)
		ends = append(ends, c.emitOp(poJmp, 0, 0, 0, 0))
		c.code[nextArm].A = int32(len(c.code))
	}
	if s.other != nil {
		c.emitOp(poPop, 0, 0, 0, 0)
		c.emitStmt(s.other)
	} else {
		c.emitOp(poCaseFail, 0, 0, int64(s.line), 0)
	}
	end := len(c.code)
	for _, e := range ends {
		c.code[e].A = int32(end)
	}
}

func (c *pacC) emitExpr(e *pExpr) {
	if e == nil || c.err != nil {
		return
	}
	switch e.op {
	case exInt:
		c.emitOp(poPushI, 0, int32(c.img.IntType), e.ival, 0)
	case exReal:
		c.emitOp(poPushF, 0, int32(c.img.RealType), 0, e.fval)
	case exChar:
		c.emitOp(poPushC, 0, int32(c.img.CharType), e.ival, 0)
	case exString:
		c.emitOp(poPushS, c.intern(e.sval), c.ty(e.typ), 0, 0)
	case exBool:
		c.emitOp(poPushB, 0, int32(c.img.BoolType), e.ival, 0)
	case exNil:
		c.emitOp(poPushNil, 0, int32(c.img.NilType), 0, 0)
	case exIdent:
		c.emitIdentR(e)
	case exCall:
		c.emitCall(e, true)
	case exUnary:
		c.emitExpr(e.x)
		c.emitOp(poUn, int32(e.tok), 0, 0, 0)
	case exBinary:
		c.emitExpr(e.x)
		c.emitExpr(e.y)
		c.emitOp(poBin, int32(e.tok), 0, int64(e.line), 0)
	case exIndex, exField, exDeref:
		c.emitAddr(e)
		c.emitOp(poLoad, 0, 0, 0, 0)
	case exSet:
		c.emitOp(poPushSet, 0, c.ty(e.typ), 0, 0)
		for _, el := range e.elems {
			if el.op == exRange {
				c.emitExpr(el.x)
				c.emitExpr(el.y)
				c.emitOp(poSetRange, 0, 0, 0, 0)
			} else {
				c.emitExpr(el)
				c.emitOp(poSetIncl, 0, 0, 0, 0)
			}
		}
	}
}

func (c *pacC) emitIdentR(e *pExpr) {
	sy := e.sym
	if sy == nil {
		c.emitAddr(e)
		c.emitOp(poLoad, 0, 0, 0, 0)
		return
	}
	if sy.kind == skConst {
		v := sy.cval
		t := c.ty(v.typ)
		ut := unwrapType(v.typ)
		if ut != nil && ut.kind == tyReal {
			c.emitOp(poPushF, 0, t, 0, v.f)
			return
		}
		if ut != nil && ut.kind == tyChar {
			c.emitOp(poPushC, 0, t, v.i, 0)
			return
		}
		if ut != nil && ut.kind == tyBoolean {
			c.emitOp(poPushB, 0, t, v.i, 0)
			return
		}
		if ut != nil && (ut.kind == tySet || len(v.bits) > 0) {
			c.emitOp(poPushSet, 0, t, 0, 0)
			for i := int64(0); i < pascalSetSpan; i++ {
				if inSet(v, i) {
					c.emitOp(poPushI, 0, int32(c.img.IntType), i, 0)
					c.emitOp(poSetIncl, 0, 0, 0, 0)
				}
			}
			return
		}
		if ut != nil && ut.isPackedCharArray() || v.s != "" {
			c.emitOp(poPushS, c.intern(v.s), t, 0, 0)
			return
		}
		c.emitOp(poPushI, 0, t, v.i, 0)
		return
	}
	if sy.kind == skStd {
		c.emitStd(&pExpr{op: exCall, name: e.name, sym: sy, line: e.line, col: e.col, typ: e.typ})
		return
	}
	if sy.kind == skProc || sy.kind == skFunc {
		if sy.procParam {
			c.emitAddr(e)
			c.emitOp(poLoad, 0, 0, 0, 0)
			c.emitOp(poCallVal, 0, 0, 0, 0)
			return
		}
		c.emitCall(&pExpr{op: exCall, name: e.name, sym: sy, line: e.line, col: e.col, typ: e.typ}, true)
		return
	}
	c.emitAddr(e)
	c.emitOp(poLoad, 0, 0, 0, 0)
}

func (c *pacC) emitAddr(e *pExpr) {
	if e == nil {
		return
	}
	switch e.op {
	case exIdent:
		if c.emitWithField(e.name) {
			return
		}
		sy := e.sym
		if sy == nil {
			c.fail("unknown identifier "+e.name, e.line, e.col)
			return
		}
		c.emitOp(poAddrVar, int32(sy.level), int32(sy.offset), int64(c.ty(sy.typ)), 0)
	case exIndex:
		c.emitAddr(e.x)
		for _, ix := range e.args {
			c.emitExpr(ix)
		}
		c.emitOp(poAddrIdx, int32(len(e.args)), 0, 0, 0)
	case exField:
		c.emitAddr(e.x)
		idx := int32(0)
		t := unwrapType(e.x.typ)
		if t != nil {
			for i, f := range t.fields {
				if f.name == e.field {
					idx = int32(i)
					break
				}
			}
		}
		c.emitOp(poAddrField, idx, 0, 0, 0)
	case exDeref:
		c.emitAddr(e.x)
		c.emitOp(poAddrDeref, 0, 0, int64(e.line), 0)
	default:
		c.emitExpr(e)
		c.fail("not an address", e.line, e.col)
	}
}

func (c *pacC) emitWithField(name string) bool {
	for i := len(c.withs) - 1; i >= 0; i-- {
		t := c.withs[i]
		if t == nil {
			continue
		}
		for _, f := range t.fields {
			if f.name == name {
				c.emitOp(poAddrWith, c.intern(name), int32(len(c.withs)-1-i), 0, 0)
				return true
			}
		}
	}
	return false
}

func (c *pacC) emitCall(e *pExpr, keep bool) {
	sy := e.sym
	if sy != nil && sy.kind == skStd {
		c.emitStd(e)
		if !keep {
			c.emitOp(poPop, 0, 0, 0, 0)
		}
		return
	}
	if sy != nil && sy.procParam {
		for i, a := range e.args {
			if i < len(sy.params) {
				ps := sy.params[i]
				if ps.procParam || ps.kind == skProc || ps.kind == skFunc {
					c.emitProcRef(a)
					continue
				}
				if ps.byRef {
					c.emitAddr(a)
					continue
				}
			}
			c.emitExpr(a)
		}
		c.emitOp(poAddrVar, int32(sy.level), int32(sy.offset), int64(c.ty(sy.typ)), 0)
		c.emitOp(poLoad, 0, 0, 0, 0)
		c.emitOp(poCallVal, 0, int32(len(e.args)), 0, 0)
		if !keep {
			c.emitOp(poPop, 0, 0, 0, 0)
		}
		return
	}
	id, ok := c.procID(sy)
	if !ok {
		c.fail("unknown procedure "+e.name, e.line, e.col)
		return
	}
	pr := c.img.Procs[id]
	for i, a := range e.args {
		var pm pacParamInfo
		if i < len(pr.Params) {
			pm = pr.Params[i]
		}
		if pm.Proc {
			c.emitProcRef(a)
		} else if pm.ByRef {
			c.emitAddr(a)
		} else {
			c.emitExpr(a)
		}
	}
	c.emitOp(poCall, id, int32(len(e.args)), 0, 0)
	if !keep {
		c.emitOp(poPop, 0, 0, 0, 0)
	}
}

func (c *pacC) procID(sy *pSym) (int32, bool) {
	if sy == nil {
		return 0, false
	}
	if sy.proc != nil {
		if id, ok := c.procMap[sy.proc]; ok {
			return id, true
		}
	}
	for i, pr := range c.procs {
		if pr.sym == sy || (pr.name == sy.name && pr.sym == sy) {
			return int32(i), true
		}
	}
	return 0, false
}

func (c *pacC) emitProcRef(e *pExpr) {
	if e == nil || e.sym == nil {
		c.fail("procedure expected", 0, 0)
		return
	}
	if e.sym.procParam {
		c.emitOp(poAddrVar, int32(e.sym.level), int32(e.sym.offset), 0, 0)
		c.emitOp(poLoad, 0, 0, 0, 0)
		return
	}
	id, ok := c.procID(e.sym)
	if !ok {
		c.fail("procedure expected", e.line, e.col)
		return
	}
	c.emitOp(poProcRef, id, 0, 0, 0)
}

func (c *pacC) emitStd(e *pExpr) {
	name := e.sym.std
	args := e.args
	switch name {
	case "WRITE", "WRITELN":
		i := 0
		if len(args) > 0 {
			u := unwrapType(args[0].typ)
			if u != nil && (u.kind == tyText || u.kind == tyFile) {
				c.emitExpr(args[0])
				i = 1
			}
		}
		if i == 0 {
			c.emitOp(poAddrVar, 0, 1, int64(c.img.TextType), 0)
			c.emitOp(poLoad, 0, 0, 0, 0)
		}
		nitem := 0
		for ; i < len(args); i++ {
			c.emitExpr(args[i])
			c.emitWidthPrec(args[i])
			nitem++
		}
		c.emitOp(poStd, c.intern(name), int32(nitem), int64(e.line), 0)
	case "READ", "READLN":
		i := 0
		if len(args) > 0 {
			u := unwrapType(args[0].typ)
			if u != nil && (u.kind == tyText || u.kind == tyFile) {
				c.emitExpr(args[0])
				i = 1
			}
		}
		if i == 0 {
			c.emitOp(poAddrVar, 0, 0, int64(c.img.TextType), 0)
			c.emitOp(poLoad, 0, 0, 0, 0)
		}
		nd := 0
		for ; i < len(args); i++ {
			c.emitAddr(args[i])
			nd++
		}
		c.emitOp(poStd, c.intern(name), int32(nd), int64(e.line), 0)
	case "NEW":
		if len(args) > 0 {
			c.emitAddr(args[0])
		}
		for i := 1; i < len(args); i++ {
			c.emitExpr(args[i])
		}
		c.emitOp(poStd, c.intern(name), int32(len(args)), int64(e.line), 0)
	case "DISPOSE":
		if len(args) > 0 {
			c.emitAddr(args[0])
		}
		c.emitOp(poStd, c.intern(name), int32(len(args)), int64(e.line), 0)
	case "RESET", "REWRITE":
		if len(args) > 0 {
			c.emitAddr(args[0])
		}
		if len(args) > 1 {
			c.emitExpr(args[1])
		} else {
			nm := "FILE.DAT"
			if len(args) > 0 && args[0].name != "" {
				nm = args[0].name + ".DAT"
			}
			c.emitOp(poPushS, c.intern(nm), 0, 0, 0)
		}
		c.emitOp(poStd, c.intern(name), 1, int64(e.line), 0)
	case "GET":
		if len(args) > 0 {
			c.emitExpr(args[0])
		} else {
			c.emitOp(poAddrVar, 0, 0, int64(c.img.TextType), 0)
			c.emitOp(poLoad, 0, 0, 0, 0)
		}
		c.emitOp(poStd, c.intern(name), 1, int64(e.line), 0)
	case "PUT":
		if len(args) > 0 {
			c.emitExpr(args[0])
		}
		c.emitOp(poStd, c.intern(name), 1, int64(e.line), 0)
	case "PACK":
		if len(args) >= 3 {
			c.emitAddr(args[0])
			c.emitExpr(args[1])
			c.emitAddr(args[2])
		}
		c.emitOp(poStd, c.intern(name), 3, int64(e.line), 0)
	case "UNPACK":
		if len(args) >= 3 {
			c.emitAddr(args[0])
			c.emitAddr(args[1])
			c.emitExpr(args[2])
		}
		c.emitOp(poStd, c.intern(name), 3, int64(e.line), 0)
	case "EOF", "EOLN":
		if len(args) > 0 {
			c.emitExpr(args[0])
		} else {
			c.emitOp(poAddrVar, 0, 0, int64(c.img.TextType), 0)
			c.emitOp(poLoad, 0, 0, 0, 0)
		}
		c.emitOp(poStd, c.intern(name), 1, int64(e.line), 0)
	default:
		for _, a := range args {
			c.emitExpr(a)
		}
		c.emitOp(poStd, c.intern(name), int32(len(args)), int64(e.line), 0)
	}
}

func (c *pacC) emitWidthPrec(a *pExpr) {
	if a != nil && a.width != nil {
		c.emitExpr(a.width)
	} else {
		c.emitOp(poPushI, 0, int32(c.img.IntType), 0, 0)
	}
	if a != nil && a.prec != nil {
		c.emitExpr(a.prec)
	} else {
		c.emitOp(poPushI, 0, int32(c.img.IntType), -1, 0)
	}
}
