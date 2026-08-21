10 EXTEND
20 C$=SYS(CHR$(6%)+CHR$(-18%))
30 A$=""
40 FOR I%=1% TO LEN(C$)
50 CH$=MID$(C$,I%,1)
60 IF CH$>="a" AND CH$<="z" THEN CH$=CHR$(ASC(CH$)-32)
70 A$=A$+CH$
80 NEXT I%
90 IF A$="" THEN 200
100 IF LEFT$(A$,4)="SHUT" THEN X$=SYS(CHR$(6%)+CHR$(-34%)+C$) \ PRINT X$; \ END
110 IF LEFT$(A$,3)="CCL" THEN X$=SYS(CHR$(6%)+CHR$(-34%)+C$) \ PRINT X$; \ END
120 IF LEFT$(A$,5)="REACT" THEN X$=SYS(CHR$(6%)+CHR$(-34%)+C$) \ PRINT X$; \ END
130 IF LEFT$(A$,6)="DSKINT" THEN X$=SYS(CHR$(6%)+CHR$(-34%)+C$) \ PRINT X$; \ END
140 IF LEFT$(A$,2)="LI" THEN 200
150 PRINT "REACT  DSKINT  CCL  SHUTUP  (privileged)"
160 END
200 PRINT "UTILITY  (privileged)"
210 PRINT "  REACT     accounts / quotas"
220 PRINT "  DSKINT    initialize a pack"
230 PRINT "  CCL       install a keyboard command"
240 PRINT "  SHUTUP    halt the system"
250 END
