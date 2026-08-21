PROGRAM CRAPS(INPUT, OUTPUT);
VAR SEED, M, B, T, PT: INTEGER;
    DONE: BOOLEAN;
FUNCTION RND(N: INTEGER): INTEGER;
BEGIN
  SEED := (SEED * 25173 + 13849) MOD 65536;
  IF N < 1 THEN RND := 0 ELSE RND := SEED MOD N + 1
END;
FUNCTION ROLL: INTEGER;
VAR A, D: INTEGER;
BEGIN
  A := RND(6);
  D := RND(6);
  WRITELN('ROLL ', A, ' AND ', D, ' -- ', A + D);
  ROLL := A + D
END;
BEGIN
  WRITELN('CRAPS. PASS LINE: 7 OR 11 WIN, 2 3 12 LOSE, ELSE THE POINT.');
  WRITELN('YOU START WITH 100. BET 0 TO QUIT.');
  WRITE('SEED? ');
  READLN(SEED);
  IF SEED < 1 THEN SEED := 1;
  M := 100;
  DONE := FALSE;
  REPEAT
    IF M < 1 THEN
    BEGIN
      WRITELN('BROKE.');
      DONE := TRUE
    END
    ELSE
    BEGIN
      WRITELN('STAKE ', M);
      WRITE('BET? ');
      READLN(B);
      IF B = 0 THEN
        DONE := TRUE
      ELSE IF (B < 0) OR (B > M) THEN
        WRITELN('BETWEEN 0 AND ', M)
      ELSE
      BEGIN
        T := ROLL;
        IF (T = 7) OR (T = 11) THEN
        BEGIN
          WRITELN('YOU WIN.');
          M := M + B
        END
        ELSE IF (T = 2) OR (T = 3) OR (T = 12) THEN
        BEGIN
          WRITELN('YOU LOSE.');
          M := M - B
        END
        ELSE
        BEGIN
          PT := T;
          WRITELN('POINT IS ', PT);
          REPEAT
            T := ROLL
          UNTIL (T = PT) OR (T = 7);
          IF T = PT THEN
          BEGIN
            WRITELN('YOU WIN.');
            M := M + B
          END
          ELSE
          BEGIN
            WRITELN('YOU LOSE.');
            M := M - B
          END
        END
      END
    END
  UNTIL DONE;
  WRITELN('YOU LEAVE WITH ', M)
END.
