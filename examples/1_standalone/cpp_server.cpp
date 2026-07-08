// C++ server exporting "add", "greet" functions.
// Can be called from Go, Python, Rust, TypeScript, Java, or C++ clients.
//
// Build (compile the C core sources as C, then link — a single g++ command
// mixing .c and .cpp inputs will misparse the .c files as C++):
//   for f in codec framing client server errors; do
//     gcc -std=c99 -pthread -Ic/include -c c/src/$f.c -o /tmp/$f.o
//   done
//   g++ -std=c++17 -pthread -Icpp/include/callwire -Ic/include \
//     examples/1_standalone/cpp_server.cpp /tmp/{codec,framing,client,server,errors}.o \
//     -o cpp_server
//
// Run: ./cpp_server
#include "callwire.hpp"
#include <iostream>

int main() {
    callwire::Server server("0.0.0.0", 9090);

    server.exportFunc("add", [](const std::vector<callwire::Value> &args) -> callwire::Value {
        return callwire::Value(args[0].asInt64() + args[1].asInt64());
    });

    server.exportFunc("greet", [](const std::vector<callwire::Value> &args) -> callwire::Value {
        return callwire::Value("Hello, " + args[0].asString() + "!");
    });

    std::cout << "Callwire C++ server listening on :9090\n";
    server.serve();
    return 0;
}
