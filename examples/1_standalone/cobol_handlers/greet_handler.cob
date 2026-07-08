      *> Callwire handler subprogram — registered via callwire_cobol_export_str1.
       >>SOURCE FORMAT FREE
       IDENTIFICATION DIVISION.
       PROGRAM-ID. GREET-HANDLER.
       DATA DIVISION.
       LINKAGE SECTION.
       01 LS-NAME   PIC X(256).
       01 LS-RESULT PIC X(256).
       PROCEDURE DIVISION USING LS-NAME LS-RESULT.
           MOVE SPACES TO LS-RESULT.
           STRING "Hello, " DELIMITED BY SIZE
               FUNCTION TRIM(LS-NAME) DELIMITED BY SIZE
               "!" DELIMITED BY SIZE
               INTO LS-RESULT
           END-STRING.
           GOBACK.
