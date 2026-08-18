package rsts

import (
	"encoding/binary"
	"math/rand"
	"time"
)

const pcodeMaxOps = 10_000_000

type pforFrame struct {
	name   string
	end    float64
	step   float64
	bodyIP int
}

type pfnFrame struct {
	retIP   int
	params  []string
	saved   map[string]value
	present map[string]bool
}

type pvm struct {
	m         *Machine
	img       *pcodeImage
	pc        int
	stack     []value
	gosub     []int
	fors      []pforFrame
	fns       map[string]pcodeFn
	calls     []pfnFrame
	immediate bool
	print     stmt
	hasInChan bool
	inChan    float64
}

func (m *Machine) runImage(img *pcodeImage, immediate bool) error {
	vm := &pvm{
		m:         m,
		img:       img,
		fns:       map[string]pcodeFn{},
		immediate: immediate,
	}
	if !immediate && m.startLine > 0 {
		ip, ok := img.lineIP(m.startLine)
		if !ok {
			m.startLine = 0
			return basicErr("Undefined line number")
		}
		vm.pc = ip
		m.startLine = 0
	}
	m.data = append([]value(nil), img.Data...)
	m.dataPtr = 0
	m.running = true
	m.Stopped = false
	m.HasLine = !immediate
	started, waitBase := m.startCPU()
	defer func() {
		m.chargeCPU(started, waitBase)
		m.running = false
		if immediate {
			m.HasLine = false
			return
		}
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

func (vm *pvm) run() error {
	m := vm.m
	ops := 0
	for m.running {
		ops++
		if ops > pcodeMaxOps {
			return m.err("Too many iterations")
		}
		if m.intr.Load() {
			return ErrInterrupt
		}
		if ops&1023 == 0 && m.IO.PollInterrupt != nil && m.IO.PollInterrupt() {
			return ErrInterrupt
		}
		if vm.pc >= len(vm.img.Code) {
			return nil
		}
		op := vm.img.Code[vm.pc]
		vm.pc++
		if err := vm.step(op); err != nil {
			err = attachLine(err, m.CurrentLine)
			if tj, ok := m.trap(err); ok {
				ip, found := vm.img.lineIP(tj.line)
				if !found {
					return basicErrAt("Undefined line number", m.CurrentLine)
				}
				vm.pc = ip
				continue
			}
			return err
		}
	}
	return nil
}

func (vm *pvm) step(op byte) error {
	m := vm.m
	switch op {
	case opHalt, opEnd:
		m.running = false
		return nil
	case opStop:
		m.Stopped = true
		m.running = false
		return nil
	case opLine:
		m.CurrentLine = int(vm.u32())
		m.HasLine = true
		return nil
	case opPushNum:
		vm.push(numValue(vm.img.Nums[vm.u16()]))
		return nil
	case opPushStr:
		vm.push(strValue(vm.str()))
		return nil
	case opPush1:
		vm.push(numValue(1))
		return nil
	case opLoadVar:
		vm.push(m.getVar(vm.str()))
		return nil
	case opStoreVar:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		return m.assign(&varRef{name: vm.str()}, v)
	case opLoadArr:
		name := vm.str()
		n := int(vm.u8())
		idxs, err := vm.popIdx(n)
		if err != nil {
			return err
		}
		v, err := m.getArray(name, idxs)
		if err != nil {
			return err
		}
		vm.push(v)
		return nil
	case opStoreArr:
		name := vm.str()
		n := int(vm.u8())
		idxs, err := vm.popIdx(n)
		if err != nil {
			return err
		}
		v, err := vm.pop()
		if err != nil {
			return err
		}
		return m.assign(&varRef{name: name, indices: idxLits(idxs)}, v)
	case opAdd, opSub, opMul, opDiv, opIDiv, opMod, opPow, opEq, opNe, opLt, opLe, opGt, opGe, opAnd, opOr:
		r, err := vm.pop()
		if err != nil {
			return err
		}
		l, err := vm.pop()
		if err != nil {
			return err
		}
		v, err := m.binOp(binOpName(op), l, r)
		if err != nil {
			return err
		}
		vm.push(v)
		return nil
	case opNot:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		x, err := m.numVal(v)
		if err != nil {
			return err
		}
		vm.push(numValue(float64(^int(x))))
		return nil
	case opNeg:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		x, err := m.numVal(v)
		if err != nil {
			return err
		}
		vm.push(numValue(-x))
		return nil
	case opPos:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		x, err := m.numVal(v)
		if err != nil {
			return err
		}
		vm.push(numValue(x))
		return nil
	case opCall:
		return vm.call()
	case opJump:
		vm.pc = int(vm.u32())
		return nil
	case opJumpFalse, opJumpTrue:
		ip := int(vm.u32())
		v, err := vm.pop()
		if err != nil {
			return err
		}
		t := m.truth(v)
		if op == opJumpFalse {
			t = !t
		}
		if t {
			vm.pc = ip
		}
		return nil
	case opGoto:
		if vm.immediate {
			return basicErr("Illegal in immediate mode")
		}
		n, err := vm.popNum()
		if err != nil {
			return err
		}
		return vm.gotoLine(int(n))
	case opGosub:
		if vm.immediate {
			return basicErr("Illegal in immediate mode")
		}
		n, err := vm.popNum()
		if err != nil {
			return err
		}
		vm.gosub = append(vm.gosub, vm.pc)
		return vm.gotoLine(int(n))
	case opReturn:
		if vm.immediate {
			return basicErr("Illegal in immediate mode")
		}
		if len(vm.gosub) == 0 {
			return m.err("RETURN without GOSUB")
		}
		vm.pc = vm.gosub[len(vm.gosub)-1]
		vm.gosub = vm.gosub[:len(vm.gosub)-1]
		return nil
	case opForBegin:
		return vm.forBegin()
	case opForNext:
		return vm.forNext()
	case opOnError:
		n, err := vm.popNum()
		if err != nil {
			return err
		}
		m.onErrorLine = int(n)
		return nil
	case opResumeNext:
		return vm.resume(-1)
	case opResumeRetry:
		return vm.resume(0)
	case opResumeLine:
		n, err := vm.popNum()
		if err != nil {
			return err
		}
		return vm.resume(int(n))
	case opDim:
		name := vm.str()
		n := int(vm.u8())
		bounds := make([]int, n)
		for i := n - 1; i >= 0; i-- {
			v, err := vm.popNum()
			if err != nil {
				return err
			}
			bounds[i] = int(v)
		}
		return m.dimArray(name, bounds)
	case opRead:
		name := vm.str()
		n := int(vm.u8())
		idxs, err := vm.popIdx(n)
		if err != nil {
			return err
		}
		if m.dataPtr >= len(m.data) {
			return m.err("Out of data")
		}
		v := m.data[m.dataPtr]
		m.dataPtr++
		t := &varRef{name: name, indices: idxLits(idxs)}
		cv, err := m.coerceInput(t, v)
		if err != nil {
			return err
		}
		return m.assign(t, cv)
	case opRestore:
		m.dataPtr = 0
		return nil
	case opPrintStart:
		flags := vm.u8()
		vm.print = stmt{kind: stPrint, trailing: "NL"}
		if flags&flagNoNL != 0 {
			vm.print.trailing = "NONE"
		}
		if flags&flagChan != 0 {
			ch, err := vm.popNum()
			if err != nil {
				return err
			}
			vm.print.hasChan = true
			vm.print.channel = numLit{v: ch}
		}
		return nil
	case opPrintItem:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		vm.print.items = append(vm.print.items, printItem{expr: valueLit(v)})
		return nil
	case opPrintComma:
		vm.print.items = append(vm.print.items, printItem{sep: "TAB"})
		return nil
	case opPrintEnd:
		return m.doPrint(vm.print)
	case opPrintUsing:
		n := int(vm.u8())
		flags := vm.u8()
		vals := make([]value, n)
		for i := n - 1; i >= 0; i-- {
			v, err := vm.pop()
			if err != nil {
				return err
			}
			vals[i] = v
		}
		fmtv, err := vm.pop()
		if err != nil {
			return err
		}
		st := stmt{kind: stPrint, hasUsing: true, using: valueLit(fmtv), trailing: "NL"}
		if flags&flagNoNL != 0 {
			st.trailing = "NONE"
		}
		if flags&flagChan != 0 {
			ch, err := vm.popNum()
			if err != nil {
				return err
			}
			st.hasChan = true
			st.channel = numLit{v: ch}
		}
		for _, v := range vals {
			st.items = append(st.items, printItem{expr: valueLit(v)})
		}
		return m.doPrintUsing(st)
	case opInputChan:
		ch, err := vm.popNum()
		if err != nil {
			return err
		}
		vm.hasInChan = true
		vm.inChan = ch
		return nil
	case opInputOne:
		return vm.inputOne()
	case opLineInput:
		name := vm.str()
		n := int(vm.u8())
		idxs, err := vm.popIdx(n)
		if err != nil {
			return err
		}
		st := stmt{kind: stLineInput, target: &varRef{name: name, indices: idxLits(idxs)}}
		if vm.hasInChan {
			st.hasChan = true
			st.channel = numLit{v: vm.inChan}
		}
		return m.doLineInput(st)
	case opOpen:
		mode := vm.str()
		flags := vm.u8()
		st := stmt{kind: stOpen, mode: mode, hasChan: true}
		if flags&openMap != 0 {
			st.mapName = vm.str()
		}
		if flags&openVirtual != 0 {
			st.org = "VIRTUAL"
		}
		if flags&openRecSize != 0 {
			rs, err := vm.popNum()
			if err != nil {
				return err
			}
			st.recSize = numLit{v: rs}
		}
		ch, err := vm.popNum()
		if err != nil {
			return err
		}
		path, err := vm.pop()
		if err != nil {
			return err
		}
		st.channel = numLit{v: ch}
		st.path = valueLit(path)
		return m.doOpen(st)
	case opClose:
		if vm.u8() == 0 {
			m.CloseAllFiles()
			return nil
		}
		ch, err := vm.popNum()
		if err != nil {
			return err
		}
		n := int(ch)
		if f := m.Files[n]; f != nil {
			closeChanFile(f)
		}
		delete(m.Files, n)
		return nil
	case opRandomize:
		if vm.u8() != 0 {
			n, err := vm.popNum()
			if err != nil {
				return err
			}
			m.rng = rand.New(rand.NewSource(int64(n)))
			return nil
		}
		m.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
		return nil
	case opOnJump:
		n := int(vm.u8())
		gosub := vm.u8() != 0
		lines := make([]float64, n)
		for i := n - 1; i >= 0; i-- {
			v, err := vm.popNum()
			if err != nil {
				return err
			}
			lines[i] = v
		}
		idx, err := vm.popNum()
		if err != nil {
			return err
		}
		i := int(idx)
		if i < 1 || i > n {
			return nil
		}
		if gosub {
			if vm.immediate {
				return basicErr("Illegal in immediate mode")
			}
			vm.gosub = append(vm.gosub, vm.pc)
		}
		return vm.gotoLine(int(lines[i-1]))
	case opDefFn:
		name := vm.str()
		n := int(vm.u8())
		params := make([]string, n)
		for i := 0; i < n; i++ {
			params[i] = vm.str()
		}
		body := int(vm.u32())
		vm.fns[name] = pcodeFn{Name: name, Params: params, IP: body}
		return nil
	case opFnReturn:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		if len(vm.calls) == 0 {
			return m.err("Syntax error")
		}
		fr := vm.calls[len(vm.calls)-1]
		vm.calls = vm.calls[:len(vm.calls)-1]
		for _, p := range fr.params {
			if fr.present[p] {
				m.vars[p] = fr.saved[p]
			} else {
				delete(m.vars, p)
			}
		}
		vm.pc = fr.retIP
		vm.push(v)
		return nil
	case opChangeStr:
		from := vm.str()
		to := vm.str()
		return m.doChange(stmt{kind: stChange, fromName: from, toName: to})
	case opChangeArr:
		to := vm.str()
		v, err := vm.pop()
		if err != nil {
			return err
		}
		return m.doChange(stmt{kind: stChange, toName: to, fromExpr: valueLit(v)})
	case opGet, opPut:
		flags := vm.u8()
		st := stmt{hasChan: true}
		if flags&flagRec != 0 {
			rec, err := vm.popNum()
			if err != nil {
				return err
			}
			st.hasRec = true
			st.record = numLit{v: rec}
		}
		ch, err := vm.popNum()
		if err != nil {
			return err
		}
		st.channel = numLit{v: ch}
		if op == opGet {
			return m.doGet(st)
		}
		return m.doPut(st)
	case opField:
		n := int(vm.u8())
		names := make([]string, n)
		for i := 0; i < n; i++ {
			names[i] = vm.str()
		}
		lens := make([]float64, n)
		for i := n - 1; i >= 0; i-- {
			v, err := vm.popNum()
			if err != nil {
				return err
			}
			lens[i] = v
		}
		ch, err := vm.popNum()
		if err != nil {
			return err
		}
		st := stmt{kind: stField, hasChan: true, channel: numLit{v: ch}}
		for i := 0; i < n; i++ {
			st.fields = append(st.fields, fieldItem{length: numLit{v: lens[i]}, name: names[i]})
		}
		return m.doField(st)
	case opLset, opRset:
		name := vm.str()
		v, err := vm.pop()
		if err != nil {
			return err
		}
		st := stmt{target: &varRef{name: name}, expr: valueLit(v)}
		return m.doLRset(st, op == opRset)
	case opPop:
		_, err := vm.pop()
		return err
	case opInputFile:
		n := int(vm.u8())
		names := make([]string, n)
		ndims := make([]int, n)
		for i := 0; i < n; i++ {
			names[i] = vm.str()
			ndims[i] = int(vm.u8())
		}
		targets := make([]*varRef, n)
		for i := n - 1; i >= 0; i-- {
			idxs, err := vm.popIdx(ndims[i])
			if err != nil {
				return err
			}
			targets[i] = &varRef{name: names[i], indices: idxLits(idxs)}
		}
		ch, err := vm.popNum()
		if err != nil {
			return err
		}
		return m.doInput(stmt{kind: stInput, hasChan: true, channel: numLit{v: ch}, targets: targets})
	case opMat:
		return vm.execMat()
	case opMap:
		return vm.execMap()
	case opChain:
		return vm.execChain()
	case opSleep:
		return vm.execSleep()
	case opKill:
		path, err := vm.pop()
		if err != nil {
			return err
		}
		return m.doKillPath(m.strVal(path))
	case opName:
		newv, err := vm.pop()
		if err != nil {
			return err
		}
		oldv, err := vm.pop()
		if err != nil {
			return err
		}
		return m.doNamePath(m.strVal(oldv), m.strVal(newv))
	case opDimVirt:
		name := vm.str()
		n := int(vm.u8())
		flags := vm.u8()
		strLen := 0
		if flags&2 != 0 {
			v, err := vm.popNum()
			if err != nil {
				return err
			}
			strLen = int(v)
		}
		bounds := make([]int, n)
		for i := n - 1; i >= 0; i-- {
			v, err := vm.popNum()
			if err != nil {
				return err
			}
			bounds[i] = int(v)
		}
		ch := 0
		if flags&1 != 0 {
			v, err := vm.popNum()
			if err != nil {
				return err
			}
			ch = int(v)
		}
		return m.dimVirtArray(name, bounds, ch, strLen)
	default:
		return m.err("Compiled file")
	}
}

func (vm *pvm) execMat() error {
	kind := vm.u8()
	st := stmt{kind: stMat}
	switch kind {
	case matRead, matInput:
		n := int(vm.u8())
		st.matNames = make([]string, n)
		for i := 0; i < n; i++ {
			st.matNames[i] = vm.str()
		}
		if kind == matRead {
			st.matKind = "READ"
		} else {
			st.matKind = "INPUT"
		}
	case matPrint:
		tr := vm.u8()
		n := int(vm.u8())
		st.matKind = "PRINT"
		st.matNames = make([]string, n)
		for i := 0; i < n; i++ {
			st.matNames[i] = vm.str()
		}
		switch tr {
		case 1:
			st.trailing = "TAB"
		case 2:
			st.trailing = "NONE"
		default:
			st.trailing = "NL"
		}
	case matZer, matCon, matIdn:
		st.matDest = vm.str()
		switch kind {
		case matZer:
			st.matKind = "ZER"
		case matCon:
			st.matKind = "CON"
		default:
			st.matKind = "IDN"
		}
	case matZerRedim, matConRedim, matIdnRedim:
		st.matDest = vm.str()
		n := int(vm.u8())
		bounds := make([]expr, n)
		for i := n - 1; i >= 0; i-- {
			v, err := vm.popNum()
			if err != nil {
				return err
			}
			bounds[i] = numLit{v: v}
		}
		st.matBounds = bounds
		switch kind {
		case matZerRedim:
			st.matKind = "ZER"
		case matConRedim:
			st.matKind = "CON"
		default:
			st.matKind = "IDN"
		}
	case matCopy, matTrn, matInv:
		st.matDest = vm.str()
		st.matLeft = vm.str()
		switch kind {
		case matCopy:
			st.matKind = "COPY"
		case matTrn:
			st.matKind = "TRN"
		default:
			st.matKind = "INV"
		}
	case matAdd, matSub, matMul:
		st.matDest = vm.str()
		st.matLeft = vm.str()
		st.matRight = vm.str()
		switch kind {
		case matAdd:
			st.matKind = "ADD"
		case matSub:
			st.matKind = "SUB"
		default:
			st.matKind = "MUL"
		}
	case matScale:
		st.matKind = "SCALE"
		st.matDest = vm.str()
		st.matLeft = vm.str()
		k, err := vm.popNum()
		if err != nil {
			return err
		}
		st.expr = numLit{v: k}
	default:
		return vm.m.err("Compiled file")
	}
	return vm.m.doMat(st)
}

func (vm *pvm) execMap() error {
	name := vm.str()
	n := int(vm.u8())
	fields := make([]mapField, n)
	hasSize := make([]bool, n)
	for i := 0; i < n; i++ {
		fields[i].typ = vm.str()
		fields[i].name = vm.str()
		hasSize[i] = vm.u8() != 0
	}
	for i := n - 1; i >= 0; i-- {
		if !hasSize[i] {
			continue
		}
		sz, err := vm.popNum()
		if err != nil {
			return err
		}
		fields[i].size = numLit{v: sz}
	}
	return vm.m.doMap(stmt{kind: stMap, mapName: name, mapFields: fields})
}

func (vm *pvm) call() error {
	name := vm.str()
	argc := int(vm.u8())
	args := make([]value, argc)
	for i := argc - 1; i >= 0; i-- {
		v, err := vm.pop()
		if err != nil {
			return err
		}
		args[i] = v
	}
	if fn, ok := vm.fns[name]; ok {
		if len(args) != len(fn.Params) {
			return vm.m.err("Argument count")
		}
		// The parameters are restored on return, and so is the variable
		// that carries a multi-line function's result, so that a
		// function calling itself does not clobber the outer call.
		fr := pfnFrame{retIP: vm.pc, params: append(append([]string{}, fn.Params...), name),
			saved: map[string]value{}, present: map[string]bool{}}
		for _, p := range fr.params {
			if v, ok := vm.m.vars[p]; ok {
				fr.saved[p] = v
				fr.present[p] = true
			}
		}
		for i, p := range fn.Params {
			vm.m.vars[p] = args[i]
		}
		vm.calls = append(vm.calls, fr)
		vm.pc = fn.IP
		return nil
	}
	v, err := vm.m.call(name, args)
	if err != nil {
		return err
	}
	vm.push(v)
	return nil
}

func (vm *pvm) forBegin() error {
	name := vm.str()
	fail := int(vm.u32())
	step, err := vm.popNum()
	if err != nil {
		return err
	}
	end, err := vm.popNum()
	if err != nil {
		return err
	}
	start, err := vm.popNum()
	if err != nil {
		return err
	}
	vm.m.setVar(name, numValue(start))
	should := start <= end
	if step < 0 {
		should = start >= end
	}
	if !should {
		vm.pc = fail
		return nil
	}
	vm.fors = append(vm.fors, pforFrame{name: name, end: end, step: step, bodyIP: vm.pc})
	return nil
}

func (vm *pvm) forNext() error {
	want := vm.u16()
	var name string
	if want != pcodeNoName {
		if int(want) >= len(vm.img.Strings) {
			return vm.m.err("Compiled file")
		}
		name = vm.img.Strings[want]
	}
	if len(vm.fors) == 0 {
		return vm.m.err("NEXT without FOR")
	}
	if name != "" {
		for len(vm.fors) > 0 && vm.fors[len(vm.fors)-1].name != name {
			vm.fors = vm.fors[:len(vm.fors)-1]
		}
		if len(vm.fors) == 0 {
			return vm.m.err("NEXT without FOR")
		}
	}
	fr := vm.fors[len(vm.fors)-1]
	cur, err := vm.m.numVal(vm.m.getVar(fr.name))
	if err != nil {
		return err
	}
	cur += fr.step
	vm.m.setVar(fr.name, numValue(cur))
	cont := cur <= fr.end
	if fr.step < 0 {
		cont = cur >= fr.end
	}
	if cont {
		vm.pc = fr.bodyIP
		return nil
	}
	vm.fors = vm.fors[:len(vm.fors)-1]
	return nil
}

func (vm *pvm) resume(line int) error {
	m := vm.m
	if !m.inHandler && m.errNum == 0 {
		return m.err("RESUME without error")
	}
	m.inHandler = false
	if line < 0 {
		ip, ok := vm.img.nextLineIP(m.resumeLine)
		if !ok {
			return basicErrAt("Undefined line number", m.CurrentLine)
		}
		vm.pc = ip
		return nil
	}
	if line == 0 {
		ip, ok := vm.img.lineIP(m.resumeLine)
		if !ok {
			return basicErrAt("Undefined line number", m.CurrentLine)
		}
		vm.pc = ip
		return nil
	}
	return vm.gotoLine(line)
}

func (vm *pvm) gotoLine(line int) error {
	ip, ok := vm.img.lineIP(line)
	if !ok {
		return basicErrAt("Undefined line number", vm.m.CurrentLine)
	}
	vm.pc = ip
	return nil
}

func (vm *pvm) inputOne() error {
	name := vm.str()
	n := int(vm.u8())
	flags := vm.u8()
	prompt := vm.str()
	idxs, err := vm.popIdx(n)
	if err != nil {
		return err
	}
	st := stmt{kind: stInput, targets: []*varRef{{name: name, indices: idxLits(idxs)}}}
	if flags&flagChan != 0 || vm.hasInChan {
		st.hasChan = true
		st.channel = numLit{v: vm.inChan}
	}
	if flags&flagPrompt != 0 && flags&flagFirst != 0 {
		st.hasPrompt = true
		st.prompt = &prompt
	}
	return vm.m.doInput(st)
}

func (vm *pvm) push(v value) { vm.stack = append(vm.stack, v) }

func (vm *pvm) pop() (value, error) {
	if len(vm.stack) == 0 {
		return value{}, vm.m.err("Syntax error")
	}
	v := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return v, nil
}

func (vm *pvm) popNum() (float64, error) {
	v, err := vm.pop()
	if err != nil {
		return 0, err
	}
	return vm.m.numVal(v)
}

func (vm *pvm) popIdx(n int) ([]int, error) {
	if n == 0 {
		return nil, nil
	}
	out := make([]int, n)
	for i := n - 1; i >= 0; i-- {
		v, err := vm.popNum()
		if err != nil {
			return nil, err
		}
		out[i] = int(v)
	}
	return out, nil
}

func (vm *pvm) u8() byte {
	if vm.pc >= len(vm.img.Code) {
		return 0
	}
	b := vm.img.Code[vm.pc]
	vm.pc++
	return b
}

func (vm *pvm) u16() uint16 {
	if vm.pc+1 >= len(vm.img.Code) {
		vm.pc = len(vm.img.Code)
		return 0
	}
	v := binary.LittleEndian.Uint16(vm.img.Code[vm.pc:])
	vm.pc += 2
	return v
}

func (vm *pvm) u32() uint32 {
	if vm.pc+3 >= len(vm.img.Code) {
		vm.pc = len(vm.img.Code)
		return 0
	}
	v := binary.LittleEndian.Uint32(vm.img.Code[vm.pc:])
	vm.pc += 4
	return v
}

func (vm *pvm) str() string {
	i := vm.u16()
	if int(i) >= len(vm.img.Strings) {
		return ""
	}
	return vm.img.Strings[i]
}

func (vm *pvm) execChain() error {
	flags := vm.u8()
	line := 0
	if flags&1 != 0 {
		n, err := vm.popNum()
		if err != nil {
			return err
		}
		line = int(n)
	}
	path, err := vm.pop()
	if err != nil {
		return err
	}
	vm.m.chainTo = vm.m.strVal(path)
	vm.m.chainLine = line
	vm.m.running = false
	return nil
}

func (vm *pvm) execSleep() error {
	n, err := vm.popNum()
	if err != nil {
		return err
	}
	if n < 0 {
		n = 0
	}
	start := time.Now()
	deadline := start.Add(time.Duration(n * float64(time.Second)))
	defer vm.m.noteWait(start)
	for time.Now().Before(deadline) {
		if vm.m.Interrupted() {
			return ErrInterrupt
		}
		left := time.Until(deadline)
		if left > 50*time.Millisecond {
			left = 50 * time.Millisecond
		}
		time.Sleep(left)
	}
	return nil
}

func idxLits(idxs []int) []expr {
	if len(idxs) == 0 {
		return nil
	}
	out := make([]expr, len(idxs))
	for i, n := range idxs {
		out[i] = numLit{v: float64(n)}
	}
	return out
}

func binOpName(op byte) string {
	switch op {
	case opAdd:
		return "+"
	case opSub:
		return "-"
	case opMul:
		return "*"
	case opDiv:
		return "/"
	case opIDiv:
		return `\`
	case opMod:
		return "MOD"
	case opPow:
		return "^"
	case opEq:
		return "="
	case opNe:
		return "<>"
	case opLt:
		return "<"
	case opLe:
		return "<="
	case opGt:
		return ">"
	case opGe:
		return ">="
	case opAnd:
		return "AND"
	case opOr:
		return "OR"
	}
	return "+"
}
