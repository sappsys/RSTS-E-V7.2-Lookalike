package rsts

import "strings"

// Job sizes are measured the way RSTS/E V7.2 measured them: in K-words of
// PDP-11 storage, where a word is two bytes. The figures below are the
// sizes BASIC-PLUS used on an 11/70, not the sizes the Go values happen to
// occupy on the host, so SYSTAT reports what a V7.2 operator would expect.
const (
	wordBytes  = 2
	kWordBytes = 1024 * wordBytes

	intBytes    = 2 // integer %: one word
	floatBytes  = 4 // floating: two words, FP11 single precision
	strDescData = 4 // string descriptor: length word + pointer word
	arrayHdr    = 8 // array header: element count and bounds

	// One buffer per open channel. A virtual array or a RECORDSIZE file
	// uses its own record buffer instead of the standard 512 bytes.
	fileBufBytes = 512

	// Job control block, channel table, expression and GOSUB stacks. Every
	// job carries this whether or not a program is loaded.
	jobBaseBytes = 1536

	// A job that has nothing loaded still occupies this much. RSTS never
	// showed a job smaller than 2K.
	minJobKW = 2

	// Resident sizes for the parts of the system that are not per-job.
	// The BASIC-PLUS RTS is reentrant: one copy serves every job.
	MonitorKW = 96
	RTSKW     = 16
)

func varBytes(name string, v value) int {
	switch {
	case strings.HasSuffix(name, "$"):
		return strDescData + len(v.str)
	case strings.HasSuffix(name, "%"):
		return intBytes
	default:
		return floatBytes
	}
}

func elementBytes(name string) int {
	switch {
	case strings.HasSuffix(name, "$"):
		return strDescData
	case strings.HasSuffix(name, "%"):
		return intBytes
	default:
		return floatBytes
	}
}

// MemoryBytes is the job's working storage: the program, its variables and
// arrays, the string pool, and one buffer per open file.
func (m *Machine) MemoryBytes() int {
	if m == nil {
		return jobBaseBytes
	}
	total := jobBaseBytes

	// Source text is held tokenised, plus a line number and a link word.
	for num, text := range m.Program {
		_ = num
		total += len(text) + 2*wordBytes
	}
	if m.Compiled && m.Image != nil {
		total += m.imageBytes(m.Image)
	}

	for name, v := range m.vars {
		total += varBytes(name, v)
	}

	for name, a := range m.arrays {
		if a == nil {
			continue
		}
		total += arrayHdr
		if a.virtChan != 0 {
			// A virtual array lives in the file, not in the job.
			continue
		}
		total += len(a.data) * elementBytes(name)
		if strings.HasSuffix(name, "$") {
			for _, v := range a.data {
				total += len(v.str)
			}
		}
	}

	for _, area := range m.maps {
		if area != nil {
			total += len(area.buf)
		}
	}

	for _, f := range m.Files {
		if f == nil {
			continue
		}
		switch {
		case len(f.buf) > 0:
			total += len(f.buf)
		case f.recSize > 0:
			total += f.recSize
		default:
			total += fileBufBytes
		}
	}

	for _, d := range m.data {
		total += floatBytes
		if d.isStr {
			total += len(d.str)
		}
	}

	for name, fn := range m.functions {
		total += len(name) + 2*wordBytes
		for _, p := range fn.params {
			total += len(p) + wordBytes
		}
	}

	total += len(m.gosub) * wordBytes
	total += len(m.forStack) * (floatBytes * 3)
	return total
}

func (m *Machine) imageBytes(img *pcodeImage) int {
	n := len(img.Code)
	for _, s := range img.Strings {
		n += len(s) + wordBytes
	}
	n += len(img.Nums) * floatBytes
	n += len(img.Lines) * 2 * wordBytes
	return n
}

// SizeKW is the job size SYSTAT prints, in K-words, rounded up the way a
// memory allocator hands out whole blocks.
func (m *Machine) SizeKW() int {
	kw := (m.MemoryBytes() + kWordBytes - 1) / kWordBytes
	if kw < minJobKW {
		kw = minJobKW
	}
	return kw
}
