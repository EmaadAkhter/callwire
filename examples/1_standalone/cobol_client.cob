       >>SOURCE FORMAT FREE
      *> COBOL client calling a local server (any language) on localhost:9090.
      *>
      *> Build (compile the C core + shim as C, then link with cobc):
      *>   for f in codec framing client server errors; do
      *>     gcc -std=c99 -pthread -Ic/include -c c/src/$f.c -o /tmp/$f.o
      *>   done
      *>   gcc -std=c99 -Ic/include -c cobol/src/cobol_shim.c -o /tmp/shim.o
      *>   cobc -x examples/1_standalone/cobol_client.cob -o cobol_client \
      *>     /tmp/shim.o /tmp/codec.o /tmp/framing.o /tmp/client.o /tmp/server.o /tmp/errors.o -lpthread
      *>
      *> Run: ./cobol_client
       IDENTIFICATION DIVISION.
       PROGRAM-ID. COBOL-CLIENT.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-ADDR-RAW    PIC X(9)  VALUE "localhost".
       01 WS-ADDR        PIC X(10).
       01 WS-PORT        PIC S9(9) COMP-5 VALUE 9090.
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

       PROCEDURE DIVISION.
           STRING FUNCTION TRIM(WS-ADDR-RAW) DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-ADDR END-STRING.

           CALL "callwire_cobol_connect" USING BY REFERENCE WS-ADDR BY VALUE WS-PORT RETURNING WS-CLIENT-PTR END-CALL.
           IF WS-CLIENT-PTR = NULL
               DISPLAY "connect failed"
               STOP RUN
           END-IF.

           STRING "add" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-FUNC-ADD END-STRING.
           CALL "callwire_cobol_call_ints" USING
               BY VALUE WS-CLIENT-PTR
               BY REFERENCE WS-FUNC-ADD
               BY REFERENCE WS-ARGS
               BY VALUE WS-ARGC
               BY REFERENCE WS-INT-RESULT
               RETURNING WS-RC
           END-CALL.
           IF WS-RC = 0
               DISPLAY "add(10, 20) = " WS-INT-RESULT
           ELSE
               DISPLAY "add() call failed"
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
           IF WS-RC = 0
               DISPLAY 'greet("World") = ' FUNCTION TRIM(WS-STR-RESULT)
           ELSE
               DISPLAY "greet() call failed"
           END-IF.

           CALL "callwire_cobol_close" USING BY VALUE WS-CLIENT-PTR END-CALL.
           STOP RUN.
