       >>SOURCE FORMAT FREE
      *> COBOL server: registers ADD-HANDLER/GREET-HANDLER subprograms as
      *> "add"/"greet" RPC exports, then serves. Verifies the export
      *> (server) direction of the COBOL SDK — a client in any other
      *> language calls this process over real TCP.
       IDENTIFICATION DIVISION.
       PROGRAM-ID. TEST-SERVER.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-ADDR-RAW  PIC X(7) VALUE "0.0.0.0".
       01 WS-ADDR      PIC X(8).
       01 WS-PORT      PIC S9(9) COMP-5 VALUE 19499.
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
               DISPLAY "FAIL: server bind failed"
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
           IF WS-RC NOT = 0
               DISPLAY "FAIL: export add failed"
               STOP RUN
           END-IF.

           STRING "greet" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-FUNC-GREET END-STRING.
           STRING "GREET-HANDLER" DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-PROG-GREET END-STRING.
           CALL "callwire_cobol_export_str1" USING
               BY VALUE WS-SERVER-PTR
               BY REFERENCE WS-FUNC-GREET
               BY REFERENCE WS-PROG-GREET
               RETURNING WS-RC
           END-CALL.
           IF WS-RC NOT = 0
               DISPLAY "FAIL: export greet failed"
               STOP RUN
           END-IF.

           DISPLAY "Callwire COBOL server listening on :19499".
           CALL "callwire_cobol_server_serve" USING BY VALUE WS-SERVER-PTR RETURNING WS-RC END-CALL.
           STOP RUN.
