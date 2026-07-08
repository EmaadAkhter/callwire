       >>SOURCE FORMAT FREE
      *> COBOL export script: exports "add" on a fixed port via a separate
      *> handler subprogram (handlers/add_handler.cob). INIT paragraph
      *> performs setup and is PERFORMed first thing in the main paragraph
      *> — COBOL has no free-standing functions outside PROCEDURE DIVISION,
      *> so a PERFORMed paragraph named INIT is the closest equivalent to
      *> the init()-function convention used by every other language here.
       IDENTIFICATION DIVISION.
       PROGRAM-ID. EXPORT-COBOL.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-ADDR-RAW  PIC X(7) VALUE "0.0.0.0".
       01 WS-ADDR      PIC X(8).
       01 WS-PORT      PIC S9(9) COMP-5 VALUE 9109.
       01 WS-SERVER-PTR USAGE POINTER.
       01 WS-FUNC-ADD    PIC X(4).
       01 WS-PROG-ADD    PIC X(11).
       01 WS-RC          PIC S9(9) COMP-5.

       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM INIT.
           DISPLAY "COBOL matrix export listening on :9109".
           CALL "callwire_cobol_server_serve" USING BY VALUE WS-SERVER-PTR RETURNING WS-RC END-CALL.
           STOP RUN.

       INIT.
           STRING FUNCTION TRIM(WS-ADDR-RAW) DELIMITED BY SIZE LOW-VALUE DELIMITED BY SIZE INTO WS-ADDR END-STRING.
           CALL "callwire_cobol_server_new" USING BY REFERENCE WS-ADDR BY VALUE WS-PORT
               RETURNING WS-SERVER-PTR END-CALL.
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
