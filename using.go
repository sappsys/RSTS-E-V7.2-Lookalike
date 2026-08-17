package rsts

import (
	"math"
	"strconv"
	"strings"
)

func formatUsing(format string, vals []value) (string, error) {
	var b strings.Builder
	vi := 0
	i := 0
	for i < len(format) {
		if format[i] == '_' && i+1 < len(format) {
			b.WriteByte(format[i+1])
			i += 2
			continue
		}
		if isUsingNumericStart(format, i) {
			field, next := scanNumericField(format, i)
			if vi >= len(vals) {
				return "", basicErr("PRINT USING")
			}
			b.WriteString(formatNumericField(field, vals[vi]))
			vi++
			i = next
			continue
		}
		if format[i] == '!' {
			if vi >= len(vals) {
				return "", basicErr("PRINT USING")
			}
			s := valueString(vals[vi])
			if s == "" {
				b.WriteByte(' ')
			} else {
				b.WriteByte(s[0])
			}
			vi++
			i++
			continue
		}
		if format[i] == '&' {
			if vi >= len(vals) {
				return "", basicErr("PRINT USING")
			}
			b.WriteString(valueString(vals[vi]))
			vi++
			i++
			continue
		}
		if format[i] == '\\' {
			j := i + 1
			for j < len(format) && format[j] != '\\' {
				j++
			}
			width := 1
			if j < len(format) {
				width = j - i + 1
				j++
			}
			if vi >= len(vals) {
				return "", basicErr("PRINT USING")
			}
			s := valueString(vals[vi])
			if len(s) > width {
				s = s[:width]
			} else {
				s = padRight(s, width)
			}
			b.WriteString(s)
			vi++
			i = j
			continue
		}
		b.WriteByte(format[i])
		i++
	}
	return b.String(), nil
}

func valueString(v value) string {
	if v.isStr {
		return v.str
	}
	return fmtNum(v.num)
}

func isUsingNumericStart(format string, i int) bool {
	ch := format[i]
	return ch == '#' || ch == '*' || ch == '$' || ch == '+' || (ch == '.' && i+1 < len(format) && format[i+1] == '#')
}

func scanNumericField(format string, i int) (string, int) {
	start := i
	for i < len(format) {
		ch := format[i]
		if ch == '#' || ch == '*' || ch == '$' || ch == '+' || ch == '-' || ch == '.' || ch == '^' || ch == ',' {
			i++
			continue
		}
		break
	}
	return format[start:i], i
}

func formatNumericField(field string, v value) string {
	n := 0.0
	if !v.isStr {
		n = v.num
	} else {
		n, _ = strconv.ParseFloat(strings.TrimSpace(v.str), 64)
	}
	intDigits := 0
	fracDigits := 0
	exp := strings.Contains(field, "^")
	afterDot := false
	dollar := strings.Contains(field, "$")
	star := strings.Contains(field, "*")
	plus := strings.HasPrefix(field, "+") || strings.HasSuffix(field, "+")
	for _, ch := range field {
		switch ch {
		case '.':
			afterDot = true
		case '#', '*', '$':
			if afterDot {
				fracDigits++
			} else if ch != '$' || intDigits > 0 {
				if ch == '#' || ch == '*' {
					intDigits++
				}
			} else {
				// leading $
			}
		}
	}
	if intDigits == 0 {
		intDigits = 1
	}
	neg := n < 0
	if neg {
		n = -n
	}
	if exp {
		s := strconv.FormatFloat(n, 'E', fracDigits, 64)
		if plus && !neg {
			s = "+" + s
		} else if neg {
			s = "-" + s
		}
		if len(s) < len(field) {
			s = strings.Repeat(" ", len(field)-len(s)) + s
		}
		return s
	}
	scale := math.Pow(10, float64(fracDigits))
	n = math.Round(n*scale) / scale
	intPart := int64(n)
	frac := int64(math.Round((n - float64(intPart)) * scale))
	if fracDigits == 0 {
		frac = 0
	}
	num := strconv.FormatInt(intPart, 10)
	if fracDigits > 0 {
		fs := strconv.FormatInt(frac, 10)
		if len(fs) < fracDigits {
			fs = strings.Repeat("0", fracDigits-len(fs)) + fs
		}
		if len(fs) > fracDigits {
			fs = fs[len(fs)-fracDigits:]
		}
		num += "." + fs
	}
	sign := ""
	if neg {
		sign = "-"
	} else if plus {
		sign = "+"
	}
	prefix := ""
	if dollar {
		prefix = "$"
	}
	body := prefix + sign + num
	width := len(field)
	if len(body) > width {
		return "%" + body
	}
	pad := width - len(body)
	fill := " "
	if star {
		fill = "*"
	}
	return strings.Repeat(fill, pad) + body
}
