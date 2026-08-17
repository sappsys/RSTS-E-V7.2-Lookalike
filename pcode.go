package rsts

import (
	"encoding/binary"
	"math"
	"strings"
)

// Private BASIC-PLUS bytecode. Digital's V7 .BAC P-code is not used.
//
// This is the execution engine: RUN, COMPILE, and immediate mode all
// compile to these opcodes. When you add a statement, modifier, operator,
// or built-in, add an opcode here and teach the compiler and VM.

const (
	pcodeMagic   = "PBAC1\n"
	pcodeVersion = 1
	pcodeNoName  = 0xFFFF
)

const (
	opHalt byte = iota
	opLine
	opPushNum
	opPushStr
	opPush1
	opLoadVar
	opStoreVar
	opLoadArr
	opStoreArr
	opAdd
	opSub
	opMul
	opDiv
	opIDiv
	opMod
	opPow
	opEq
	opNe
	opLt
	opLe
	opGt
	opGe
	opAnd
	opOr
	opNot
	opNeg
	opPos
	opCall
	opJump
	opJumpFalse
	opJumpTrue
	opGoto
	opGosub
	opReturn
	opEnd
	opStop
	opForBegin
	opForNext
	opOnError
	opResumeNext
	opResumeRetry
	opResumeLine
	opDim
	opRead
	opRestore
	opPrintStart
	opPrintItem
	opPrintComma
	opPrintEnd
	opPrintUsing
	opInputChan
	opInputOne
	opLineInput
	opOpen
	opClose
	opRandomize
	opOnJump
	opDefFn
	opFnReturn
	opChangeStr
	opChangeArr
	opGet
	opPut
	opField
	opLset
	opRset
	opPop
	opInputFile
	opMat
	opMap
)

const (
	flagChan   = 1
	flagNoNL   = 2
	flagPrompt = 4
	flagFirst  = 8
	flagRec    = 16
)

const (
	openRecSize = 1
	openVirtual = 2
	openMap     = 4
)

const (
	matRead byte = iota
	matPrint
	matInput
	matZer
	matCon
	matIdn
	matCopy
	matAdd
	matSub
	matMul
	matScale
	matTrn
	matInv
)

type pcodeLine struct {
	Num int
	IP  int
}

type pcodeFn struct {
	Name   string
	Params []string
	IP     int
}

type pcodeImage struct {
	Strings []string
	Nums    []float64
	Data    []value
	Lines   []pcodeLine
	Fns     []pcodeFn
	Code    []byte
	HaltIP  int
}

func (img *pcodeImage) lineIP(line int) (int, bool) {
	for _, l := range img.Lines {
		if l.Num == line {
			return l.IP, true
		}
	}
	return 0, false
}

func (img *pcodeImage) nextLineIP(line int) (int, bool) {
	for i, l := range img.Lines {
		if l.Num == line {
			if i+1 < len(img.Lines) {
				return img.Lines[i+1].IP, true
			}
			return img.HaltIP, true
		}
	}
	return 0, false
}

func (img *pcodeImage) Marshal() []byte {
	var b []byte
	b = append(b, pcodeMagic...)
	b = putU32(b, pcodeVersion)
	b = putU32(b, uint32(len(img.Strings)))
	for _, s := range img.Strings {
		b = putU32(b, uint32(len(s)))
		b = append(b, s...)
	}
	b = putU32(b, uint32(len(img.Nums)))
	for _, n := range img.Nums {
		b = putU64(b, math.Float64bits(n))
	}
	b = putU32(b, uint32(len(img.Data)))
	for _, v := range img.Data {
		if v.isStr {
			b = append(b, 1)
			b = putU32(b, uint32(img.mustInternLookup(v.str)))
		} else {
			b = append(b, 0)
			b = putU64(b, math.Float64bits(v.num))
		}
	}
	b = putU32(b, uint32(len(img.Lines)))
	for _, l := range img.Lines {
		b = putU32(b, uint32(l.Num))
		b = putU32(b, uint32(l.IP))
	}
	b = putU32(b, uint32(img.HaltIP))
	b = putU32(b, uint32(len(img.Code)))
	b = append(b, img.Code...)
	return b
}

func (img *pcodeImage) mustInternLookup(s string) uint32 {
	for i, t := range img.Strings {
		if t == s {
			return uint32(i)
		}
	}
	return 0
}

func unmarshalPcode(raw string) (*pcodeImage, error) {
	if !strings.HasPrefix(raw, pcodeMagic) {
		return nil, basicErr("Compiled file")
	}
	p := []byte(raw[len(pcodeMagic):])
	off := 0
	u32 := func() (uint32, error) {
		if off+4 > len(p) {
			return 0, basicErr("Compiled file")
		}
		v := binary.LittleEndian.Uint32(p[off:])
		off += 4
		return v, nil
	}
	u64 := func() (uint64, error) {
		if off+8 > len(p) {
			return 0, basicErr("Compiled file")
		}
		v := binary.LittleEndian.Uint64(p[off:])
		off += 8
		return v, nil
	}
	ver, err := u32()
	if err != nil {
		return nil, err
	}
	if ver != pcodeVersion {
		return nil, basicErr("Compiled file")
	}
	img := &pcodeImage{}
	n, err := u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < n; i++ {
		ln, err := u32()
		if err != nil {
			return nil, err
		}
		if off+int(ln) > len(p) {
			return nil, basicErr("Compiled file")
		}
		img.Strings = append(img.Strings, string(p[off:off+int(ln)]))
		off += int(ln)
	}
	n, err = u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < n; i++ {
		bits, err := u64()
		if err != nil {
			return nil, err
		}
		img.Nums = append(img.Nums, math.Float64frombits(bits))
	}
	n, err = u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < n; i++ {
		if off >= len(p) {
			return nil, basicErr("Compiled file")
		}
		isStr := p[off]
		off++
		if isStr != 0 {
			idx, err := u32()
			if err != nil {
				return nil, err
			}
			if int(idx) >= len(img.Strings) {
				return nil, basicErr("Compiled file")
			}
			img.Data = append(img.Data, strValue(img.Strings[idx]))
		} else {
			bits, err := u64()
			if err != nil {
				return nil, err
			}
			img.Data = append(img.Data, numValue(math.Float64frombits(bits)))
		}
	}
	n, err = u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < n; i++ {
		num, err := u32()
		if err != nil {
			return nil, err
		}
		ip, err := u32()
		if err != nil {
			return nil, err
		}
		img.Lines = append(img.Lines, pcodeLine{Num: int(num), IP: int(ip)})
	}
	halt, err := u32()
	if err != nil {
		return nil, err
	}
	img.HaltIP = int(halt)
	n, err = u32()
	if err != nil {
		return nil, err
	}
	if off+int(n) > len(p) {
		return nil, basicErr("Compiled file")
	}
	img.Code = append([]byte(nil), p[off:off+int(n)]...)
	return img, nil
}

func wrapPcode(img *pcodeImage) string {
	return wrapBAC(string(img.Marshal()))
}

func putU32(b []byte, v uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}

func putU64(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}

func valueLit(v value) expr {
	if v.isStr {
		return strLit{v: v.str}
	}
	return numLit{v: v.num}
}
