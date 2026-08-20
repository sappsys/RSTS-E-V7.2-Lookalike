package rsts

type pExprOp int

const (
	exIdent pExprOp = iota
	exInt
	exReal
	exChar
	exString
	exBool
	exNil
	exUnary
	exBinary
	exCall
	exIndex
	exField
	exDeref
	exSet
	exRange // a..b inside a set
)

type pExpr struct {
	op    pExprOp
	tok   ptokKind
	name  string
	ival  int64
	fval  float64
	sval  string
	x, y  *pExpr
	args  []*pExpr
	elems []*pExpr
	field string
	width *pExpr
	prec  *pExpr
	line  int
	col   int
	typ   *pType
	sym   *pSym
}

type pStmtKind int

const (
	pstEmpty pStmtKind = iota
	pstAssign
	pstCall
	pstIf
	pstCase
	pstWhile
	pstRepeat
	pstFor
	pstWith
	pstGoto
	pstLabel
	pstCompound
)

type pCaseArm struct {
	consts []*pExpr
	body   *pStmt
}

type pStmt struct {
	kind   pStmtKind
	line   int
	col    int
	lhs    *pExpr
	rhs    *pExpr
	cond   *pExpr
	body   *pStmt
	els    *pStmt
	list   []*pStmt
	arms   []pCaseArm
	other  *pStmt
	name   string
	lab    int64
	downto bool
}

type pParam struct {
	names    []string
	typ      *pTypeRef
	byRef    bool
	procHead *pProcDecl // procedural or functional parameter
}

type pTypeRef struct {
	name      string
	packed    bool
	enum      []string
	lo, hi    *pExpr
	arrIdx    []*pTypeRef
	arrElem   *pTypeRef
	fields    []pRecField
	variant   *pVariant
	setElem   *pTypeRef
	fileElem  *pTypeRef
	ptrTo     *pTypeRef
	conf      []pConfBound
	line, col int
	kind      pTypeRefKind
}

type pConfBound struct {
	lo, hi  string
	idxType *pTypeRef
}

type pTypeRefKind int

const (
	trNamed pTypeRefKind = iota
	trEnum
	trSubrange
	trArray
	trRecord
	trSet
	trFile
	trPointer
)

type pRecField struct {
	names []string
	typ   *pTypeRef
}

type pVariant struct {
	tagName string
	tagType *pTypeRef
	arms    []pVarArm
}

type pVarArm struct {
	consts []*pExpr
	fields []pRecField
	nested *pVariant
}

type pDecl struct {
	consts []pConstDecl
	types  []pTypeDecl
	vars   []pVarDecl
	procs  []*pProcDecl
}

type pConstDecl struct {
	name string
	val  *pExpr
	line int
}

type pTypeDecl struct {
	name string
	typ  *pTypeRef
	line int
}

type pVarDecl struct {
	names []string
	typ   *pTypeRef
	line  int
}

type pProcDecl struct {
	name   string
	params []pParam
	ret    *pTypeRef // nil = procedure
	fwd    bool
	block  *pBlock
	line   int
	sym    *pSym
	retOff int
	nslot  int
}

type pBlock struct {
	labels []int64
	decl   pDecl
	body   *pStmt
	nslot  int
}

type pProgram struct {
	name     string
	files    []string
	block    *pBlock
	src      string
	intType  *pType
	realType *pType
	boolType *pType
	charType *pType
	textType *pType
	nilType  *pType
	scopes   []*pScope
}

type pType struct {
	kind    pKind
	name    string
	enums   []string
	lo, hi  int64
	base    *pType
	index   *pType
	elem    *pType
	packed  bool
	fields  []pField
	tagName string
	tagType *pType
	ptrTo   *pType
	ptrName string // unresolved ^ident
	conf    bool
	confLo  string
	confHi  string
}

type pKind int

const (
	tyInteger pKind = iota
	tyReal
	tyBoolean
	tyChar
	tyEnum
	tySubrange
	tyArray
	tyRecord
	tySet
	tyPointer
	tyFile
	tyText
	tyNil
	tyProc
	tyDummy
)

type pField struct {
	name string
	typ  *pType
}

type pSymKind int

const (
	skConst pSymKind = iota
	skType
	skVar
	skProc
	skFunc
	skStd
)

type pSym struct {
	name      string
	kind      pSymKind
	typ       *pType
	cval      pVal
	level     int
	offset    int
	byRef     bool
	proc      *pProcDecl
	std       string
	params    []*pSym
	ret       *pType
	procParam bool
	param     bool    // true if a procedure/function parameter
	confB     []*pSym // lo, hi, ... for a conformant array parameter
}

type pScope struct {
	parent *pScope
	level  int
	syms   map[string]*pSym
	nslot  int
}

type pVal struct {
	typ   *pType
	i     int64
	f     float64
	s     string
	bits  []uint64
	elems []pVal
	ptr   *pHeap
	file  *pFile
	ref   *pVal // VAR parameter alias
	proc  *pSym // bound procedural/functional parameter
	slink *pFrame
}

type pHeap struct {
	id   int64
	val  pVal
	live bool
}

func (t *pType) ordinal() bool {
	if t == nil {
		return false
	}
	switch t.kind {
	case tyInteger, tyBoolean, tyChar, tyEnum, tySubrange:
		return true
	}
	return false
}

func (t *pType) rangeBounds() (int64, int64) {
	if t == nil {
		return 0, 0
	}
	switch t.kind {
	case tyInteger:
		return -pascalMaxInt, pascalMaxInt
	case tyBoolean:
		return 0, 1
	case tyChar:
		return 0, 255
	case tyEnum:
		return 0, int64(len(t.enums) - 1)
	case tySubrange:
		return t.lo, t.hi
	}
	return 0, 0
}

func (t *pType) arrayLen() int {
	if t == nil || t.kind != tyArray || t.index == nil || t.conf {
		return 0
	}
	lo, hi := t.index.rangeBounds()
	n := hi - lo + 1
	if n < 0 || n > 1<<20 {
		return 0
	}
	return int(n)
}

func (t *pType) isPackedCharArray() bool {
	if t == nil || t.kind != tyArray || t.elem == nil {
		return false
	}
	e := unwrapType(t.elem)
	return e != nil && e.kind == tyChar
}

func unwrapType(t *pType) *pType {
	for t != nil && t.kind == tySubrange && t.base != nil {
		t = t.base
	}
	return t
}

func ordInRange(t *pType, i int64) bool {
	if t == nil || !t.ordinal() {
		return true
	}
	lo, hi := t.rangeBounds()
	return i >= lo && i <= hi
}

func numericType(t *pType) bool {
	t = unwrapType(t)
	return t != nil && (t.kind == tyInteger || t.kind == tyReal || t.kind == tySubrange)
}

func isoDivMod(i, j int64) (q, r int64) {
	q = i / j
	r = i % j
	if r < 0 {
		r += j
		q--
	}
	return
}

func nestPrefix(label, from []*pStmt) bool {
	if len(label) > len(from) {
		return false
	}
	for i := range label {
		if label[i] != from[i] {
			return false
		}
	}
	return true
}

func sameType(a, b *pType) bool {
	a, b = unwrapType(a), unwrapType(b)
	if a == nil || b == nil {
		return a == b
	}
	if a == b {
		return true
	}
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case tyInteger, tyReal, tyBoolean, tyChar, tyText, tyNil:
		return true
	case tyEnum:
		return a.name != "" && a.name == b.name
	case tyArray:
		if a.conf || b.conf {
			return sameType(a.elem, b.elem)
		}
		return a.arrayLen() == b.arrayLen() && sameType(a.elem, b.elem)
	case tyRecord:
		if len(a.fields) != len(b.fields) {
			return false
		}
		for i := range a.fields {
			if a.fields[i].name != b.fields[i].name || !sameType(a.fields[i].typ, b.fields[i].typ) {
				return false
			}
		}
		return true
	case tySet:
		return sameType(a.elem, b.elem)
	case tyPointer:
		return a.ptrTo == b.ptrTo || (a.ptrTo != nil && b.ptrTo != nil && a.ptrTo.name != "" && a.ptrTo.name == b.ptrTo.name)
	}
	return false
}
