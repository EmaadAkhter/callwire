// C++ export script: exports "add" on a fixed port. init() performs setup
// and is called first thing in main().
#include "callwire.hpp"
#include <iostream>
#include <memory>

static const int MATRIX_PORT = 9107;
static std::unique_ptr<callwire::Server> g_server;

static void init() {
    g_server = std::make_unique<callwire::Server>("0.0.0.0", MATRIX_PORT);
    g_server->exportFunc("add", [](int64_t a, int64_t b) { return a + b; });
}

int main() {
    init();
    std::cout << "C++ matrix export listening on :" << MATRIX_PORT << "\n";
    g_server->serve();
    return 0;
}
