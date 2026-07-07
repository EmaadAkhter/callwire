"""
Standalone Python Client Example
=================================
Connects directly to a running Go server at localhost:9090.

Prerequisites:
  1. Start the Go server:
       cd examples/1_standalone
       go run go_server.go

  2. In a separate terminal, run this client:
       cd examples/1_standalone
       python python_client.py
"""

import sys
import os

# Use the local callwire package from the repo
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "python"))

# Dynamically import remote functions directly from callwire
from callwire import add, greet


def main():
    print("── Calling imported Go functions ──────────────────────────")
    try:
        # Call 'add' as a local function!
        result = add(15, 27)
        print(f"  add(15, 27)        = {result}")

        # Call 'greet'
        greeting = greet("Developer")
        print(f"  greet('Developer') = {greeting!r}")
    except Exception as e:
        print(f"[error] Call failed: {e}")
        print("Is the Go server running? (go run go_server.go)")
        sys.exit(1)


if __name__ == "__main__":
    main()
