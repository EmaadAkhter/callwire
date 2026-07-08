      *> Callwire handler subprogram — registered via callwire_cobol_export_int2.
       >>SOURCE FORMAT FREE
       IDENTIFICATION DIVISION.
       PROGRAM-ID. ADD-HANDLER.
       DATA DIVISION.
       LINKAGE SECTION.
       01 LS-A      PIC S9(18) COMP-5.
       01 LS-B      PIC S9(18) COMP-5.
       01 LS-RESULT PIC S9(18) COMP-5.
       PROCEDURE DIVISION USING LS-A LS-B LS-RESULT.
           COMPUTE LS-RESULT = LS-A + LS-B.
           GOBACK.
