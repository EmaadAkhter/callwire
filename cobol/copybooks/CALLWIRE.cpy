      *> Callwire COBOL copybook — common WORKING-STORAGE fields for calling
      *> the callwire_cobol_* shim (cobol/src/cobol_shim.c) via CALL.
      *> COPY this into WORKING-STORAGE SECTION of a client program.
       01 CW-CLIENT-PTR   USAGE POINTER.
       01 CW-PORT         PIC S9(9) COMP-5.
       01 CW-ARGC         PIC S9(9) COMP-5.
       01 CW-RC           PIC S9(9) COMP-5.
       01 CW-INT-RESULT   PIC S9(18) COMP-5.
