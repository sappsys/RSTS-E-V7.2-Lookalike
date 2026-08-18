package rsts

import "strings"

type expr interface{ exprNode() }

type numLit struct{ v float64 }
type strLit struct{ v string }
type varRef struct {
	name    string
	indices []expr
}
type callExpr struct {
	name string
	args []expr
}
type unaryExpr struct {
	op string
	x  expr
}
type binExpr struct {
	op string
	l  expr
	r  expr
}

func (numLit) exprNode()    {}
func (strLit) exprNode()    {}
func (varRef) exprNode()    {}
func (callExpr) exprNode()  {}
func (unaryExpr) exprNode() {}
func (binExpr) exprNode()   {}

type stmtKind int

const (
	stRem stmtKind = iota
	stLet
	stPrint
	stInput
	stLineInput
	stGoto
	stGosub
	stReturn
	stIf
	stFor
	stNext
	stWhile
	stDim
	stData
	stRead
	stRestore
	stEnd
	stStop
	stOpen
	stClose
	stRandomize
	stOnGoto
	stOnGosub
	stOnError
	stResume
	stDef
	stUntil
	stChange
	stGet
	stPut
	stField
	stLset
	stRset
	stMat
	stMap
	stDefMulti
	stFnEnd
	stFnExit
	stChain
	stSleep
	stKill
	stName
)

type printItem struct {
	sep  string // "", "TAB", "NONE" — if set, this is a separator
	expr expr
}

type branch struct {
	isLine bool
	line   expr
	stmts  []stmt
}

type modifier struct {
	kind   string // IF UNLESS WHILE UNTIL FOR
	cond   expr
	forVar string
	start  expr
	end    expr
	step   expr
}

type fieldItem struct {
	length expr
	name   string
}

type mapField struct {
	typ  string
	name string
	size expr
}

type dimItem struct {
	name    string
	bounds  []expr
	channel expr
	strLen  expr
}

type value struct {
	isStr bool
	num   float64
	str   string
}

type stmt struct {
	kind      stmtKind
	target    *varRef
	expr      expr
	channel   expr
	items     []printItem
	trailing  string
	prompt    *string
	targets   []*varRef
	cond      expr
	thenPart  *branch
	elsePart  *branch
	forVar    string
	start     expr
	end       expr
	step      expr
	nextVar   string
	arrays    []dimItem
	data      []value
	path      expr
	mode      string
	seed      expr
	lines     []expr
	fnName    string
	params    []string
	fnExpr    expr
	hasChan   bool
	hasSeed   bool
	hasPrompt bool
	hasUsing  bool
	using     expr
	recSize   expr
	hasRec    bool
	record    expr
	resumeNxt bool
	fromName  string
	toName    string
	fromExpr  expr
	fields    []fieldItem
	mods      []modifier
	matKind   string
	matDest   string
	matLeft   string
	matRight  string
	matNames  []string
	matBounds []expr
	org       string
	mapName   string
	mapFields []mapField
}

type parser struct {
	tokens  []token
	i       int
	markRef bool
	refs    []int // byte offsets of line numbers used as references
}

func parseSourceLine(text string) ([]stmt, error) {
	toks, err := tokenize(text)
	if err != nil {
		return nil, err
	}
	p := parser{tokens: toks}
	return p.parseLine()
}

// lineRefOffsets returns where in the text each line number reference
// begins: the targets of GOTO, GOSUB, ON ... GOTO/GOSUB, THEN, ELSE,
// RESUME and ON ERROR GOTO. RENUM rewrites exactly these, which is why it
// asks the grammar rather than guessing at the text.
func lineRefOffsets(text string) ([]int, error) {
	toks, err := tokenize(text)
	if err != nil {
		return nil, err
	}
	p := parser{tokens: toks, markRef: true}
	if _, err := p.parseLine(); err != nil {
		return nil, err
	}
	return p.refs, nil
}

func (p *parser) tok() token { return p.tokens[p.i] }

func (p *parser) peek() token {
	j := p.i + 1
	if j >= len(p.tokens) {
		j = len(p.tokens) - 1
	}
	return p.tokens[j]
}

func (p *parser) acceptKind(k tokKind) bool {
	if p.tok().kind == k {
		p.i++
		return true
	}
	return false
}

func (p *parser) acceptOp(op string) bool {
	if p.tok().kind == tokOp && p.tok().text == op {
		p.i++
		return true
	}
	return false
}

func (p *parser) acceptKw(kw string) bool {
	if p.tok().kind == tokKeyword && p.tok().text == kw {
		p.i++
		return true
	}
	return false
}

func (p *parser) expectKind(k tokKind) error {
	if p.acceptKind(k) {
		return nil
	}
	return basicErr("Syntax error")
}

func (p *parser) expectOp(op string) error {
	if p.acceptOp(op) {
		return nil
	}
	return basicErr("Syntax error")
}

func (p *parser) expectKw(kw string) error {
	if p.acceptKw(kw) {
		return nil
	}
	return basicErr("Syntax error")
}

func (p *parser) parseLine() ([]stmt, error) {
	var stmts []stmt
	for p.tok().kind != tokEOF && p.tok().kind != tokEOL {
		if p.tok().kind == tokKeyword && p.tok().text == "REM" {
			stmts = append(stmts, stmt{kind: stRem})
			break
		}
		s, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
		if p.acceptKind(tokBackslash) || p.acceptKind(tokColon) {
			continue
		}
		break
	}
	return stmts, nil
}

func (p *parser) parseStatement() (stmt, error) {
	s, err := p.parseBareStatement()
	if err != nil {
		return s, err
	}
	if err := p.parseModifiers(&s); err != nil {
		return s, err
	}
	return s, nil
}

func (p *parser) parseBareStatement() (stmt, error) {
	t := p.tok()
	if t.kind == tokKeyword {
		switch t.text {
		case "PRINT":
			return p.parsePrint()
		case "INPUT":
			return p.parseInput()
		case "LET":
			if err := p.expectKw("LET"); err != nil {
				return stmt{}, err
			}
			return p.parseAssignment()
		case "GOTO":
			p.i++
			line, err := p.lineRef()
			if err != nil {
				return stmt{}, err
			}
			return stmt{kind: stGoto, expr: line}, nil
		case "GOSUB":
			p.i++
			line, err := p.lineRef()
			if err != nil {
				return stmt{}, err
			}
			return stmt{kind: stGosub, expr: line}, nil
		case "RETURN":
			p.i++
			return stmt{kind: stReturn}, nil
		case "IF":
			return p.parseIf()
		case "FOR":
			return p.parseFor()
		case "NEXT":
			return p.parseNext()
		case "WHILE":
			return p.parseWhile()
		case "UNTIL":
			return p.parseUntil()
		case "DIM":
			return p.parseDim()
		case "DATA":
			return p.parseData()
		case "READ":
			return p.parseRead()
		case "RESTORE":
			p.i++
			return stmt{kind: stRestore}, nil
		case "END":
			p.i++
			return stmt{kind: stEnd}, nil
		case "STOP":
			p.i++
			return stmt{kind: stStop}, nil
		case "OPEN":
			return p.parseOpen()
		case "CLOSE":
			return p.parseClose()
		case "RANDOMIZE":
			return p.parseRandomize()
		case "ON":
			return p.parseOn()
		case "DEF":
			return p.parseDef()
		case "FNEND":
			p.i++
			return stmt{kind: stFnEnd}, nil
		case "FNEXIT":
			p.i++
			return stmt{kind: stFnExit}, nil
		case "LINE":
			return p.parseLineInput()
		case "CHANGE":
			return p.parseChange()
		case "GET":
			return p.parseGetPut(stGet)
		case "PUT":
			return p.parseGetPut(stPut)
		case "FIELD":
			return p.parseField()
		case "LSET":
			return p.parseLRset(stLset)
		case "RSET":
			return p.parseLRset(stRset)
		case "RESUME":
			return p.parseResume()
		case "MAT":
			return p.parseMat()
		case "MAP":
			return p.parseMap()
		case "CHAIN":
			return p.parseChain()
		case "SLEEP":
			return p.parseSleep()
		case "KILL":
			return p.parseKill()
		case "NAME":
			return p.parseName()
		default:
			return stmt{}, basicErr("Syntax error")
		}
	}
	if t.kind == tokIdent {
		return p.parseAssignment()
	}
	return stmt{}, basicErr("Syntax error")
}

func (p *parser) atStop() bool {
	k := p.tok().kind
	return k == tokEOF || k == tokEOL || k == tokBackslash || k == tokColon
}

func (p *parser) openClause() bool {
	if p.tok().kind != tokKeyword {
		return false
	}
	switch p.tok().text {
	case "RECORDSIZE", "ORGANIZATION", "MAP":
		return true
	}
	return false
}

func (p *parser) acceptName(name string) bool {
	if (p.tok().kind == tokIdent || p.tok().kind == tokKeyword) && p.tok().text == name {
		p.i++
		return true
	}
	return false
}

func (p *parser) backslashStartsStatement() bool {
	j := p.i + 1
	if j >= len(p.tokens) {
		return true
	}
	nxt := p.tokens[j]
	if nxt.kind == tokEOF || nxt.kind == tokEOL {
		return true
	}
	if nxt.kind == tokKeyword && (statementStarters[nxt.text] || nxt.text == "ELSE") {
		return true
	}
	if nxt.kind != tokIdent {
		return false
	}
	j++
	if j < len(p.tokens) && p.tokens[j].kind == tokLParen {
		depth := 1
		j++
		for j < len(p.tokens) && depth > 0 {
			switch p.tokens[j].kind {
			case tokLParen:
				depth++
			case tokRParen:
				depth--
			case tokEOF, tokEOL:
				return false
			}
			j++
		}
	}
	return j < len(p.tokens) && p.tokens[j].kind == tokOp && p.tokens[j].text == "="
}

func (p *parser) parsePrint() (stmt, error) {
	if err := p.expectKw("PRINT"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stPrint, trailing: "NL"}
	if p.acceptKind(tokHash) {
		ch, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.channel = ch
		s.hasChan = true
		p.acceptKind(tokComma)
	}
	if p.acceptKw("USING") {
		u, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.hasUsing = true
		s.using = u
		p.acceptKind(tokComma)
	}
	for {
		if p.atStop() {
			break
		}
		if p.tok().kind == tokKeyword && p.tok().text == "ELSE" {
			break
		}
		if p.modifierStart() {
			break
		}
		if p.tok().kind == tokComma || p.tok().kind == tokSemi {
			if p.tok().kind == tokComma {
				s.trailing = "TAB"
			} else {
				s.trailing = "NONE"
			}
			p.i++
			if p.atStop() || (p.tok().kind == tokKeyword && (statementStarters[p.tok().text] || p.tok().text == "ELSE")) {
				break
			}
			continue
		}
		if p.tok().kind == tokKeyword && statementStarters[p.tok().text] && p.tok().text != "TAB" && p.tok().text != "SPC" {
			break
		}
		e, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.items = append(s.items, printItem{expr: e})
		s.trailing = "NL"
		if p.acceptKind(tokComma) {
			s.items = append(s.items, printItem{sep: "TAB"})
			s.trailing = "TAB"
		} else if p.acceptKind(tokSemi) {
			s.items = append(s.items, printItem{sep: "NONE"})
			s.trailing = "NONE"
		} else {
			break
		}
	}
	return s, nil
}

func (p *parser) parseInput() (stmt, error) {
	if err := p.expectKw("INPUT"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stInput}
	if p.acceptKind(tokHash) {
		ch, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.channel = ch
		s.hasChan = true
		p.acceptKind(tokComma)
	}
	if p.tok().kind == tokString {
		pr := p.tok().text
		s.prompt = &pr
		s.hasPrompt = true
		p.i++
		if !p.acceptKind(tokSemi) && !p.acceptKind(tokComma) {
			return stmt{}, basicErr("Syntax error")
		}
	}
	v, err := p.parseVarRef()
	if err != nil {
		return stmt{}, err
	}
	s.targets = []*varRef{v}
	for p.acceptKind(tokComma) {
		v, err := p.parseVarRef()
		if err != nil {
			return stmt{}, err
		}
		s.targets = append(s.targets, v)
	}
	return s, nil
}

func (p *parser) parseLineInput() (stmt, error) {
	if err := p.expectKw("LINE"); err != nil {
		return stmt{}, err
	}
	if err := p.expectKw("INPUT"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stLineInput}
	if p.acceptKind(tokHash) {
		ch, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.channel = ch
		s.hasChan = true
		p.acceptKind(tokComma)
	}
	v, err := p.parseVarRef()
	if err != nil {
		return stmt{}, err
	}
	s.target = v
	return s, nil
}

func (p *parser) parseAssignment() (stmt, error) {
	v, err := p.parseVarRef()
	if err != nil {
		return stmt{}, err
	}
	if err := p.expectOp("="); err != nil {
		return stmt{}, err
	}
	e, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	return stmt{kind: stLet, target: v, expr: e}, nil
}

func (p *parser) parseIf() (stmt, error) {
	if err := p.expectKw("IF"); err != nil {
		return stmt{}, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	if err := p.expectKw("THEN"); err != nil {
		return stmt{}, err
	}
	thenPart, err := p.parseBranch(true)
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stIf, cond: cond, thenPart: thenPart}
	if p.acceptKw("ELSE") {
		elsePart, err := p.parseBranch(false)
		if err != nil {
			return stmt{}, err
		}
		s.elsePart = elsePart
	}
	return s, nil
}

func (p *parser) parseBranch(stopElse bool) (*branch, error) {
	if p.tok().kind == tokNumber {
		line, err := p.lineRef()
		if err != nil {
			return nil, err
		}
		return &branch{isLine: true, line: line}, nil
	}
	var stmts []stmt
	for p.tok().kind != tokEOF && p.tok().kind != tokEOL {
		if stopElse && p.tok().kind == tokKeyword && p.tok().text == "ELSE" {
			break
		}
		if p.tok().kind == tokBackslash || p.tok().kind == tokColon {
			break
		}
		st, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, st)
		break
	}
	return &branch{stmts: stmts}, nil
}

func (p *parser) parseFor() (stmt, error) {
	if err := p.expectKw("FOR"); err != nil {
		return stmt{}, err
	}
	name, err := p.identName()
	if err != nil {
		return stmt{}, err
	}
	if err := p.expectOp("="); err != nil {
		return stmt{}, err
	}
	start, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	if err := p.expectKw("TO"); err != nil {
		return stmt{}, err
	}
	end, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stFor, forVar: name, start: start, end: end}
	if p.acceptKw("STEP") {
		step, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.step = step
	}
	return s, nil
}

func (p *parser) parseNext() (stmt, error) {
	if err := p.expectKw("NEXT"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stNext}
	if p.tok().kind == tokIdent {
		name, err := p.identName()
		if err != nil {
			return stmt{}, err
		}
		s.nextVar = name
	}
	return s, nil
}

func (p *parser) parseWhile() (stmt, error) {
	if err := p.expectKw("WHILE"); err != nil {
		return stmt{}, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	return stmt{kind: stWhile, cond: cond}, nil
}

func (p *parser) parseUntil() (stmt, error) {
	if err := p.expectKw("UNTIL"); err != nil {
		return stmt{}, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	return stmt{kind: stUntil, cond: cond}, nil
}

func (p *parser) parseDim() (stmt, error) {
	if err := p.expectKw("DIM"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stDim}
	var ch expr
	for {
		if p.acceptKind(tokHash) {
			c, err := p.parseExpr()
			if err != nil {
				return stmt{}, err
			}
			ch = c
			if err := p.expectKind(tokComma); err != nil {
				return stmt{}, err
			}
		}
		item, err := p.parseDimItem()
		if err != nil {
			return stmt{}, err
		}
		item.channel = ch
		if item.channel != nil && strings.HasSuffix(item.name, "$") && p.acceptOp("=") {
			ln, err := p.parseExpr()
			if err != nil {
				return stmt{}, err
			}
			item.strLen = ln
		}
		s.arrays = append(s.arrays, item)
		if !p.acceptKind(tokComma) {
			break
		}
	}
	return s, nil
}

func (p *parser) parseDimItem() (dimItem, error) {
	name, err := p.identName()
	if err != nil {
		return dimItem{}, err
	}
	if err := p.expectKind(tokLParen); err != nil {
		return dimItem{}, err
	}
	b, err := p.parseExpr()
	if err != nil {
		return dimItem{}, err
	}
	item := dimItem{name: name, bounds: []expr{b}}
	for p.acceptKind(tokComma) {
		b, err := p.parseExpr()
		if err != nil {
			return dimItem{}, err
		}
		item.bounds = append(item.bounds, b)
	}
	if err := p.expectKind(tokRParen); err != nil {
		return dimItem{}, err
	}
	return item, nil
}

func (p *parser) parseData() (stmt, error) {
	if err := p.expectKw("DATA"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stData}
	for {
		t := p.tok()
		switch {
		case t.kind == tokString:
			s.data = append(s.data, value{isStr: true, str: t.text})
			p.i++
		case t.kind == tokNumber:
			s.data = append(s.data, value{num: t.num})
			p.i++
		case t.kind == tokOp && t.text == "-":
			p.i++
			if p.tok().kind != tokNumber {
				return stmt{}, basicErr("Syntax error")
			}
			s.data = append(s.data, value{num: -p.tok().num})
			p.i++
		case t.kind == tokIdent:
			s.data = append(s.data, value{isStr: true, str: t.text})
			p.i++
		default:
			return s, nil
		}
		if !p.acceptKind(tokComma) {
			break
		}
	}
	return s, nil
}

func (p *parser) parseRead() (stmt, error) {
	if err := p.expectKw("READ"); err != nil {
		return stmt{}, err
	}
	v, err := p.parseVarRef()
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stRead, targets: []*varRef{v}}
	for p.acceptKind(tokComma) {
		v, err := p.parseVarRef()
		if err != nil {
			return stmt{}, err
		}
		s.targets = append(s.targets, v)
	}
	return s, nil
}

func (p *parser) parseOpen() (stmt, error) {
	if err := p.expectKw("OPEN"); err != nil {
		return stmt{}, err
	}
	path, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	mode := "RANDOM"
	if p.acceptKw("FOR") {
		if p.tok().kind == tokKeyword && (p.tok().text == "INPUT" || p.tok().text == "OUTPUT" || p.tok().text == "APPEND") {
			mode = p.tok().text
			p.i++
		} else {
			return stmt{}, basicErr("Syntax error")
		}
	}
	if err := p.expectKw("AS"); err != nil {
		return stmt{}, err
	}
	p.acceptKw("FILE")
	p.acceptKind(tokHash)
	ch, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stOpen, path: path, mode: mode, channel: ch, hasChan: true}
	for {
		if p.atStop() || p.modifierStart() {
			break
		}
		if p.tok().kind == tokComma {
			p.i++
			if p.atStop() || p.modifierStart() {
				break
			}
		} else if !p.openClause() {
			break
		}
		switch {
		case p.acceptKw("RECORDSIZE"):
			rs, err := p.parseExpr()
			if err != nil {
				return stmt{}, err
			}
			s.recSize = rs
		case p.acceptKw("ORGANIZATION"):
			if !p.acceptKw("VIRTUAL") && !p.acceptName("VIRTUAL") {
				return stmt{}, basicErr("Syntax error")
			}
			s.org = "VIRTUAL"
		case p.acceptKw("MAP"):
			name, err := p.identName()
			if err != nil {
				return stmt{}, err
			}
			s.mapName = name
		default:
			return stmt{}, basicErr("Syntax error")
		}
	}
	return s, nil
}

func (p *parser) parseClose() (stmt, error) {
	if err := p.expectKw("CLOSE"); err != nil {
		return stmt{}, err
	}
	p.acceptKw("FILE")
	p.acceptKind(tokHash)
	s := stmt{kind: stClose}
	if !p.atStop() {
		ch, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.channel = ch
		s.hasChan = true
	}
	return s, nil
}

func (p *parser) parseRandomize() (stmt, error) {
	if err := p.expectKw("RANDOMIZE"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stRandomize}
	if !p.atStop() && p.tok().kind != tokKeyword {
		e, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.seed = e
		s.hasSeed = true
	}
	return s, nil
}

func (p *parser) parseOn() (stmt, error) {
	if err := p.expectKw("ON"); err != nil {
		return stmt{}, err
	}
	if p.acceptKw("ERROR") {
		if err := p.expectKw("GOTO"); err != nil {
			return stmt{}, err
		}
		line, err := p.lineRef()
		if err != nil {
			return stmt{}, err
		}
		return stmt{kind: stOnError, expr: line}, nil
	}
	e, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	kind := stOnGoto
	if p.acceptKw("GOTO") {
		kind = stOnGoto
	} else if p.acceptKw("GOSUB") {
		kind = stOnGosub
	} else {
		return stmt{}, basicErr("Syntax error")
	}
	line, err := p.lineRef()
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: kind, expr: e, lines: []expr{line}}
	for p.acceptKind(tokComma) {
		line, err := p.lineRef()
		if err != nil {
			return stmt{}, err
		}
		s.lines = append(s.lines, line)
	}
	return s, nil
}

func (p *parser) parseDef() (stmt, error) {
	if err := p.expectKw("DEF"); err != nil {
		return stmt{}, err
	}
	if p.tok().kind != tokIdent || !stringsHasPrefix(p.tok().text, "FN") {
		return stmt{}, basicErr("Syntax error")
	}
	name, err := p.identName()
	if err != nil {
		return stmt{}, err
	}
	var params []string
	if p.acceptKind(tokLParen) {
		if p.tok().kind == tokIdent {
			n, err := p.identName()
			if err != nil {
				return stmt{}, err
			}
			params = append(params, n)
			for p.acceptKind(tokComma) {
				n, err := p.identName()
				if err != nil {
					return stmt{}, err
				}
				params = append(params, n)
			}
		}
		if err := p.expectKind(tokRParen); err != nil {
			return stmt{}, err
		}
	}
	// With no = the definition runs on over the following lines until
	// FNEND, and the value is whatever the body last assigned to the
	// function's own name.
	if !p.acceptOp("=") {
		return stmt{kind: stDefMulti, fnName: name, params: params}, nil
	}
	body, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	return stmt{kind: stDef, fnName: name, params: params, fnExpr: body}, nil
}

func stringsHasPrefix(s, pfx string) bool {
	return len(s) >= len(pfx) && s[:len(pfx)] == pfx
}

func stringsHasSuffix(s, sfx string) bool {
	return len(s) >= len(sfx) && s[len(s)-len(sfx):] == sfx
}

func (p *parser) lineRef() (expr, error) {
	if p.tok().kind == tokNumber {
		v := p.tok().num
		if p.markRef {
			p.refs = append(p.refs, p.tok().pos)
		}
		p.i++
		return numLit{v: v}, nil
	}
	return p.parseExpr()
}

func (p *parser) identName() (string, error) {
	if p.tok().kind != tokIdent {
		return "", basicErr("Syntax error")
	}
	name := p.tok().text
	p.i++
	return name, nil
}

func (p *parser) parseVarRef() (*varRef, error) {
	name, err := p.identName()
	if err != nil {
		return nil, err
	}
	v := &varRef{name: name}
	if p.acceptKind(tokLParen) {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		v.indices = append(v.indices, e)
		for p.acceptKind(tokComma) {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			v.indices = append(v.indices, e)
		}
		if err := p.expectKind(tokRParen); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func (p *parser) parseExpr() (expr, error) { return p.parseOr() }

func (p *parser) parseOr() (expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.acceptKw("OR") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binExpr{op: "OR", l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.acceptKw("AND") {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = binExpr{op: "AND", l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseNot() (expr, error) {
	if p.acceptKw("NOT") {
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return unaryExpr{op: "NOT", x: x}, nil
	}
	return p.parseRel()
}

func (p *parser) parseRel() (expr, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.tok().kind == tokOp {
		op := p.tok().text
		switch op {
		case "=", "<>", "<", ">", "<=", ">=":
			p.i++
			right, err := p.parseAdd()
			if err != nil {
				return nil, err
			}
			left = binExpr{op: op, l: left, r: right}
			continue
		}
		break
	}
	return left, nil
}

func (p *parser) parseAdd() (expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.tok().kind == tokOp && (p.tok().text == "+" || p.tok().text == "-") {
		op := p.tok().text
		p.i++
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = binExpr{op: op, l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseMul() (expr, error) {
	left, err := p.parsePow()
	if err != nil {
		return nil, err
	}
	for {
		if p.tok().kind == tokOp && (p.tok().text == "*" || p.tok().text == "/") {
			op := p.tok().text
			p.i++
			right, err := p.parsePow()
			if err != nil {
				return nil, err
			}
			left = binExpr{op: op, l: left, r: right}
			continue
		}
		if p.acceptKw("MOD") {
			right, err := p.parsePow()
			if err != nil {
				return nil, err
			}
			left = binExpr{op: "MOD", l: left, r: right}
			continue
		}
		if p.tok().kind == tokBackslash {
			if p.backslashStartsStatement() {
				break
			}
			p.i++
			right, err := p.parsePow()
			if err != nil {
				return nil, err
			}
			left = binExpr{op: `\`, l: left, r: right}
			continue
		}
		break
	}
	return left, nil
}

func (p *parser) parsePow() (expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if p.tok().kind == tokOp && p.tok().text == "^" {
		p.i++
		right, err := p.parsePow()
		if err != nil {
			return nil, err
		}
		return binExpr{op: "^", l: left, r: right}, nil
	}
	return left, nil
}

func (p *parser) parseUnary() (expr, error) {
	if p.tok().kind == tokOp && (p.tok().text == "+" || p.tok().text == "-") {
		op := p.tok().text
		p.i++
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryExpr{op: op, x: x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (expr, error) {
	if p.acceptKind(tokLParen) {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expectKind(tokRParen); err != nil {
			return nil, err
		}
		return e, nil
	}
	if p.tok().kind == tokNumber {
		v := p.tok().num
		p.i++
		return numLit{v: v}, nil
	}
	if p.tok().kind == tokString {
		v := p.tok().text
		p.i++
		return strLit{v: v}, nil
	}
	if p.tok().kind == tokKeyword && (p.tok().text == "TAB" || p.tok().text == "SPC") {
		name := p.tok().text
		p.i++
		if err := p.expectKind(tokLParen); err != nil {
			return nil, err
		}
		a, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expectKind(tokRParen); err != nil {
			return nil, err
		}
		return callExpr{name: name, args: []expr{a}}, nil
	}
	if p.tok().kind == tokIdent {
		name := p.tok().text
		p.i++
		if p.acceptKind(tokLParen) {
			var args []expr
			if p.tok().kind != tokRParen {
				a, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				for p.acceptKind(tokComma) {
					a, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
				}
			}
			if err := p.expectKind(tokRParen); err != nil {
				return nil, err
			}
			if builtins[name] || stringsHasPrefix(name, "FN") {
				return callExpr{name: name, args: args}, nil
			}
			return varRef{name: name, indices: args}, nil
		}
		if name == "DATE$" || name == "TIME$" || name == "DATE" || name == "TIME" || name == "PI" || builtins[name] {
			return callExpr{name: name}, nil
		}
		return varRef{name: name}, nil
	}
	return nil, basicErr("Syntax error")
}

func (p *parser) modifierStart() bool {
	if p.tok().kind != tokKeyword {
		return false
	}
	switch p.tok().text {
	case "IF", "UNLESS", "WHILE", "UNTIL", "FOR":
		return true
	}
	return false
}

func canModify(k stmtKind) bool {
	switch k {
	case stRem, stData, stFor, stNext, stWhile, stUntil, stDef, stOnError, stResume,
		stDefMulti, stFnEnd:
		return false
	}
	return true
}

func (p *parser) parseModifiers(s *stmt) error {
	if !canModify(s.kind) {
		return nil
	}
	for p.modifierStart() {
		kw := p.tok().text
		p.i++
		mod := modifier{kind: kw}
		switch kw {
		case "IF", "UNLESS", "WHILE", "UNTIL":
			cond, err := p.parseExpr()
			if err != nil {
				return err
			}
			mod.cond = cond
		case "FOR":
			name, err := p.identName()
			if err != nil {
				return err
			}
			if err := p.expectOp("="); err != nil {
				return err
			}
			start, err := p.parseExpr()
			if err != nil {
				return err
			}
			if err := p.expectKw("TO"); err != nil {
				return err
			}
			end, err := p.parseExpr()
			if err != nil {
				return err
			}
			mod.forVar = name
			mod.start = start
			mod.end = end
			if p.acceptKw("STEP") {
				step, err := p.parseExpr()
				if err != nil {
					return err
				}
				mod.step = step
			}
		}
		s.mods = append(s.mods, mod)
	}
	return nil
}

func (p *parser) parseChange() (stmt, error) {
	if err := p.expectKw("CHANGE"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stChange}
	if p.tok().kind == tokIdent {
		next := tokEOF
		if p.i+1 < len(p.tokens) {
			next = p.tokens[p.i+1].kind
		}
		if next == tokLParen {
			e, err := p.parseExpr()
			if err != nil {
				return stmt{}, err
			}
			s.fromExpr = e
		} else {
			name, err := p.identName()
			if err != nil {
				return stmt{}, err
			}
			s.fromName = name
			if stringsHasSuffix(name, "$") {
				s.fromExpr = varRef{name: name}
			}
		}
	} else {
		e, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.fromExpr = e
	}
	if err := p.expectKw("TO"); err != nil {
		return stmt{}, err
	}
	to, err := p.identName()
	if err != nil {
		return stmt{}, err
	}
	s.toName = to
	return s, nil
}

func (p *parser) parseGetPut(kind stmtKind) (stmt, error) {
	p.i++ // GET or PUT already matched
	p.acceptKind(tokHash)
	ch, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: kind, channel: ch, hasChan: true}
	if p.tok().kind == tokComma {
		p.i++
	}
	if p.acceptKw("RECORD") {
		rec, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.record = rec
		s.hasRec = true
	}
	return s, nil
}

func (p *parser) parseField() (stmt, error) {
	if err := p.expectKw("FIELD"); err != nil {
		return stmt{}, err
	}
	p.acceptKind(tokHash)
	ch, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stField, channel: ch, hasChan: true}
	if !p.acceptKind(tokComma) {
		return stmt{}, basicErr("Syntax error")
	}
	for {
		length, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		if err := p.expectKw("AS"); err != nil {
			return stmt{}, err
		}
		name, err := p.identName()
		if err != nil {
			return stmt{}, err
		}
		s.fields = append(s.fields, fieldItem{length: length, name: name})
		if !p.acceptKind(tokComma) {
			break
		}
	}
	return s, nil
}

func (p *parser) parseLRset(kind stmtKind) (stmt, error) {
	p.i++
	v, err := p.parseVarRef()
	if err != nil {
		return stmt{}, err
	}
	if err := p.expectOp("="); err != nil {
		return stmt{}, err
	}
	e, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	return stmt{kind: kind, target: v, expr: e}, nil
}

func (p *parser) parseResume() (stmt, error) {
	if err := p.expectKw("RESUME"); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stResume}
	if p.acceptKw("NEXT") {
		s.resumeNxt = true
		return s, nil
	}
	if !p.atStop() && !p.modifierStart() {
		line, err := p.lineRef()
		if err != nil {
			return stmt{}, err
		}
		s.expr = line
	}
	return s, nil
}

func (p *parser) parseMat() (stmt, error) {
	if err := p.expectKw("MAT"); err != nil {
		return stmt{}, err
	}
	if p.acceptKw("READ") {
		return p.parseMatList("READ")
	}
	if p.acceptKw("PRINT") {
		return p.parseMatPrint()
	}
	if p.acceptKw("INPUT") {
		return p.parseMatList("INPUT")
	}
	dest, err := p.identName()
	if err != nil {
		return stmt{}, err
	}
	if err := p.expectOp("="); err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stMat, matDest: dest}
	if p.acceptKind(tokLParen) {
		sc, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		if err := p.expectKind(tokRParen); err != nil {
			return stmt{}, err
		}
		if err := p.expectOp("*"); err != nil {
			return stmt{}, err
		}
		src, err := p.identName()
		if err != nil {
			return stmt{}, err
		}
		s.matKind = "SCALE"
		s.expr = sc
		s.matLeft = src
		return s, nil
	}
	name, err := p.identName()
	if err != nil {
		return stmt{}, err
	}
	switch name {
	case "ZER", "CON", "IDN":
		s.matKind = name
		if p.acceptKind(tokLParen) {
			if !p.acceptKind(tokRParen) {
				b, err := p.parseExpr()
				if err != nil {
					return stmt{}, err
				}
				s.matBounds = []expr{b}
				for p.acceptKind(tokComma) {
					b, err := p.parseExpr()
					if err != nil {
						return stmt{}, err
					}
					s.matBounds = append(s.matBounds, b)
				}
				if err := p.expectKind(tokRParen); err != nil {
					return stmt{}, err
				}
			}
		}
		return s, nil
	case "TRN", "INV":
		if err := p.expectKind(tokLParen); err != nil {
			return stmt{}, err
		}
		src, err := p.identName()
		if err != nil {
			return stmt{}, err
		}
		if err := p.expectKind(tokRParen); err != nil {
			return stmt{}, err
		}
		s.matKind = name
		s.matLeft = src
		return s, nil
	}
	if p.acceptOp("+") {
		right, err := p.identName()
		if err != nil {
			return stmt{}, err
		}
		s.matKind = "ADD"
		s.matLeft = name
		s.matRight = right
		return s, nil
	}
	if p.acceptOp("-") {
		right, err := p.identName()
		if err != nil {
			return stmt{}, err
		}
		s.matKind = "SUB"
		s.matLeft = name
		s.matRight = right
		return s, nil
	}
	if p.acceptOp("*") {
		right, err := p.identName()
		if err != nil {
			return stmt{}, err
		}
		s.matKind = "MUL"
		s.matLeft = name
		s.matRight = right
		return s, nil
	}
	s.matKind = "COPY"
	s.matLeft = name
	return s, nil
}

func (p *parser) parseMatList(kind string) (stmt, error) {
	s := stmt{kind: stMat, matKind: kind}
	name, err := p.identName()
	if err != nil {
		return stmt{}, err
	}
	s.matNames = []string{name}
	for p.acceptKind(tokComma) {
		name, err := p.identName()
		if err != nil {
			return stmt{}, err
		}
		s.matNames = append(s.matNames, name)
	}
	return s, nil
}

func (p *parser) parseMatPrint() (stmt, error) {
	s := stmt{kind: stMat, matKind: "PRINT", trailing: "NL"}
	name, err := p.identName()
	if err != nil {
		return stmt{}, err
	}
	s.matNames = []string{name}
	for {
		if p.acceptKind(tokComma) {
			s.trailing = "TAB"
			if p.atStop() || p.modifierStart() {
				break
			}
			name, err := p.identName()
			if err != nil {
				return stmt{}, err
			}
			s.matNames = append(s.matNames, name)
			continue
		}
		if p.acceptKind(tokSemi) {
			s.trailing = "NONE"
			if p.atStop() || p.modifierStart() {
				break
			}
			name, err := p.identName()
			if err != nil {
				return stmt{}, err
			}
			s.matNames = append(s.matNames, name)
			continue
		}
		break
	}
	return s, nil
}

func (p *parser) parseChain() (stmt, error) {
	if err := p.expectKw("CHAIN"); err != nil {
		return stmt{}, err
	}
	path, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stChain, path: path}
	if p.acceptKw("LINE") {
		line, err := p.parseExpr()
		if err != nil {
			return stmt{}, err
		}
		s.expr = line
	}
	return s, nil
}

func (p *parser) parseSleep() (stmt, error) {
	if err := p.expectKw("SLEEP"); err != nil {
		return stmt{}, err
	}
	n, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	return stmt{kind: stSleep, expr: n}, nil
}

func (p *parser) parseKill() (stmt, error) {
	if err := p.expectKw("KILL"); err != nil {
		return stmt{}, err
	}
	path, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	return stmt{kind: stKill, path: path}, nil
}

func (p *parser) parseName() (stmt, error) {
	if err := p.expectKw("NAME"); err != nil {
		return stmt{}, err
	}
	old, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	if err := p.expectKw("AS"); err != nil {
		return stmt{}, err
	}
	newp, err := p.parseExpr()
	if err != nil {
		return stmt{}, err
	}
	return stmt{kind: stName, fromExpr: old, expr: newp}, nil
}

func (p *parser) parseMap() (stmt, error) {
	if err := p.expectKw("MAP"); err != nil {
		return stmt{}, err
	}
	if err := p.expectKind(tokLParen); err != nil {
		return stmt{}, err
	}
	name, err := p.identName()
	if err != nil {
		return stmt{}, err
	}
	if err := p.expectKind(tokRParen); err != nil {
		return stmt{}, err
	}
	field, err := p.parseMapField()
	if err != nil {
		return stmt{}, err
	}
	s := stmt{kind: stMap, mapName: name, mapFields: []mapField{field}}
	for p.acceptKind(tokComma) {
		field, err := p.parseMapField()
		if err != nil {
			return stmt{}, err
		}
		s.mapFields = append(s.mapFields, field)
	}
	return s, nil
}

func (p *parser) parseMapField() (mapField, error) {
	f := mapField{}
	if p.tok().kind == tokIdent || p.tok().kind == tokKeyword {
		if mapTypeName(p.tok().text) {
			f.typ = p.tok().text
			p.i++
		}
	}
	name, err := p.identName()
	if err != nil {
		return mapField{}, err
	}
	f.name = name
	if f.typ == "" {
		if strings.HasSuffix(name, "$") {
			f.typ = "STRING"
		} else {
			f.typ = "INTEGER"
		}
	}
	if p.acceptOp("=") {
		sz, err := p.parseExpr()
		if err != nil {
			return mapField{}, err
		}
		f.size = sz
	}
	return f, nil
}

func mapTypeName(s string) bool {
	switch s {
	case "LONG", "STRING", "WORD", "BYTE", "INTEGER", "SINGLE", "DOUBLE", "FLOAT":
		return true
	}
	return false
}
