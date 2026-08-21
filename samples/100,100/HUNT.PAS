PROGRAM HUNT(INPUT, OUTPUT);
VAR SEED, P, W, P1, P2, B1, B2, AR, D: INTEGER;
    C: ARRAY[1..20, 1..3] OF INTEGER;
    A: CHAR;
    DONE: BOOLEAN;
FUNCTION RND(N: INTEGER): INTEGER;
BEGIN
  SEED := (SEED * 25173 + 13849) MOD 65536;
  IF N < 1 THEN RND := 0 ELSE RND := SEED MOD N + 1
END;
PROCEDURE LINK(R, X, Y, Z: INTEGER);
BEGIN
  C[R, 1] := X;
  C[R, 2] := Y;
  C[R, 3] := Z
END;
PROCEDURE SETUP;
BEGIN
  LINK(1, 2, 5, 8);
  LINK(2, 1, 3, 10);
  LINK(3, 2, 4, 12);
  LINK(4, 3, 5, 14);
  LINK(5, 1, 4, 6);
  LINK(6, 5, 7, 15);
  LINK(7, 6, 8, 17);
  LINK(8, 1, 7, 9);
  LINK(9, 8, 10, 18);
  LINK(10, 2, 9, 11);
  LINK(11, 10, 12, 19);
  LINK(12, 3, 11, 13);
  LINK(13, 12, 14, 20);
  LINK(14, 4, 13, 15);
  LINK(15, 6, 14, 16);
  LINK(16, 15, 17, 20);
  LINK(17, 7, 16, 18);
  LINK(18, 9, 17, 19);
  LINK(19, 11, 18, 20);
  LINK(20, 13, 16, 19)
END;
FUNCTION ROAD(A, B: INTEGER): BOOLEAN;
VAR J: INTEGER;
    OK: BOOLEAN;
BEGIN
  OK := FALSE;
  FOR J := 1 TO 3 DO
    IF C[A, J] = B THEN OK := TRUE;
  ROAD := OK
END;
PROCEDURE PLACE;
BEGIN
  P := RND(20);
  REPEAT W := RND(20) UNTIL W <> P;
  REPEAT P1 := RND(20) UNTIL (P1 <> P) AND (P1 <> W);
  REPEAT P2 := RND(20) UNTIL (P2 <> P) AND (P2 <> W) AND (P2 <> P1);
  REPEAT B1 := RND(20) UNTIL (B1 <> P) AND (B1 <> W) AND (B1 <> P1) AND (B1 <> P2);
  REPEAT B2 := RND(20) UNTIL (B2 <> P) AND (B2 <> W) AND (B2 <> P1) AND (B2 <> P2) AND (B2 <> B1)
END;
PROCEDURE HAZARD;
VAR FLY: BOOLEAN;
BEGIN
  FLY := TRUE;
  WHILE FLY AND (NOT DONE) DO
  BEGIN
    FLY := FALSE;
    IF P = W THEN
    BEGIN
      WRITELN('THE WUMPUS GOT YOU.');
      DONE := TRUE
    END
    ELSE IF (P = P1) OR (P = P2) THEN
    BEGIN
      WRITELN('YYYYIIIIEEEE -- A PIT.');
      DONE := TRUE
    END
    ELSE IF (P = B1) OR (P = B2) THEN
    BEGIN
      WRITELN('SNATCHED BY SUPER BATS.');
      P := RND(20);
      FLY := TRUE
    END
  END
END;
PROCEDURE LOOK;
VAR J, R: INTEGER;
    SMELL, DRAFT, BATS: BOOLEAN;
BEGIN
  WRITELN;
  WRITELN('YOU ARE IN CAVE ', P);
  WRITE('TUNNELS LEAD TO');
  FOR J := 1 TO 3 DO WRITE(' ', C[P, J]);
  WRITELN;
  SMELL := FALSE;
  DRAFT := FALSE;
  BATS := FALSE;
  FOR J := 1 TO 3 DO
  BEGIN
    R := C[P, J];
    IF R = W THEN SMELL := TRUE;
    IF (R = P1) OR (R = P2) THEN DRAFT := TRUE;
    IF (R = B1) OR (R = B2) THEN BATS := TRUE
  END;
  IF SMELL THEN WRITELN('I SMELL A WUMPUS.');
  IF DRAFT THEN WRITELN('I FEEL A DRAFT.');
  IF BATS THEN WRITELN('BATS NEARBY.')
END;
BEGIN
  WRITELN('HUNT THE WUMPUS -- 20 CAVES, EACH WITH 3 TUNNELS.');
  WRITELN('MOVE OR SHOOT. PITS AND BATS AND ONE WUMPUS.');
  WRITE('SEED? ');
  READLN(SEED);
  IF SEED < 1 THEN SEED := 1;
  SETUP;
  PLACE;
  AR := 5;
  DONE := FALSE;
  REPEAT
    LOOK;
    WRITE('MOVE OR SHOOT (M/S, Q TO QUIT)? ');
    READLN(A);
    IF (A >= 'a') AND (A <= 'z') THEN A := CHR(ORD(A) - 32);
    IF A = 'Q' THEN
      DONE := TRUE
    ELSE IF A = 'M' THEN
    BEGIN
      WRITE('TO CAVE (0 QUITS)? ');
      READLN(D);
      IF D = 0 THEN
        DONE := TRUE
      ELSE IF NOT ROAD(P, D) THEN
        WRITELN('NOT CONNECTED.')
      ELSE
      BEGIN
        P := D;
        HAZARD
      END
    END
    ELSE IF A = 'S' THEN
    BEGIN
      IF AR < 1 THEN
        WRITELN('NO ARROWS.')
      ELSE
      BEGIN
        WRITE('SHOOT INTO CAVE (0 QUITS)? ');
        READLN(D);
        IF D = 0 THEN
          DONE := TRUE
        ELSE IF NOT ROAD(P, D) THEN
          WRITELN('ARROWS AREN''T THAT CROOKED.')
        ELSE
        BEGIN
          AR := AR - 1;
          IF D = W THEN
          BEGIN
            WRITELN('AHA! YOU GOT THE WUMPUS.');
            DONE := TRUE
          END
          ELSE
          BEGIN
            WRITELN('MISSED.');
            IF RND(4) <= 3 THEN W := C[W, RND(3)];
            IF W = P THEN
            BEGIN
              WRITELN('THE WUMPUS WANDERS IN AND EATS YOU.');
              DONE := TRUE
            END
            ELSE IF AR < 1 THEN
            BEGIN
              WRITELN('THAT WAS THE LAST ARROW.');
              DONE := TRUE
            END
          END
        END
      END
    END
  UNTIL DONE;
  WRITELN('ENOUGH.')
END.
