// C++ client calling a local server (any language) on localhost:9090.
//
// Build (compile the C core sources as C, then link — a single g++ command
// mixing .c and .cpp inputs will misparse the .c files as C++):
//   for f in codec framing client server errors; do
//     gcc -std=c99 -pthread -Ic/include -c c/src/$f.c -o /tmp/$f.o
//   done
//   g++ -std=c++17 -pthread -Icpp/include/callwire -Ic/include \
//     examples/1_standalone/cpp_client.cpp /tmp/{codec,framing,client,server,errors}.o \
//     -o cpp_client
//
// Run: ./cpp_client
#include "callwire.hpp"
#include <iostream>

int main() {
    callwire::Client client("localhost", 9090);

    auto result = client.call("add", {callwire::Value(10), callwire::Value(20)});
    std::cout << "add(10, 20) = " << result.asInt64() << "\n";

    auto greeting = client.call("greet", {callwire::Value("World")});
    std::cout << "greet(\"World\") = " << greeting.asString() << "\n";

    return 0;
}
