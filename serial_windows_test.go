//go:build windows

package rsts

import "testing"

func TestCOMNamePrefix(t *testing.T) {
	cases := map[string]string{
		"COM1":     `\\.\COM1`,
		"COM10":    `\\.\COM10`,
		`\\.\COM3`: `\\.\COM3`,
		`\\?\COM3`: `\\?\COM3`,
	}
	for in, want := range cases {
		if got := comName(in); got != want {
			t.Errorf("comName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The flag bits stand in for a C bitfield, so a typo in one would quietly
// set the wrong control line. Check the shape of them.
func TestDCBFlagBits(t *testing.T) {
	if dcbDtrEnable&dcbDtrMask != dcbDtrEnable {
		t.Error("the DTR enable value is outside its own mask")
	}
	if dcbRtsEnable&dcbRtsMask != dcbRtsEnable {
		t.Error("the RTS enable value is outside its own mask")
	}
	if dcbDtrMask&dcbRtsMask != 0 {
		t.Error("the DTR and RTS fields overlap")
	}
	// Everything we clear for 9600 8N1 with no flow control, and
	// everything we then set, must not collide.
	clear := dcbParityCheck | dcbOutxCtsFlow | dcbOutxDsrFlow |
		dcbDsrSensitivity | dcbOutX | dcbInX | dcbErrorChar | dcbNull |
		dcbAbortOnError | dcbDtrMask | dcbRtsMask
	set := dcbBinary | dcbDtrEnable | dcbRtsEnable
	if clear&dcbBinary != 0 {
		t.Error("binary mode would be cleared, and Windows requires it")
	}
	if got := (^clear)&set | (set &^ clear); got != set {
		t.Errorf("the set and cleared bits do not compose: %#x", got)
	}
}
