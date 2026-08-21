PROGRAM MAZE(INPUT, OUTPUT);
VAR B: ARRAY[1..6] OF PACKED ARRAY[1..7] OF CHAR;
    PX, PY, NX, NY: INTEGER;
    C: CHAR;
    DONE: BOOLEAN;
PROCEDURE SHOW;
VAR X, Y: INTEGER;
BEGIN
  FOR Y := 1 TO 6 DO
  BEGIN
    FOR X := 1 TO 7 DO
      IF (X = PX) AND (Y = PY) THEN WRITE('*') ELSE WRITE(B[Y][X]);
    WRITELN
  END
END;
BEGIN
  WRITELN('A SMALL MAZE. N S E W TO MOVE, Q TO QUIT. * IS YOU.');
  B[1] := '#######';
  B[2] := '#S    #';
  B[3] := '# ### #';
  B[4] := '#   # #';
  B[5] := '### #E#';
  B[6] := '#######';
  PX := 2;
  PY := 2;
  B[2][2] := ' ';
  DONE := FALSE;
  REPEAT
    WRITELN;
    SHOW;
    IF B[PY][PX] = 'E' THEN
    BEGIN
      WRITELN('YOU ESCAPED.');
      DONE := TRUE
    END
    ELSE
    BEGIN
      WRITE('MOVE? ');
      READLN(C);
      IF (C >= 'a') AND (C <= 'z') THEN C := CHR(ORD(C) - 32);
      NX := PX;
      NY := PY;
      CASE C OF
        'N': NY := PY - 1;
        'S': NY := PY + 1;
        'E': NX := PX + 1;
        'W': NX := PX - 1;
        'Q': DONE := TRUE
      END;
      IF NOT DONE THEN
        IF (NY < 1) OR (NY > 6) OR (NX < 1) OR (NX > 7) THEN
          WRITELN('WALL.')
        ELSE IF B[NY][NX] = '#' THEN
          WRITELN('WALL.')
        ELSE
        BEGIN
          PX := NX;
          PY := NY
        END
    END
  UNTIL DONE;
  IF B[PY][PX] <> 'E' THEN WRITELN('ENOUGH.')
END.
