import json
import os
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse, parse_qs

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "python"))

from callwire import export, configure, ref

HOST = os.getenv("CALLWIRE_DEMO_HOST", "localhost")
CALLWIRE_PORT = int(os.getenv("PY_CALLWIRE_PORT", "9099"))
HTTP_PORT = int(os.getenv("PY_HTTP_PORT", "8088"))
GO_CALLWIRE = os.getenv("GO_CALLWIRE_ADDR", "localhost:9098")

if ":" in GO_CALLWIRE:
    GO_HOST, GO_PORT_STR = GO_CALLWIRE.rsplit(":", 1)
else:
    GO_HOST, GO_PORT_STR = GO_CALLWIRE, "9098"
GO_PORT = int(GO_PORT_STR)

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
        go_ref[name] = ref(name, host=GO_HOST, port=GO_PORT)
    return go_ref[name]

def _py(name):
    if name not in py_ref:
        py_ref[name] = ref(name)
    return py_ref[name]

def _parse_args(qs):
    args = []
    for key in sorted(qs.keys()):
        if key == "func" or not qs[key]:
            continue
        v = qs[key][0]
        if key in ("i", "int"):
            args.append(int(v))
        elif key in ("f", "float"):
            args.append(float(v))
        else:
            args.append(v)
    return args


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urlparse(self.path)
        qs = parse_qs(parsed.query)
        path = parsed.path.rstrip("/")

        if path == "/python-to-go":
            self._call(_go, qs)
        elif path == "/python-to-python":
            self._call(_py, qs)
        elif path == "/health":
            self._health()
        elif path == "/demo":
            self._demo()
        elif path == "/go-to-python":
            self._info_route("Go → Python", "go_all.go can call Python via localhost:9099", ["greet(name)", "reverse(s)"])
        else:
            self._info()

    def _call(self, source, qs):
        func = (qs.get("func") or [None])[0]
        if not func:
            self._error("missing func parameter", 400)
            return
        args = _parse_args(qs)
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
                "/health": "status and linked services",
                "/demo": "demo calls you can run directly",
                "/python-to-go?func=upper&s=world": "Python client → Go server",
                "/python-to-python?func=greet&s=you": "Python client → Python server",
            },
        })

    def _health(self):
        self._ok({
            "status": "ok",
            "services": {
                "python_callwire": f"{HOST}:{CALLWIRE_PORT}",
                "python_http": f":{HTTP_PORT}",
                "go_callwire": GO_CALLWIRE,
            },
        })

    def _demo(self):
        self._ok({
            "overview": "Python all-in-one demo",
            "calls": [
                {"title": "Python -> Python (greet)", "url": "/python-to-python?func=greet&s=world"},
                {"title": "Python -> Python (reverse)", "url": "/python-to-python?func=reverse&s=hello"},
                {"title": "Python -> Go (double)", "url": "/python-to-go?func=double&i=10"},
                {"title": "Python -> Go (upper)", "url": "/python-to-go?func=upper&s=world"},
            ],
            "also_available": "Go-side demo routes are served at http://localhost:8089/demo",
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
