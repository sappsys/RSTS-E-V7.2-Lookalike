package rsts

func checkPascal(prog *pProgram) error {
	c := &pChecker{prog: prog}
	c.intType = &pType{kind: tyInteger, name: "INTEGER"}
	c.realType = &pType{kind: tyReal, name: "REAL"}
	c.boolType = &pType{kind: tyBoolean, name: "BOOLEAN"}
	c.charType = &pType{kind: tyChar, name: "CHAR"}
	c.textType = &pType{kind: tyText, name: "TEXT"}
	c.nilType = &pType{kind: tyNil, name: "NIL"}
	prog.intType, prog.realType, prog.boolType = c.intType, c.realType, c.boolType
	prog.charType, prog.textType, prog.nilType = c.charType, c.textType, c.nilType
	c.push()
	c.declType("INTEGER", c.intType)
	c.declType("REAL", c.realType)
	c.declType("BOOLEAN", c.boolType)
	c.declType("CHAR", c.charType)
	c.declType("TEXT", c.textType)
	c.declConst("MAXINT", c.intType, pVal{typ: c.intType, i: pascalMaxInt})
	c.declConst("TRUE", c.boolType, pVal{typ: c.boolType, i: 1})
	c.declConst("FALSE", c.boolType, pVal{typ: c.boolType, i: 0})
	for _, n := range []string{
		"ABS", "SQR", "SIN", "COS", "EXP", "LN", "SQRT", "ARCTAN",
		"TRUNC", "ROUND", "ORD", "CHR", "SUCC", "PRED", "ODD", "EOF", "EOLN",
		"READ", "READLN", "WRITE", "WRITELN", "NEW", "DISPOSE",
		"RESET", "REWRITE", "PAGE", "PACK", "UNPACK", "GET", "PUT",
	} {
		c.declStd(n)
	}
	in := c.declVar("INPUT", c.textType)
	out := c.declVar("OUTPUT", c.textType)
	_ = in
	_ = out
	if err := c.checkBlock(prog.block); err != nil {
		return err
	}
	c.resolvePtrs()
	if c.err != nil {
		return c.err
	}
	return nil
}

type pChecker struct {
	prog                                                     *pProgram
	scope                                                    *pScope
	intType, realType, boolType, charType, textType, nilType *pType
	err                                                      error
	ptrs                                                     []*pType
	withs                                                    []*pType
	curFn                                                    *pProcDecl
	path                                                     []*pStmt
	labAt                                                    map[int64][]*pStmt
	gotos                                                    []pGotoRef
	okLabs                                                   map[int64]bool
}

type pGotoRef struct {
	lab       int64
	path      []*pStmt
	line, col int
}

func (c *pChecker) fail(msg string, line, col int) {
	if c.err == nil {
		c.err = pasErr(msg, line, col)
	}
}

func (c *pChecker) push() {
	s := &pScope{parent: c.scope, syms: map[string]*pSym{}}
	if c.scope != nil {
		s.level = c.scope.level + 1
		s.nslot = 0
	}
	c.scope = s
	c.prog.scopes = append(c.prog.scopes, s)
}

func (c *pChecker) pop() { c.scope = c.scope.parent }

func (c *pChecker) lookup(name string) *pSym {
	for _, w := range c.withs {
		if w != nil && w.kind == tyRecord {
			for _, f := range w.fields {
				if f.name == name {
					return &pSym{name: name, kind: skVar, typ: f.typ, std: "FIELD"}
				}
			}
		}
	}
	for s := c.scope; s != nil; s = s.parent {
		if sy, ok := s.syms[name]; ok {
			return sy
		}
	}
	return nil
}

func (c *pChecker) declare(name string, sy *pSym) {
	sy.name = name
	sy.level = c.scope.level
	if old, exists := c.scope.syms[name]; exists {
		if (sy.kind == skProc || sy.kind == skFunc) && old.proc != nil && old.proc.fwd {
			c.scope.syms[name] = sy
			return
		}
		c.fail("duplicate identifier "+name, 0, 0)
		return
	}
	c.scope.syms[name] = sy
}

func (c *pChecker) declType(name string, t *pType) {
	t.name = name
	c.declare(name, &pSym{kind: skType, typ: t})
}

func (c *pChecker) declConst(name string, t *pType, v pVal) {
	c.declare(name, &pSym{kind: skConst, typ: t, cval: v})
}

func (c *pChecker) declVar(name string, t *pType) *pSym {
	sy := &pSym{kind: skVar, typ: t, offset: c.scope.nslot}
	c.scope.nslot++
	c.declare(name, sy)
	return sy
}

func (c *pChecker) declStd(name string) {
	c.declare(name, &pSym{kind: skStd, std: name})
}

func (c *pChecker) checkBlock(b *pBlock) error {
	if b == nil {
		return nil
	}
	c.push()
	defer c.pop()
	for _, d := range b.decl.consts {
		v, t := c.constVal(d.val)
		c.declConst(d.name, t, v)
	}
	for _, d := range b.decl.types {
		t := c.resolveType(d.typ)
		c.declType(d.name, t)
	}
	c.resolvePtrs()
	for _, d := range b.decl.vars {
		t := c.resolveType(d.typ)
		for _, n := range d.names {
			c.declVar(n, t)
		}
	}
	for _, pr := range b.decl.procs {
		c.declProcHeader(pr)
	}
	for _, pr := range b.decl.procs {
		if pr.fwd || pr.block == nil {
			continue
		}
		c.checkProcBody(pr)
	}
	c.checkStmts(b)
	return c.err
}

func (c *pChecker) declProcHeader(pr *pProcDecl) {
	kind := skProc
	if pr.ret != nil {
		kind = skFunc
	}
	sy := &pSym{kind: kind, proc: pr, std: ""}
	if pr.ret != nil {
		sy.ret = c.resolveType(pr.ret)
		sy.typ = sy.ret
	}
	// If this completes a FORWARD, reuse the existing symbol.
	if old := c.lookup(pr.name); old != nil && (old.kind == skProc || old.kind == skFunc) && old.proc != nil && old.proc.fwd {
		if len(pr.params) == 0 {
			pr.params = old.proc.params
		}
		if pr.ret == nil {
			pr.ret = old.proc.ret
		}
		old.proc = pr
		pr.sym = old
		if sy.ret != nil {
			old.ret = sy.ret
			old.typ = sy.ret
		}
		return
	}
	c.declare(pr.name, sy)
	pr.sym = sy
}

func (c *pChecker) checkProcBody(pr *pProcDecl) {
	prev := c.curFn
	c.curFn = pr
	c.push()
	sy := pr.sym
	if sy == nil {
		sy = c.lookup(pr.name)
		pr.sym = sy
	}
	for _, pm := range pr.params {
		if pm.procHead != nil {
			s := c.bindProcParam(pm.procHead)
			if sy != nil {
				sy.params = append(sy.params, s)
			}
			continue
		}
		t := c.resolveType(pm.typ)
		for _, n := range pm.names {
			s := c.declVar(n, t)
			s.byRef = pm.byRef
			s.param = true
			c.bindConformant(s, t)
			if sy != nil {
				sy.params = append(sy.params, s)
			}
		}
	}
	if pr.ret != nil {
		s := c.declVar(pr.name, c.resolveType(pr.ret))
		pr.retOff = s.offset
	}
	_ = c.checkBlockInner(pr.block)
	pr.nslot = c.scope.nslot
	if pr.block != nil {
		pr.block.nslot = c.scope.nslot
	}
	c.pop()
	c.curFn = prev
}

func (c *pChecker) bindProcParam(head *pProcDecl) *pSym {
	kind := skProc
	if head.ret != nil {
		kind = skFunc
	}
	s := &pSym{kind: kind, proc: head, procParam: true, offset: c.scope.nslot}
	c.scope.nslot++
	if head.ret != nil {
		s.ret = c.resolveType(head.ret)
		s.typ = s.ret
	}
	for _, pm := range head.params {
		if pm.procHead != nil {
			s.params = append(s.params, c.bindProcParam(pm.procHead))
			continue
		}
		t := c.resolveType(pm.typ)
		for _, n := range pm.names {
			ps := &pSym{name: n, kind: skVar, typ: t, byRef: pm.byRef}
			s.params = append(s.params, ps)
		}
	}
	c.declare(head.name, s)
	return s
}

func (c *pChecker) bindConformant(arr *pSym, t *pType) {
	for u := t; u != nil && u.kind == tyArray && u.conf; u = u.elem {
		if u.confLo != "" {
			lo := c.declVar(u.confLo, c.intType)
			if u.index != nil {
				lo.typ = u.index
			}
			arr.confB = append(arr.confB, lo)
		}
		if u.confHi != "" {
			hi := c.declVar(u.confHi, c.intType)
			if u.index != nil {
				hi.typ = u.index
			}
			arr.confB = append(arr.confB, hi)
		}
	}
}

func (c *pChecker) checkBlockInner(b *pBlock) error {
	if b == nil {
		return nil
	}
	for _, d := range b.decl.consts {
		v, t := c.constVal(d.val)
		c.declConst(d.name, t, v)
	}
	for _, d := range b.decl.types {
		t := c.resolveType(d.typ)
		c.declType(d.name, t)
	}
	c.resolvePtrs()
	for _, d := range b.decl.vars {
		t := c.resolveType(d.typ)
		for _, n := range d.names {
			c.declVar(n, t)
		}
	}
	for _, pr := range b.decl.procs {
		c.declProcHeader(pr)
	}
	for _, pr := range b.decl.procs {
		if pr.fwd || pr.block == nil {
			continue
		}
		c.checkProcBody(pr)
	}
	c.checkStmts(b)
	return c.err
}

func (c *pChecker) checkStmts(b *pBlock) {
	if b == nil {
		return
	}
	oldPath, oldLab, oldGo, oldOK := c.path, c.labAt, c.gotos, c.okLabs
	c.path = nil
	c.labAt = map[int64][]*pStmt{}
	c.gotos = nil
	c.okLabs = map[int64]bool{}
	for k, v := range oldOK {
		c.okLabs[k] = v
	}
	for _, lab := range b.labels {
		c.okLabs[lab] = true
	}
	c.checkStmt(b.body)
	c.resolveGotos()
	b.nslot = c.scope.nslot
	c.path, c.labAt, c.gotos, c.okLabs = oldPath, oldLab, oldGo, oldOK
}

func (c *pChecker) resolveGotos() {
	for _, g := range c.gotos {
		if !c.okLabs[g.lab] {
			c.fail("undeclared label", g.line, g.col)
			continue
		}
		at, ok := c.labAt[g.lab]
		if !ok {
			c.fail("undefined label", g.line, g.col)
			continue
		}
		if !nestPrefix(at, g.path) {
			c.fail("GOTO into a structured statement", g.line, g.col)
		}
	}
}

func (c *pChecker) resolvePtrs() {
	for _, t := range c.ptrs {
		if t.ptrTo != nil {
			continue
		}
		if t.ptrName == "" {
			continue
		}
		sy := c.lookup(t.ptrName)
		if sy == nil || sy.kind != skType {
			c.fail("unknown type "+t.ptrName, 0, 0)
			continue
		}
		t.ptrTo = sy.typ
	}
}

func (c *pChecker) resolveType(r *pTypeRef) *pType {
	if r == nil {
		return c.intType
	}
	switch r.kind {
	case trNamed:
		sy := c.lookup(r.name)
		if sy == nil || sy.kind != skType {
			c.fail("unknown type "+r.name, r.line, r.col)
			return c.intType
		}
		return sy.typ
	case trEnum:
		t := &pType{kind: tyEnum, enums: r.enum}
		for i, n := range r.enum {
			c.declConst(n, t, pVal{typ: t, i: int64(i)})
		}
		return t
	case trSubrange:
		lo, _ := c.constVal(r.lo)
		hi, _ := c.constVal(r.hi)
		base := lo.typ
		if base == nil {
			base = c.intType
		}
		return &pType{kind: tySubrange, lo: lo.i, hi: hi.i, base: unwrapType(base)}
	case trArray:
		elem := c.resolveType(r.arrElem)
		t := &pType{kind: tyArray, packed: r.packed, elem: elem}
		if len(r.conf) > 0 {
			for i := len(r.conf) - 1; i >= 0; i-- {
				b := r.conf[i]
				idx := c.resolveType(b.idxType)
				if idx != nil && !idx.ordinal() {
					idx = c.intType
				}
				cur := &pType{kind: tyArray, packed: r.packed, index: idx, elem: elem, conf: true, confLo: b.lo, confHi: b.hi}
				elem = cur
				t = cur
			}
			return t
		}
		for i := len(r.arrIdx) - 1; i >= 0; i-- {
			idx := c.resolveType(r.arrIdx[i])
			if !idx.ordinal() {
				c.fail("array index must be ordinal", r.line, r.col)
			}
			cur := &pType{kind: tyArray, packed: r.packed, index: idx, elem: elem}
			elem = cur
			t = cur
		}
		return t
	case trRecord:
		t := &pType{kind: tyRecord, packed: r.packed}
		for _, f := range r.fields {
			ft := c.resolveType(f.typ)
			for _, n := range f.names {
				t.fields = append(t.fields, pField{name: n, typ: ft})
			}
		}
		if r.variant != nil {
			c.addVariant(t, r.variant)
		}
		return t
	case trSet:
		e := c.resolveType(r.setElem)
		if !e.ordinal() {
			c.fail("set of ordinal type expected", r.line, r.col)
		}
		return &pType{kind: tySet, elem: e}
	case trFile:
		if r.fileElem == nil {
			return c.textType
		}
		return &pType{kind: tyFile, elem: c.resolveType(r.fileElem)}
	case trPointer:
		t := &pType{kind: tyPointer, ptrName: r.ptrTo.name}
		if r.ptrTo != nil && r.ptrTo.kind == trNamed {
			if sy := c.lookup(r.ptrTo.name); sy != nil && sy.kind == skType {
				t.ptrTo = sy.typ
			} else {
				t.ptrName = r.ptrTo.name
				c.ptrs = append(c.ptrs, t)
			}
		} else if r.ptrTo != nil {
			t.ptrTo = c.resolveType(r.ptrTo)
		}
		return t
	}
	return c.intType
}

func (c *pChecker) addVariant(t *pType, v *pVariant) {
	if v.tagName != "" {
		tt := c.resolveType(v.tagType)
		t.fields = append(t.fields, pField{name: v.tagName, typ: tt})
		t.tagName = v.tagName
		t.tagType = tt
	}
	for _, arm := range v.arms {
		for _, f := range arm.fields {
			ft := c.resolveType(f.typ)
			for _, n := range f.names {
				t.fields = append(t.fields, pField{name: n, typ: ft})
			}
		}
		if arm.nested != nil {
			c.addVariant(t, arm.nested)
		}
	}
}

func (c *pChecker) constVal(e *pExpr) (pVal, *pType) {
	if e == nil {
		return pVal{typ: c.intType}, c.intType
	}
	c.checkExpr(e)
	switch e.op {
	case exInt:
		return pVal{typ: c.intType, i: e.ival}, c.intType
	case exReal:
		return pVal{typ: c.realType, f: e.fval}, c.realType
	case exChar:
		return pVal{typ: c.charType, i: e.ival, s: e.sval}, c.charType
	case exString:
		t := packedCharArray(c, int64(len(e.sval)))
		return pVal{typ: t, s: e.sval}, t
	case exBool:
		return pVal{typ: c.boolType, i: e.ival}, c.boolType
	case exIdent:
		sy := c.lookup(e.name)
		if sy != nil && sy.kind == skConst {
			return sy.cval, sy.typ
		}
		c.fail("constant expected", e.line, e.col)
	case exUnary:
		v, t := c.constVal(e.x)
		if e.tok == tkMinus {
			if t != nil && t.kind == tyReal {
				v.f = -v.f
			} else {
				v.i = -v.i
			}
		}
		if e.tok == tkNot {
			if v.i == 0 {
				v.i = 1
			} else {
				v.i = 0
			}
			t = c.boolType
			v.typ = t
		}
		return v, t
	case exBinary:
		x, xt := c.constVal(e.x)
		y, yt := c.constVal(e.y)
		_ = yt
		switch e.tok {
		case tkPlus:
			if xt != nil && xt.kind == tyReal {
				return pVal{typ: c.realType, f: asReal(x) + asReal(y)}, c.realType
			}
			return pVal{typ: c.intType, i: x.i + y.i}, c.intType
		case tkMinus:
			if xt != nil && xt.kind == tyReal {
				return pVal{typ: c.realType, f: asReal(x) - asReal(y)}, c.realType
			}
			return pVal{typ: c.intType, i: x.i - y.i}, c.intType
		case tkStar:
			if xt != nil && xt.kind == tyReal {
				return pVal{typ: c.realType, f: asReal(x) * asReal(y)}, c.realType
			}
			return pVal{typ: c.intType, i: x.i * y.i}, c.intType
		case tkSlash:
			return pVal{typ: c.realType, f: asReal(x) / asReal(y)}, c.realType
		case tkDiv:
			if y.i == 0 {
				c.fail("division by zero", e.line, e.col)
				return pVal{typ: c.intType}, c.intType
			}
			return pVal{typ: c.intType, i: x.i / y.i}, c.intType
		case tkMod:
			if y.i == 0 {
				c.fail("division by zero", e.line, e.col)
				return pVal{typ: c.intType}, c.intType
			}
			return pVal{typ: c.intType, i: x.i % y.i}, c.intType
		case tkEq, tkNe, tkLt, tkLe, tkGt, tkGe:
			cmp := compareVal(x, y)
			ok := false
			switch e.tok {
			case tkEq:
				ok = cmp == 0
			case tkNe:
				ok = cmp != 0
			case tkLt:
				ok = cmp < 0
			case tkLe:
				ok = cmp <= 0
			case tkGt:
				ok = cmp > 0
			case tkGe:
				ok = cmp >= 0
			}
			return pVal{typ: c.boolType, i: boolI(ok)}, c.boolType
		case tkAnd:
			return pVal{typ: c.boolType, i: boolI(x.i != 0 && y.i != 0)}, c.boolType
		case tkOr:
			return pVal{typ: c.boolType, i: boolI(x.i != 0 || y.i != 0)}, c.boolType
		}
	case exCall:
		c.checkExpr(e)
		if e.sym != nil && e.sym.kind == skStd && len(e.args) > 0 {
			v, t := c.constVal(e.args[0])
			switch e.sym.std {
			case "ORD":
				return pVal{typ: c.intType, i: v.i}, c.intType
			case "CHR":
				return pVal{typ: c.charType, i: v.i & 255, s: string(byte(v.i))}, c.charType
			case "ABS":
				if t != nil && t.kind == tyReal {
					if v.f < 0 {
						v.f = -v.f
					}
					return v, t
				}
				if v.i < 0 {
					v.i = -v.i
				}
				return v, c.intType
			case "SQR":
				if t != nil && t.kind == tyReal {
					v.f = v.f * v.f
					return v, t
				}
				v.i = v.i * v.i
				return v, c.intType
			case "SUCC":
				v.i++
				return v, t
			case "PRED":
				v.i--
				return v, t
			}
		}
	}
	c.fail("constant expression expected", e.line, e.col)
	return pVal{typ: c.intType}, c.intType
}

func packedCharArray(c *pChecker, n int64) *pType {
	idx := &pType{kind: tySubrange, lo: 1, hi: n, base: c.intType}
	return &pType{kind: tyArray, packed: true, index: idx, elem: c.charType}
}

func (c *pChecker) checkStmt(s *pStmt) {
	if s == nil || c.err != nil {
		return
	}
	switch s.kind {
	case pstCompound:
		for _, x := range s.list {
			c.checkStmt(x)
		}
	case pstAssign:
		c.checkExpr(s.lhs)
		c.checkExpr(s.rhs)
		if s.lhs != nil && s.lhs.op == exIdent && s.lhs.sym != nil {
			k := s.lhs.sym.kind
			if k == skProc || k == skFunc || k == skStd || k == skConst || k == skType {
				c.fail("cannot assign to "+s.lhs.name, s.line, s.col)
			}
		}
		if s.lhs != nil && !assignable(s.lhs.typ, s.rhs.typ) {
			c.fail("type mismatch in assignment", s.line, s.col)
		}
	case pstCall:
		c.checkExpr(s.lhs)
		if s.lhs != nil && s.lhs.op == exIdent && s.lhs.sym != nil {
			k := s.lhs.sym.kind
			if k != skProc && k != skFunc && k != skStd {
				c.fail("procedure expected", s.line, s.col)
			}
		}
	case pstIf:
		c.checkExpr(s.cond)
		c.requireBool(s.cond, s.line, s.col)
		c.checkBranch(s.body)
		c.checkBranch(s.els)
	case pstWhile:
		c.checkExpr(s.cond)
		c.requireBool(s.cond, s.line, s.col)
		c.checkBranch(s.body)
	case pstRepeat:
		c.path = append(c.path, s)
		for _, x := range s.list {
			c.checkStmt(x)
		}
		c.path = c.path[:len(c.path)-1]
		c.checkExpr(s.cond)
		c.requireBool(s.cond, s.line, s.col)
	case pstFor:
		c.checkExpr(s.lhs)
		c.checkExpr(s.rhs)
		c.checkExpr(s.cond)
		c.checkForControl(s)
		c.checkBranch(s.body)
	case pstCase:
		c.checkExpr(s.cond)
		if s.cond.typ != nil && !s.cond.typ.ordinal() {
			c.fail("CASE selector must be ordinal", s.line, s.col)
		}
		c.checkCaseArms(s)
	case pstWith:
		var added int
		for _, w := range s.list {
			if w != nil && w.lhs != nil {
				c.checkExpr(w.lhs)
				c.withs = append(c.withs, unwrapType(w.lhs.typ))
				added++
			}
		}
		c.checkBranch(s.body)
		c.withs = c.withs[:len(c.withs)-added]
	case pstGoto:
		if !c.okLabs[s.lab] {
			c.fail("undeclared label", s.line, s.col)
		}
		c.gotos = append(c.gotos, pGotoRef{
			lab: s.lab, path: append([]*pStmt(nil), c.path...),
			line: s.line, col: s.col,
		})
	case pstEmpty:
	case pstLabel:
		c.labAt[s.lab] = append([]*pStmt(nil), c.path...)
		c.checkStmt(s.body)
	}
}

func (c *pChecker) checkBranch(s *pStmt) {
	if s == nil {
		return
	}
	c.path = append(c.path, s)
	c.checkStmt(s)
	c.path = c.path[:len(c.path)-1]
}

func (c *pChecker) requireBool(e *pExpr, line, col int) {
	if e == nil || unwrapType(e.typ) == nil {
		return
	}
	if unwrapType(e.typ).kind != tyBoolean {
		c.fail("boolean expected", line, col)
	}
}

func (c *pChecker) checkForControl(s *pStmt) {
	if s.lhs == nil {
		return
	}
	if s.lhs.typ != nil && !s.lhs.typ.ordinal() {
		c.fail("FOR control must be ordinal", s.line, s.col)
	}
	if s.lhs.op != exIdent || s.lhs.sym == nil || s.lhs.sym.kind != skVar {
		c.fail("FOR control must be an entire variable", s.line, s.col)
		return
	}
	sy := s.lhs.sym
	if sy.param || sy.byRef || sy.level != c.scope.level {
		c.fail("FOR control must be local to this block", s.line, s.col)
	}
	if c.threatened(s.body, sy) {
		c.fail("FOR control is threatened in the loop", s.line, s.col)
	}
}

func (c *pChecker) threatened(s *pStmt, sy *pSym) bool {
	if s == nil || sy == nil {
		return false
	}
	switch s.kind {
	case pstAssign:
		return c.writesVar(s.lhs, sy)
	case pstCall:
		return c.callThreatens(s.lhs, sy)
	case pstCompound, pstRepeat:
		for _, x := range s.list {
			if c.threatened(x, sy) {
				return true
			}
		}
	case pstIf:
		return c.threatened(s.body, sy) || c.threatened(s.els, sy)
	case pstWhile, pstFor, pstWith, pstLabel:
		return c.threatened(s.body, sy)
	case pstCase:
		if c.threatened(s.other, sy) {
			return true
		}
		for _, a := range s.arms {
			if c.threatened(a.body, sy) {
				return true
			}
		}
	}
	return false
}

func (c *pChecker) writesVar(e *pExpr, sy *pSym) bool {
	return e != nil && e.op == exIdent && sy != nil && e.name == sy.name
}

func (c *pChecker) callThreatens(e *pExpr, sy *pSym) bool {
	if e == nil {
		return false
	}
	if e.sym != nil && e.sym.kind == skStd && (e.sym.std == "READ" || e.sym.std == "READLN") {
		for _, a := range e.args {
			if c.writesVar(a, sy) {
				return true
			}
		}
		return false
	}
	formals := []*pSym{}
	if e.sym != nil {
		formals = e.sym.params
	}
	for i, a := range e.args {
		if i < len(formals) && formals[i] != nil && formals[i].byRef && c.writesVar(a, sy) {
			return true
		}
	}
	return false
}

func (c *pChecker) checkCaseArms(s *pStmt) {
	seen := map[int64]bool{}
	for _, a := range s.arms {
		for _, k := range a.consts {
			c.checkExpr(k)
			if k.op == exRange {
				lo, _ := c.constVal(k.x)
				hi, _ := c.constVal(k.y)
				for i := lo.i; i <= hi.i; i++ {
					if seen[i] {
						c.fail("duplicate case label", k.line, k.col)
					}
					seen[i] = true
				}
			} else {
				v, _ := c.constVal(k)
				if seen[v.i] {
					c.fail("duplicate case label", k.line, k.col)
				}
				seen[v.i] = true
			}
		}
		c.checkBranch(a.body)
	}
	c.checkBranch(s.other)
}

func (c *pChecker) checkExpr(e *pExpr) {
	if e == nil || c.err != nil {
		return
	}
	switch e.op {
	case exInt:
		e.typ = c.intType
	case exReal:
		e.typ = c.realType
	case exChar:
		e.typ = c.charType
	case exString:
		e.typ = packedCharArray(c, int64(len(e.sval)))
	case exBool:
		e.typ = c.boolType
	case exNil:
		e.typ = c.nilType
	case exIdent:
		sy := c.lookup(e.name)
		if sy == nil {
			c.fail("unknown identifier "+e.name, e.line, e.col)
			e.typ = c.intType
			return
		}
		e.sym = sy
		e.typ = sy.typ
		if sy.kind == skStd {
			e.typ = c.intType
		}
		if sy.kind == skFunc && c.curFn != nil && sy.proc == c.curFn {
			// function name as result variable
			e.typ = sy.ret
		}
	case exCall:
		c.checkCall(e)
	case exUnary:
		c.checkExpr(e.x)
		e.typ = e.x.typ
		if e.tok == tkNot {
			c.requireBool(e.x, e.line, e.col)
			e.typ = c.boolType
		}
	case exBinary:
		c.checkExpr(e.x)
		c.checkExpr(e.y)
		e.typ = c.binType(e)
	case exIndex:
		c.checkExpr(e.x)
		for _, a := range e.args {
			c.checkExpr(a)
		}
		t := e.x.typ
		for range e.args {
			if t == nil || t.kind != tyArray {
				c.fail("array expected", e.line, e.col)
				e.typ = c.intType
				return
			}
			t = t.elem
		}
		e.typ = t
	case exField:
		c.checkExpr(e.x)
		t := unwrapType(e.x.typ)
		if t == nil || t.kind != tyRecord {
			c.fail("record expected", e.line, e.col)
			e.typ = c.intType
			return
		}
		for _, f := range t.fields {
			if f.name == e.field {
				e.typ = f.typ
				return
			}
		}
		c.fail("unknown field "+e.field, e.line, e.col)
		e.typ = c.intType
	case exDeref:
		c.checkExpr(e.x)
		t := unwrapType(e.x.typ)
		if t != nil && t.kind == tyPointer && t.ptrTo != nil {
			e.typ = t.ptrTo
			return
		}
		if t != nil && t.kind == tyText {
			e.typ = c.charType
			return
		}
		if t != nil && t.kind == tyFile {
			if t.elem != nil {
				e.typ = t.elem
			} else {
				e.typ = c.charType
			}
			return
		}
		c.fail("pointer expected", e.line, e.col)
		e.typ = c.intType
	case exSet:
		var elem *pType
		for _, el := range e.elems {
			if el.op == exRange {
				c.checkExpr(el.x)
				c.checkExpr(el.y)
				elem = el.x.typ
			} else {
				c.checkExpr(el)
				elem = el.typ
			}
		}
		if elem == nil {
			elem = c.intType
		}
		e.typ = &pType{kind: tySet, elem: elem}
	case exRange:
		c.checkExpr(e.x)
		c.checkExpr(e.y)
		e.typ = e.x.typ
	}
}

func (c *pChecker) checkCall(e *pExpr) {
	sy := c.lookup(e.name)
	if e.x != nil {
		c.checkExpr(e.x)
	}
	if c.curFn != nil && e.name == c.curFn.name {
		if c.curFn.sym != nil {
			sy = c.curFn.sym
		}
	}
	if sy == nil && e.op == exCall && e.name != "" {
		c.fail("unknown identifier "+e.name, e.line, e.col)
		e.typ = c.intType
		return
	}
	if sy != nil && sy.kind == skStd {
		for _, a := range e.args {
			c.checkExpr(a)
		}
		if sy.std == "READ" || sy.std == "READLN" {
			for _, a := range e.args {
				u := unwrapType(a.typ)
				if u != nil && (u.kind == tyText || u.kind == tyFile) {
					continue
				}
				if !isVarAccess(a) {
					c.fail("READ needs a variable", a.line, a.col)
				}
			}
		}
		e.sym = sy
		e.typ = c.stdType(sy.std, e)
		return
	}
	if sy != nil && (sy.kind == skProc || sy.kind == skFunc || sy.kind == skStd) {
		e.sym = sy
		for i, a := range e.args {
			if i < len(sy.params) && sy.params[i] != nil && (sy.params[i].kind == skProc || sy.params[i].kind == skFunc) {
				c.checkExpr(a)
				if a.sym == nil || !procCompat(sy.params[i], a.sym) {
					c.fail("incompatible procedural parameter", a.line, a.col)
				}
				continue
			}
			c.checkExpr(a)
			if i < len(sy.params) && sy.params[i] != nil && sy.params[i].byRef {
				if !isVarAccess(a) {
					c.fail("VAR parameter needs a variable", a.line, a.col)
				}
			}
		}
		if sy.kind == skFunc {
			e.typ = sy.ret
			if e.typ == nil {
				e.typ = c.intType
			}
		} else {
			e.typ = &pType{kind: tyDummy}
		}
		return
	}
	// designator ( ... ) parsed as ident call — already have args
	if e.name != "" {
		if sy == nil {
			c.fail("unknown identifier "+e.name, e.line, e.col)
		}
	}
	e.typ = c.intType
}

func (c *pChecker) stdType(name string, e *pExpr) *pType {
	switch name {
	case "ABS", "SQR":
		if len(e.args) > 0 && unwrapType(e.args[0].typ) != nil && unwrapType(e.args[0].typ).kind == tyReal {
			return c.realType
		}
		return c.intType
	case "SIN", "COS", "EXP", "LN", "SQRT", "ARCTAN":
		return c.realType
	case "TRUNC", "ROUND", "ORD":
		return c.intType
	case "CHR":
		return c.charType
	case "SUCC", "PRED":
		if len(e.args) > 0 {
			return e.args[0].typ
		}
		return c.intType
	case "ODD", "EOF", "EOLN":
		return c.boolType
	default:
		return &pType{kind: tyDummy}
	}
}

func (c *pChecker) binType(e *pExpr) *pType {
	xt, yt := unwrapType(e.x.typ), unwrapType(e.y.typ)
	switch e.tok {
	case tkEq, tkNe:
		if xt != nil && (xt.kind == tyFile || xt.kind == tyText) {
			c.fail("files cannot be compared", e.line, e.col)
		}
		return c.boolType
	case tkLt, tkLe, tkGt, tkGe:
		if xt != nil && xt.kind == tyPointer {
			c.fail("pointers only allow = and <>", e.line, e.col)
		}
		return c.boolType
	case tkIn:
		return c.boolType
	case tkAnd, tkOr:
		c.requireBool(e.x, e.line, e.col)
		c.requireBool(e.y, e.line, e.col)
		return c.boolType
	case tkSlash:
		if !numericType(e.x.typ) || !numericType(e.y.typ) {
			c.fail("numeric operands expected", e.line, e.col)
		}
		return c.realType
	case tkDiv, tkMod:
		if xt == nil || yt == nil || xt.kind != tyInteger || yt.kind != tyInteger {
			c.fail("DIV and MOD need integers", e.line, e.col)
		}
		return c.intType
	case tkPlus, tkMinus, tkStar:
		if xt != nil && xt.kind == tySet {
			return e.x.typ
		}
		if !numericType(e.x.typ) || !numericType(e.y.typ) {
			c.fail("numeric operands expected", e.line, e.col)
		}
		if (xt != nil && xt.kind == tyReal) || (yt != nil && yt.kind == tyReal) {
			return c.realType
		}
		return c.intType
	}
	return c.intType
}

func isVarAccess(e *pExpr) bool {
	if e == nil {
		return false
	}
	switch e.op {
	case exIdent:
		return e.sym != nil && (e.sym.kind == skVar || e.sym.std == "FIELD")
	case exIndex, exField, exDeref:
		return true
	}
	return false
}

func procCompat(formal, actual *pSym) bool {
	if formal == nil || actual == nil {
		return false
	}
	if actual.kind != skProc && actual.kind != skFunc {
		return false
	}
	if formal.kind != actual.kind {
		return false
	}
	if (formal.ret == nil) != (actual.ret == nil) {
		return false
	}
	if formal.ret != nil && actual.ret != nil && !sameType(formal.ret, actual.ret) {
		return false
	}
	if len(formal.params) != len(actual.params) {
		return false
	}
	for i := range formal.params {
		if formal.params[i].byRef != actual.params[i].byRef {
			return false
		}
		if !sameType(formal.params[i].typ, actual.params[i].typ) {
			return false
		}
	}
	return true
}

func assignable(dst, src *pType) bool {
	dst, src = unwrapType(dst), unwrapType(src)
	if dst == nil || src == nil {
		return true
	}
	if dst.kind == tyFile || dst.kind == tyText || src.kind == tyFile || src.kind == tyText {
		return false
	}
	if sameType(dst, src) {
		return true
	}
	if dst.kind == tyReal && (src.kind == tyInteger || src.kind == tySubrange) {
		return true
	}
	if dst.kind == tyInteger && src.kind == tySubrange {
		return true
	}
	if dst.kind == tySubrange && (src.kind == tyInteger || src.kind == tySubrange || (dst.base != nil && src.kind == dst.base.kind)) {
		return true
	}
	if dst.kind == tyPointer && src.kind == tyNil {
		return true
	}
	if dst.kind == tySet && src.kind == tySet {
		return true
	}
	if dst.isPackedCharArray() && src.isPackedCharArray() {
		return dst.arrayLen() >= src.arrayLen() || src.arrayLen() == 0
	}
	if dst.kind == tyChar && src.isPackedCharArray() && src.arrayLen() == 1 {
		return true
	}
	return false
}
