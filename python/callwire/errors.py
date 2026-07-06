_EXPORTED_ERRORS = {
    "ValueError": ValueError,
    "TypeError": TypeError,
    "KeyError": KeyError,
}


def exception_to_wire(exc: Exception) -> tuple[str, str]:
    exc_name = type(exc).__name__
    if exc_name not in _EXPORTED_ERRORS:
        return "InternalError", "an internal error occurred"
    return exc_name, str(exc)
