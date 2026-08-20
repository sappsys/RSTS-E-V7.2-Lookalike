package rsts

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strings"
)

type pRT struct {
	prog    *pProgram
	host    PascalHost
	fp      *pFrame
	heap    []*pHeap
	heapN   int64
	in      *pFile
	out     *pFile
	files   map[string]*pFile
	mem     map[string]string
	stop    bool
	withs   []withRef
	aliases []pAlias
}

type pFrame struct {
	level int
	slink *pFrame
	slots []pVal
	syms  *pScope
}

func evalPascal(prog *pProgram, host PascalHost) error {
	rt := &pRT{prog: prog, host: host, files: map[string]*pFile{}, mem: map[string]string{}}
	rt.in = &pFile{text: true, name: "INPUT", mode: 1}
	rt.out = &pFile{text: true, name: "OUTPUT", mode: 2}
	glob := &pFrame{level: 0, slots: make([]pVal, 8)}
	glob.slots[0] = pVal{typ: prog.textType, file: rt.in}
	glob.slots[1] = pVal{typ: prog.textType, file: rt.out}
	rt.in.buffer = pVal{typ: prog.charType}
	rt.out.buffer = pVal{typ: prog.charType}
	fr := &pFrame{level: 1, slink: glob, slots: make([]pVal, 64)}
	rt.fp = fr
	_, err := rt.exec(prog.block.body)
	rt.flushFiles()
	return err
}

func (rt *pRT) exec(s *pStmt) (*int64, error) {
	if s == nil || rt.stop {
		return nil, nil
	}
	if rt.host.PollStop != nil && rt.host.PollStop() {
		rt.stop = true
		return nil, fmt.Errorf("^C")
	}
	switch s.kind {
	case pstEmpty:
		return nil, nil
	case pstCompound:
		for i := 0; i < len(s.list); i++ {
			g, err := rt.exec(s.list[i])
			if err != nil {
				return nil, err
			}
			if g != nil {
				for j, t := range s.list {
					if t.kind == pstLabel && t.lab == *g {
						i = j - 1
						g = nil
						break
					}
				}
				if g != nil {
					return g, nil
				}
			}
		}
	case pstLabel:
		return rt.exec(s.body)
	case pstAssign:
		lv, err := rt.lvalue(s.lhs)
		if err != nil {
			return nil, err
		}
		v, err := rt.eval(s.rhs)
		if err != nil {
			return nil, err
		}
		if err := rangeAssign(lv.typ, v, s.line, s.col); err != nil {
			return nil, err
		}
		assignVal(lv, v)
	case pstCall:
		_, err := rt.eval(s.lhs)
		return nil, err
	case pstIf:
		v, err := rt.eval(s.cond)
		if err != nil {
			return nil, err
		}
		if v.i != 0 {
			return rt.exec(s.body)
		}
		return rt.exec(s.els)
	case pstWhile:
		for {
			v, err := rt.eval(s.cond)
			if err != nil {
				return nil, err
			}
			if v.i == 0 {
				return nil, nil
			}
			g, err := rt.exec(s.body)
			if err != nil || g != nil {
				return g, err
			}
		}
	case pstRepeat:
		for {
			for _, x := range s.list {
				g, err := rt.exec(x)
				if err != nil || g != nil {
					return g, err
				}
			}
			v, err := rt.eval(s.cond)
			if err != nil {
				return nil, err
			}
			if v.i != 0 {
				return nil, nil
			}
		}
	case pstFor:
		lv, err := rt.lvalue(s.lhs)
		if err != nil {
			return nil, err
		}
		a, err := rt.eval(s.rhs)
		if err != nil {
			return nil, err
		}
		b, err := rt.eval(s.cond)
		if err != nil {
			return nil, err
		}
		lv.i = a.i
		lv.typ = s.lhs.typ
		step := int64(1)
		if s.downto {
			step = -1
		}
		for {
			if s.downto && lv.i < b.i {
				break
			}
			if !s.downto && lv.i > b.i {
				break
			}
			g, err := rt.exec(s.body)
			if err != nil || g != nil {
				return g, err
			}
			lv.i += step
		}
	case pstCase:
		v, err := rt.eval(s.cond)
		if err != nil {
			return nil, err
		}
		for _, arm := range s.arms {
			for _, k := range arm.consts {
				if k.op == exRange {
					a, err := rt.eval(k.x)
					if err != nil {
						return nil, err
					}
					b, err := rt.eval(k.y)
					if err != nil {
						return nil, err
					}
					if v.i >= a.i && v.i <= b.i {
						return rt.exec(arm.body)
					}
					continue
				}
				cv, err := rt.eval(k)
				if err != nil {
					return nil, err
				}
				if cv.i == v.i {
					return rt.exec(arm.body)
				}
			}
		}
		if s.other != nil {
			return rt.exec(s.other)
		}
		return nil, pasErr("case selector matches no constant", s.line, s.col)
	case pstWith:
		var n int
		for _, w := range s.list {
			if w != nil && w.lhs != nil {
				lv, err := rt.lvalue(w.lhs)
				if err != nil {
					return nil, err
				}
				rt.pushWith(lv)
				n++
			}
		}
		g, err := rt.exec(s.body)
		rt.popWith(n)
		return g, err
	case pstGoto:
		lab := s.lab
		return &lab, nil
	}
	return nil, nil
}

type withRef struct {
	val *pVal
	typ *pType
}

func (rt *pRT) pushWith(v *pVal) {
	if rt.fp == nil {
		return
	}
	// stash on a side stack
	rt.withs = append(rt.withs, withRef{val: v, typ: v.typ})
}

func (rt *pRT) popWith(n int) {
	if n > len(rt.withs) {
		n = len(rt.withs)
	}
	rt.withs = rt.withs[:len(rt.withs)-n]
}

func (rt *pRT) eval(e *pExpr) (pVal, error) {
	if e == nil {
		return pVal{}, nil
	}
	switch e.op {
	case exInt:
		return pVal{typ: rt.prog.intType, i: e.ival}, nil
	case exReal:
		return pVal{typ: rt.prog.realType, f: e.fval}, nil
	case exChar:
		return pVal{typ: rt.prog.charType, i: e.ival, s: e.sval}, nil
	case exString:
		return pVal{typ: e.typ, s: e.sval}, nil
	case exBool:
		return pVal{typ: rt.prog.boolType, i: e.ival}, nil
	case exNil:
		return pVal{typ: rt.prog.nilType}, nil
	case exIdent:
		if e.sym != nil && e.sym.kind == skConst {
			return copyVal(e.sym.cval), nil
		}
		if e.sym != nil && e.sym.kind == skStd {
			return rt.stdCall(e.sym.std, nil)
		}
		if e.sym != nil && (e.sym.kind == skProc || e.sym.kind == skFunc) {
			return rt.callProc(&pExpr{op: exCall, name: e.name, sym: e.sym, line: e.line, col: e.col})
		}
		lv, err := rt.lvalue(e)
		if err != nil {
			return pVal{}, err
		}
		return copyVal(*lv), nil
	case exCall:
		if e.sym != nil && e.sym.kind == skStd {
			return rt.stdCall(e.sym.std, e.args)
		}
		return rt.callProc(e)
	case exUnary:
		v, err := rt.eval(e.x)
		if err != nil {
			return pVal{}, err
		}
		switch e.tok {
		case tkMinus:
			if unwrapType(v.typ) != nil && unwrapType(v.typ).kind == tyReal {
				v.f = -v.f
			} else {
				v.i = -v.i
			}
		case tkNot:
			if v.i == 0 {
				v.i = 1
			} else {
				v.i = 0
			}
			v.typ = rt.prog.boolType
		}
		return v, nil
	case exBinary:
		return rt.evalBin(e)
	case exIndex, exField, exDeref:
		lv, err := rt.lvalue(e)
		if err != nil {
			return pVal{}, err
		}
		return copyVal(*lv), nil
	case exSet:
		t := e.typ
		if t == nil {
			t = &pType{kind: tySet, elem: rt.prog.intType}
		}
		v := pVal{typ: t}
		for _, el := range e.elems {
			if el.op == exRange {
				a, err := rt.eval(el.x)
				if err != nil {
					return pVal{}, err
				}
				b, err := rt.eval(el.y)
				if err != nil {
					return pVal{}, err
				}
				for i := a.i; i <= b.i; i++ {
					setBit(&v, i, true)
				}
			} else {
				a, err := rt.eval(el)
				if err != nil {
					return pVal{}, err
				}
				setBit(&v, a.i, true)
			}
		}
		return v, nil
	}
	return pVal{}, pasErr("internal expr", e.line, e.col)
}

func (rt *pRT) evalBin(e *pExpr) (pVal, error) {
	x, err := rt.eval(e.x)
	if err != nil {
		return pVal{}, err
	}
	y, err := rt.eval(e.y)
	if err != nil {
		return pVal{}, err
	}
	xt, yt := unwrapType(x.typ), unwrapType(y.typ)
	switch e.tok {
	case tkPlus, tkMinus, tkStar, tkSlash, tkDiv, tkMod:
		if xt != nil && xt.kind == tySet {
			return setOp(e.tok, x, y), nil
		}
		xr := xt != nil && xt.kind == tyReal
		yr := yt != nil && yt.kind == tyReal
		if e.tok == tkSlash || xr || yr {
			xf, yf := asReal(x), asReal(y)
			r := pVal{typ: rt.prog.realType}
			switch e.tok {
			case tkPlus:
				r.f = xf + yf
			case tkMinus:
				r.f = xf - yf
			case tkStar:
				r.f = xf * yf
			case tkSlash:
				if yf == 0 {
					return pVal{}, pasErr("division by zero", e.line, e.col)
				}
				r.f = xf / yf
			}
			return r, nil
		}
		r := pVal{typ: rt.prog.intType}
		switch e.tok {
		case tkPlus:
			r.i = x.i + y.i
		case tkMinus:
			r.i = x.i - y.i
		case tkStar:
			r.i = x.i * y.i
		case tkDiv:
			if y.i == 0 {
				return pVal{}, pasErr("division by zero", e.line, e.col)
			}
			r.i = x.i / y.i
		case tkMod:
			if y.i <= 0 {
				return pVal{}, pasErr("MOD divisor must be positive", e.line, e.col)
			}
			_, m := isoDivMod(x.i, y.i)
			r.i = m
		}
		if r.i < -pascalMaxInt || r.i > pascalMaxInt {
			return pVal{}, pasErr("integer overflow", e.line, e.col)
		}
		return r, nil
	case tkAnd:
		return pVal{typ: rt.prog.boolType, i: boolI(x.i != 0 && y.i != 0)}, nil
	case tkOr:
		return pVal{typ: rt.prog.boolType, i: boolI(x.i != 0 || y.i != 0)}, nil
	case tkIn:
		return pVal{typ: rt.prog.boolType, i: boolI(inSet(y, x.i))}, nil
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
		return pVal{typ: rt.prog.boolType, i: boolI(ok)}, nil
	}
	return pVal{}, pasErr("operator", e.line, e.col)
}

func boolI(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func asReal(v pVal) float64 {
	if unwrapType(v.typ) != nil && unwrapType(v.typ).kind == tyReal {
		return v.f
	}
	return float64(v.i)
}

func compareVal(a, b pVal) int {
	at, bt := unwrapType(a.typ), unwrapType(b.typ)
	if at != nil && at.kind == tySet {
		eq := setEq(a, b)
		if eq {
			return 0
		}
		if setSubset(a, b) {
			return -1
		}
		return 1
	}
	if at != nil && at.isPackedCharArray() {
		return strings.Compare(strings.TrimRight(packedString(a), " "), strings.TrimRight(packedString(b), " "))
	}
	if at != nil && at.kind == tyReal || bt != nil && bt.kind == tyReal {
		d := asReal(a) - asReal(b)
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
		return 0
	}
	if at != nil && at.kind == tyPointer {
		var aid, bid int64
		if a.ptr != nil {
			aid = a.ptr.id
		}
		if b.ptr != nil {
			bid = b.ptr.id
		}
		switch {
		case aid < bid:
			return -1
		case aid > bid:
			return 1
		}
		return 0
	}
	if (a.s != "" || b.s != "") && (at == nil || !at.ordinal()) && (bt == nil || !bt.ordinal()) {
		return strings.Compare(a.s, b.s)
	}
	switch {
	case a.i < b.i:
		return -1
	case a.i > b.i:
		return 1
	}
	return 0
}

func (rt *pRT) lvalue(e *pExpr) (*pVal, error) {
	if e == nil {
		return nil, pasErr("lvalue", 0, 0)
	}
	switch e.op {
	case exIdent:
		if wr := rt.withField(e.name); wr != nil {
			return wr, nil
		}
		if e.sym != nil && e.sym.kind == skConst {
			v := copyVal(e.sym.cval)
			return &v, nil
		}
		return rt.varSlot(e.sym, e.name, e.line, e.col)
	case exIndex:
		base, err := rt.lvalue(e.x)
		if err != nil {
			return nil, err
		}
		t := e.x.typ
		if base.typ != nil && base.typ.kind == tyArray {
			t = base.typ
		}
		v := base
		for _, ix := range e.args {
			iv, err := rt.eval(ix)
			if err != nil {
				return nil, err
			}
			if t == nil || t.kind != tyArray {
				return nil, pasErr("array expected", e.line, e.col)
			}
			expandPacked(v, t)
			lo, _ := t.index.rangeBounds()
			i := int(iv.i - lo)
			if i < 0 || i >= len(v.elems) {
				return nil, pasErr("index out of range", e.line, e.col)
			}
			v = &v.elems[i]
			t = t.elem
		}
		return v, nil
	case exField:
		base, err := rt.lvalue(e.x)
		if err != nil {
			return nil, err
		}
		t := unwrapType(e.x.typ)
		if t == nil || t.kind != tyRecord {
			return nil, pasErr("record expected", e.line, e.col)
		}
		for i, f := range t.fields {
			if f.name == e.field {
				if i >= len(base.elems) {
					return nil, pasErr("record field", e.line, e.col)
				}
				return &base.elems[i], nil
			}
		}
		return nil, pasErr("unknown field "+e.field, e.line, e.col)
	case exDeref:
		p, err := rt.lvalue(e.x)
		if err != nil {
			return nil, err
		}
		if p.file != nil {
			return &p.file.buffer, nil
		}
		if p.ptr == nil || !p.ptr.live {
			return nil, pasErr("nil pointer dereference", e.line, e.col)
		}
		return &p.ptr.val, nil
	}
	v, err := rt.eval(e)
	if err != nil {
		return nil, err
	}
	tmp := v
	return &tmp, nil
}

func (rt *pRT) withField(name string) *pVal {
	for i := len(rt.withs) - 1; i >= 0; i-- {
		w := rt.withs[i]
		t := unwrapType(w.typ)
		if t == nil || t.kind != tyRecord {
			continue
		}
		for j, f := range t.fields {
			if f.name == name {
				if j < len(w.val.elems) {
					return &w.val.elems[j]
				}
			}
		}
	}
	return nil
}

func (rt *pRT) varSlot(sy *pSym, name string, line, col int) (*pVal, error) {
	if sy == nil {
		return nil, pasErr("unknown identifier "+name, line, col)
	}
	if sy.kind == skConst {
		v := copyVal(sy.cval)
		return &v, nil
	}
	f := rt.fp
	if f == nil {
		return nil, pasErr("no frame", line, col)
	}
	for f != nil && f.level > sy.level {
		f = f.slink
	}
	if f == nil {
		// globals live on the outermost frame; INPUT/OUTPUT on a side table
		if name == "INPUT" {
			return &pVal{typ: rt.prog.textType, file: rt.in}, nil
		}
		if name == "OUTPUT" {
			return &pVal{typ: rt.prog.textType, file: rt.out}, nil
		}
		return nil, pasErr("unknown identifier "+name, line, col)
	}
	rt.ensureSlot(f, sy)
	slot := &f.slots[sy.offset]
	if slot.ref != nil {
		return slot.ref, nil
	}
	return slot, nil
}

func (rt *pRT) ensureSlot(f *pFrame, sy *pSym) {
	if sy.offset >= len(f.slots) {
		n := make([]pVal, sy.offset+8)
		copy(n, f.slots)
		f.slots = n
	}
	if f.slots[sy.offset].typ == nil && sy.typ != nil && f.slots[sy.offset].ref == nil && f.slots[sy.offset].proc == nil {
		f.slots[sy.offset] = zeroVal(sy.typ)
	}
}

func expandPacked(v *pVal, t *pType) {
	if v == nil || t == nil || !t.isPackedCharArray() {
		return
	}
	n := t.arrayLen()
	if n < 0 {
		n = 0
	}
	if len(v.elems) == n {
		return
	}
	v.elems = make([]pVal, n)
	for i := 0; i < n; i++ {
		ch := byte(' ')
		if i < len(v.s) {
			ch = v.s[i]
		}
		v.elems[i] = pVal{typ: t.elem, i: int64(ch), s: string(ch)}
	}
}

func packedString(v pVal) string {
	if len(v.elems) > 0 {
		b := make([]byte, len(v.elems))
		for i := range v.elems {
			b[i] = byte(v.elems[i].i)
		}
		return string(b)
	}
	return v.s
}

func zeroVal(t *pType) pVal {
	if t == nil {
		return pVal{}
	}
	v := pVal{typ: t}
	switch t.kind {
	case tyArray:
		if t.isPackedCharArray() {
			n := t.arrayLen()
			if n < 0 {
				n = 0
			}
			v.s = strings.Repeat(" ", n)
			return v
		}
		n := t.arrayLen()
		if n < 0 {
			n = 0
		}
		v.elems = make([]pVal, n)
		for i := range v.elems {
			v.elems[i] = zeroVal(t.elem)
		}
	case tyRecord:
		v.elems = make([]pVal, len(t.fields))
		for i, f := range t.fields {
			v.elems[i] = zeroVal(f.typ)
		}
	}
	return v
}

func copyVal(v pVal) pVal {
	out := v
	if v.elems != nil {
		out.elems = make([]pVal, len(v.elems))
		for i := range v.elems {
			out.elems[i] = copyVal(v.elems[i])
		}
	}
	if v.bits != nil {
		out.bits = append([]uint64(nil), v.bits...)
	}
	return out
}

func assignVal(dst *pVal, src pVal) {
	if dst == nil {
		return
	}
	dt, st := unwrapType(dst.typ), unwrapType(src.typ)
	if dt != nil && dt.kind == tyReal && (st == nil || st.kind != tyReal) {
		dst.f = float64(src.i)
		dst.typ = dt
		return
	}
	if dt != nil && dt.isPackedCharArray() {
		n := dt.arrayLen()
		s := packedString(src)
		if st != nil && st.kind == tyChar {
			s = string(byte(src.i))
		}
		if len(s) < n {
			s = s + strings.Repeat(" ", n-len(s))
		}
		if len(s) > n {
			s = s[:n]
		}
		dst.s = s
		dst.elems = nil
		dst.typ = dt
		return
	}
	c := copyVal(src)
	c.typ = dst.typ
	if dst.typ == nil {
		c.typ = src.typ
	}
	*dst = c
}

func rangeAssign(dst *pType, v pVal, line, col int) error {
	if dst == nil || !dst.ordinal() {
		return nil
	}
	if !ordInRange(dst, v.i) {
		return pasErr("value out of range", line, col)
	}
	return nil
}

func setOrdIndex(elem *pType, ord int64) (int64, bool) {
	if elem != nil {
		lo, hi := elem.rangeBounds()
		span := hi - lo + 1
		if span > 0 && span <= pascalSetSpan && (ord < lo || ord > hi) {
			return 0, false
		}
	}
	if ord >= 0 && ord < pascalSetSpan {
		return ord, true
	}
	if elem == nil {
		return 0, false
	}
	lo, _ := elem.rangeBounds()
	i := ord - lo
	if i < 0 || i >= pascalSetSpan {
		return 0, false
	}
	return i, true
}

func setBit(v *pVal, ord int64, on bool) {
	if v.typ == nil {
		return
	}
	i, ok := setOrdIndex(v.typ.elem, ord)
	if !ok {
		return
	}
	word, bit := int(i/64), uint(i%64)
	for len(v.bits) <= word {
		v.bits = append(v.bits, 0)
	}
	if on {
		v.bits[word] |= 1 << bit
	} else {
		v.bits[word] &^= 1 << bit
	}
}

func inSet(s pVal, ord int64) bool {
	if s.typ == nil {
		return false
	}
	i, ok := setOrdIndex(s.typ.elem, ord)
	if !ok {
		return false
	}
	word, bit := int(i/64), uint(i%64)
	if word >= len(s.bits) {
		return false
	}
	return s.bits[word]&(1<<bit) != 0
}

func setOp(op ptokKind, a, b pVal) pVal {
	r := pVal{typ: a.typ}
	n := len(a.bits)
	if len(b.bits) > n {
		n = len(b.bits)
	}
	r.bits = make([]uint64, n)
	for i := 0; i < n; i++ {
		var av, bv uint64
		if i < len(a.bits) {
			av = a.bits[i]
		}
		if i < len(b.bits) {
			bv = b.bits[i]
		}
		switch op {
		case tkPlus:
			r.bits[i] = av | bv
		case tkStar:
			r.bits[i] = av & bv
		case tkMinus:
			r.bits[i] = av &^ bv
		}
	}
	return r
}

func setEq(a, b pVal) bool {
	n := len(a.bits)
	if len(b.bits) > n {
		n = len(b.bits)
	}
	for i := 0; i < n; i++ {
		var av, bv uint64
		if i < len(a.bits) {
			av = a.bits[i]
		}
		if i < len(b.bits) {
			bv = b.bits[i]
		}
		if av != bv {
			return false
		}
	}
	return true
}

func setSubset(a, b pVal) bool {
	n := len(a.bits)
	if len(b.bits) > n {
		n = len(b.bits)
	}
	for i := 0; i < n; i++ {
		var av, bv uint64
		if i < len(a.bits) {
			av = a.bits[i]
		}
		if i < len(b.bits) {
			bv = b.bits[i]
		}
		if av&^bv != 0 {
			return false
		}
	}
	return true
}

func (rt *pRT) resolveCallee(sy *pSym, line, col int) (*pSym, *pFrame, error) {
	if sy == nil {
		return nil, nil, pasErr("call", line, col)
	}
	if sy.procParam {
		slot, err := rt.varSlot(sy, sy.name, line, col)
		if err != nil {
			return nil, nil, err
		}
		if slot.proc == nil {
			return nil, nil, pasErr("unbound procedure "+sy.name, line, col)
		}
		return slot.proc, slot.slink, nil
	}
	slink := rt.fp
	for slink != nil && slink.level > sy.level {
		slink = slink.slink
	}
	return sy, slink, nil
}

func (rt *pRT) bindProcArg(arg *pExpr) (pVal, error) {
	sy := arg.sym
	if sy != nil && (sy.kind == skProc || sy.kind == skFunc) {
		cal, sl, err := rt.resolveCallee(sy, arg.line, arg.col)
		if err != nil {
			return pVal{}, err
		}
		return pVal{proc: cal, slink: sl}, nil
	}
	return pVal{}, pasErr("procedure expected", arg.line, arg.col)
}

func (rt *pRT) callProc(e *pExpr) (pVal, error) {
	sy, slink, err := rt.resolveCallee(e.sym, e.line, e.col)
	if err != nil {
		return pVal{}, err
	}
	pr := sy.proc
	if pr == nil || pr.block == nil {
		return pVal{}, pasErr("undefined procedure "+sy.name, e.line, e.col)
	}
	if slink == nil {
		slink = rt.fp
		for slink != nil && slink.level > sy.level {
			slink = slink.slink
		}
	}
	fr := &pFrame{level: sy.level + 1, slink: slink, slots: make([]pVal, 64+len(sy.params))}
	for i, ps := range sy.params {
		if i >= len(e.args) {
			break
		}
		fr.slots = growSlots(fr.slots, ps.offset)
		if ps.procParam || ps.kind == skProc || ps.kind == skFunc {
			pv, err := rt.bindProcArg(e.args[i])
			if err != nil {
				return pVal{}, err
			}
			fr.slots[ps.offset] = pv
			continue
		}
		if ps.byRef {
			lv, err := rt.lvalue(e.args[i])
			if err != nil {
				return pVal{}, err
			}
			fr.slots[ps.offset].ref = lv
			fr.slots[ps.offset].typ = lv.typ
			if lv.typ == nil {
				fr.slots[ps.offset].typ = ps.typ
			}
			rt.bindConfBounds(fr, ps, lv.typ)
			continue
		}
		v, err := rt.eval(e.args[i])
		if err != nil {
			return pVal{}, err
		}
		if ps.typ != nil && ps.typ.conf {
			z := copyVal(v)
			if v.typ != nil {
				z.typ = v.typ
			}
			fr.slots[ps.offset] = z
			rt.bindConfBounds(fr, ps, z.typ)
			continue
		}
		z := zeroVal(ps.typ)
		assignVal(&z, v)
		if v.typ != nil && v.typ.kind == tyArray {
			z.typ = v.typ
		}
		fr.slots[ps.offset] = z
		rt.bindConfBounds(fr, ps, z.typ)
	}
	if sy.kind == skFunc && sy.ret != nil {
		fr.slots = growSlots(fr.slots, pr.retOff)
		if fr.slots[pr.retOff].typ == nil {
			fr.slots[pr.retOff] = zeroVal(sy.ret)
		}
	}
	prev := rt.fp
	rt.fp = fr
	_, err = rt.exec(pr.block.body)
	rt.fp = prev
	if err != nil {
		return pVal{}, err
	}
	if sy.kind == skFunc {
		off := pr.retOff
		fr.slots = growSlots(fr.slots, off)
		if fr.slots[off].typ == nil {
			fr.slots[off] = zeroVal(sy.ret)
		}
		return copyVal(fr.slots[off]), nil
	}
	return pVal{typ: &pType{kind: tyDummy}}, nil
}

func (rt *pRT) bindConfBounds(fr *pFrame, ps *pSym, t *pType) {
	if ps == nil || len(ps.confB) == 0 {
		return
	}
	bi := 0
	for u := t; u != nil && u.kind == tyArray && bi < len(ps.confB); u = u.elem {
		lo, hi := int64(0), int64(0)
		if u.index != nil {
			lo, hi = u.index.rangeBounds()
		}
		fr.slots = growSlots(fr.slots, ps.confB[bi].offset)
		fr.slots[ps.confB[bi].offset] = pVal{typ: rt.prog.intType, i: lo}
		if bi+1 < len(ps.confB) {
			fr.slots = growSlots(fr.slots, ps.confB[bi+1].offset)
			fr.slots[ps.confB[bi+1].offset] = pVal{typ: rt.prog.intType, i: hi}
		}
		bi += 2
	}
}

type pAlias struct {
	fr  *pFrame
	off int
	dst *pVal
}

func (rt *pRT) flushAliases(fr *pFrame) {
	left := rt.aliases[:0]
	for _, a := range rt.aliases {
		if a.fr == fr {
			if a.off < len(fr.slots) {
				*a.dst = fr.slots[a.off]
			}
		} else {
			left = append(left, a)
		}
	}
	rt.aliases = left
}

func growSlots(s []pVal, off int) []pVal {
	if off < len(s) {
		return s
	}
	n := make([]pVal, off+8)
	copy(n, s)
	return n
}

func (rt *pRT) stdCall(name string, args []*pExpr) (pVal, error) {
	switch name {
	case "WRITELN", "WRITE":
		return rt.doWrite(args, name == "WRITELN")
	case "READLN", "READ":
		return rt.doRead(args, name == "READLN")
	case "ABS":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		if unwrapType(v.typ) != nil && unwrapType(v.typ).kind == tyReal {
			if v.f < 0 {
				v.f = -v.f
			}
			return v, nil
		}
		if v.i < 0 {
			v.i = -v.i
		}
		return v, nil
	case "SQR":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		if unwrapType(v.typ) != nil && unwrapType(v.typ).kind == tyReal {
			v.f = v.f * v.f
			return v, nil
		}
		v.i = v.i * v.i
		return v, nil
	case "SIN", "COS", "EXP", "LN", "SQRT", "ARCTAN":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		x := asReal(v)
		r := pVal{typ: rt.prog.realType}
		switch name {
		case "SIN":
			r.f = math.Sin(x)
		case "COS":
			r.f = math.Cos(x)
		case "EXP":
			r.f = math.Exp(x)
		case "LN":
			r.f = math.Log(x)
		case "SQRT":
			r.f = math.Sqrt(x)
		case "ARCTAN":
			r.f = math.Atan(x)
		}
		return r, nil
	case "TRUNC":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		return pVal{typ: rt.prog.intType, i: int64(asReal(v))}, nil
	case "ROUND":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		return pVal{typ: rt.prog.intType, i: int64(math.Round(asReal(v)))}, nil
	case "ORD":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		return pVal{typ: rt.prog.intType, i: v.i}, nil
	case "CHR":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		if v.i < 0 || v.i > 255 {
			return pVal{}, pasErr("CHR argument out of range", args[0].line, args[0].col)
		}
		return pVal{typ: rt.prog.charType, i: v.i & 255, s: string(byte(v.i))}, nil
	case "SUCC":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		v.i++
		if !ordInRange(v.typ, v.i) {
			return pVal{}, pasErr("SUCC out of range", args[0].line, args[0].col)
		}
		return v, nil
	case "PRED":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		v.i--
		if !ordInRange(v.typ, v.i) {
			return pVal{}, pasErr("PRED out of range", args[0].line, args[0].col)
		}
		return v, nil
	case "ODD":
		v, err := rt.arg1(args)
		if err != nil {
			return pVal{}, err
		}
		return pVal{typ: rt.prog.boolType, i: v.i & 1}, nil
	case "EOF":
		f := rt.in
		if len(args) > 0 {
			fv, err := rt.eval(args[0])
			if err != nil {
				return pVal{}, err
			}
			if fv.file != nil {
				f = fv.file
			}
		}
		return pVal{typ: rt.prog.boolType, i: boolI(f != nil && f.eof)}, nil
	case "EOLN":
		f := rt.in
		if len(args) > 0 {
			fv, err := rt.eval(args[0])
			if err != nil {
				return pVal{}, err
			}
			if fv.file != nil {
				f = fv.file
			}
		}
		return pVal{typ: rt.prog.boolType, i: boolI(f != nil && f.eoln)}, nil
	case "NEW":
		if len(args) < 1 {
			return pVal{}, pasErr("NEW needs a pointer", 0, 0)
		}
		lv, err := rt.lvalue(args[0])
		if err != nil {
			return pVal{}, err
		}
		t := unwrapType(args[0].typ)
		base := rt.prog.intType
		if t != nil && t.ptrTo != nil {
			base = t.ptrTo
		}
		rt.heapN++
		h := &pHeap{id: rt.heapN, live: true, val: zeroVal(base)}
		for ti := 1; ti < len(args); ti++ {
			tv, err := rt.eval(args[ti])
			if err != nil {
				return pVal{}, err
			}
			rt.applyNewTag(&h.val, tv)
		}
		rt.heap = append(rt.heap, h)
		lv.ptr = h
		lv.typ = t
		return pVal{}, nil
	case "DISPOSE":
		if len(args) < 1 {
			return pVal{}, nil
		}
		lv, err := rt.lvalue(args[0])
		if err != nil {
			return pVal{}, err
		}
		if lv.ptr != nil {
			lv.ptr.live = false
			lv.ptr = nil
		}
		return pVal{}, nil
	case "RESET", "REWRITE":
		return rt.doOpen(args, name == "REWRITE")
	case "GET":
		return rt.doGet(args)
	case "PUT":
		return rt.doPut(args)
	case "PAGE":
		rt.writeOut("\f")
		return pVal{}, nil
	case "PACK":
		return rt.doPackUnpack(args, true)
	case "UNPACK":
		return rt.doPackUnpack(args, false)
	}
	return pVal{}, pasErr("unknown standard "+name, 0, 0)
}

func (rt *pRT) arg1(args []*pExpr) (pVal, error) {
	if len(args) < 1 {
		return pVal{}, pasErr("argument expected", 0, 0)
	}
	return rt.eval(args[0])
}

type pFile struct {
	text     bool
	name     string
	mode     int // 0 closed 1 in 2 out
	r        *bufio.Reader
	w        strings.Builder
	buf      string
	pos      int
	eof      bool
	eoln     bool
	body     string
	elems    []pVal
	epos     int
	buffer   pVal
	elemType *pType
}

func (rt *pRT) flushFiles() {
	for name, f := range rt.files {
		if f != nil && f.mode == 2 && rt.host.OpenWrite != nil {
			_ = rt.host.OpenWrite(name, f.w.String())
		}
	}
}

func (rt *pRT) writeOut(s string) {
	if rt.host.Write != nil {
		rt.host.Write(s)
	}
}

func (rt *pRT) doWrite(args []*pExpr, nl bool) (pVal, error) {
	i := 0
	dest := rt.out
	if len(args) > 0 && args[0].typ != nil {
		u := unwrapType(args[0].typ)
		if u != nil && (u.kind == tyText || u.kind == tyFile) {
			fv, err := rt.eval(args[0])
			if err != nil {
				return pVal{}, err
			}
			if fv.file != nil {
				dest = fv.file
			}
			i = 1
		}
	}
	if dest != nil && !dest.text {
		for ; i < len(args); i++ {
			v, err := rt.eval(args[i])
			if err != nil {
				return pVal{}, err
			}
			dest.buffer = copyVal(v)
			if dest.elemType != nil {
				dest.buffer.typ = dest.elemType
			}
			dest.elems = append(dest.elems, copyVal(dest.buffer))
		}
		return pVal{}, nil
	}
	var b strings.Builder
	for ; i < len(args); i++ {
		v, err := rt.eval(args[i])
		if err != nil {
			return pVal{}, err
		}
		w, d := 0, -1
		if args[i].width != nil {
			wv, err := rt.eval(args[i].width)
			if err != nil {
				return pVal{}, err
			}
			w = int(wv.i)
		}
		if args[i].prec != nil {
			dv, err := rt.eval(args[i].prec)
			if err != nil {
				return pVal{}, err
			}
			d = int(dv.i)
		}
		b.WriteString(formatPas(v, w, d))
	}
	if nl {
		b.WriteByte('\n')
	}
	if dest == rt.out || dest == nil {
		rt.writeOut(b.String())
	} else {
		dest.w.WriteString(b.String())
	}
	return pVal{}, nil
}

func formatPas(v pVal, width, dec int) string {
	t := unwrapType(v.typ)
	s := ""
	switch {
	case t != nil && t.kind == tyReal:
		if dec >= 0 {
			s = fmt.Sprintf("%.*f", dec, v.f)
		} else {
			s = fmt.Sprintf("%g", v.f)
		}
	case t != nil && t.kind == tyBoolean:
		if v.i != 0 {
			s = "TRUE"
		} else {
			s = "FALSE"
		}
	case t != nil && t.kind == tyChar:
		s = string(byte(v.i))
	case t != nil && t.isPackedCharArray():
		s = strings.TrimRight(packedString(v), " ")
		if s == "" {
			s = packedString(v)
		}
	case t != nil && t.kind == tyEnum && v.i >= 0 && int(v.i) < len(t.enums):
		s = t.enums[v.i]
	default:
		s = fmt.Sprintf("%d", v.i)
	}
	if width > len(s) {
		s = strings.Repeat(" ", width-len(s)) + s
	}
	return s
}

func (rt *pRT) refill(f *pFile) error {
	if f == nil {
		f = rt.in
	}
	if f.pos < len(f.buf) {
		return nil
	}
	if f != rt.in {
		f.eof = true
		f.eoln = true
		return io.EOF
	}
	if rt.host.ReadLine == nil {
		f.eof = true
		f.eoln = true
		return io.EOF
	}
	line, err := rt.host.ReadLine()
	if err != nil {
		f.eof = true
		f.eoln = true
		return err
	}
	f.buf = line + "\n"
	f.pos = 0
	f.eoln = false
	f.eof = false
	return nil
}

func (rt *pRT) doRead(args []*pExpr, ln bool) (pVal, error) {
	i := 0
	src := rt.in
	if len(args) > 0 && args[0].typ != nil {
		u := unwrapType(args[0].typ)
		if u != nil && (u.kind == tyText || u.kind == tyFile) {
			fv, err := rt.eval(args[0])
			if err != nil {
				return pVal{}, err
			}
			if fv.file != nil {
				src = fv.file
			}
			i = 1
		}
	}
	for ; i < len(args); i++ {
		lv, err := rt.lvalue(args[i])
		if err != nil {
			return pVal{}, err
		}
		if src != nil && !src.text {
			if src.eof {
				return pVal{}, io.EOF
			}
			assignVal(lv, src.buffer)
			src.epos++
			rt.fillTypedBuffer(src)
			continue
		}
		t := unwrapType(args[i].typ)
		if err := rt.refill(src); err != nil && src.pos >= len(src.buf) {
			return pVal{}, err
		}
		s := ""
		if src.pos < len(src.buf) {
			s = src.buf[src.pos:]
		}
		switch {
		case t != nil && t.kind == tyChar:
			if s == "" {
				lv.i = 10
				lv.s = "\n"
			} else {
				lv.i = int64(s[0])
				lv.s = s[:1]
				src.pos++
			}
		case t != nil && t.isPackedCharArray():
			line := strings.TrimRight(s, "\n")
			assignVal(lv, pVal{typ: t, s: line})
			nl := strings.IndexByte(s, '\n')
			if nl >= 0 {
				src.pos += nl + 1
			} else {
				src.pos = len(src.buf)
			}
		default:
			s = skipWS(s)
			num, rest := scanNum(s)
			src.pos = len(src.buf) - len(rest)
			if t != nil && t.kind == tyReal {
				var f float64
				fmt.Sscanf(num, "%f", &f)
				lv.f = f
				lv.typ = t
			} else {
				var n int64
				fmt.Sscanf(num, "%d", &n)
				lv.i = n
				lv.typ = t
				if err := rangeAssign(args[i].typ, pVal{typ: args[i].typ, i: n}, args[i].line, args[i].col); err != nil {
					return pVal{}, err
				}
			}
		}
		if src.pos >= len(src.buf) || (src.pos < len(src.buf) && src.buf[src.pos] == '\n') {
			src.eoln = true
		}
	}
	if ln {
		if src.pos < len(src.buf) {
			if j := strings.IndexByte(src.buf[src.pos:], '\n'); j >= 0 {
				src.pos += j + 1
			} else {
				src.pos = len(src.buf)
			}
		}
		src.eoln = true
	}
	return pVal{}, nil
}

func skipWS(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	return s[i:]
}

func scanNum(s string) (num, rest string) {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	return s[:i], s[i:]
}

func (rt *pRT) doOpen(args []*pExpr, rewrite bool) (pVal, error) {
	if len(args) < 1 {
		return pVal{}, pasErr("file expected", 0, 0)
	}
	lv, err := rt.lvalue(args[0])
	if err != nil {
		return pVal{}, err
	}
	name := "FILE.DAT"
	if args[0].name != "" {
		name = args[0].name + ".DAT"
	}
	if len(args) > 1 {
		v, err := rt.eval(args[1])
		if err != nil {
			return pVal{}, err
		}
		if v.s != "" {
			name = strings.TrimSpace(v.s)
		}
	}
	f := &pFile{text: true, name: name}
	if u := unwrapType(args[0].typ); u != nil && u.kind == tyFile {
		f.text = false
		f.elemType = u.elem
		if f.elemType != nil {
			f.buffer = zeroVal(f.elemType)
		}
	} else {
		f.buffer = pVal{typ: rt.prog.charType}
	}
	if rewrite {
		f.mode = 2
		f.eof = true
	} else {
		f.mode = 1
		if f.text {
			if rt.host.OpenRead != nil {
				body, err := rt.host.OpenRead(name)
				if err != nil {
					return pVal{}, err
				}
				f.body = body
				f.r = bufio.NewReader(strings.NewReader(body))
				f.buf = body
			}
			rt.fillTextBuffer(f)
		} else if old := rt.files[name]; old != nil && !old.text {
			f.elems = old.elems
			f.epos = 0
			rt.fillTypedBuffer(f)
		} else {
			f.eof = true
		}
	}
	lv.file = f
	rt.files[name] = f
	return pVal{}, nil
}

func (rt *pRT) fillTextBuffer(f *pFile) {
	if f.pos >= len(f.buf) {
		if err := rt.refill(f); err != nil {
			f.eof = true
			f.eoln = true
			return
		}
	}
	if f.pos >= len(f.buf) {
		f.eof = true
		f.eoln = true
		return
	}
	ch := f.buf[f.pos]
	f.buffer = pVal{typ: rt.prog.charType, i: int64(ch), s: string(ch)}
	f.eoln = ch == '\n'
	f.eof = false
}

func (rt *pRT) fillTypedBuffer(f *pFile) {
	if f.epos >= len(f.elems) {
		f.eof = true
		return
	}
	f.buffer = copyVal(f.elems[f.epos])
	f.eof = false
}

func (rt *pRT) doGet(args []*pExpr) (pVal, error) {
	f := rt.in
	if len(args) > 0 {
		fv, err := rt.eval(args[0])
		if err != nil {
			return pVal{}, err
		}
		if fv.file != nil {
			f = fv.file
		}
	}
	if f == nil {
		return pVal{}, pasErr("GET needs a file", 0, 0)
	}
	if f.text {
		if f.pos < len(f.buf) {
			f.pos++
		}
		rt.fillTextBuffer(f)
		return pVal{}, nil
	}
	if !f.eof {
		f.epos++
	}
	rt.fillTypedBuffer(f)
	return pVal{}, nil
}

func (rt *pRT) doPut(args []*pExpr) (pVal, error) {
	if len(args) < 1 {
		return pVal{}, pasErr("PUT needs a file", 0, 0)
	}
	fv, err := rt.eval(args[0])
	if err != nil {
		return pVal{}, err
	}
	f := fv.file
	if f == nil {
		return pVal{}, pasErr("PUT needs a file", 0, 0)
	}
	if f.text {
		ch := byte(f.buffer.i)
		if f.buffer.s != "" {
			ch = f.buffer.s[0]
		}
		f.w.WriteByte(ch)
		return pVal{}, nil
	}
	f.elems = append(f.elems, copyVal(f.buffer))
	return pVal{}, nil
}

func (rt *pRT) applyNewTag(rec *pVal, tag pVal) {
	t := unwrapType(rec.typ)
	if t == nil || t.kind != tyRecord || t.tagName == "" {
		return
	}
	for i, f := range t.fields {
		if f.name == t.tagName {
			if i < len(rec.elems) {
				assignVal(&rec.elems[i], tag)
			}
			return
		}
	}
}

func (rt *pRT) doPackUnpack(args []*pExpr, pack bool) (pVal, error) {
	if len(args) < 3 {
		return pVal{}, pasErr("PACK/UNPACK needs 3 arguments", 0, 0)
	}
	if pack {
		a, err := rt.lvalue(args[0])
		if err != nil {
			return pVal{}, err
		}
		iv, err := rt.eval(args[1])
		if err != nil {
			return pVal{}, err
		}
		z, err := rt.lvalue(args[2])
		if err != nil {
			return pVal{}, err
		}
		at := a.typ
		if at == nil {
			at = args[0].typ
		}
		zt := z.typ
		if zt == nil {
			zt = args[2].typ
		}
		expandPacked(a, at)
		expandPacked(z, zt)
		n := 0
		if zt != nil {
			n = zt.arrayLen()
		}
		if n == 0 {
			n = len(z.elems)
		}
		lo := int64(0)
		if at != nil && at.index != nil {
			lo, _ = at.index.rangeBounds()
		}
		start := int(iv.i - lo)
		if z.elems == nil {
			z.elems = make([]pVal, n)
		}
		for k := 0; k < n && start+k < len(a.elems); k++ {
			if start+k >= 0 {
				z.elems[k] = copyVal(a.elems[start+k])
			}
		}
		if zt != nil && zt.isPackedCharArray() {
			z.s = packedString(*z)
		}
		return pVal{}, nil
	}
	z, err := rt.lvalue(args[0])
	if err != nil {
		return pVal{}, err
	}
	a, err := rt.lvalue(args[1])
	if err != nil {
		return pVal{}, err
	}
	iv, err := rt.eval(args[2])
	if err != nil {
		return pVal{}, err
	}
	zt := z.typ
	if zt == nil {
		zt = args[0].typ
	}
	at := a.typ
	if at == nil {
		at = args[1].typ
	}
	expandPacked(z, zt)
	expandPacked(a, at)
	n := len(z.elems)
	lo := int64(0)
	if at != nil && at.index != nil {
		lo, _ = at.index.rangeBounds()
	}
	start := int(iv.i - lo)
	if a.elems == nil {
		a.elems = make([]pVal, start+n)
	}
	for k := 0; k < n && start+k < len(a.elems); k++ {
		if start+k >= 0 {
			a.elems[start+k] = copyVal(z.elems[k])
		}
	}
	return pVal{}, nil
}
