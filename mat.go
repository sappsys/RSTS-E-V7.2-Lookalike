package rsts

import (
	"strings"
)

func (m *Machine) doMat(s stmt) error {
	switch s.matKind {
	case "READ":
		for _, name := range s.matNames {
			if err := m.matRead(name); err != nil {
				return err
			}
		}
		return nil
	case "PRINT":
		return m.matPrint(s)
	case "INPUT":
		for _, name := range s.matNames {
			if err := m.matInput(name); err != nil {
				return err
			}
		}
		return nil
	case "ZER", "CON", "IDN":
		if len(s.matBounds) > 0 {
			bounds := make([]int, len(s.matBounds))
			for i, b := range s.matBounds {
				n, err := m.evalNum(b)
				if err != nil {
					return err
				}
				bounds[i] = int(n)
			}
			if s.matKind == "IDN" && len(bounds) == 1 {
				bounds = []int{bounds[0], bounds[0]}
			}
			if err := m.dimArray(s.matDest, bounds); err != nil {
				return err
			}
		}
		return m.matFill(s.matDest, s.matKind)
	case "COPY":
		return m.matCopy(s.matDest, s.matLeft)
	case "ADD", "SUB":
		return m.matAddSub(s.matDest, s.matLeft, s.matRight, s.matKind == "ADD")
	case "MUL":
		return m.matMul(s.matDest, s.matLeft, s.matRight)
	case "SCALE":
		k, err := m.evalNum(s.expr)
		if err != nil {
			return err
		}
		return m.matScale(s.matDest, s.matLeft, k)
	case "TRN":
		return m.matTrn(s.matDest, s.matLeft)
	case "INV":
		return m.matInv(s.matDest, s.matLeft)
	default:
		return m.err("Syntax error")
	}
}

func (m *Machine) matInfo(name string) (*arrayInfo, error) {
	if name == "" {
		return nil, m.err("Syntax error")
	}
	info := m.arrays[name]
	if info == nil {
		if err := m.dimArray(name, []int{10, 10}); err != nil {
			return nil, err
		}
		info = m.arrays[name]
	}
	if len(info.dims) < 1 || len(info.dims) > 2 {
		return nil, m.err("Subscript out of range")
	}
	return info, nil
}

func (m *Machine) matSize(info *arrayInfo) (rows, cols int) {
	if len(info.dims) == 1 {
		return 1, info.dims[0]
	}
	return info.dims[0], info.dims[1]
}

func (m *Machine) matGet(name string, r, c int, twoD bool) (value, error) {
	if twoD {
		return m.getArray(name, []int{r, c})
	}
	return m.getArray(name, []int{c})
}

func (m *Machine) matSet(name string, r, c int, twoD bool, v value) error {
	if twoD {
		return m.setArray(name, []int{r, c}, v)
	}
	return m.setArray(name, []int{c}, v)
}

func (m *Machine) matRead(name string) error {
	info, err := m.matInfo(name)
	if err != nil {
		return err
	}
	rows, cols := m.matSize(info)
	twoD := len(info.dims) == 2
	for i := 1; i <= rows; i++ {
		for j := 1; j <= cols; j++ {
			if m.dataPtr >= len(m.data) {
				return m.err("Out of data")
			}
			v := m.data[m.dataPtr]
			m.dataPtr++
			cv, err := m.coerceVar(name, v)
			if err != nil {
				return err
			}
			rr, cc := i, j
			if !twoD {
				rr, cc = 1, (i-1)*cols+j
			}
			if err := m.matSet(name, rr, cc, twoD, cv); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Machine) matPrint(s stmt) error {
	packed := s.trailing == "NONE"
	for mi, name := range s.matNames {
		if mi > 0 {
			if m.IO.Write != nil {
				m.IO.Write("", true)
			}
		}
		info, err := m.matInfo(name)
		if err != nil {
			return err
		}
		rows, cols := m.matSize(info)
		twoD := len(info.dims) == 2
		for i := 1; i <= rows; i++ {
			var parts []string
			col := 0
			for j := 1; j <= cols; j++ {
				rr, cc := i, j
				if !twoD {
					rr, cc = 1, (i-1)*cols+j
				}
				v, err := m.matGet(name, rr, cc, twoD)
				if err != nil {
					return err
				}
				text := m.strVal(v)
				if !v.isStr && !strings.HasPrefix(text, "-") && !strings.HasPrefix(text, " ") {
					text = " " + text
				}
				if packed {
					parts = append(parts, text)
					col += len(text)
				} else {
					width := 14 - (col % 14)
					if width == 0 {
						width = 14
					}
					if j > 1 {
						parts = append(parts, strings.Repeat(" ", width))
						col += width
					}
					parts = append(parts, text)
					col += len(text)
				}
			}
			line := strings.Join(parts, "")
			if m.IO.Write != nil {
				m.IO.Write(line, true)
			}
		}
	}
	return nil
}

func (m *Machine) matInput(name string) error {
	info, err := m.matInfo(name)
	if err != nil {
		return err
	}
	rows, cols := m.matSize(info)
	twoD := len(info.dims) == 2
	need := rows * cols
	var vals []string
	for len(vals) < need {
		raw, err := m.readInput("? ")
		if err != nil {
			return err
		}
		vals = append(vals, splitInput(raw)...)
	}
	n := 0
	for i := 1; i <= rows; i++ {
		for j := 1; j <= cols; j++ {
			cv, err := m.coerceInput(&varRef{name: name}, value{isStr: true, str: vals[n]})
			if err != nil {
				return err
			}
			n++
			rr, cc := i, j
			if !twoD {
				rr, cc = 1, (i-1)*cols+j
			}
			if err := m.matSet(name, rr, cc, twoD, cv); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Machine) matFill(dest, kind string) error {
	info, err := m.matInfo(dest)
	if err != nil {
		return err
	}
	rows, cols := m.matSize(info)
	twoD := len(info.dims) == 2
	for i := 1; i <= rows; i++ {
		for j := 1; j <= cols; j++ {
			rr, cc := i, j
			if !twoD {
				rr, cc = 1, (i-1)*cols+j
			}
			var v value
			switch kind {
			case "ZER":
				v = numValue(0)
			case "CON":
				v = numValue(1)
			case "IDN":
				if twoD && i == j {
					v = numValue(1)
				} else {
					v = numValue(0)
				}
			}
			if err := m.matSet(dest, rr, cc, twoD, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Machine) matCopy(dest, src string) error {
	si, err := m.matInfo(src)
	if err != nil {
		return err
	}
	if err := m.matEnsureDest(dest, si); err != nil {
		return err
	}
	rows, cols := m.matSize(si)
	twoD := len(si.dims) == 2
	di := m.arrays[dest]
	d2 := len(di.dims) == 2
	for i := 1; i <= rows; i++ {
		for j := 1; j <= cols; j++ {
			rr, cc := i, j
			if !twoD {
				rr, cc = 1, (i-1)*cols+j
			}
			v, err := m.matGet(src, rr, cc, twoD)
			if err != nil {
				return err
			}
			dr, dc := i, j
			if !d2 {
				dr, dc = 1, (i-1)*cols+j
			}
			if err := m.matSet(dest, dr, dc, d2, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Machine) matEnsureDest(dest string, src *arrayInfo) error {
	if dest == "" {
		return m.err("Syntax error")
	}
	if m.arrays[dest] == nil {
		return m.dimArray(dest, append([]int(nil), src.dims...))
	}
	return nil
}

func (m *Machine) matAddSub(dest, left, right string, add bool) error {
	li, err := m.matInfo(left)
	if err != nil {
		return err
	}
	ri, err := m.matInfo(right)
	if err != nil {
		return err
	}
	lr, lc := m.matSize(li)
	rr, rc := m.matSize(ri)
	if lr != rr || lc != rc {
		return m.err("Incompatible dimensions")
	}
	if err := m.matEnsureDest(dest, li); err != nil {
		return err
	}
	l2, r2 := len(li.dims) == 2, len(ri.dims) == 2
	d2 := len(m.arrays[dest].dims) == 2
	for i := 1; i <= lr; i++ {
		for j := 1; j <= lc; j++ {
			a, err := m.matGet(left, i, j, l2)
			if err != nil {
				return err
			}
			b, err := m.matGet(right, i, j, r2)
			if err != nil {
				return err
			}
			an, err := m.numVal(a)
			if err != nil {
				return err
			}
			bn, err := m.numVal(b)
			if err != nil {
				return err
			}
			v := an + bn
			if !add {
				v = an - bn
			}
			if err := m.matSet(dest, i, j, d2, numValue(v)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Machine) matMul(dest, left, right string) error {
	li, err := m.matInfo(left)
	if err != nil {
		return err
	}
	ri, err := m.matInfo(right)
	if err != nil {
		return err
	}
	ar, ac := m.matSize(li)
	br, bc := m.matSize(ri)
	if ac != br {
		return m.err("Incompatible dimensions")
	}
	if m.arrays[dest] == nil {
		if err := m.dimArray(dest, []int{ar, bc}); err != nil {
			return err
		}
	}
	l2, r2 := len(li.dims) == 2, len(ri.dims) == 2
	tmp := make([][]float64, ar)
	for i := 0; i < ar; i++ {
		tmp[i] = make([]float64, bc)
		for j := 0; j < bc; j++ {
			sum := 0.0
			for k := 1; k <= ac; k++ {
				av, err := m.matGet(left, i+1, k, l2)
				if err != nil {
					return err
				}
				bv, err := m.matGet(right, k, j+1, r2)
				if err != nil {
					return err
				}
				an, err := m.numVal(av)
				if err != nil {
					return err
				}
				bn, err := m.numVal(bv)
				if err != nil {
					return err
				}
				sum += an * bn
			}
			tmp[i][j] = sum
		}
	}
	d2 := len(m.arrays[dest].dims) == 2
	for i := 1; i <= ar; i++ {
		for j := 1; j <= bc; j++ {
			if err := m.matSet(dest, i, j, d2, numValue(tmp[i-1][j-1])); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Machine) matScale(dest, src string, k float64) error {
	si, err := m.matInfo(src)
	if err != nil {
		return err
	}
	if err := m.matEnsureDest(dest, si); err != nil {
		return err
	}
	rows, cols := m.matSize(si)
	s2 := len(si.dims) == 2
	d2 := len(m.arrays[dest].dims) == 2
	for i := 1; i <= rows; i++ {
		for j := 1; j <= cols; j++ {
			v, err := m.matGet(src, i, j, s2)
			if err != nil {
				return err
			}
			n, err := m.numVal(v)
			if err != nil {
				return err
			}
			if err := m.matSet(dest, i, j, d2, numValue(n*k)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Machine) matTrn(dest, src string) error {
	si, err := m.matInfo(src)
	if err != nil {
		return err
	}
	rows, cols := m.matSize(si)
	if m.arrays[dest] == nil {
		if err := m.dimArray(dest, []int{cols, rows}); err != nil {
			return err
		}
	}
	s2 := len(si.dims) == 2
	tmp := make([][]value, cols)
	for j := 0; j < cols; j++ {
		tmp[j] = make([]value, rows)
		for i := 0; i < rows; i++ {
			v, err := m.matGet(src, i+1, j+1, s2)
			if err != nil {
				return err
			}
			tmp[j][i] = v
		}
	}
	d2 := len(m.arrays[dest].dims) == 2
	for i := 1; i <= cols; i++ {
		for j := 1; j <= rows; j++ {
			if err := m.matSet(dest, i, j, d2, tmp[i-1][j-1]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Machine) matInv(dest, src string) error {
	si, err := m.matInfo(src)
	if err != nil {
		return err
	}
	n, cols := m.matSize(si)
	if n != cols || len(si.dims) != 2 {
		return m.err("Can't invert matrix")
	}
	a := make([][]float64, n)
	for i := 0; i < n; i++ {
		a[i] = make([]float64, 2*n)
		for j := 0; j < n; j++ {
			v, err := m.matGet(src, i+1, j+1, true)
			if err != nil {
				return err
			}
			x, err := m.numVal(v)
			if err != nil {
				return err
			}
			a[i][j] = x
		}
		a[i][n+i] = 1
	}
	for col := 0; col < n; col++ {
		pivot := col
		best := absFloat(a[col][col])
		for r := col + 1; r < n; r++ {
			if absFloat(a[r][col]) > best {
				best = absFloat(a[r][col])
				pivot = r
			}
		}
		if best < 1e-12 {
			return m.err("Can't invert matrix")
		}
		a[col], a[pivot] = a[pivot], a[col]
		div := a[col][col]
		for j := 0; j < 2*n; j++ {
			a[col][j] /= div
		}
		for r := 0; r < n; r++ {
			if r == col {
				continue
			}
			f := a[r][col]
			for j := 0; j < 2*n; j++ {
				a[r][j] -= f * a[col][j]
			}
		}
	}
	if err := m.matEnsureDest(dest, si); err != nil {
		return err
	}
	d2 := len(m.arrays[dest].dims) == 2
	for i := 1; i <= n; i++ {
		for j := 1; j <= n; j++ {
			if err := m.matSet(dest, i, j, d2, numValue(a[i-1][n+j-1])); err != nil {
				return err
			}
		}
	}
	return nil
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
