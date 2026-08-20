package rsts

import (
	"encoding/binary"
	"math"
	"strings"
)

// Private Pascal bytecode. Not a PDP-11 .TSK and not UCSD P-code.
const (
	pacMagic   = "RSTS/E PAC V7\n"
	pacVersion = 1
)

const (
	poHalt byte = iota
	poPushI
	poPushF
	poPushC
	poPushS
	poPushB
	poPushNil
	poPushSet
	poAddrVar
	poAddrIdx
	poAddrField
	poAddrDeref
	poAddrWith
	poLoad
	poStore
	poDup
	poPop
	poBin
	poUn
	poJmp
	poJz
	poCall
	poCallVal
	poProcRef
	poRet
	poWithPush
	poWithPop
	poStd
	poNew
	poSetIncl
	poSetRange
	poCaseFail
	poDupAddr
)

type pacInst struct {
	Op byte
	A  int32
	B  int32
	I  int64
	F  float64
}

type pacParamInfo struct {
	Off   int32
	ByRef bool
	Proc  bool
	Typ   int32
	ConfN int32
	Conf  []int32 // lo, hi offsets
}

type pacProcInfo struct {
	Name    string
	IP      int
	Level   int32
	NSlot   int32
	RetOff  int32
	IsFunc  bool
	Params  []pacParamInfo
	RetType int32
}

type pacTypeInfo struct {
	Kind    int32
	Lo, Hi  int64
	Name    string
	Packed  bool
	Conf    bool
	Elem    int32
	Index   int32
	PtrTo   int32
	Base    int32
	Enums   []string
	Fields  []pacFieldInfo
	TagName string
	TagType int32
	PtrName string
}

type pacFieldInfo struct {
	Name string
	Typ  int32
}

type pacImage struct {
	Strings   []string
	Code      []pacInst
	Procs     []pacProcInfo
	TypeInfo  []pacTypeInfo
	Types     []*pType // filled after unmarshal / compile
	Entry     int
	MainNSlot int
	IntType   int32
	RealType  int32
	BoolType  int32
	CharType  int32
	TextType  int32
	NilType   int32
}

func wrapPAC(img *pacImage) string {
	return pacMagic + string(img.marshal())
}

func unwrapPAC(text string) (string, bool) {
	if strings.HasPrefix(text, pacMagic) {
		return text[len(pacMagic):], true
	}
	return text, false
}

func (img *pacImage) marshal() []byte {
	var b []byte
	b = putU32(b, pacVersion)
	b = putU32(b, uint32(len(img.Strings)))
	for _, s := range img.Strings {
		b = putU32(b, uint32(len(s)))
		b = append(b, s...)
	}
	b = putU32(b, uint32(len(img.Code)))
	for _, in := range img.Code {
		b = append(b, in.Op)
		b = putU32(b, uint32(in.A))
		b = putU32(b, uint32(in.B))
		b = putU64(b, uint64(in.I))
		b = putU64(b, math.Float64bits(in.F))
	}
	b = putU32(b, uint32(len(img.Procs)))
	for _, p := range img.Procs {
		b = putU32(b, uint32(len(p.Name)))
		b = append(b, p.Name...)
		b = putU32(b, uint32(p.IP))
		b = putU32(b, uint32(p.Level))
		b = putU32(b, uint32(p.NSlot))
		b = putU32(b, uint32(p.RetOff))
		if p.IsFunc {
			b = append(b, 1)
		} else {
			b = append(b, 0)
		}
		b = putU32(b, uint32(p.RetType))
		b = putU32(b, uint32(len(p.Params)))
		for _, pm := range p.Params {
			b = putU32(b, uint32(pm.Off))
			b = append(b, boolByte(pm.ByRef), boolByte(pm.Proc))
			b = putU32(b, uint32(pm.Typ))
			b = putU32(b, uint32(len(pm.Conf)))
			for _, c := range pm.Conf {
				b = putU32(b, uint32(c))
			}
		}
	}
	b = putU32(b, uint32(len(img.TypeInfo)))
	for _, t := range img.TypeInfo {
		b = putU32(b, uint32(t.Kind))
		b = putU64(b, uint64(t.Lo))
		b = putU64(b, uint64(t.Hi))
		b = putU32(b, uint32(len(t.Name)))
		b = append(b, t.Name...)
		b = append(b, boolByte(t.Packed), boolByte(t.Conf))
		b = putU32(b, uint32(t.Elem))
		b = putU32(b, uint32(t.Index))
		b = putU32(b, uint32(t.PtrTo))
		b = putU32(b, uint32(t.Base))
		b = putU32(b, uint32(len(t.Enums)))
		for _, e := range t.Enums {
			b = putU32(b, uint32(len(e)))
			b = append(b, e...)
		}
		b = putU32(b, uint32(len(t.Fields)))
		for _, f := range t.Fields {
			b = putU32(b, uint32(len(f.Name)))
			b = append(b, f.Name...)
			b = putU32(b, uint32(f.Typ))
		}
		b = putU32(b, uint32(len(t.TagName)))
		b = append(b, t.TagName...)
		b = putU32(b, uint32(t.TagType))
		b = putU32(b, uint32(len(t.PtrName)))
		b = append(b, t.PtrName...)
	}
	b = putU32(b, uint32(img.Entry))
	b = putU32(b, uint32(img.MainNSlot))
	b = putU32(b, uint32(img.IntType))
	b = putU32(b, uint32(img.RealType))
	b = putU32(b, uint32(img.BoolType))
	b = putU32(b, uint32(img.CharType))
	b = putU32(b, uint32(img.TextType))
	b = putU32(b, uint32(img.NilType))
	return b
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func unmarshalPAC(raw string) (*pacImage, error) {
	payload, ok := unwrapPAC(raw)
	if !ok {
		return nil, pasErr("Compiled file", 0, 0)
	}
	p := []byte(payload)
	off := 0
	u32 := func() (uint32, error) {
		if off+4 > len(p) {
			return 0, pasErr("Compiled file", 0, 0)
		}
		v := binary.LittleEndian.Uint32(p[off:])
		off += 4
		return v, nil
	}
	u64 := func() (uint64, error) {
		if off+8 > len(p) {
			return 0, pasErr("Compiled file", 0, 0)
		}
		v := binary.LittleEndian.Uint64(p[off:])
		off += 8
		return v, nil
	}
	str := func() (string, error) {
		n, err := u32()
		if err != nil {
			return "", err
		}
		if off+int(n) > len(p) {
			return "", pasErr("Compiled file", 0, 0)
		}
		s := string(p[off : off+int(n)])
		off += int(n)
		return s, nil
	}
	ver, err := u32()
	if err != nil {
		return nil, err
	}
	if ver != pacVersion {
		return nil, pasErr("Compiled file", 0, 0)
	}
	img := &pacImage{}
	n, err := u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < n; i++ {
		s, err := str()
		if err != nil {
			return nil, err
		}
		img.Strings = append(img.Strings, s)
	}
	n, err = u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < n; i++ {
		if off >= len(p) {
			return nil, pasErr("Compiled file", 0, 0)
		}
		in := pacInst{Op: p[off]}
		off++
		a, err := u32()
		if err != nil {
			return nil, err
		}
		b, err := u32()
		if err != nil {
			return nil, err
		}
		iv, err := u64()
		if err != nil {
			return nil, err
		}
		fv, err := u64()
		if err != nil {
			return nil, err
		}
		in.A, in.B, in.I, in.F = int32(a), int32(b), int64(iv), math.Float64frombits(fv)
		img.Code = append(img.Code, in)
	}
	n, err = u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < n; i++ {
		name, err := str()
		if err != nil {
			return nil, err
		}
		ip, err := u32()
		if err != nil {
			return nil, err
		}
		level, err := u32()
		if err != nil {
			return nil, err
		}
		nslot, err := u32()
		if err != nil {
			return nil, err
		}
		retOff, err := u32()
		if err != nil {
			return nil, err
		}
		if off >= len(p) {
			return nil, pasErr("Compiled file", 0, 0)
		}
		isFunc := p[off] != 0
		off++
		retType, err := u32()
		if err != nil {
			return nil, err
		}
		np, err := u32()
		if err != nil {
			return nil, err
		}
		pr := pacProcInfo{Name: name, IP: int(ip), Level: int32(level), NSlot: int32(nslot), RetOff: int32(retOff), IsFunc: isFunc, RetType: int32(retType)}
		for j := uint32(0); j < np; j++ {
			offv, err := u32()
			if err != nil {
				return nil, err
			}
			if off+2 > len(p) {
				return nil, pasErr("Compiled file", 0, 0)
			}
			byRef, proc := p[off] != 0, p[off+1] != 0
			off += 2
			typ, err := u32()
			if err != nil {
				return nil, err
			}
			nc, err := u32()
			if err != nil {
				return nil, err
			}
			pm := pacParamInfo{Off: int32(offv), ByRef: byRef, Proc: proc, Typ: int32(typ)}
			for k := uint32(0); k < nc; k++ {
				cv, err := u32()
				if err != nil {
					return nil, err
				}
				pm.Conf = append(pm.Conf, int32(cv))
			}
			pr.Params = append(pr.Params, pm)
		}
		img.Procs = append(img.Procs, pr)
	}
	n, err = u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < n; i++ {
		kind, err := u32()
		if err != nil {
			return nil, err
		}
		lo, err := u64()
		if err != nil {
			return nil, err
		}
		hi, err := u64()
		if err != nil {
			return nil, err
		}
		name, err := str()
		if err != nil {
			return nil, err
		}
		if off+2 > len(p) {
			return nil, pasErr("Compiled file", 0, 0)
		}
		packed, conf := p[off] != 0, p[off+1] != 0
		off += 2
		elem, err := u32()
		if err != nil {
			return nil, err
		}
		index, err := u32()
		if err != nil {
			return nil, err
		}
		ptrTo, err := u32()
		if err != nil {
			return nil, err
		}
		base, err := u32()
		if err != nil {
			return nil, err
		}
		ne, err := u32()
		if err != nil {
			return nil, err
		}
		t := pacTypeInfo{Kind: int32(kind), Lo: int64(lo), Hi: int64(hi), Name: name, Packed: packed, Conf: conf, Elem: int32(elem), Index: int32(index), PtrTo: int32(ptrTo), Base: int32(base)}
		for j := uint32(0); j < ne; j++ {
			e, err := str()
			if err != nil {
				return nil, err
			}
			t.Enums = append(t.Enums, e)
		}
		nf, err := u32()
		if err != nil {
			return nil, err
		}
		for j := uint32(0); j < nf; j++ {
			fn, err := str()
			if err != nil {
				return nil, err
			}
			ft, err := u32()
			if err != nil {
				return nil, err
			}
			t.Fields = append(t.Fields, pacFieldInfo{Name: fn, Typ: int32(ft)})
		}
		tag, err := str()
		if err != nil {
			return nil, err
		}
		tt, err := u32()
		if err != nil {
			return nil, err
		}
		pn, err := str()
		if err != nil {
			return nil, err
		}
		t.TagName, t.TagType, t.PtrName = tag, int32(tt), pn
		img.TypeInfo = append(img.TypeInfo, t)
	}
	entry, err := u32()
	if err != nil {
		return nil, err
	}
	mainN, err := u32()
	if err != nil {
		return nil, err
	}
	img.Entry, img.MainNSlot = int(entry), int(mainN)
	for _, dst := range []*int32{&img.IntType, &img.RealType, &img.BoolType, &img.CharType, &img.TextType, &img.NilType} {
		v, err := u32()
		if err != nil {
			return nil, err
		}
		*dst = int32(v)
	}
	img.rebuildTypes()
	return img, nil
}

func (img *pacImage) rebuildTypes() {
	img.Types = make([]*pType, len(img.TypeInfo)+1)
	for i, ti := range img.TypeInfo {
		img.Types[i+1] = &pType{
			kind: pKind(ti.Kind), lo: ti.Lo, hi: ti.Hi, name: ti.Name,
			packed: ti.Packed, conf: ti.Conf, enums: ti.Enums,
			tagName: ti.TagName, ptrName: ti.PtrName,
		}
	}
	link := func(id int32) *pType {
		if id <= 0 || int(id) >= len(img.Types) {
			return nil
		}
		return img.Types[id]
	}
	for i, ti := range img.TypeInfo {
		t := img.Types[i+1]
		t.elem = link(ti.Elem)
		t.index = link(ti.Index)
		t.ptrTo = link(ti.PtrTo)
		t.base = link(ti.Base)
		t.tagType = link(ti.TagType)
		for _, f := range ti.Fields {
			t.fields = append(t.fields, pField{name: f.Name, typ: link(f.Typ)})
		}
	}
}

func (img *pacImage) typ(id int32) *pType {
	if id <= 0 || int(id) >= len(img.Types) {
		return nil
	}
	return img.Types[id]
}

func (img *pacImage) str(id int32) string {
	if id < 0 || int(id) >= len(img.Strings) {
		return ""
	}
	return img.Strings[id]
}
