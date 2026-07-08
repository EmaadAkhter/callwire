       >>SOURCE FORMAT FREE
      *> COBOL server exporting "add" and "greet" — callable from any other
      *> language's client (Go, Python, Rust, TypeScript, Java, C, C++, Swift).
      *>
      *> Handlers are separate COBOL subprograms (cobol_handlers/*.cob),
      *> compiled as dynamically-loadable modules and dispatched through
      *> libcob's cob_call() — see cobol/README.md for why (and a naming
      *> gotcha: the compiled .dylib basename must preserve dashes from the
      *> PROGRAM-ID, e.g. ADD-HANDLER -> add-handler.dylib, not add_handler.dylib).
      *>
      *> Build:
      *>   for f in codec framing client server errors; do
      *>     gcc -std=c99 -pthread -Ic/include -c c/src/$f.c -o /tmp/$f.o
      *>   done
      *>   gcc -std=c99 -Ic/include -I/opt/homebrew/include -c cobol/src/cobol_shim.c -o /tmp/shim.o
      *>   cobc -m examples/1_standalone/cobol_handlers/add_handler.cob -o /tmp/add-handler.dylib
      *>   cobc -m examples/1_standalone/cobol_handlers/greet_handler.cob -o /tmp/greet-handler.dylib
      *>   cobc -x examples/1_standalone/cobol_server.cob -o cobol_server \
      *>     /tmp/shim.o /tmp/codec.o /tmp/framing.o /tmp/client.o /tmp/server.o /tmp/errors.o -lpthread
      *>
      *> Run: COB_LIBRARY_PATH=/tmp ./cobol_server
       IDENTIFICATION DIVISION.
       PROGRAM-ID. COBOL-SERVER.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-ADDR-RAW  PIC X(7) VALUE "0.0.0.0".
       01 WS-ADDR      PIC X(8).
       01 WS-PORT      PIC S9(9) COMP-5 VALUE 9090.
       01 WS-SERVER-PTR USAGE POINTER.

       01 WS-FUNC-ADD    PIC X(4).
       01 WS-PROG-ADD    PIC X(11).
       01 WS-FUNC-GREET  PIC X(6).
       01 WS-PROG-GREET  PIC X(14).
       01 WS-RC          PIC S9(9) COMP-5.

       PROCEDURE DIVISION.
           STRING FUNCTION TRIM(WS-ADDR-RAW) DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-ADDR END-STRING.

           CALL "callwire_cobol_server_new" USING BY REFERENCE WS-ADDR BY VALUE WS-PORT RETURNING WS-SERVER-PTR END-CALL.
           IF WS-SERVER-PTR = NULL
               DISPLAY "server bind failed"
               STOP RUN
           END-IF.

           STRING "add" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-FUNC-ADD END-STRING.
           STRING "ADD-HANDLER" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-PROG-ADD END-STRING.
           CALL "callwire_cobol_export_int2" USING
               BY VALUE WS-SERVER-PTR
               BY REFERENCE WS-FUNC-ADD
               BY REFERENCE WS-PROG-ADD
               RETURNING WS-RC
           END-CALL.

           STRING "greet" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-FUNC-GREET END-STRING.
           STRING "GREET-HANDLER" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-PROG-GREET END-STRING.
           CALL "callwire_cobol_export_str1" USING
               BY VALUE WS-SERVER-PTR
               BY REFERENCE WS-FUNC-GREET
               BY REFERENCE WS-PROG-GREET
               RETURNING WS-RC
           END-CALL.

           DISPLAY "Callwire COBOL server listening on :9090".
           CALL "callwire_cobol_server_serve" USING BY VALUE WS-SERVER-PTR RETURNING WS-RC END-CALL.
           STOP RUN.
