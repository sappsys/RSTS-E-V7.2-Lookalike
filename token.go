package rsts

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type BasicError struct {
	Msg  string
	Line int // 0 means no line
	Code int
}

func (e *BasicError) Error() string {
	if e.Line != 0 {
		return fmt.Sprintf("?%s at line %d", e.Msg, e.Line)
	}
	return "?" + e.Msg
}

func basicErr(msg string) error { return &BasicError{Msg: msg} }

func basicErrAt(msg string, line int) error { return &BasicError{Msg: msg, Line: line} }

func attachLine(err error, line int) error {
	if err == nil {
		return nil
	}
	if be, ok := err.(*BasicError); ok && be.Line == 0 && line != 0 {
		return &BasicError{Msg: be.Msg, Line: line}
	}
	return err
}

var keywords = map[string]bool{
	"PRINT": true, "INPUT": true, "LET": true, "GOTO": true, "GOSUB": true,
	"RETURN": true, "IF": true, "THEN": true, "ELSE": true, "FOR": true,
	"TO": true, "STEP": true, "NEXT": true, "WHILE": true, "UNTIL": true,
	"DIM": true, "DATA": true, "READ": true, "RESTORE": true, "END": true,
	"STOP": true, "REM": true, "RUN": true, "LIST": true, "NEW": true,
	"OLD": true, "SAVE": true, "DELETE": true, "OPEN": true, "CLOSE": true,
	"AS": true, "FILE": true, "OUTPUT": true, "APPEND": true, "RANDOMIZE": true,
	"ON": true, "DEF": true, "AND": true, "OR": true, "NOT": true, "MOD": true,
	"GO": true, "TAB": true, "SPC": true, "LINE": true, "REPLACE": true,
	"CLEAR": true, "CHAIN": true, "NAME": true, "ERROR": true, "RESUME": true,
	"CHANGE": true, "USING": true, "GET": true, "PUT": true, "FIELD": true,
	"LSET": true, "RSET": true, "RECORD": true, "RECORDSIZE": true, "UNLESS": true,
	"MAT": true, "MAP": true, "ORGANIZATION": true, "VIRTUAL": true,
}

var statementStarters = map[string]bool{
	"PRINT": true, "INPUT": true, "LET": true, "GOTO": true, "GOSUB": true,
	"RETURN": true, "IF": true, "FOR": true, "NEXT": true, "WHILE": true,
	"DIM": true, "DATA": true, "READ": true, "RESTORE": true, "END": true,
	"STOP": true, "REM": true, "OPEN": true, "CLOSE": true, "RANDOMIZE": true,
	"ON": true, "DEF": true, "CHANGE": true, "LINE": true, "UNTIL": true,
	"GET": true, "PUT": true, "FIELD": true, "LSET": true, "RSET": true,
	"RESUME": true, "MAT": true, "MAP": true,
}

var builtins = map[string]bool{
	"ABS": true, "INT": true, "SGN": true, "SQR": true, "SIN": true, "COS": true,
	"TAN": true, "ATN": true, "LOG": true, "EXP": true, "RND": true, "LEN": true,
	"LEFT$": true, "RIGHT$": true, "MID$": true, "INSTR": true, "CHR$": true,
	"ASC": true, "STR$": true, "VAL": true, "TAB": true, "SPC": true, "DATE$": true,
	"TIME$": true, "SPACE$": true, "STRING$": true, "POS": true, "FIX": true,
	"LOG10": true, "PI": true, "SYS": true, "ERR": true, "ERL": true,
	"CVT%$": true, "CVT$%": true, "CVTF$": true, "CVT$F": true, "CVT$$": true,
	"PEEK": true, "SWAP%": true, "TIME": true, "DATE": true,
	"NUM1$": true, "NUM$": true,
}

type tokKind int

const (
	tokEOF tokKind = iota
	tokEOL
	tokNumber
	tokString
	tokIdent
	tokKeyword
	tokOp
	tokLParen
	tokRParen
	tokComma
	tokSemi
	tokHash
	tokBackslash
	tokColon
)

type token struct {
	kind tokKind
	num  float64
	text string
	pos  int
}

func tokenize(text string) ([]token, error) {
	t := tokenizer{text: text}
	return t.run()
}

type tokenizer struct {
	text string
	i    int
}

func (t *tokenizer) run() ([]token, error) {
	var tokens []token
	n := len(t.text)
	for t.i < n {
		ch := t.text[t.i]
		if ch == ' ' || ch == '\t' || ch == '\r' {
			t.i++
			continue
		}
		if ch == '\n' {
			tokens = append(tokens, token{kind: tokEOL, text: "\n", pos: t.i})
			t.i++
			continue
		}
		if ch == '!' {
			for t.i < n && t.text[t.i] != '\\' && t.text[t.i] != '\n' {
				t.i++
			}
			continue
		}
		if ch == '"' {
			tok, err := t.readString()
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, tok)
			continue
		}
		if unicode.IsDigit(rune(ch)) || (ch == '.' && t.i+1 < n && unicode.IsDigit(rune(t.text[t.i+1]))) {
			tok, err := t.readNumber()
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, tok)
			continue
		}
		if unicode.IsLetter(rune(ch)) {
			tokens = append(tokens, t.readIdent())
			continue
		}
		pos := t.i
		if t.i+1 < n {
			two := t.text[t.i : t.i+2]
			switch two {
			case "<>", "<=", ">=", "==":
				op := two
				if two == "==" {
					op = "<>"
				}
				tokens = append(tokens, token{kind: tokOp, text: op, pos: pos})
				t.i += 2
				continue
			}
		}
		switch ch {
		case '=', '+', '-', '*', '/', '^', '<', '>':
			tokens = append(tokens, token{kind: tokOp, text: string(ch), pos: pos})
			t.i++
		case '\\':
			tokens = append(tokens, token{kind: tokBackslash, text: `\`, pos: pos})
			t.i++
		case '(':
			tokens = append(tokens, token{kind: tokLParen, text: "(", pos: pos})
			t.i++
		case ')':
			tokens = append(tokens, token{kind: tokRParen, text: ")", pos: pos})
			t.i++
		case ',':
			tokens = append(tokens, token{kind: tokComma, text: ",", pos: pos})
			t.i++
		case ';':
			tokens = append(tokens, token{kind: tokSemi, text: ";", pos: pos})
			t.i++
		case '#':
			tokens = append(tokens, token{kind: tokHash, text: "#", pos: pos})
			t.i++
		case ':':
			tokens = append(tokens, token{kind: tokColon, text: ":", pos: pos})
			t.i++
		default:
			return nil, basicErr(fmt.Sprintf("Illegal character '%c'", ch))
		}
	}
	tokens = append(tokens, token{kind: tokEOF, pos: t.i})
	return tokens, nil
}

func (t *tokenizer) readString() (token, error) {
	pos := t.i
	t.i++
	var b strings.Builder
	n := len(t.text)
	for t.i < n {
		ch := t.text[t.i]
		if ch == '"' {
			if t.i+1 < n && t.text[t.i+1] == '"' {
				b.WriteByte('"')
				t.i += 2
				continue
			}
			t.i++
			return token{kind: tokString, text: b.String(), pos: pos}, nil
		}
		b.WriteByte(ch)
		t.i++
	}
	return token{}, basicErr("Unclosed string")
}

func (t *tokenizer) readNumber() (token, error) {
	pos := t.i
	n := len(t.text)
	start := t.i
	for t.i < n && (unicode.IsDigit(rune(t.text[t.i])) || t.text[t.i] == '.') {
		t.i++
	}
	if t.i < n && (t.text[t.i] == 'e' || t.text[t.i] == 'E') {
		t.i++
		if t.i < n && (t.text[t.i] == '+' || t.text[t.i] == '-') {
			t.i++
		}
		for t.i < n && unicode.IsDigit(rune(t.text[t.i])) {
			t.i++
		}
	}
	raw := t.text[start:t.i]
	integer := false
	if t.i < n && t.text[t.i] == '%' {
		integer = true
		t.i++
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return token{}, basicErr("Illegal number")
	}
	if integer {
		v = float64(int(v))
	}
	return token{kind: tokNumber, num: v, pos: pos}, nil
}

func (t *tokenizer) readIdent() token {
	pos := t.i
	n := len(t.text)
	start := t.i
	for t.i < n {
		ch := t.text[t.i]
		if unicode.IsLetter(rune(ch)) || unicode.IsDigit(rune(ch)) || ch == '_' || ch == '$' || ch == '%' {
			t.i++
			continue
		}
		break
	}
	name := strings.ToUpper(t.text[start:t.i])
	if name == "GO" && t.peekWord() == "TO" {
		t.skipWS()
		t.i += 2
		return token{kind: tokKeyword, text: "GOTO", pos: pos}
	}
	if keywords[name] {
		return token{kind: tokKeyword, text: name, pos: pos}
	}
	return token{kind: tokIdent, text: name, pos: pos}
}

func (t *tokenizer) skipWS() {
	for t.i < len(t.text) && (t.text[t.i] == ' ' || t.text[t.i] == '\t') {
		t.i++
	}
}

func (t *tokenizer) peekWord() string {
	j := t.i
	n := len(t.text)
	for j < n && (t.text[j] == ' ' || t.text[j] == '\t') {
		j++
	}
	k := j
	for k < n && unicode.IsLetter(rune(t.text[k])) {
		k++
	}
	return strings.ToUpper(t.text[j:k])
}
