package rsts

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type ptokKind int

const (
	tkEOF ptokKind = iota
	tkIdent
	tkInt
	tkReal
	tkString
	tkPlus
	tkMinus
	tkStar
	tkSlash
	tkAssign
	tkEq
	tkNe
	tkLt
	tkLe
	tkGt
	tkGe
	tkLParen
	tkRParen
	tkLBrack
	tkRBrack
	tkComma
	tkSemi
	tkColon
	tkDot
	tkDotDot
	tkCaret
	tkProgram
	tkConst
	tkType
	tkVar
	tkProcedure
	tkFunction
	tkBegin
	tkEnd
	tkIf
	tkThen
	tkElse
	tkCase
	tkOf
	tkWhile
	tkDo
	tkRepeat
	tkUntil
	tkFor
	tkTo
	tkDownto
	tkWith
	tkGoto
	tkLabel
	tkPacked
	tkArray
	tkRecord
	tkSet
	tkFile
	tkNil
	tkNot
	tkAnd
	tkOr
	tkIn
	tkDiv
	tkMod
	tkForward
	tkOtherwise
	tkTrue
	tkFalse
)

var pasKeywords = map[string]ptokKind{
	"PROGRAM": tkProgram, "CONST": tkConst, "TYPE": tkType, "VAR": tkVar,
	"PROCEDURE": tkProcedure, "FUNCTION": tkFunction,
	"BEGIN": tkBegin, "END": tkEnd, "IF": tkIf, "THEN": tkThen, "ELSE": tkElse,
	"CASE": tkCase, "OF": tkOf, "WHILE": tkWhile, "DO": tkDo,
	"REPEAT": tkRepeat, "UNTIL": tkUntil, "FOR": tkFor, "TO": tkTo,
	"DOWNTO": tkDownto, "WITH": tkWith, "GOTO": tkGoto, "LABEL": tkLabel,
	"PACKED": tkPacked, "ARRAY": tkArray, "RECORD": tkRecord, "SET": tkSet,
	"FILE": tkFile, "NIL": tkNil, "NOT": tkNot, "AND": tkAnd, "OR": tkOr,
	"IN": tkIn, "DIV": tkDiv, "MOD": tkMod, "FORWARD": tkForward,
	"OTHERWISE": tkOtherwise, "TRUE": tkTrue, "FALSE": tkFalse,
}

type ptok struct {
	kind ptokKind
	lit  string
	ival int64
	fval float64
	line int
	col  int
}

type pLex struct {
	src  string
	i    int
	line int
	col  int
	tok  ptok
}

func newPLex(src string) *pLex {
	lx := &pLex{src: src, line: 1, col: 1}
	lx.next()
	return lx
}

func (l *pLex) peekByte() byte {
	if l.i >= len(l.src) {
		return 0
	}
	return l.src[l.i]
}

func (l *pLex) take() byte {
	if l.i >= len(l.src) {
		return 0
	}
	b := l.src[l.i]
	l.i++
	if b == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return b
}

func (l *pLex) skipSpaceAndComments() {
	for {
		for l.i < len(l.src) {
			c := l.peekByte()
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				l.take()
				continue
			}
			break
		}
		if l.peekByte() == '{' {
			l.take()
			for l.i < len(l.src) && l.peekByte() != '}' {
				l.take()
			}
			if l.i < len(l.src) {
				l.take()
			}
			continue
		}
		if l.peekByte() == '(' && l.i+1 < len(l.src) && l.src[l.i+1] == '*' {
			l.take()
			l.take()
			for l.i+1 < len(l.src) && !(l.peekByte() == '*' && l.src[l.i+1] == ')') {
				l.take()
			}
			if l.i+1 < len(l.src) {
				l.take()
				l.take()
			}
			continue
		}
		return
	}
}

func (l *pLex) next() {
	l.skipSpaceAndComments()
	tok := ptok{line: l.line, col: l.col}
	if l.i >= len(l.src) {
		tok.kind = tkEOF
		l.tok = tok
		return
	}
	c := l.peekByte()
	switch {
	case isPasIdentStart(c):
		start := l.i
		for l.i < len(l.src) && isPasIdent(l.peekByte()) {
			l.take()
		}
		lit := l.src[start:l.i]
		up := strings.ToUpper(lit)
		if k, ok := pasKeywords[up]; ok {
			tok.kind = k
			tok.lit = up
		} else {
			tok.kind = tkIdent
			tok.lit = up
		}
	case c >= '0' && c <= '9':
		l.scanNumber(&tok)
	case c == '\'':
		l.scanString(&tok)
	default:
		l.take()
		switch c {
		case '+':
			tok.kind = tkPlus
		case '-':
			tok.kind = tkMinus
		case '*':
			tok.kind = tkStar
		case '/':
			tok.kind = tkSlash
		case '=':
			tok.kind = tkEq
		case '(':
			tok.kind = tkLParen
		case ')':
			tok.kind = tkRParen
		case '[':
			tok.kind = tkLBrack
		case ']':
			tok.kind = tkRBrack
		case ',':
			tok.kind = tkComma
		case ';':
			tok.kind = tkSemi
		case '^':
			tok.kind = tkCaret
		case ':':
			if l.peekByte() == '=' {
				l.take()
				tok.kind = tkAssign
			} else {
				tok.kind = tkColon
			}
		case '<':
			switch l.peekByte() {
			case '=':
				l.take()
				tok.kind = tkLe
			case '>':
				l.take()
				tok.kind = tkNe
			default:
				tok.kind = tkLt
			}
		case '>':
			if l.peekByte() == '=' {
				l.take()
				tok.kind = tkGe
			} else {
				tok.kind = tkGt
			}
		case '.':
			if l.peekByte() == '.' {
				l.take()
				tok.kind = tkDotDot
			} else {
				tok.kind = tkDot
			}
		default:
			tok.kind = tkEOF
			tok.lit = string(c)
		}
	}
	l.tok = tok
}

func (l *pLex) scanNumber(tok *ptok) {
	start := l.i
	for l.i < len(l.src) && l.peekByte() >= '0' && l.peekByte() <= '9' {
		l.take()
	}
	// 1..2 must not become a real.
	if l.peekByte() == '.' && l.i+1 < len(l.src) && l.src[l.i+1] != '.' && l.src[l.i+1] >= '0' && l.src[l.i+1] <= '9' {
		l.take()
		for l.i < len(l.src) && l.peekByte() >= '0' && l.peekByte() <= '9' {
			l.take()
		}
		if l.peekByte() == 'e' || l.peekByte() == 'E' {
			l.take()
			if l.peekByte() == '+' || l.peekByte() == '-' {
				l.take()
			}
			for l.i < len(l.src) && l.peekByte() >= '0' && l.peekByte() <= '9' {
				l.take()
			}
		}
		tok.kind = tkReal
		tok.lit = l.src[start:l.i]
		tok.fval, _ = strconv.ParseFloat(tok.lit, 64)
		return
	}
	if l.peekByte() == 'e' || l.peekByte() == 'E' {
		l.take()
		if l.peekByte() == '+' || l.peekByte() == '-' {
			l.take()
		}
		for l.i < len(l.src) && l.peekByte() >= '0' && l.peekByte() <= '9' {
			l.take()
		}
		tok.kind = tkReal
		tok.lit = l.src[start:l.i]
		tok.fval, _ = strconv.ParseFloat(tok.lit, 64)
		return
	}
	tok.kind = tkInt
	tok.lit = l.src[start:l.i]
	tok.ival, _ = strconv.ParseInt(tok.lit, 10, 64)
}

func (l *pLex) scanString(tok *ptok) {
	l.take() // '
	var b strings.Builder
	for l.i < len(l.src) {
		c := l.take()
		if c == '\'' {
			if l.peekByte() == '\'' {
				l.take()
				b.WriteByte('\'')
				continue
			}
			break
		}
		if c == 0 {
			break
		}
		b.WriteByte(c)
	}
	tok.kind = tkString
	tok.lit = b.String()
}

func isPasIdentStart(c byte) bool {
	return unicode.IsLetter(rune(c))
}

func isPasIdent(c byte) bool {
	r, _ := utf8.DecodeRune([]byte{c})
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
