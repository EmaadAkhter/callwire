// End-to-end C++ SDK test: real server thread + real client over TCP.
#include "callwire.hpp"
#include <cassert>
#include <iostream>
#include <thread>
#include <chrono>

static const int TEST_PORT = 19199;

int main() {
    callwire::Server server("0.0.0.0", TEST_PORT);

    server.exportFunc("add", [](const std::vector<callwire::Value> &args) -> callwire::Value {
        return callwire::Value(args[0].asInt64() + args[1].asInt64());
    });

    server.exportFunc("greet", [](const std::vector<callwire::Value> &args) -> callwire::Value {
        return callwire::Value("Hello, " + args[0].asString() + "!");
    });

    // A second closure capturing external state — exercises that each
    // registration gets its own routed handler, not a shared dispatch slot.
    int callCount = 0;
    server.exportFunc("counter", [&callCount](const std::vector<callwire::Value> &) -> callwire::Value {
        return callwire::Value(static_cast<int64_t>(++callCount));
    });

    std::thread serverThread([&server]() { server.serve(); });
    std::this_thread::sleep_for(std::chrono::milliseconds(100));

    {
        callwire::Client client("127.0.0.1", TEST_PORT);

        auto result = client.call("add", {callwire::Value(10), callwire::Value(20)});
        assert(result.asInt64() == 30);
        std::cout << "test_unary_add: OK\n";

        auto greeting = client.call("greet", {callwire::Value("World")});
        assert(greeting.asString() == "Hello, World!");
        std::cout << "test_unary_greet: OK\n";

        auto c1 = client.call("counter");
        auto c2 = client.call("counter");
        assert(c1.asInt64() == 1);
        assert(c2.asInt64() == 2);
        std::cout << "test_closure_state: OK (counter reached " << c2.asInt64() << ")\n";

        try {
            client.call("nonexistent");
            assert(false && "expected CallwireException");
        } catch (const callwire::CallwireException &e) {
            std::cout << "test_not_found: OK (" << e.what() << ")\n";
        }
    }

    server.close();
    serverThread.join();

    std::cout << "All C++ loopback tests passed.\n";
    return 0;
}
