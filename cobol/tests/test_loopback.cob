       >>SOURCE FORMAT FREE
      *> End-to-end COBOL loopback test: connects to a real callwire server
      *> (any language) over TCP, calls add(10,20) via the int shim and
      *> greet("World") via the string shim.
       IDENTIFICATION DIVISION.
       PROGRAM-ID. TEST-LOOPBACK.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-ADDR-RAW    PIC X(9)  VALUE "127.0.0.1".
       01 WS-ADDR        PIC X(10).
       01 WS-PORT        PIC S9(9) COMP-5 VALUE 19299.
       01 WS-CLIENT-PTR  USAGE POINTER.

       01 WS-FUNC-ADD    PIC X(4).
       01 WS-ARGS.
          05 WS-ARG1     PIC S9(18) COMP-5 VALUE 10.
          05 WS-ARG2     PIC S9(18) COMP-5 VALUE 20.
       01 WS-ARGC        PIC S9(9) COMP-5 VALUE 2.
       01 WS-INT-RESULT  PIC S9(18) COMP-5.
       01 WS-RC          PIC S9(9) COMP-5.

       01 WS-FUNC-GREET  PIC X(6).
       01 WS-GREET-ARG   PIC X(6).
       01 WS-STR-RESULT  PIC X(64).
       01 WS-FUNC-NOTFOUND PIC X(12).

       PROCEDURE DIVISION.
           STRING FUNCTION TRIM(WS-ADDR-RAW) DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-ADDR END-STRING.

           CALL "callwire_cobol_connect" USING BY REFERENCE WS-ADDR BY VALUE WS-PORT RETURNING WS-CLIENT-PTR END-CALL.

           IF WS-CLIENT-PTR = NULL
               DISPLAY "FAIL: connect failed"
               STOP RUN
           END-IF.
           DISPLAY "connect: OK".

           STRING "add" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-FUNC-ADD END-STRING.

           CALL "callwire_cobol_call_ints" USING
               BY VALUE WS-CLIENT-PTR
               BY REFERENCE WS-FUNC-ADD
               BY REFERENCE WS-ARGS
               BY VALUE WS-ARGC
               BY REFERENCE WS-INT-RESULT
               RETURNING WS-RC
           END-CALL.

           IF WS-RC NOT = 0
               DISPLAY "FAIL: add() call failed"
               STOP RUN
           END-IF.

           IF WS-INT-RESULT = 30
               DISPLAY "test_unary_add: OK (10 + 20 = " WS-INT-RESULT ")"
           ELSE
               DISPLAY "FAIL: add(10,20) returned " WS-INT-RESULT " expected 30"
               STOP RUN
           END-IF.

           STRING "greet" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-FUNC-GREET END-STRING.
           STRING "World" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-GREET-ARG END-STRING.

           CALL "callwire_cobol_call_str" USING
               BY VALUE WS-CLIENT-PTR
               BY REFERENCE WS-FUNC-GREET
               BY REFERENCE WS-GREET-ARG
               BY REFERENCE WS-STR-RESULT
               BY VALUE 64
               RETURNING WS-RC
           END-CALL.

           IF WS-RC NOT = 0
               DISPLAY "FAIL: greet() call failed"
               STOP RUN
           END-IF.

           IF WS-STR-RESULT(1:13) = "Hello, World!"
               DISPLAY "test_unary_greet: OK (Hello, World!)"
           ELSE
               DISPLAY "FAIL: greet(World) returned unexpected content"
               STOP RUN
           END-IF.

           STRING "nonexistent" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-FUNC-NOTFOUND END-STRING.
           CALL "callwire_cobol_call_ints" USING
               BY VALUE WS-CLIENT-PTR
               BY REFERENCE WS-FUNC-NOTFOUND
               BY REFERENCE WS-ARGS
               BY VALUE WS-ARGC
               BY REFERENCE WS-INT-RESULT
               RETURNING WS-RC
           END-CALL.
           IF WS-RC NOT = 0
               DISPLAY "test_not_found: OK (call correctly failed)"
           ELSE
               DISPLAY "FAIL: call to nonexistent function should have failed"
               STOP RUN
           END-IF.

           CALL "callwire_cobol_close" USING BY VALUE WS-CLIENT-PTR END-CALL.
           DISPLAY "All COBOL loopback tests passed.".
           STOP RUN.
