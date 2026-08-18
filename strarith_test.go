package rsts

import (
	"strings"
	"testing"
)

func TestStringArithmeticIsExact(t *testing.T) {
	cases := []struct{ expr, want string }{
		{`SUM$("0.10","0.20")`, "0.30"},
		{`SUM$("-1.5","0.25")`, "-1.25"},
		{`DIF$("1000000000000000000000","1")`, "999999999999999999999"},
		{`DIF$("1","2")`, "-1"},
		{`PROD$("1.10","3")`, "3.30"},
		{`PROD$("-2","2.5")`, "-5.0"},
		{`QUO$("10","4")`, "2.5"},
		{`QUO$("9","3")`, "3"},
		{`PLACE$("3.14159",2)`, "3.14"},
		{`PLACE$("3.145",2)`, "3.15"},
		{`PLACE$("-3.145",2)`, "-3.15"},
		{`PLACE$("1234",-2)`, "1200"},
		{`PLACE$("1250",-2)`, "1300"},
	}
	for _, c := range cases {
		got := strings.TrimSpace(immediate(t, "PRINT "+c.expr))
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.expr, got, c.want)
		}
	}
}

// The point of string arithmetic is that money does not drift.
func TestStringArithmeticBeatsFloat(t *testing.T) {
	out := strings.TrimSpace(runProgram(t, `10 T$ = "0"
20 FOR I = 1 TO 10
30   T$ = SUM$(T$, "0.10")
40 NEXT I
50 PRINT T$
60 END
`))
	if out != "1.00" {
		t.Fatalf("ten dimes came to %q", out)
	}
}

func TestStringCompare(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{`COMP%("2.50","2.5")`, "0"},
		{`COMP%("2.51","2.5")`, "1"},
		{`COMP%("2.49","2.5")`, "-1"},
		{`COMP%("-3","2")`, "-1"},
	}
	for _, c := range cases {
		got := strings.TrimSpace(immediate(t, "PRINT "+c.expr))
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.expr, got, c.want)
		}
	}
}

func TestQuoRepeating(t *testing.T) {
	got := strings.TrimSpace(immediate(t, `PRINT QUO$("1","3")`))
	if !strings.HasPrefix(got, "0.3333333333") {
		t.Fatalf("1/3 = %q", got)
	}
	if len(got) < 20 {
		t.Fatalf("1/3 was carried to only %d characters: %q", len(got), got)
	}
}

func TestStringArithmeticRejectsRubbish(t *testing.T) {
	c := &capture{}
	m := NewMachine(IO{Write: c.write, Read: c.read})
	if err := m.ExecImmediate(`PRINT SUM$("12","ABC")`); err == nil {
		t.Fatal("letters are not a number")
	}
	if err := m.ExecImmediate(`PRINT QUO$("1","0")`); err == nil {
		t.Fatal("dividing by zero should be an error")
	}
}

func TestRad50(t *testing.T) {
	cases := map[float64]string{
		0:     "   ",
		1:     "  A",
		2:     "  B",
		40:    " A ",
		1600:  "A  ",
		1641:  "AAA",
		28844: "RAD", // ((18*40)+1)*40+4
	}
	for word, want := range cases {
		if got := rad50String(word); got != want {
			t.Errorf("RAD$(%v) = %q, want %q", word, got, want)
		}
	}
	// The word is sixteen bits, so a negative comes back as its unsigned
	// twin rather than being rejected.
	if got := rad50String(-1); len(got) != 3 {
		t.Errorf("RAD$(-1) = %q", got)
	}
}

func TestRad50FromBasic(t *testing.T) {
	got := strings.TrimSpace(immediate(t, `PRINT RAD$(1641)`))
	if got != "AAA" {
		t.Fatalf("RAD$(1641) = %q", got)
	}
}
