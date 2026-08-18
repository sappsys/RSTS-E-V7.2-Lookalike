package rsts

import (
	"math"
	"math/big"
	"strings"
)

// BASIC-PLUS string arithmetic. A number held as characters can be as
// long as you like and is exact, which is why business programs of the
// period kept money in strings and added it with SUM$ rather than
// trusting a two-word float.
//
//	SUM$(a$,b$)   DIF$(a$,b$)   PROD$(a$,b$)   QUO$(a$,b$)
//	COMP%(a$,b$)  PLACE$(a$,n%)
//
// A value is carried here as an integer and a scale, so 12.34 is 1234
// with a scale of 2 and nothing is ever rounded except where the
// operation says so.

// quoDigits is how far QUO$ carries a division that does not come out
// exactly.
const quoDigits = 30

type decimal struct {
	mant  *big.Int // signed
	scale int      // digits after the point
}

func parseDecimal(s string) (decimal, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return decimal{mant: big.NewInt(0)}, nil
	}
	neg := false
	switch t[0] {
	case '+':
		t = t[1:]
	case '-':
		neg = true
		t = t[1:]
	}
	intPart, frac, hasPoint := strings.Cut(t, ".")
	if hasPoint && strings.Contains(frac, ".") {
		return decimal{}, basicErr("Illegal number")
	}
	digits := intPart + frac
	if digits == "" {
		return decimal{}, basicErr("Illegal number")
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return decimal{}, basicErr("Illegal number")
		}
	}
	m, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return decimal{}, basicErr("Illegal number")
	}
	if neg {
		m.Neg(m)
	}
	return decimal{mant: m, scale: len(frac)}, nil
}

// rescale returns the mantissa as it would be at the given scale, which
// must not be smaller than the one it has.
func (d decimal) rescale(scale int) *big.Int {
	if scale == d.scale {
		return new(big.Int).Set(d.mant)
	}
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale-d.scale)), nil)
	return new(big.Int).Mul(d.mant, pow)
}

func (d decimal) String() string {
	m := new(big.Int).Abs(d.mant)
	digits := m.String()
	sign := ""
	if d.mant.Sign() < 0 {
		sign = "-"
	}
	if d.scale <= 0 {
		return sign + digits
	}
	for len(digits) <= d.scale {
		digits = "0" + digits
	}
	cut := len(digits) - d.scale
	return sign + digits[:cut] + "." + digits[cut:]
}

func align(a, b decimal) (*big.Int, *big.Int, int) {
	scale := a.scale
	if b.scale > scale {
		scale = b.scale
	}
	return a.rescale(scale), b.rescale(scale), scale
}

func strSum(a, b string) (string, error) {
	x, err := parseDecimal(a)
	if err != nil {
		return "", err
	}
	y, err := parseDecimal(b)
	if err != nil {
		return "", err
	}
	xm, ym, scale := align(x, y)
	return decimal{mant: new(big.Int).Add(xm, ym), scale: scale}.String(), nil
}

func strDif(a, b string) (string, error) {
	x, err := parseDecimal(a)
	if err != nil {
		return "", err
	}
	y, err := parseDecimal(b)
	if err != nil {
		return "", err
	}
	xm, ym, scale := align(x, y)
	return decimal{mant: new(big.Int).Sub(xm, ym), scale: scale}.String(), nil
}

func strProd(a, b string) (string, error) {
	x, err := parseDecimal(a)
	if err != nil {
		return "", err
	}
	y, err := parseDecimal(b)
	if err != nil {
		return "", err
	}
	return decimal{
		mant:  new(big.Int).Mul(x.mant, y.mant),
		scale: x.scale + y.scale,
	}.String(), nil
}

func strQuo(a, b string) (string, error) {
	x, err := parseDecimal(a)
	if err != nil {
		return "", err
	}
	y, err := parseDecimal(b)
	if err != nil {
		return "", err
	}
	if y.mant.Sign() == 0 {
		return "", basicErr("Division by 0")
	}
	// Carry the division far enough to be useful, then drop the trailing
	// zeros an exact result leaves behind.
	scale := quoDigits
	num := new(big.Int).Mul(x.rescale(x.scale), new(big.Int).Exp(
		big.NewInt(10), big.NewInt(int64(scale+y.scale)), nil))
	q := new(big.Int).Quo(num, y.mant)
	out := decimal{mant: q, scale: scale + x.scale}
	return trimDecimal(out).String(), nil
}

// trimDecimal drops trailing fractional zeros without changing the value.
func trimDecimal(d decimal) decimal {
	ten := big.NewInt(10)
	m := new(big.Int).Set(d.mant)
	scale := d.scale
	rem := new(big.Int)
	for scale > 0 {
		q, r := new(big.Int).QuoRem(m, ten, rem)
		if r.Sign() != 0 {
			break
		}
		m = q
		scale--
	}
	return decimal{mant: m, scale: scale}
}

func strComp(a, b string) (float64, error) {
	x, err := parseDecimal(a)
	if err != nil {
		return 0, err
	}
	y, err := parseDecimal(b)
	if err != nil {
		return 0, err
	}
	xm, ym, _ := align(x, y)
	return float64(xm.Cmp(ym)), nil
}

// strPlace rounds at a given place: a positive count is digits after the
// point, a negative one rounds to tens, hundreds and so on.
func strPlace(a string, at int) (string, error) {
	x, err := parseDecimal(a)
	if err != nil {
		return "", err
	}
	if at >= x.scale && at >= 0 {
		return x.String(), nil
	}
	drop := x.scale - at
	if drop <= 0 {
		return x.String(), nil
	}
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(drop)), nil)
	half := new(big.Int).Quo(pow, big.NewInt(2))
	m := new(big.Int).Set(x.mant)
	neg := m.Sign() < 0
	m.Abs(m)
	// Round half away from zero, the way a clerk would.
	m.Add(m, half)
	m.Quo(m, pow)
	if neg {
		m.Neg(m)
	}
	scale := at
	if scale < 0 {
		m.Mul(m, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-at)), nil))
		scale = 0
	}
	return decimal{mant: m, scale: scale}.String(), nil
}

// Radix-50 packs three characters into one 16-bit word. RSTS used it for
// file names in directory entries, and RAD$ unpacks a word back into the
// characters.
const rad50Chars = " ABCDEFGHIJKLMNOPQRSTUVWXYZ$.?0123456789"

func rad50String(word float64) string {
	n := int(math.Round(word))
	if n < 0 {
		n += 1 << 16
	}
	n &= 0xFFFF
	out := []byte{' ', ' ', ' '}
	for i := 2; i >= 0; i-- {
		out[i] = rad50Chars[n%40]
		n /= 40
	}
	return string(out)
}
