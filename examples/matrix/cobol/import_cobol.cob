       >>SOURCE FORMAT FREE
      *> COBOL import script: calls "add"(10,20) on every OTHER language's
      *> matrix export server (best-effort — SKIP if a port isn't reachable).
       IDENTIFICATION DIVISION.
       PROGRAM-ID. IMPORT-COBOL.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-ADDR-RAW  PIC X(9) VALUE "127.0.0.1".
       01 WS-ADDR      PIC X(10).
       01 WS-CLIENT-PTR USAGE POINTER.
       01 WS-FUNC-ADD  PIC X(4).
       01 WS-ARGS.
          05 WS-ARG1   PIC S9(18) COMP-5 VALUE 10.
          05 WS-ARG2   PIC S9(18) COMP-5 VALUE 20.
       01 WS-ARGC       PIC S9(9) COMP-5 VALUE 2.
       01 WS-INT-RESULT PIC S9(18) COMP-5.
       01 WS-RC         PIC S9(9) COMP-5.

       01 WS-PORT-TABLE.
          05 FILLER PIC X(8) VALUE "go      ". 05 FILLER PIC S9(9) COMP-5 VALUE 9101.
          05 FILLER PIC X(8) VALUE "python  ". 05 FILLER PIC S9(9) COMP-5 VALUE 9102.
          05 FILLER PIC X(8) VALUE "rust    ". 05 FILLER PIC S9(9) COMP-5 VALUE 9103.
          05 FILLER PIC X(8) VALUE "ts      ". 05 FILLER PIC S9(9) COMP-5 VALUE 9104.
          05 FILLER PIC X(8) VALUE "java    ". 05 FILLER PIC S9(9) COMP-5 VALUE 9105.
          05 FILLER PIC X(8) VALUE "c       ". 05 FILLER PIC S9(9) COMP-5 VALUE 9106.
          05 FILLER PIC X(8) VALUE "cpp     ". 05 FILLER PIC S9(9) COMP-5 VALUE 9107.
          05 FILLER PIC X(8) VALUE "swift   ". 05 FILLER PIC S9(9) COMP-5 VALUE 9108.
       01 WS-TARGETS REDEFINES WS-PORT-TABLE.
          05 WS-TARGET OCCURS 8 TIMES.
             10 WS-TARGET-NAME PIC X(8).
             10 WS-TARGET-PORT PIC S9(9) COMP-5.

       01 WS-IDX PIC S9(9) COMP-5.

       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM INIT.

           STRING "add" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-FUNC-ADD END-STRING.

           PERFORM VARYING WS-IDX FROM 1 BY 1 UNTIL WS-IDX > 8
               CALL "callwire_cobol_connect" USING
                   BY REFERENCE WS-ADDR
                   BY VALUE WS-TARGET-PORT(WS-IDX)
                   RETURNING WS-CLIENT-PTR
               END-CALL

               IF WS-CLIENT-PTR = NULL
                   DISPLAY FUNCTION TRIM(WS-TARGET-NAME(WS-IDX)) " SKIP (not running)"
               ELSE
                   CALL "callwire_cobol_call_ints" USING
                       BY VALUE WS-CLIENT-PTR
                       BY REFERENCE WS-FUNC-ADD
                       BY REFERENCE WS-ARGS
                       BY VALUE WS-ARGC
                       BY REFERENCE WS-INT-RESULT
                       RETURNING WS-RC
                   END-CALL

                   IF WS-RC = 0
                       DISPLAY FUNCTION TRIM(WS-TARGET-NAME(WS-IDX)) " OK  add(10,20) = " WS-INT-RESULT
                   ELSE
                       DISPLAY FUNCTION TRIM(WS-TARGET-NAME(WS-IDX)) " SKIP (call failed)"
                   END-IF

                   CALL "callwire_cobol_close" USING BY VALUE WS-CLIENT-PTR END-CALL
               END-IF
           END-PERFORM.

           STOP RUN.

       INIT.
           STRING FUNCTION TRIM(WS-ADDR-RAW) DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-ADDR END-STRING.
