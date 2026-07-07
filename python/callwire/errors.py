_ALLOWED_ERROR_NAMES = frozenset({
    "ValueError", "TypeError", "KeyError", "IndexError",
    "RuntimeError", "ZeroDivisionError", "ArithmeticError",
    "FloatingPointError", "OverflowError",
    "AssertionError", "AttributeError",
    "ImportError", "ModuleNotFoundError",
    "NameError", "UnboundLocalError",
    "LookupError", "StopIteration",
    "EOFError", "RecursionError", "MemoryError",
    "NotImplementedError", "SystemError",
    "OSError", "FileNotFoundError", "PermissionError",
    "TimeoutError", "ConnectionError",
    "ConnectionRefusedError", "ConnectionResetError",
    "ConnectionAbortedError", "BrokenPipeError",
    "BlockingIOError", "ChildProcessError",
    "InterruptedError", "IsADirectoryError",
    "NotADirectoryError", "ProcessLookupError",
    "FileExistsError",
    "UnicodeError", "UnicodeDecodeError", "UnicodeEncodeError",
    "UnicodeTranslateError",
    "EnvironmentError", "IOError",
    "SyntaxError", "IndentationError", "TabError",
    "ReferenceError", "PendingDeprecationWarning",
    "BytesWarning", "DeprecationWarning", "FutureWarning",
    "ImportWarning", "ResourceWarning", "RuntimeWarning",
    "SyntaxWarning", "UnicodeWarning", "UserWarning",
    "Warning",
})


def exception_to_wire(exc: Exception) -> tuple[str, str]:
    exc_name = type(exc).__name__
    if exc_name in _ALLOWED_ERROR_NAMES:
        return exc_name, str(exc)
    return "InternalError", "an internal error occurred"
