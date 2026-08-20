package rsts

type pParser struct {
	lx  *pLex
	err error
}

func parsePascal(src string) (*pProgram, error) {
	p := &pParser{lx: newPLex(src)}
	prog := p.parseProgram()
	if p.err != nil {
		return nil, p.err
	}
	if p.lx.tok.kind != tkEOF && p.lx.tok.kind != tkDot {
		// program already consumed the final dot
	}
	prog.src = src
	return prog, nil
}

func (p *pParser) fail(msg string) {
	if p.err == nil {
		p.err = pasErr(msg, p.lx.tok.line, p.lx.tok.col)
	}
}

func (p *pParser) got(k ptokKind) bool {
	if p.lx.tok.kind == k {
		p.lx.next()
		return true
	}
	return false
}

func (p *pParser) want(k ptokKind, msg string) {
	if !p.got(k) {
		p.fail(msg)
	}
}

func (p *pParser) ident() string {
	if p.lx.tok.kind != tkIdent {
		p.fail("identifier expected")
		return ""
	}
	s := p.lx.tok.lit
	p.lx.next()
	return s
}

func (p *pParser) parseProgram() *pProgram {
	prog := &pProgram{}
	p.want(tkProgram, "PROGRAM expected")
	prog.name = p.ident()
	if p.got(tkLParen) {
		for {
			if p.lx.tok.kind == tkIdent {
				prog.files = append(prog.files, p.ident())
			} else {
				break
			}
			if !p.got(tkComma) {
				break
			}
		}
		p.want(tkRParen, ") expected")
	}
	p.want(tkSemi, "; expected")
	prog.block = p.parseBlock()
	p.want(tkDot, ". expected")
	return prog
}

func (p *pParser) parseBlock() *pBlock {
	b := &pBlock{}
	if p.got(tkLabel) {
		for {
			if p.lx.tok.kind == tkInt {
				b.labels = append(b.labels, p.lx.tok.ival)
				p.lx.next()
			} else {
				p.fail("label number expected")
				break
			}
			if !p.got(tkComma) {
				break
			}
		}
		p.want(tkSemi, "; expected")
	}
	if p.got(tkConst) {
		for p.lx.tok.kind == tkIdent {
			d := pConstDecl{name: p.ident(), line: p.lx.tok.line}
			p.want(tkEq, "= expected")
			d.val = p.parseExpr()
			b.decl.consts = append(b.decl.consts, d)
			p.want(tkSemi, "; expected")
			if p.err != nil {
				return b
			}
		}
	}
	if p.got(tkType) {
		for p.lx.tok.kind == tkIdent {
			d := pTypeDecl{name: p.ident(), line: p.lx.tok.line}
			p.want(tkEq, "= expected")
			d.typ = p.parseType()
			b.decl.types = append(b.decl.types, d)
			p.want(tkSemi, "; expected")
			if p.err != nil {
				return b
			}
		}
	}
	if p.got(tkVar) {
		for p.lx.tok.kind == tkIdent {
			d := pVarDecl{line: p.lx.tok.line}
			d.names = p.identList()
			p.want(tkColon, ": expected")
			d.typ = p.parseType()
			b.decl.vars = append(b.decl.vars, d)
			p.want(tkSemi, "; expected")
			if p.err != nil {
				return b
			}
		}
	}
	for p.lx.tok.kind == tkProcedure || p.lx.tok.kind == tkFunction {
		b.decl.procs = append(b.decl.procs, p.parseProc())
		if p.err != nil {
			return b
		}
	}
	b.body = p.parseCompound()
	return b
}

func (p *pParser) parseProc() *pProcDecl {
	d := &pProcDecl{line: p.lx.tok.line}
	isFunc := p.lx.tok.kind == tkFunction
	p.lx.next()
	d.name = p.ident()
	if p.got(tkLParen) {
		if p.lx.tok.kind != tkRParen {
			d.params = p.parseParams()
		}
		p.want(tkRParen, ") expected")
	}
	if isFunc {
		if p.got(tkColon) {
			d.ret = p.parseType()
		}
	}
	p.want(tkSemi, "; expected")
	if p.got(tkForward) {
		d.fwd = true
		p.want(tkSemi, "; expected")
		return d
	}
	d.block = p.parseBlock()
	p.want(tkSemi, "; expected")
	return d
}

func (p *pParser) parseParams() []pParam {
	var out []pParam
	for {
		if p.lx.tok.kind == tkProcedure || p.lx.tok.kind == tkFunction {
			out = append(out, p.parseProcParam())
			if !p.got(tkSemi) {
				break
			}
			continue
		}
		pm := pParam{}
		if p.got(tkVar) {
			pm.byRef = true
		}
		pm.names = p.identList()
		p.want(tkColon, ": expected")
		pm.typ = p.parseType()
		out = append(out, pm)
		if !p.got(tkSemi) {
			break
		}
	}
	return out
}

func (p *pParser) parseProcParam() pParam {
	d := &pProcDecl{line: p.lx.tok.line}
	isFunc := p.lx.tok.kind == tkFunction
	p.lx.next()
	d.name = p.ident()
	if p.got(tkLParen) {
		if p.lx.tok.kind != tkRParen {
			d.params = p.parseParams()
		}
		p.want(tkRParen, ") expected")
	}
	if isFunc {
		p.want(tkColon, ": expected")
		d.ret = p.parseType()
	}
	return pParam{names: []string{d.name}, procHead: d}
}

func (p *pParser) identList() []string {
	var n []string
	n = append(n, p.ident())
	for p.got(tkComma) {
		n = append(n, p.ident())
	}
	return n
}

func (p *pParser) parseType() *pTypeRef {
	t := &pTypeRef{line: p.lx.tok.line, col: p.lx.tok.col}
	if p.got(tkPacked) {
		t.packed = true
	}
	switch p.lx.tok.kind {
	case tkIdent:
		t.kind = trNamed
		t.name = p.ident()
		if t.name != "" && p.lx.tok.kind == tkDotDot {
			// ident .. expr  (named const as subrange bound)
			lo := &pExpr{op: exIdent, name: t.name, line: t.line, col: t.col}
			p.lx.next()
			t.kind = trSubrange
			t.lo = lo
			t.hi = p.parseExpr()
		}
		return t
	case tkLParen:
		p.lx.next()
		t.kind = trEnum
		t.enum = p.identList()
		p.want(tkRParen, ") expected")
		return t
	case tkInt, tkString, tkTrue, tkFalse, tkMinus, tkPlus:
		t.kind = trSubrange
		t.lo = p.parseExpr()
		p.want(tkDotDot, ".. expected")
		t.hi = p.parseExpr()
		return t
	case tkCaret:
		p.lx.next()
		t.kind = trPointer
		t.ptrTo = &pTypeRef{kind: trNamed, name: p.ident(), line: t.line}
		return t
	case tkArray:
		p.lx.next()
		t.kind = trArray
		p.want(tkLBrack, "[ expected")
		for {
			idx := p.parseType()
			if p.got(tkColon) {
				// conformant: l..h : ordinaltype
				bt := p.parseType()
				lo, hi := "", ""
				if idx.kind == trSubrange {
					if idx.lo != nil && idx.lo.op == exIdent {
						lo = idx.lo.name
					}
					if idx.hi != nil && idx.hi.op == exIdent {
						hi = idx.hi.name
					}
				}
				if lo == "" || hi == "" {
					p.fail("conformant array bounds must be identifiers")
				}
				t.conf = append(t.conf, pConfBound{lo: lo, hi: hi, idxType: bt})
			} else {
				t.arrIdx = append(t.arrIdx, idx)
			}
			if !p.got(tkComma) && !p.got(tkSemi) {
				break
			}
		}
		p.want(tkRBrack, "] expected")
		p.want(tkOf, "OF expected")
		t.arrElem = p.parseType()
		return t
	case tkRecord:
		p.lx.next()
		t.kind = trRecord
		t.fields, t.variant = p.parseFieldList()
		p.want(tkEnd, "END expected")
		return t
	case tkSet:
		p.lx.next()
		t.kind = trSet
		p.want(tkOf, "OF expected")
		t.setElem = p.parseType()
		return t
	case tkFile:
		p.lx.next()
		t.kind = trFile
		if p.got(tkOf) {
			t.fileElem = p.parseType()
		}
		return t
	}
	p.fail("type expected")
	return t
}

func (p *pParser) parseFieldList() ([]pRecField, *pVariant) {
	var fields []pRecField
	for p.lx.tok.kind == tkIdent {
		f := pRecField{names: p.identList()}
		p.want(tkColon, ": expected")
		f.typ = p.parseType()
		fields = append(fields, f)
		if p.lx.tok.kind == tkEnd || p.lx.tok.kind == tkRParen || p.lx.tok.kind == tkCase {
			break
		}
		p.want(tkSemi, "; expected")
		if p.lx.tok.kind == tkEnd || p.lx.tok.kind == tkRParen || p.lx.tok.kind == tkCase {
			break
		}
	}
	var v *pVariant
	if p.got(tkCase) {
		v = &pVariant{}
		if p.lx.tok.kind == tkIdent {
			name := p.ident()
			if p.got(tkColon) {
				v.tagName = name
				v.tagType = p.parseType()
			} else {
				v.tagType = &pTypeRef{kind: trNamed, name: name, line: p.lx.tok.line}
			}
		} else {
			v.tagType = p.parseType()
		}
		p.want(tkOf, "OF expected")
		for p.lx.tok.kind != tkEnd && p.lx.tok.kind != tkRParen && p.lx.tok.kind != tkEOF {
			arm := pVarArm{}
			arm.consts = p.parseLabelList()
			p.want(tkColon, ": expected")
			p.want(tkLParen, "( expected")
			arm.fields, arm.nested = p.parseFieldList()
			p.want(tkRParen, ") expected")
			v.arms = append(v.arms, arm)
			if p.lx.tok.kind == tkEnd || p.lx.tok.kind == tkRParen {
				break
			}
			p.got(tkSemi)
			if p.lx.tok.kind == tkEnd || p.lx.tok.kind == tkRParen {
				break
			}
		}
	}
	return fields, v
}

func (p *pParser) parseLabelList() []*pExpr {
	var labels []*pExpr
	for {
		a := p.parseExpr()
		if p.got(tkDotDot) {
			a = &pExpr{op: exRange, x: a, y: p.parseExpr(), line: a.line, col: a.col}
		}
		labels = append(labels, a)
		if !p.got(tkComma) {
			break
		}
	}
	return labels
}

func (p *pParser) parseCompound() *pStmt {
	p.want(tkBegin, "BEGIN expected")
	s := &pStmt{kind: pstCompound, line: p.lx.tok.line}
	s.list = p.parseStmtList(tkEnd)
	p.want(tkEnd, "END expected")
	return s
}

func (p *pParser) parseStmtList(stop ptokKind) []*pStmt {
	var list []*pStmt
	for p.lx.tok.kind != stop && p.lx.tok.kind != tkEOF && p.lx.tok.kind != tkUntil && p.err == nil {
		list = append(list, p.parseStmt())
		if p.lx.tok.kind == stop || p.lx.tok.kind == tkUntil {
			break
		}
		if !p.got(tkSemi) {
			break
		}
	}
	return list
}

func (p *pParser) parseStmt() *pStmt {
	s := &pStmt{line: p.lx.tok.line, col: p.lx.tok.col}
	if p.lx.tok.kind == tkInt {
		s.kind = pstLabel
		s.lab = p.lx.tok.ival
		p.lx.next()
		p.want(tkColon, ": expected")
		inner := p.parseStmt()
		s.body = inner
		return s
	}
	switch p.lx.tok.kind {
	case tkBegin:
		return p.parseCompound()
	case tkIf:
		p.lx.next()
		s.kind = pstIf
		s.cond = p.parseExpr()
		p.want(tkThen, "THEN expected")
		s.body = p.parseStmt()
		if p.got(tkElse) {
			s.els = p.parseStmt()
		}
		return s
	case tkCase:
		p.lx.next()
		s.kind = pstCase
		s.cond = p.parseExpr()
		p.want(tkOf, "OF expected")
		for p.lx.tok.kind != tkEnd && p.lx.tok.kind != tkOtherwise && p.lx.tok.kind != tkEOF {
			arm := pCaseArm{}
			arm.consts = p.parseLabelList()
			p.want(tkColon, ": expected")
			arm.body = p.parseStmt()
			s.arms = append(s.arms, arm)
			if p.lx.tok.kind == tkEnd || p.lx.tok.kind == tkOtherwise {
				break
			}
			p.got(tkSemi)
		}
		if p.got(tkOtherwise) {
			p.got(tkColon)
			s.other = p.parseStmt()
		}
		p.got(tkSemi)
		p.want(tkEnd, "END expected")
		return s
	case tkWhile:
		p.lx.next()
		s.kind = pstWhile
		s.cond = p.parseExpr()
		p.want(tkDo, "DO expected")
		s.body = p.parseStmt()
		return s
	case tkRepeat:
		p.lx.next()
		s.kind = pstRepeat
		s.list = p.parseStmtList(tkUntil)
		p.want(tkUntil, "UNTIL expected")
		s.cond = p.parseExpr()
		return s
	case tkFor:
		p.lx.next()
		s.kind = pstFor
		s.lhs = p.parseDesignator()
		p.want(tkAssign, ":= expected")
		s.rhs = p.parseExpr()
		if p.got(tkDownto) {
			s.downto = true
		} else {
			p.want(tkTo, "TO expected")
		}
		s.cond = p.parseExpr() // end value
		p.want(tkDo, "DO expected")
		s.body = p.parseStmt()
		return s
	case tkWith:
		p.lx.next()
		s.kind = pstWith
		for {
			s.list = append(s.list, &pStmt{kind: pstEmpty, lhs: p.parseDesignator()})
			if !p.got(tkComma) {
				break
			}
		}
		p.want(tkDo, "DO expected")
		s.body = p.parseStmt()
		return s
	case tkGoto:
		p.lx.next()
		s.kind = pstGoto
		if p.lx.tok.kind != tkInt {
			p.fail("label expected")
			return s
		}
		s.lab = p.lx.tok.ival
		p.lx.next()
		return s
	case tkIdent, tkTrue, tkFalse:
		// assignment or call
		ex := p.parseDesignator()
		if p.got(tkAssign) {
			s.kind = pstAssign
			s.lhs = ex
			s.rhs = p.parseExpr()
			return s
		}
		s.kind = pstCall
		s.lhs = ex
		return s
	case tkSemi, tkEnd, tkUntil, tkElse:
		s.kind = pstEmpty
		return s
	}
	s.kind = pstEmpty
	return s
}

func (p *pParser) parseDesignator() *pExpr {
	ex := p.parsePrimary()
	for {
		switch p.lx.tok.kind {
		case tkDot:
			p.lx.next()
			ex = &pExpr{op: exField, x: ex, field: p.ident(), line: ex.line, col: ex.col}
		case tkLBrack:
			p.lx.next()
			ix := &pExpr{op: exIndex, x: ex, line: ex.line, col: ex.col}
			for {
				ix.args = append(ix.args, p.parseExpr())
				if !p.got(tkComma) {
					break
				}
			}
			p.want(tkRBrack, "] expected")
			ex = ix
		case tkCaret:
			p.lx.next()
			ex = &pExpr{op: exDeref, x: ex, line: ex.line, col: ex.col}
		default:
			return ex
		}
	}
}

func (p *pParser) parseExpr() *pExpr { return p.parseRel() }

func (p *pParser) parseRel() *pExpr {
	x := p.parseAdd()
	switch p.lx.tok.kind {
	case tkEq, tkNe, tkLt, tkLe, tkGt, tkGe, tkIn:
		op := p.lx.tok.kind
		line, col := p.lx.tok.line, p.lx.tok.col
		p.lx.next()
		return &pExpr{op: exBinary, tok: op, x: x, y: p.parseAdd(), line: line, col: col}
	}
	return x
}

func (p *pParser) parseAdd() *pExpr {
	x := p.parseMul()
	for p.lx.tok.kind == tkPlus || p.lx.tok.kind == tkMinus || p.lx.tok.kind == tkOr {
		op := p.lx.tok.kind
		line, col := p.lx.tok.line, p.lx.tok.col
		p.lx.next()
		x = &pExpr{op: exBinary, tok: op, x: x, y: p.parseMul(), line: line, col: col}
	}
	return x
}

func (p *pParser) parseMul() *pExpr {
	x := p.parseUnary()
	for p.lx.tok.kind == tkStar || p.lx.tok.kind == tkSlash || p.lx.tok.kind == tkDiv || p.lx.tok.kind == tkMod || p.lx.tok.kind == tkAnd {
		op := p.lx.tok.kind
		line, col := p.lx.tok.line, p.lx.tok.col
		p.lx.next()
		x = &pExpr{op: exBinary, tok: op, x: x, y: p.parseUnary(), line: line, col: col}
	}
	return x
}

func (p *pParser) parseUnary() *pExpr {
	switch p.lx.tok.kind {
	case tkPlus, tkMinus, tkNot:
		op := p.lx.tok.kind
		line, col := p.lx.tok.line, p.lx.tok.col
		p.lx.next()
		return &pExpr{op: exUnary, tok: op, x: p.parseUnary(), line: line, col: col}
	}
	return p.parseDesignator()
}

func (p *pParser) parsePrimary() *pExpr {
	t := p.lx.tok
	switch t.kind {
	case tkIdent:
		name := p.ident()
		ex := &pExpr{op: exIdent, name: name, line: t.line, col: t.col}
		if p.got(tkLParen) {
			ex.op = exCall
			if p.lx.tok.kind != tkRParen {
				for {
					a := p.parseExpr()
					if p.got(tkColon) {
						a.width = p.parseExpr()
						if p.got(tkColon) {
							a.prec = p.parseExpr()
						}
					}
					ex.args = append(ex.args, a)
					if !p.got(tkComma) {
						break
					}
				}
			}
			p.want(tkRParen, ") expected")
		}
		return ex
	case tkTrue:
		p.lx.next()
		return &pExpr{op: exBool, ival: 1, line: t.line, col: t.col}
	case tkFalse:
		p.lx.next()
		return &pExpr{op: exBool, ival: 0, line: t.line, col: t.col}
	case tkNil:
		p.lx.next()
		return &pExpr{op: exNil, line: t.line, col: t.col}
	case tkInt:
		p.lx.next()
		return &pExpr{op: exInt, ival: t.ival, line: t.line, col: t.col}
	case tkReal:
		p.lx.next()
		return &pExpr{op: exReal, fval: t.fval, line: t.line, col: t.col}
	case tkString:
		p.lx.next()
		if len(t.lit) == 1 {
			return &pExpr{op: exChar, ival: int64(t.lit[0]), sval: t.lit, line: t.line, col: t.col}
		}
		return &pExpr{op: exString, sval: t.lit, line: t.line, col: t.col}
	case tkLParen:
		p.lx.next()
		ex := p.parseExpr()
		p.want(tkRParen, ") expected")
		return ex
	case tkLBrack:
		p.lx.next()
		ex := &pExpr{op: exSet, line: t.line, col: t.col}
		if p.lx.tok.kind != tkRBrack {
			for {
				a := p.parseExpr()
				if p.got(tkDotDot) {
					a = &pExpr{op: exRange, x: a, y: p.parseExpr(), line: a.line, col: a.col}
				}
				ex.elems = append(ex.elems, a)
				if !p.got(tkComma) {
					break
				}
			}
		}
		p.want(tkRBrack, "] expected")
		return ex
	}
	p.fail("expression expected")
	p.lx.next()
	return &pExpr{op: exInt, line: t.line, col: t.col}
}
