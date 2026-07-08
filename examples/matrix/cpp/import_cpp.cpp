// C++ import script: calls "add"(10,20) on every OTHER language's matrix
// export server (best-effort — SKIP if a port isn't reachable).
#include "callwire.hpp"
#include <iostream>
#include <vector>
#include <utility>

static void init() {} // no setup needed for a pure client script

int main() {
    init();

    std::vector<std::pair<std::string, int>> targets = {
        {"go", 9101}, {"python", 9102}, {"rust", 9103}, {"ts", 9104},
        {"java", 9105}, {"c", 9106}, {"swift", 9108}, {"cobol", 9109},
    };

    for (auto &[name, port] : targets) {
        try {
            callwire::Client client("127.0.0.1", port);
            int64_t result = client.call<int64_t>("add", 10, 20);
            std::cout << name << "\tOK  add(10,20) = " << result << "\n";
        } catch (const callwire::CallwireException &e) {
            std::cout << name << "\tSKIP (" << e.what() << ")\n";
        }
    }

    return 0;
}
