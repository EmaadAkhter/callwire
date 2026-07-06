import json
import os
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse, parse_qs

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "python"))

from callwire import export, configure, ref

HOST = "localhost"
CALLWIRE_PORT = 9099
HTTP_PORT = 8088
GO_CALLWIRE = "localhost:9098"

configure(host=HOST, port=CALLWIRE_PORT)

@export
def greet(name):
    return f"hello {name}"

@export
def reverse(s):
    return s[::-1]

go_ref = None
py_ref = {}

def _go(name):
    global go_ref
    if go_ref is None:
        go_ref = {}
    if name not in go_ref:
        go_ref[name] = ref(name, host="localhost", port=9098)
    return go_ref[name]

def _py(name):
    if name not in py_ref:
        py_ref[name] = ref(name)
    return py_ref[name]


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urlparse(self.path)
        qs = parse_qs(parsed.query)
        path = parsed.path.rstrip("/")

        if path == "/python-to-go":
            self._call(_go, qs)
        elif path == "/python-to-python":
            self._call(_py, qs)
        elif path == "/go-to-python":
            self._info_route("Go → Python", "go_all.go can call Python via localhost:9099", ["greet(name)", "reverse(s)"])
        else:
            self._info()

    def _call(self, source, qs):
        func = (qs.get("func") or [None])[0]
        if not func:
            self._error("missing func parameter", 400)
            return
        args = []
        for key, vals in qs.items():
            if key == "func" or not vals:
                continue
            v = vals[0]
            args.append(int(v) if key in ("i", "int") else float(v) if key in ("f", "float") else v)
        try:
            result = source(func)(*args)
            self._ok({"result": result})
        except Exception as e:
            self._error(str(e), 500)

    def _info_route(self, direction, how, exports):
        self._ok({"direction": direction, "how": how, "exports": exports})

    def _info(self):
        self._ok({
            "server": "Python All-in-One",
            "callwire": f"{HOST}:{CALLWIRE_PORT}",
            "http": f":{HTTP_PORT}",
            "endpoints": {
                "/python-to-go?func=upper&s=world": "Python client → Go server",
                "/python-to-python?func=greet&s=you": "Python client → Python server",
            },
        })

    def _ok(self, data):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

    def _error(self, msg, code=500):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"error": msg}).encode())

    def log_message(self, *a):
        pass


if __name__ == "__main__":
    print(f"[Python] callwire on {HOST}:{CALLWIRE_PORT}, HTTP on :{HTTP_PORT}")
    HTTPServer((HOST, HTTP_PORT), Handler).serve_forever()
