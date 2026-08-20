package rsts

import (
	"strings"
	"testing"
)

func runPascal(t *testing.T, src string, inputs ...string) string {
	t.Helper()
	c := &capture{inputs: inputs}
	err := RunPascal(src, pascalHostFromIO(IO{Write: c.write, Read: c.read}))
	if err != nil {
		t.Fatalf("pascal: %v\n%s", err, src)
	}
	return c.out.String()
}

func wantPascal(t *testing.T, src, want string, inputs ...string) {
	t.Helper()
	got := strings.TrimSpace(runPascal(t, src, inputs...))
	want = strings.TrimSpace(want)
	if got != want {
		t.Fatalf("got %q want %q\n%s", got, want, src)
	}
}

func wrapPas(body string) string {
	return "PROGRAM T(INPUT, OUTPUT);\n" + body + "\n."
}

func TestPascalHello(t *testing.T) {
	wantPascal(t, samples["100,100"]["HELLO.PAS"], "HELLO FROM PASCAL")
}

func TestPascalFact(t *testing.T) {
	wantPascal(t, samples["100,100"]["FACT.PAS"], "720")
}

func TestPascalNestedProc(t *testing.T) {
	src := wrapPas(`
PROCEDURE OUTER;
  VAR X: INTEGER;
  PROCEDURE INNER;
  BEGIN
    WRITELN(X)
  END;
BEGIN
  X := 7;
  INNER
END;
BEGIN
  OUTER
END`)
	wantPascal(t, src, "7")
}

func TestPascalVarParam(t *testing.T) {
	src := wrapPas(`
VAR A, B: INTEGER;
PROCEDURE SWAP(VAR X, Y: INTEGER);
VAR T: INTEGER;
BEGIN
  T := X; X := Y; Y := T
END;
BEGIN
  A := 1; B := 2;
  SWAP(A, B);
  WRITELN(A, ' ', B)
END`)
	wantPascal(t, src, "2 1")
}

func TestPascalRecordWith(t *testing.T) {
	src := wrapPas(`
TYPE REC = RECORD X, Y: INTEGER END;
VAR R: REC;
BEGIN
  WITH R DO
  BEGIN
    X := 3;
    Y := 4
  END;
  WRITELN(R.X, ' ', R.Y)
END`)
	wantPascal(t, src, "3 4")
}

func TestPascalVariant(t *testing.T) {
	src := wrapPas(`
TYPE REC = RECORD
  CASE B: BOOLEAN OF
    TRUE: (X: INTEGER);
    FALSE: (C: CHAR)
END;
VAR R: REC;
BEGIN
  R.B := TRUE;
  R.X := 9;
  WRITELN(R.X)
END`)
	wantPascal(t, src, "9")
}

func TestPascalSets(t *testing.T) {
	src := wrapPas(`
VAR S: SET OF 1..10;
BEGIN
  S := [1, 3..5];
  IF 4 IN S THEN WRITE('Y') ELSE WRITE('N');
  IF 2 IN S THEN WRITE('Y') ELSE WRITE('N');
  S := S + [2];
  IF 2 IN S THEN WRITELN('Y') ELSE WRITELN('N')
END`)
	wantPascal(t, src, "YNY")
}

func TestPascalPointer(t *testing.T) {
	src := wrapPas(`
TYPE PINT = ^NODE;
     NODE = RECORD V: INTEGER; NXT: PINT END;
VAR P: PINT;
BEGIN
  NEW(P);
  P^.V := 11;
  WRITELN(P^.V);
  DISPOSE(P)
END`)
	wantPascal(t, src, "11")
}

func TestPascalCaseOtherwise(t *testing.T) {
	src := wrapPas(`
VAR I: INTEGER;
BEGIN
  I := 3;
  CASE I OF
    1: WRITELN('A');
    2: WRITELN('B')
    OTHERWISE WRITELN('C')
  END
END`)
	wantPascal(t, src, "C")
}

func TestPascalLoops(t *testing.T) {
	src := wrapPas(`
VAR I, N: INTEGER;
BEGIN
  N := 0;
  FOR I := 1 TO 4 DO N := N + I;
  I := 0;
  WHILE I < 3 DO
  BEGIN
    N := N + 1;
    I := I + 1
  END;
  REPEAT
    N := N + 1
  UNTIL N >= 15;
  WRITELN(N)
END`)
	wantPascal(t, src, "15")
}

func TestPascalPackedString(t *testing.T) {
	src := wrapPas(`
VAR S: PACKED ARRAY[1..5] OF CHAR;
BEGIN
  S := 'HI';
  S[3] := 'Z';
  WRITELN(S)
END`)
	wantPascal(t, src, "HIZ")
}

func TestPascalWriteFormat(t *testing.T) {
	src := wrapPas(`
BEGIN
  WRITELN(12:4, 3.5:6:2)
END`)
	wantPascal(t, src, "  12  3.50")
}

func TestPascalGoto(t *testing.T) {
	src := wrapPas(`
LABEL 99;
BEGIN
  GOTO 99;
  WRITELN('NO');
99:
  WRITELN('YES')
END`)
	wantPascal(t, src, "YES")
}

func TestPascalEnumSubrangeArray(t *testing.T) {
	src := wrapPas(`
TYPE COLOR = (RED, GREEN, BLUE);
VAR C: COLOR;
    I: 1..10;
    A: ARRAY[1..3] OF INTEGER;
BEGIN
  C := GREEN;
  I := 8;
  A[1] := 4;
  A[2] := 5;
  A[3] := A[1] + A[2];
  WRITELN(ORD(C), ' ', I, ' ', A[3], ' ', C)
END`)
	wantPascal(t, src, "1 8 9 GREEN")
}

func TestPascalStdFuncs(t *testing.T) {
	src := wrapPas(`
BEGIN
  WRITELN(ABS(-3), ' ', SQR(4), ' ', TRUNC(3.9), ' ', ORD('A'), ' ', CHR(66), ' ', SUCC(1), ' ', PRED(3), ' ', ODD(3))
END`)
	wantPascal(t, src, "3 16 3 65 B 2 2 TRUE")
}

func TestPascalForward(t *testing.T) {
	src := wrapPas(`
PROCEDURE P(N: INTEGER); FORWARD;
PROCEDURE P(N: INTEGER);
BEGIN
  WRITELN(N)
END;
BEGIN
  P(7)
END`)
	wantPascal(t, src, "7")
}

func TestPascalRead(t *testing.T) {
	src := wrapPas(`
VAR N: INTEGER;
    S: PACKED ARRAY[1..8] OF CHAR;
BEGIN
  READLN(N);
  READLN(S);
  WRITELN(N, ' ', S)
END`)
	wantPascal(t, src, "42 HELLO", "42", "HELLO")
}

func TestPascalCommentsAndDiv(t *testing.T) {
	src := wrapPas(`
{ brace comment }
(* star comment *)
BEGIN
  WRITELN(7 DIV 2, ' ', 7 MOD 2, ' ', 1 + 2 * 3)
END`)
	wantPascal(t, src, "3 1 7")
}

func TestPascalBoolean(t *testing.T) {
	src := wrapPas(`
VAR B: BOOLEAN;
BEGIN
  B := TRUE AND NOT FALSE;
  IF B OR FALSE THEN WRITELN('OK')
END`)
	wantPascal(t, src, "OK")
}

func TestPascalConstType(t *testing.T) {
	src := wrapPas(`
CONST N = 3;
TYPE VEC = ARRAY[1..N] OF INTEGER;
VAR A: VEC;
    I: INTEGER;
BEGIN
  FOR I := 1 TO N DO A[I] := I * I;
  WRITELN(A[1], ' ', A[2], ' ', A[3])
END`)
	wantPascal(t, src, "1 4 9")
}

func TestPascalCompileError(t *testing.T) {
	_, err := CompilePascal("PROGRAM X; BEGIN FOO END.")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Pascal") {
		t.Fatalf("error %v", err)
	}
}

func TestPascalSamplesSeeded(t *testing.T) {
	sh, _ := guestShell(t)
	for _, name := range []string{
		"HELLO.PAS", "FACT.PAS",
		"BAGELS.PAS", "HAMURA.PAS", "HUNT.PAS", "MAZE.PAS", "CHOMP.PAS", "CRAPS.PAS",
	} {
		spec, err := ParseFileSpec(name, "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := sh.Disk.ReadText(spec, 100, 100, false)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != samples["100,100"][name] {
			t.Errorf("%s on disk does not match the sample", name)
		}
	}
}

func TestPascalShellCommands(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("COMPILE HELLO.PAS")
	if !strings.Contains(out.String(), "HELLO compiled") {
		t.Fatalf("compile: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("RUN HELLO.PAS")
	if !strings.Contains(out.String(), "HELLO FROM PASCAL") {
		t.Fatalf("run hello.pas: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("RUN FACT.PAS")
	if !strings.Contains(out.String(), "720") {
		t.Fatalf("run fact.pas: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("RUNNH FACT")
	got := strings.TrimSpace(out.String())
	if got != "720" {
		t.Fatalf("runnh fact: %q", got)
	}
}

func TestPascalWorkspace(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("NEW DEMO.PAS")
	if !sh.pascalOn {
		t.Fatal("NEW DEMO.PAS should start a Pascal workspace")
	}
	if sh.Basic.ProgramName != "DEMO" {
		t.Fatalf("name: %q", sh.Basic.ProgramName)
	}
	if len(sh.Basic.Program) != 0 {
		t.Fatal("BASIC program should be empty")
	}
	out.Reset()
	sh.Dispatch("10 PRINT 1")
	if !strings.Contains(out.String(), "Not a BASIC program") {
		t.Fatalf("numbered line: %q", out.String())
	}
	src := "PROGRAM T(OUTPUT); BEGIN WRITELN('HI') END.\n"
	sh.pascalSrc = src
	out.Reset()
	sh.Dispatch("LISTNH")
	if !strings.Contains(out.String(), "WRITELN('HI')") {
		t.Fatalf("list: %q", out.String())
	}
	if err := sh.cmdSave("", false); err != nil {
		t.Fatal(err)
	}
	spec, err := ParseFileSpec("DEMO.PAS", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := sh.Disk.ReadText(spec, 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Fatalf("saved PAS: %q", got)
	}
	out.Reset()
	if err := sh.cmdCompile(""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "DEMO compiled") {
		t.Fatalf("compile memory: %q", out.String())
	}
	pac, err := ParseFileSpec("DEMO.PAC", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sh.Disk.ReadText(pac, 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, pacMagic) {
		t.Fatal("COMPILE of Pascal memory should write a .PAC")
	}
	out.Reset()
	sh.Dispatch("RUNNH")
	if strings.TrimSpace(out.String()) != "HI" {
		t.Fatalf("run memory: %q", out.String())
	}

	sh.Dispatch("NEW FOO")
	if sh.pascalOn {
		t.Fatal("NEW FOO should leave Pascal")
	}

	if err := sh.cmdOld("HELLO.PAS"); err != nil {
		t.Fatal(err)
	}
	if !sh.pascalOn {
		t.Fatal("OLD HELLO.PAS should load Pascal")
	}
	if len(sh.Basic.Program) != 0 {
		t.Fatal("OLD HELLO.PAS should not load BASIC")
	}
	out.Reset()
	sh.Dispatch("RUNNH")
	if !strings.Contains(out.String(), "HELLO FROM PASCAL") {
		t.Fatalf("run old pas: %q", out.String())
	}

	if err := sh.cmdOld("HELLO"); err != nil {
		t.Fatal(err)
	}
	if sh.pascalOn {
		t.Fatal("OLD HELLO.BAS should leave Pascal")
	}
}

func TestPascalPACFile(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("COMPILE HELLO.PAS")
	if !strings.Contains(out.String(), "HELLO compiled") {
		t.Fatalf("compile: %q", out.String())
	}
	spec, err := ParseFileSpec("HELLO.PAC", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sh.Disk.ReadText(spec, 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, pacMagic) {
		t.Fatal("COMPILE should write a .PAC image")
	}
	img, err := unmarshalPAC(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := &capture{}
	if err := runPascalImage(img, pascalHostFromIO(IO{Write: c.write})); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(c.out.String()) != "HELLO FROM PASCAL" {
		t.Fatalf("PAC run: %q", c.out.String())
	}
	out.Reset()
	sh.Dispatch("RUNNH HELLO.PAC")
	if strings.TrimSpace(out.String()) != "HELLO FROM PASCAL" {
		t.Fatalf("RUN HELLO.PAC: %q", out.String())
	}
	if err := sh.cmdType("HELLO.PAC"); err == nil || !strings.Contains(err.Error(), "Compiled file") {
		t.Fatalf("TYPE PAC: %v", err)
	}
}

func TestCompilePascalThenRun(t *testing.T) {
	sh, out := guestShell(t)
	if err := sh.cmdCompile("FACT"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "FACT compiled") {
		t.Fatalf("compile fact: %q", out.String())
	}
	spec, err := ParseFileSpec("FACT.PAC", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sh.Disk.ReadText(spec, 100, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, pacMagic) {
		t.Fatal("COMPILE FACT should write FACT.PAC")
	}
	out.Reset()
	sh.Dispatch("RUNNH FACT")
	if strings.TrimSpace(out.String()) != "720" {
		t.Fatalf("run fact after compile: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("RUNNH HELLO")
	if strings.Contains(out.String(), "HELLO FROM PASCAL") {
		t.Fatalf("RUN HELLO should prefer HELLO.BAS: %q", out.String())
	}
	out.Reset()
	sh.Dispatch("RUNNH HELLO.PAS")
	if strings.TrimSpace(out.String()) != "HELLO FROM PASCAL" {
		t.Fatalf("RUN HELLO.PAS: %q", out.String())
	}
}

func TestCompilePascalPrivilege(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	if err := sh.cmdCompile("FACT.PAS<232>"); err == nil {
		t.Fatal("guest should not set privilege bit on a .PAC")
	}

	sh.cmdBye("")
	sh.Login("SYSTEM", "SYSTEM")
	src := "PROGRAM P(OUTPUT); BEGIN WRITELN('OK') END.\n"
	spec, err := ParseFileSpec("PRIV.PAS", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.Disk.WriteText(spec, 1, 2, true, src, defaultProt); err != nil {
		t.Fatal(err)
	}
	if err := sh.cmdCompile("PRIV.PAS<232>"); err != nil {
		t.Fatal(err)
	}
	prot, err := sh.Disk.Prot(mustSpec(t, "PRIV.PAC"), 1, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if prot != privCompiledProt {
		t.Fatalf("priv PAC prot: %d", prot)
	}

	sh.cmdBye("")
	sh.Login("GUEST", "GUEST")
	if err := sh.cmdType("$PRIV.PAC"); err == nil {
		t.Fatal("guest TYPE of privileged .PAC")
	}
	var out strings.Builder
	sh.out = &out
	sh.cmdRun("$PRIV.PAC", false)
	if strings.TrimSpace(out.String()) != "OK" {
		t.Fatalf("guest RUN of <232> PAC: %q", out.String())
	}
	if sh.tempPriv {
		t.Fatal("temp privilege should drop after RUN")
	}
}

func TestPascalPACRoundtrip(t *testing.T) {
	src := samples["100,100"]["FACT.PAS"]
	body, err := CompilePascalPAC(src)
	if err != nil {
		t.Fatal(err)
	}
	img, err := unmarshalPAC(body)
	if err != nil {
		t.Fatal(err)
	}
	c := &capture{}
	if err := runPascalImage(img, pascalHostFromIO(IO{Write: c.write})); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(c.out.String()) != "720" {
		t.Fatalf("got %q", c.out.String())
	}
}

func TestPascalDowntoAndArray2D(t *testing.T) {
	src := wrapPas(`
VAR I, N: INTEGER;
    A: ARRAY[1..2, 1..2] OF INTEGER;
BEGIN
  N := 0;
  FOR I := 3 DOWNTO 1 DO N := N + I;
  A[1, 1] := 9;
  A[2, 2] := A[1, 1] + 1;
  WRITELN(N, ' ', A[2, 2])
END`)
	wantPascal(t, src, "6 10")
}

func TestPascalSetOpsAndNil(t *testing.T) {
	src := wrapPas(`
TYPE PINT = ^INTEGER;
VAR S, T: SET OF 0..7;
    P: PINT;
BEGIN
  S := [1, 2, 3];
  T := [2, 3, 4];
  IF 1 IN (S - T) THEN WRITE('A');
  IF 2 IN (S * T) THEN WRITE('B');
  IF 4 IN (S + T) THEN WRITE('C');
  P := NIL;
  IF P = NIL THEN WRITELN('D')
END`)
	wantPascal(t, src, "ABCD")
}

func TestPascalConstExpr(t *testing.T) {
	src := wrapPas(`
CONST N = 2 * 3 + 1;
      C = ORD('A');
BEGIN
  WRITELN(N, ' ', C)
END`)
	wantPascal(t, src, "7 65")
}

func TestPascalCaseRange(t *testing.T) {
	src := wrapPas(`
VAR I: INTEGER;
BEGIN
  I := 2;
  CASE I OF
    1..3: WRITE('A');
    4, 5: WRITE('B')
    OTHERWISE WRITE('C')
  END;
  I := 5;
  CASE I OF
    1..3: WRITELN('A');
    4, 5: WRITELN('B')
    OTHERWISE WRITELN('C')
  END
END`)
	wantPascal(t, src, "AB")
}

func TestPascalProcParam(t *testing.T) {
	src := wrapPas(`
PROCEDURE TWICE(PROCEDURE P);
BEGIN
  P;
  P
END;
PROCEDURE HELLO;
BEGIN
  WRITE('A')
END;
BEGIN
  TWICE(HELLO);
  WRITELN
END`)
	wantPascal(t, src, "AA")
}

func TestPascalFuncParam(t *testing.T) {
	src := wrapPas(`
FUNCTION APPLY(FUNCTION F(X: INTEGER): INTEGER; N: INTEGER): INTEGER;
BEGIN
  APPLY := F(N)
END;
FUNCTION SQUARE(X: INTEGER): INTEGER;
BEGIN
  SQUARE := X * X
END;
BEGIN
  WRITELN(APPLY(SQUARE, 5))
END`)
	wantPascal(t, src, "25")
}

func TestPascalConformantArray(t *testing.T) {
	src := wrapPas(`
VAR B: ARRAY[1..4] OF INTEGER;
PROCEDURE SUM(A: ARRAY[L..H: INTEGER] OF INTEGER);
VAR I, S: INTEGER;
BEGIN
  S := 0;
  FOR I := L TO H DO S := S + A[I];
  WRITELN(S, ' ', L, ' ', H)
END;
BEGIN
  B[1] := 1; B[2] := 2; B[3] := 3; B[4] := 4;
  SUM(B)
END`)
	wantPascal(t, src, "10 1 4")
}

func TestPascalGetPut(t *testing.T) {
	src := wrapPas(`
VAR F: FILE OF INTEGER;
    N: INTEGER;
BEGIN
  REWRITE(F);
  F^ := 5;
  PUT(F);
  F^ := 7;
  PUT(F);
  RESET(F);
  N := F^;
  WRITE(N, ' ');
  GET(F);
  WRITELN(F^)
END`)
	wantPascal(t, src, "5 7")
}

func TestPascalPackUnpack(t *testing.T) {
	src := wrapPas(`
VAR A: ARRAY[1..6] OF CHAR;
    Z: PACKED ARRAY[1..3] OF CHAR;
BEGIN
  A[1] := 'A'; A[2] := 'B'; A[3] := 'C'; A[4] := 'D';
  PACK(A, 2, Z);
  WRITELN(Z)
END`)
	wantPascal(t, src, "BCD")
}

func TestPascalNewTag(t *testing.T) {
	src := wrapPas(`
TYPE REC = RECORD
  CASE B: BOOLEAN OF
    TRUE: (X: INTEGER);
    FALSE: (C: CHAR)
END;
     PREC = ^REC;
VAR P: PREC;
BEGIN
  NEW(P, TRUE);
  P^.X := 3;
  WRITELN(P^.X)
END`)
	wantPascal(t, src, "3")
}

func TestPascalParamlessFunc(t *testing.T) {
	src := wrapPas(`
FUNCTION F: INTEGER;
BEGIN
  F := 9
END;
BEGIN
  WRITELN(F)
END`)
	wantPascal(t, src, "9")
}

func TestPascalArrayAssign(t *testing.T) {
	src := wrapPas(`
VAR A, B: ARRAY[1..2] OF INTEGER;
BEGIN
  A[1] := 1; A[2] := 8;
  B := A;
  WRITELN(B[2])
END`)
	wantPascal(t, src, "8")
}

func TestPascalVarAlias(t *testing.T) {
	src := wrapPas(`
VAR A: ARRAY[1..2] OF INTEGER;
PROCEDURE INC(VAR X: INTEGER);
BEGIN
  X := X + 1
END;
BEGIN
  A[1] := 5;
  INC(A[1]);
  WRITELN(A[1])
END`)
	wantPascal(t, src, "6")
}

func TestPascalStringCompare(t *testing.T) {
	src := wrapPas(`
VAR S: PACKED ARRAY[1..4] OF CHAR;
BEGIN
  S := 'AB';
  IF S = 'AB' THEN WRITELN('Y') ELSE WRITELN('N')
END`)
	wantPascal(t, src, "Y")
}

func TestPascalHelp(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("HELP PASCAL")
	if !strings.Contains(out.String(), "COMPILE") {
		t.Fatalf("help: %q", out.String())
	}
}

// Textbook / ISO 7185 programs that a 90% compiler must run.
func TestPascalTextbook(t *testing.T) {
	cases := []struct {
		name, src, want string
		in              []string
	}{
		{"gcd", `
VAR A, B, T: INTEGER;
BEGIN
  A := 1071; B := 462;
  WHILE B <> 0 DO
  BEGIN
    T := B; B := A MOD B; A := T
  END;
  WRITELN(A)
END`, "21", nil},
		{"sieve", `
CONST N = 30;
VAR F: ARRAY[2..30] OF BOOLEAN;
    I, J: INTEGER;
BEGIN
  FOR I := 2 TO N DO F[I] := TRUE;
  I := 2;
  WHILE I * I <= N DO
  BEGIN
    IF F[I] THEN
    BEGIN
      J := I * I;
      WHILE J <= N DO
      BEGIN
        F[J] := FALSE;
        J := J + I
      END
    END;
    I := I + 1
  END;
  FOR I := 2 TO N DO
    IF F[I] THEN WRITE(I, ' ');
  WRITELN
END`, "2 3 5 7 11 13 17 19 23 29", nil},
		{"hanoi-count", `
VAR N: INTEGER;
FUNCTION MOVES(K: INTEGER): INTEGER;
BEGIN
  IF K <= 1 THEN MOVES := 1 ELSE MOVES := 2 * MOVES(K - 1) + 1
END;
BEGIN
  N := 5;
  WRITELN(MOVES(N))
END`, "31", nil},
		{"mutual-forward", `
FUNCTION ODDN(N: INTEGER): BOOLEAN; FORWARD;
FUNCTION EVENN(N: INTEGER): BOOLEAN;
BEGIN
  IF N = 0 THEN EVENN := TRUE ELSE EVENN := ODDN(N - 1)
END;
FUNCTION ODDN;
BEGIN
  IF N = 0 THEN ODDN := FALSE ELSE ODDN := EVENN(N - 1)
END;
BEGIN
  IF EVENN(4) AND NOT EVENN(5) THEN WRITELN('OK')
END`, "OK", nil},
		{"nested-static", `
PROCEDURE OUTER;
  VAR N: INTEGER;
  FUNCTION INNER(K: INTEGER): INTEGER;
  BEGIN
    INNER := N + K
  END;
BEGIN
  N := 10;
  WRITELN(INNER(3))
END;
BEGIN
  OUTER
END`, "13", nil},
		{"enum-for", `
TYPE COLOR = (RED, GREEN, BLUE);
VAR C: COLOR;
    N: INTEGER;
BEGIN
  N := 0;
  FOR C := RED TO BLUE DO N := N + ORD(C);
  WRITELN(N, ' ', SUCC(RED) = GREEN)
END`, "3 TRUE", nil},
		{"set-char", `
VAR S: SET OF CHAR;
BEGIN
  S := ['A'..'C', 'Z'];
  IF ('B' IN S) AND NOT ('D' IN S) AND ('Z' IN S) THEN WRITELN('OK')
END`, "OK", nil},
		{"set-subset", `
VAR A, B: SET OF 0..9;
BEGIN
  A := [1, 2];
  B := [1, 2, 3];
  IF (A <= B) AND NOT (B <= A) THEN WRITELN('OK')
END`, "OK", nil},
		{"with-nested", `
TYPE INNER = RECORD Y: INTEGER END;
     OUTER = RECORD X: INTEGER; I: INNER END;
VAR R: OUTER;
BEGIN
  WITH R, R.I DO
  BEGIN
    X := 1;
    Y := 2
  END;
  WRITELN(R.X, ' ', R.I.Y)
END`, "1 2", nil},
		{"with-ptr", `
TYPE PREC = ^REC;
     REC = RECORD V: INTEGER END;
VAR P: PREC;
BEGIN
  NEW(P);
  WITH P^ DO V := 8;
  WRITELN(P^.V)
END`, "8", nil},
		{"goto-from-if", `
LABEL 10;
VAR I: INTEGER;
BEGIN
  I := 1;
  IF I = 1 THEN GOTO 10;
  WRITELN('NO');
10:
  WRITELN('YES')
END`, "YES", nil},
		{"case-char", `
VAR C: CHAR;
BEGIN
  C := 'B';
  CASE C OF
    'A': WRITELN('1');
    'B', 'C': WRITELN('2');
    'D'..'F': WRITELN('3')
  END
END`, "2", nil},
		{"for-zero", `
VAR I, N: INTEGER;
BEGIN
  N := 0;
  FOR I := 1 TO 0 DO N := N + 1;
  FOR I := 5 DOWNTO 6 DO N := N + 1;
  WRITELN(N)
END`, "0", nil},
		{"repeat-once", `
VAR N: INTEGER;
BEGIN
  N := 0;
  REPEAT N := N + 1 UNTIL TRUE;
  WRITELN(N)
END`, "1", nil},
		{"type-alias", `
TYPE T = INTEGER;
VAR X: T;
BEGIN
  X := 4;
  WRITELN(X * X)
END`, "16", nil},
		{"real-exp", `
VAR R: REAL;
BEGIN
  R := 1.5E1;
  WRITELN(TRUNC(R), ' ', TRUNC(1E2))
END`, "15 100", nil},
		{"div-real", `
BEGIN
  WRITELN(TRUNC(7 / 2), ' ', 7 DIV 2)
END`, "3 3", nil},
		{"rel-and", `
BEGIN
  IF (1 < 2) AND (3 > 2) THEN WRITELN('OK')
END`, "OK", nil},
		{"empty-set", `
VAR S: SET OF 1..5;
BEGIN
  S := [];
  IF NOT (1 IN S) THEN WRITELN('OK')
END`, "OK", nil},
		{"array-index-chain", `
VAR A: ARRAY[1..2, 1..2] OF INTEGER;
BEGIN
  A[1][2] := 9;
  WRITELN(A[1, 2])
END`, "9", nil},
		{"record-assign", `
TYPE REC = RECORD A, B: INTEGER END;
VAR R, S: REC;
BEGIN
  R.A := 3; R.B := 4;
  S := R;
  WRITELN(S.A, ' ', S.B)
END`, "3 4", nil},
		{"linked-list", `
TYPE PTR = ^NODE;
     NODE = RECORD V: INTEGER; NXT: PTR END;
VAR H, P: PTR;
    S: INTEGER;
BEGIN
  H := NIL;
  NEW(P); P^.V := 3; P^.NXT := H; H := P;
  NEW(P); P^.V := 4; P^.NXT := H; H := P;
  S := 0;
  P := H;
  WHILE P <> NIL DO
  BEGIN
    S := S + P^.V;
    P := P^.NXT
  END;
  WRITELN(S)
END`, "7", nil},
		{"write-output", `
BEGIN
  WRITELN(OUTPUT, 'HI')
END`, "HI", nil},
		{"pred-char", `
BEGIN
  WRITELN(PRED('B'), SUCC('A'))
END`, "AB", nil},
		{"boolean-case", `
VAR B: BOOLEAN;
BEGIN
  B := FALSE;
  CASE B OF
    FALSE: WRITELN('F');
    TRUE: WRITELN('T')
  END
END`, "F", nil},
		{"quoted-quote", `
BEGIN
  WRITELN('IT''S')
END`, "IT'S", nil},
		{"empty-compound", `
BEGIN
  ;
  WRITELN('OK')
END`, "OK", nil},
		{"nested-if-else", `
VAR N: INTEGER;
BEGIN
  N := 2;
  IF N > 0 THEN
    IF N > 5 THEN WRITELN('A') ELSE WRITELN('B')
END`, "B", nil},
		{"maxint", `
BEGIN
  WRITELN(MAXINT > 1000)
END`, "TRUE", nil},
		{"var-record-field", `
TYPE REC = RECORD X: INTEGER END;
VAR R: REC;
PROCEDURE INC(VAR N: INTEGER);
BEGIN
  N := N + 1
END;
BEGIN
  R.X := 4;
  INC(R.X);
  WRITELN(R.X)
END`, "5", nil},
		{"value-array-copy", `
VAR A: ARRAY[1..2] OF INTEGER;
PROCEDURE TOUCH(B: ARRAY[1..2] OF INTEGER);
BEGIN
  B[1] := 0
END;
BEGIN
  A[1] := 9; A[2] := 8;
  TOUCH(A);
  WRITELN(A[1])
END`, "9", nil},
		{"comment-mix", `
BEGIN
  { (* still a brace comment *) }
  (* { still a star comment } *)
  WRITELN('OK')
END`, "OK", nil},
		{"for-char", `
VAR C: CHAR;
    N: INTEGER;
BEGIN
  N := 0;
  FOR C := 'A' TO 'C' DO N := N + 1;
  WRITELN(N)
END`, "3", nil},
		{"not-in", `
BEGIN
  IF NOT (2 IN [1, 3]) THEN WRITELN('OK')
END`, "OK", nil},
		{"function-real", `
FUNCTION HALF(X: INTEGER): REAL;
BEGIN
  HALF := X / 2
END;
BEGIN
  WRITELN(HALF(5):0:1)
END`, "2.5", nil},
		{"read-two", `
VAR A, B: INTEGER;
BEGIN
  READLN(A, B);
  WRITELN(A + B)
END`, "7", []string{"3 4"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantPascal(t, wrapPas(c.src), c.want, c.in...)
		})
	}
}

func TestPascalISOStandard(t *testing.T) {
	compileFail := func(src, want string) {
		t.Helper()
		_, err := CompilePascal(wrapPas(src))
		if err == nil {
			t.Fatalf("expected compile error %q\n%s", want, src)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("compile error %q, want %q\n%s", err, want, src)
		}
	}
	runFail := func(src, want string) {
		t.Helper()
		err := RunPascal(wrapPas(src), pascalHostFromIO(IO{}))
		if err == nil {
			t.Fatalf("expected run error %q\n%s", want, src)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("run error %q, want %q\n%s", err, want, src)
		}
	}

	wantPascal(t, wrapPas(`BEGIN WRITELN((-7) MOD 3) END`), "2")

	runFail(`
VAR I: INTEGER;
BEGIN
  I := 3;
  CASE I OF
    1: WRITELN('A')
  END
END`, "case selector")

	runFail(`
VAR X: 1..2;
BEGIN
  X := 3
END`, "out of range")

	runFail(`BEGIN WRITELN(PRED(CHR(0))) END`, "out of range")
	runFail(`BEGIN WRITELN(CHR(256)) END`, "out of range")
	runFail(`BEGIN WRITELN(1 MOD 0) END`, "MOD divisor")
	runFail(`BEGIN WRITELN(1 DIV 0) END`, "division by zero")

	compileFail(`
VAR I: INTEGER;
BEGIN
  FOR I := 1 TO 3 DO I := I + 1
END`, "threatened")

	compileFail(`
VAR N: INTEGER;
PROCEDURE P(X: INTEGER);
BEGIN
  FOR X := 1 TO 3 DO N := N + 1
END;
BEGIN
  P(1)
END`, "local to this block")

	compileFail(`
LABEL 10;
VAR I: INTEGER;
BEGIN
  I := 1;
  GOTO 10;
  WHILE I < 3 DO
  10: I := I + 1
END`, "structured statement")

	compileFail(`
VAR I: INTEGER;
BEGIN
  I := 1;
  CASE I OF
    1: WRITELN('A');
    1: WRITELN('B')
  END
END`, "duplicate case")

	compileFail(`
PROCEDURE P(VAR X: INTEGER);
BEGIN
  X := 1
END;
BEGIN
  P(2)
END`, "VAR parameter")

	compileFail(`BEGIN IF 1 AND 2 THEN WRITELN('X') END`, "boolean")
}

func TestPascalHelpISO(t *testing.T) {
	sh, out := guestShell(t)
	sh.Dispatch("HELP PASCAL")
	got := out.String()
	if !strings.Contains(got, "ISO 7185") {
		t.Fatalf("help should name ISO 7185: %q", got)
	}
	if !strings.Contains(got, "COMPILE") {
		t.Fatalf("help: %q", got)
	}
	if !strings.Contains(got, ".PAC") {
		t.Fatalf("help should mention .PAC: %q", got)
	}
}
