package rsts

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strings"
)

type pVM struct {
	rt    *pRT
	img   *pacImage
	stack []pVal
	addrs []*pVal
	ip    int
	calls []pRet
	err   error
}

type pRet struct {
	ip       int
	fp       *pFrame
	proc     int
	withMark int
}

func runPascalImage(img *pacImage, host PascalHost) error {
	if img == nil {
		return pasErr("Compiled file", 0, 0)
	}
	prog := &pProgram{
		intType:  img.typ(img.IntType),
		realType: img.typ(img.RealType),
		boolType: img.typ(img.BoolType),
		charType: img.typ(img.CharType),
		textType: img.typ(img.TextType),
		nilType:  img.typ(img.NilType),
	}
	rt := &pRT{prog: prog, host: host, files: map[string]*pFile{}, mem: map[string]string{}}
	rt.in = &pFile{text: true, name: "INPUT", mode: 1}
	rt.out = &pFile{text: true, name: "OUTPUT", mode: 2}
	rt.in.buffer = pVal{typ: prog.charType}
	rt.out.buffer = pVal{typ: prog.charType}
	glob := &pFrame{level: 0, slots: make([]pVal, 8)}
	glob.slots[0] = pVal{typ: prog.textType, file: rt.in}
	glob.slots[1] = pVal{typ: prog.textType, file: rt.out}
	n := img.MainNSlot
	if n < 1 {
		n = 1
	}
	rt.fp = &pFrame{level: 1, slink: glob, slots: make([]pVal, n)}
	vm := &pVM{rt: rt, img: img, ip: img.Entry}
	err := vm.run()
	rt.flushFiles()
	if err != nil {
		return err
	}
	return vm.err
}

func (vm *pVM) run() error {
	code := vm.img.Code
	n := 0
	for vm.ip >= 0 && vm.ip < len(code) && vm.err == nil && !vm.rt.stop {
		n++
		if n&63 == 0 && vm.rt.host.PollStop != nil && vm.rt.host.PollStop() {
			vm.rt.stop = true
			return fmt.Errorf("^C")
		}
		in := code[vm.ip]
		vm.ip++
		if err := vm.step(in); err != nil {
			return err
		}
	}
	return vm.err
}

func (vm *pVM) step(in pacInst) error {
	switch in.Op {
	case poHalt:
		vm.ip = len(vm.img.Code)
	case poPushI:
		vm.push(pVal{typ: vm.ty(in.B), i: in.I})
	case poPushF:
		vm.push(pVal{typ: vm.ty(in.B), f: in.F})
	case poPushC:
		vm.push(pVal{typ: vm.ty(in.B), i: in.I, s: string(byte(in.I))})
	case poPushS:
		vm.push(pVal{typ: vm.ty(in.B), s: vm.img.str(in.A)})
	case poPushB:
		vm.push(pVal{typ: vm.ty(in.B), i: in.I})
	case poPushNil:
		vm.push(pVal{typ: vm.ty(in.B)})
	case poPushSet:
		vm.push(pVal{typ: vm.ty(in.B)})
	case poAddrVar:
		a, err := vm.addrVar(int(in.A), int(in.B), vm.ty(int32(in.I)))
		if err != nil {
			return err
		}
		vm.pushAddr(a)
	case poAddrIdx:
		if err := vm.addrIdx(int(in.A), int(in.I)); err != nil {
			return err
		}
	case poAddrField:
		base, err := vm.popAddr()
		if err != nil {
			return err
		}
		t := unwrapType(base.typ)
		idx := int(in.A)
		if t != nil && t.kind == tyRecord && len(base.elems) < len(t.fields) {
			n := make([]pVal, len(t.fields))
			copy(n, base.elems)
			for i := range n {
				if n[i].typ == nil && i < len(t.fields) {
					n[i] = zeroVal(t.fields[i].typ)
				}
			}
			base.elems = n
		}
		if idx < 0 || idx >= len(base.elems) {
			return pasErr("record field", int(in.I), 0)
		}
		vm.pushAddr(&base.elems[idx])
	case poAddrDeref:
		p, err := vm.popAddr()
		if err != nil {
			return err
		}
		if p.file != nil {
			vm.pushAddr(&p.file.buffer)
			break
		}
		if p.ptr == nil || !p.ptr.live {
			return pasErr("nil pointer dereference", int(in.I), 0)
		}
		vm.pushAddr(&p.ptr.val)
	case poAddrWith:
		a, err := vm.addrWith(vm.img.str(in.A), int(in.B))
		if err != nil {
			return err
		}
		vm.pushAddr(a)
	case poLoad:
		a, err := vm.popAddr()
		if err != nil {
			return err
		}
		if a.ref != nil {
			a = a.ref
		}
		vm.push(copyVal(*a))
	case poStore:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.popAddr()
		if err != nil {
			return err
		}
		if a.ref != nil {
			a = a.ref
		}
		if in.B == 0 {
			if err := rangeAssign(a.typ, v, int(in.I), 0); err != nil {
				return err
			}
		}
		assignVal(a, v)
	case poDup:
		if len(vm.stack) == 0 {
			return pasErr("stack underflow", 0, 0)
		}
		vm.push(copyVal(vm.stack[len(vm.stack)-1]))
	case poPop:
		if _, err := vm.pop(); err != nil {
			return err
		}
	case poDupAddr:
		if len(vm.addrs) == 0 {
			return pasErr("stack underflow", 0, 0)
		}
		vm.pushAddr(vm.addrs[len(vm.addrs)-1])
	case poBin:
		y, err := vm.pop()
		if err != nil {
			return err
		}
		x, err := vm.pop()
		if err != nil {
			return err
		}
		r, err := vm.bin(ptokKind(in.A), x, y, int(in.I))
		if err != nil {
			return err
		}
		vm.push(r)
	case poUn:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		switch ptokKind(in.A) {
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
			v.typ = vm.rt.prog.boolType
		}
		vm.push(v)
	case poJmp:
		vm.popWiths(int(in.B))
		vm.ip = int(in.A)
	case poJz:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		if v.i == 0 {
			vm.ip = int(in.A)
		}
	case poCall:
		if err := vm.call(int(in.A), int(in.B), nil); err != nil {
			return err
		}
	case poCallVal:
		cal, err := vm.pop()
		if err != nil {
			return err
		}
		if err := vm.call(int(cal.i), int(in.B), cal.slink); err != nil {
			return err
		}
	case poProcRef:
		vm.push(vm.procRef(int(in.A)))
	case poRet:
		return vm.ret()
	case poWithPush:
		a, err := vm.popAddr()
		if err != nil {
			return err
		}
		vm.rt.pushWith(a)
	case poWithPop:
		vm.rt.popWith(1)
	case poStd:
		return vm.std(vm.img.str(in.A), int(in.B), int(in.I))
	case poSetIncl:
		el, err := vm.pop()
		if err != nil {
			return err
		}
		if len(vm.stack) == 0 {
			return pasErr("stack underflow", 0, 0)
		}
		setBit(&vm.stack[len(vm.stack)-1], el.i, true)
	case poSetRange:
		hi, err := vm.pop()
		if err != nil {
			return err
		}
		lo, err := vm.pop()
		if err != nil {
			return err
		}
		if len(vm.stack) == 0 {
			return pasErr("stack underflow", 0, 0)
		}
		for i := lo.i; i <= hi.i; i++ {
			setBit(&vm.stack[len(vm.stack)-1], i, true)
		}
	case poCaseFail:
		return pasErr("case selector matches no constant", int(in.I), 0)
	case poNew:
		return vm.std("NEW", int(in.B), int(in.I))
	}
	return nil
}

func (vm *pVM) ty(id int32) *pType {
	return vm.img.typ(id)
}

func (vm *pVM) push(v pVal) { vm.stack = append(vm.stack, v) }

func (vm *pVM) pop() (pVal, error) {
	if len(vm.stack) == 0 {
		return pVal{}, pasErr("stack underflow", 0, 0)
	}
	v := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return v, nil
}

func (vm *pVM) pushAddr(a *pVal) { vm.addrs = append(vm.addrs, a) }

func (vm *pVM) popAddr() (*pVal, error) {
	if len(vm.addrs) == 0 {
		return nil, pasErr("stack underflow", 0, 0)
	}
	a := vm.addrs[len(vm.addrs)-1]
	vm.addrs = vm.addrs[:len(vm.addrs)-1]
	return a, nil
}

func (vm *pVM) dummy() pVal {
	return pVal{typ: &pType{kind: tyDummy}}
}

func (vm *pVM) addrVar(level, off int, t *pType) (*pVal, error) {
	f := vm.rt.fp
	for f != nil && f.level > level {
		f = f.slink
	}
	if f == nil {
		return nil, pasErr("unknown identifier", 0, 0)
	}
	if off >= len(f.slots) {
		n := make([]pVal, off+8)
		copy(n, f.slots)
		f.slots = n
	}
	slot := &f.slots[off]
	if slot.ref != nil {
		return slot.ref, nil
	}
	if slot.typ == nil && t != nil && slot.proc == nil && slot.i == 0 && slot.slink == nil {
		*slot = zeroVal(t)
	}
	return slot, nil
}

func (vm *pVM) addrIdx(n, line int) error {
	idxs := make([]int64, n)
	for i := n - 1; i >= 0; i-- {
		v, err := vm.pop()
		if err != nil {
			return err
		}
		idxs[i] = v.i
	}
	base, err := vm.popAddr()
	if err != nil {
		return err
	}
	v := base
	t := v.typ
	for _, ix := range idxs {
		if t == nil || t.kind != tyArray {
			return pasErr("array expected", line, 0)
		}
		expandPacked(v, t)
		lo, _ := t.index.rangeBounds()
		i := int(ix - lo)
		if i < 0 || i >= len(v.elems) {
			return pasErr("index out of range", line, 0)
		}
		v = &v.elems[i]
		t = t.elem
	}
	if v.typ == nil {
		v.typ = t
	}
	vm.pushAddr(v)
	return nil
}

func (vm *pVM) addrWith(name string, skip int) (*pVal, error) {
	i := len(vm.rt.withs) - 1 - skip
	if i < 0 || i >= len(vm.rt.withs) {
		return nil, pasErr("unknown field "+name, 0, 0)
	}
	w := vm.rt.withs[i]
	t := unwrapType(w.typ)
	if t == nil || t.kind != tyRecord {
		return nil, pasErr("record expected", 0, 0)
	}
	for j, f := range t.fields {
		if f.name == name {
			if j >= len(w.val.elems) {
				return nil, pasErr("record field", 0, 0)
			}
			return &w.val.elems[j], nil
		}
	}
	return nil, pasErr("unknown field "+name, 0, 0)
}

func (vm *pVM) popWiths(n int) {
	if n <= 0 {
		return
	}
	vm.rt.popWith(n)
}

func (vm *pVM) procRef(id int) pVal {
	slink := vm.rt.fp
	if id >= 0 && id < len(vm.img.Procs) {
		lev := int(vm.img.Procs[id].Level)
		for slink != nil && slink.level > lev {
			slink = slink.slink
		}
	}
	return pVal{i: int64(id), slink: slink}
}

func (vm *pVM) call(id, nargs int, slink *pFrame) error {
	if id < 0 || id >= len(vm.img.Procs) {
		return pasErr("undefined procedure", 0, 0)
	}
	info := vm.img.Procs[id]
	type bound struct {
		pm   pacParamInfo
		val  pVal
		addr *pVal
	}
	args := make([]bound, nargs)
	for i := nargs - 1; i >= 0; i-- {
		var pm pacParamInfo
		if i < len(info.Params) {
			pm = info.Params[i]
		}
		args[i].pm = pm
		if pm.Proc {
			v, err := vm.pop()
			if err != nil {
				return err
			}
			args[i].val = v
		} else if pm.ByRef {
			a, err := vm.popAddr()
			if err != nil {
				return err
			}
			args[i].addr = a
		} else {
			v, err := vm.pop()
			if err != nil {
				return err
			}
			args[i].val = v
		}
	}
	if slink == nil {
		slink = vm.rt.fp
		for slink != nil && slink.level > int(info.Level) {
			slink = slink.slink
		}
	}
	nslot := int(info.NSlot)
	if nslot < 8 {
		nslot = 8
	}
	fr := &pFrame{level: int(info.Level) + 1, slink: slink, slots: make([]pVal, nslot)}
	for _, a := range args {
		off := int(a.pm.Off)
		if off >= len(fr.slots) {
			n := make([]pVal, off+8)
			copy(n, fr.slots)
			fr.slots = n
		}
		if a.pm.Proc {
			fr.slots[off] = a.val
			continue
		}
		if a.pm.ByRef {
			lv := a.addr
			if lv != nil && lv.ref != nil {
				lv = lv.ref
			}
			fr.slots[off].ref = lv
			if lv != nil {
				fr.slots[off].typ = lv.typ
				vm.bindConf(fr, a.pm, lv.typ)
			}
			continue
		}
		pt := vm.img.typ(a.pm.Typ)
		if pt != nil && pt.conf {
			z := copyVal(a.val)
			if a.val.typ != nil {
				z.typ = a.val.typ
			}
			fr.slots[off] = z
			vm.bindConf(fr, a.pm, z.typ)
			continue
		}
		z := zeroVal(pt)
		assignVal(&z, a.val)
		if a.val.typ != nil && a.val.typ.kind == tyArray {
			z.typ = a.val.typ
		}
		fr.slots[off] = z
		vm.bindConf(fr, a.pm, z.typ)
	}
	if info.IsFunc {
		off := int(info.RetOff)
		if off >= len(fr.slots) {
			n := make([]pVal, off+8)
			copy(n, fr.slots)
			fr.slots = n
		}
		if fr.slots[off].typ == nil {
			fr.slots[off] = zeroVal(vm.img.typ(info.RetType))
		}
	}
	vm.calls = append(vm.calls, pRet{ip: vm.ip, fp: vm.rt.fp, proc: id, withMark: len(vm.rt.withs)})
	vm.rt.fp = fr
	vm.ip = info.IP
	return nil
}

func (vm *pVM) bindConf(fr *pFrame, pm pacParamInfo, t *pType) {
	bi := 0
	for u := t; u != nil && u.kind == tyArray && bi < len(pm.Conf); u = u.elem {
		lo, hi := int64(0), int64(0)
		if u.index != nil {
			lo, hi = u.index.rangeBounds()
		}
		off := int(pm.Conf[bi])
		if off >= len(fr.slots) {
			n := make([]pVal, off+8)
			copy(n, fr.slots)
			fr.slots = n
		}
		fr.slots[off] = pVal{typ: vm.rt.prog.intType, i: lo}
		if bi+1 < len(pm.Conf) {
			offh := int(pm.Conf[bi+1])
			if offh >= len(fr.slots) {
				n := make([]pVal, offh+8)
				copy(n, fr.slots)
				fr.slots = n
			}
			fr.slots[offh] = pVal{typ: vm.rt.prog.intType, i: hi}
		}
		bi += 2
	}
}

func (vm *pVM) ret() error {
	if len(vm.calls) == 0 {
		vm.ip = len(vm.img.Code)
		return nil
	}
	top := vm.calls[len(vm.calls)-1]
	vm.calls = vm.calls[:len(vm.calls)-1]
	fr := vm.rt.fp
	info := vm.img.Procs[top.proc]
	var rv pVal
	if info.IsFunc {
		off := int(info.RetOff)
		if off < len(fr.slots) {
			rv = copyVal(fr.slots[off])
		} else {
			rv = zeroVal(vm.img.typ(info.RetType))
		}
	} else {
		rv = vm.dummy()
	}
	extra := len(vm.rt.withs) - top.withMark
	if extra > 0 {
		vm.rt.popWith(extra)
	}
	vm.rt.fp = top.fp
	vm.ip = top.ip
	vm.push(rv)
	return nil
}

func (vm *pVM) bin(op ptokKind, x, y pVal, line int) (pVal, error) {
	xt, yt := unwrapType(x.typ), unwrapType(y.typ)
	switch op {
	case tkPlus, tkMinus, tkStar, tkSlash, tkDiv, tkMod:
		if xt != nil && xt.kind == tySet {
			return setOp(op, x, y), nil
		}
		xr := xt != nil && xt.kind == tyReal
		yr := yt != nil && yt.kind == tyReal
		if op == tkSlash || xr || yr {
			xf, yf := asReal(x), asReal(y)
			r := pVal{typ: vm.rt.prog.realType}
			switch op {
			case tkPlus:
				r.f = xf + yf
			case tkMinus:
				r.f = xf - yf
			case tkStar:
				r.f = xf * yf
			case tkSlash:
				if yf == 0 {
					return pVal{}, pasErr("division by zero", line, 0)
				}
				r.f = xf / yf
			}
			return r, nil
		}
		r := pVal{typ: vm.rt.prog.intType}
		switch op {
		case tkPlus:
			r.i = x.i + y.i
		case tkMinus:
			r.i = x.i - y.i
		case tkStar:
			r.i = x.i * y.i
		case tkDiv:
			if y.i == 0 {
				return pVal{}, pasErr("division by zero", line, 0)
			}
			r.i = x.i / y.i
		case tkMod:
			if y.i <= 0 {
				return pVal{}, pasErr("MOD divisor must be positive", line, 0)
			}
			_, m := isoDivMod(x.i, y.i)
			r.i = m
		}
		if r.i < -pascalMaxInt || r.i > pascalMaxInt {
			return pVal{}, pasErr("integer overflow", line, 0)
		}
		return r, nil
	case tkAnd:
		return pVal{typ: vm.rt.prog.boolType, i: boolI(x.i != 0 && y.i != 0)}, nil
	case tkOr:
		return pVal{typ: vm.rt.prog.boolType, i: boolI(x.i != 0 || y.i != 0)}, nil
	case tkIn:
		return pVal{typ: vm.rt.prog.boolType, i: boolI(inSet(y, x.i))}, nil
	case tkEq, tkNe, tkLt, tkLe, tkGt, tkGe:
		cmp := compareVal(x, y)
		ok := false
		switch op {
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
		return pVal{typ: vm.rt.prog.boolType, i: boolI(ok)}, nil
	}
	return pVal{}, pasErr("operator", line, 0)
}

func (vm *pVM) std(name string, n, line int) error {
	rt := vm.rt
	switch name {
	case "WRITELN", "WRITE":
		items := make([]pVal, n)
		ws := make([]int, n)
		ds := make([]int, n)
		for i := n - 1; i >= 0; i-- {
			d, err := vm.pop()
			if err != nil {
				return err
			}
			w, err := vm.pop()
			if err != nil {
				return err
			}
			v, err := vm.pop()
			if err != nil {
				return err
			}
			items[i], ws[i], ds[i] = v, int(w.i), int(d.i)
		}
		destv, err := vm.pop()
		if err != nil {
			return err
		}
		dest := destv.file
		if dest == nil {
			dest = rt.out
		}
		if dest != nil && !dest.text {
			for _, v := range items {
				dest.buffer = copyVal(v)
				if dest.elemType != nil {
					dest.buffer.typ = dest.elemType
				}
				dest.elems = append(dest.elems, copyVal(dest.buffer))
			}
			vm.push(vm.dummy())
			return nil
		}
		var b strings.Builder
		for i, v := range items {
			b.WriteString(formatPas(v, ws[i], ds[i]))
		}
		if name == "WRITELN" {
			b.WriteByte('\n')
		}
		if dest == rt.out || dest == nil {
			rt.writeOut(b.String())
		} else {
			dest.w.WriteString(b.String())
		}
		vm.push(vm.dummy())
		return nil
	case "READLN", "READ":
		dests := make([]*pVal, n)
		for i := n - 1; i >= 0; i-- {
			a, err := vm.popAddr()
			if err != nil {
				return err
			}
			if a.ref != nil {
				a = a.ref
			}
			dests[i] = a
		}
		srcv, err := vm.pop()
		if err != nil {
			return err
		}
		src := srcv.file
		if src == nil {
			src = rt.in
		}
		if err := vm.doReadVals(src, dests, name == "READLN", line); err != nil {
			return err
		}
		vm.push(vm.dummy())
		return nil
	case "ABS":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		if unwrapType(v.typ) != nil && unwrapType(v.typ).kind == tyReal {
			if v.f < 0 {
				v.f = -v.f
			}
		} else if v.i < 0 {
			v.i = -v.i
		}
		vm.push(v)
		return nil
	case "SQR":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		if unwrapType(v.typ) != nil && unwrapType(v.typ).kind == tyReal {
			v.f = v.f * v.f
		} else {
			v.i = v.i * v.i
		}
		vm.push(v)
		return nil
	case "SIN", "COS", "EXP", "LN", "SQRT", "ARCTAN":
		v, err := vm.pop()
		if err != nil {
			return err
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
		vm.push(r)
		return nil
	case "TRUNC":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		vm.push(pVal{typ: rt.prog.intType, i: int64(asReal(v))})
		return nil
	case "ROUND":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		vm.push(pVal{typ: rt.prog.intType, i: int64(math.Round(asReal(v)))})
		return nil
	case "ORD":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		vm.push(pVal{typ: rt.prog.intType, i: v.i})
		return nil
	case "CHR":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		if v.i < 0 || v.i > 255 {
			return pasErr("CHR argument out of range", line, 0)
		}
		vm.push(pVal{typ: rt.prog.charType, i: v.i & 255, s: string(byte(v.i))})
		return nil
	case "SUCC":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		v.i++
		if !ordInRange(v.typ, v.i) {
			return pasErr("SUCC out of range", line, 0)
		}
		vm.push(v)
		return nil
	case "PRED":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		v.i--
		if !ordInRange(v.typ, v.i) {
			return pasErr("PRED out of range", line, 0)
		}
		vm.push(v)
		return nil
	case "ODD":
		v, err := vm.pop()
		if err != nil {
			return err
		}
		vm.push(pVal{typ: rt.prog.boolType, i: v.i & 1})
		return nil
	case "EOF", "EOLN":
		f := rt.in
		if n > 0 {
			fv, err := vm.pop()
			if err != nil {
				return err
			}
			if fv.file != nil {
				f = fv.file
			}
		}
		bit := f != nil && f.eof
		if name == "EOLN" {
			bit = f != nil && f.eoln
		}
		vm.push(pVal{typ: rt.prog.boolType, i: boolI(bit)})
		return nil
	case "NEW":
		tags := make([]pVal, 0, n)
		if n > 1 {
			for i := 0; i < n-1; i++ {
				v, err := vm.pop()
				if err != nil {
					return err
				}
				tags = append(tags, v)
			}
			for i, j := 0, len(tags)-1; i < j; i, j = i+1, j-1 {
				tags[i], tags[j] = tags[j], tags[i]
			}
		}
		lv, err := vm.popAddr()
		if err != nil {
			return err
		}
		t := unwrapType(lv.typ)
		base := rt.prog.intType
		if t != nil && t.ptrTo != nil {
			base = t.ptrTo
		}
		rt.heapN++
		h := &pHeap{id: rt.heapN, live: true, val: zeroVal(base)}
		for _, tv := range tags {
			rt.applyNewTag(&h.val, tv)
		}
		rt.heap = append(rt.heap, h)
		lv.ptr = h
		if t != nil {
			lv.typ = t
		}
		vm.push(vm.dummy())
		return nil
	case "DISPOSE":
		if n >= 1 {
			lv, err := vm.popAddr()
			if err != nil {
				return err
			}
			if lv.ptr != nil {
				lv.ptr.live = false
				lv.ptr = nil
			}
		}
		vm.push(vm.dummy())
		return nil
	case "RESET", "REWRITE":
		nv, err := vm.pop()
		if err != nil {
			return err
		}
		lv, err := vm.popAddr()
		if err != nil {
			return err
		}
		if err := vm.openFile(lv, strings.TrimSpace(nv.s), name == "REWRITE"); err != nil {
			return err
		}
		vm.push(vm.dummy())
		return nil
	case "GET":
		fv, err := vm.pop()
		if err != nil {
			return err
		}
		f := fv.file
		if f == nil {
			f = rt.in
		}
		if f == nil {
			return pasErr("GET needs a file", line, 0)
		}
		if f.text {
			if f.pos < len(f.buf) {
				f.pos++
			}
			rt.fillTextBuffer(f)
		} else {
			if !f.eof {
				f.epos++
			}
			rt.fillTypedBuffer(f)
		}
		vm.push(vm.dummy())
		return nil
	case "PUT":
		fv, err := vm.pop()
		if err != nil {
			return err
		}
		f := fv.file
		if f == nil {
			return pasErr("PUT needs a file", line, 0)
		}
		if f.text {
			ch := byte(f.buffer.i)
			if f.buffer.s != "" {
				ch = f.buffer.s[0]
			}
			f.w.WriteByte(ch)
		} else {
			f.elems = append(f.elems, copyVal(f.buffer))
		}
		vm.push(vm.dummy())
		return nil
	case "PAGE":
		rt.writeOut("\f")
		vm.push(vm.dummy())
		return nil
	case "PACK":
		z, err := vm.popAddr()
		if err != nil {
			return err
		}
		iv, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.popAddr()
		if err != nil {
			return err
		}
		if err := vm.pack(a, iv.i, z); err != nil {
			return err
		}
		vm.push(vm.dummy())
		return nil
	case "UNPACK":
		iv, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.popAddr()
		if err != nil {
			return err
		}
		z, err := vm.popAddr()
		if err != nil {
			return err
		}
		if err := vm.unpack(z, a, iv.i); err != nil {
			return err
		}
		vm.push(vm.dummy())
		return nil
	}
	return pasErr("unknown standard "+name, line, 0)
}

func (vm *pVM) openFile(lv *pVal, fname string, rewrite bool) error {
	rt := vm.rt
	if fname == "" {
		fname = "FILE.DAT"
	}
	f := &pFile{text: true, name: fname}
	u := unwrapType(lv.typ)
	if u != nil && u.kind == tyFile {
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
				body, err := rt.host.OpenRead(fname)
				if err != nil {
					return err
				}
				f.body = body
				f.r = bufio.NewReader(strings.NewReader(body))
				f.buf = body
			}
			rt.fillTextBuffer(f)
		} else if old := rt.files[fname]; old != nil && !old.text {
			f.elems = old.elems
			f.epos = 0
			rt.fillTypedBuffer(f)
		} else {
			f.eof = true
		}
	}
	lv.file = f
	rt.files[fname] = f
	return nil
}

func (vm *pVM) doReadVals(src *pFile, dests []*pVal, ln bool, line int) error {
	rt := vm.rt
	for _, lv := range dests {
		if src != nil && !src.text {
			if src.eof {
				return io.EOF
			}
			assignVal(lv, src.buffer)
			src.epos++
			rt.fillTypedBuffer(src)
			continue
		}
		t := unwrapType(lv.typ)
		if err := rt.refill(src); err != nil && src.pos >= len(src.buf) {
			return err
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
				if err := rangeAssign(lv.typ, pVal{typ: lv.typ, i: n}, line, 0); err != nil {
					return err
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
	return nil
}

func (vm *pVM) pack(a *pVal, iv int64, z *pVal) error {
	at, zt := a.typ, z.typ
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
	start := int(iv - lo)
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
	return nil
}

func (vm *pVM) unpack(z, a *pVal, iv int64) error {
	zt, at := z.typ, a.typ
	expandPacked(z, zt)
	expandPacked(a, at)
	n := len(z.elems)
	lo := int64(0)
	if at != nil && at.index != nil {
		lo, _ = at.index.rangeBounds()
	}
	start := int(iv - lo)
	if a.elems == nil {
		a.elems = make([]pVal, start+n)
	}
	for k := 0; k < n && start+k < len(a.elems); k++ {
		if start+k >= 0 {
			a.elems[start+k] = copyVal(z.elems[k])
		}
	}
	return nil
}
