cmake_minimum_required(VERSION 3.16)
project(CallwireCpp CXX C)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_C_STANDARD 99)

find_package(Threads REQUIRED)

set(CALLWIRE_C_CORE_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../c")

# The C++ SDK is header-only; it links directly against the C core sources
# (no prebuilt library dependency required for this repo layout).
set(CALLWIRE_CORE_SOURCES
    ${CALLWIRE_C_CORE_DIR}/src/codec.c
    ${CALLWIRE_C_CORE_DIR}/src/framing.c
    ${CALLWIRE_C_CORE_DIR}/src/client.c
    ${CALLWIRE_C_CORE_DIR}/src/server.c
    ${CALLWIRE_C_CORE_DIR}/src/errors.c
)

add_library(callwire_cpp INTERFACE)
target_include_directories(callwire_cpp INTERFACE include/callwire ${CALLWIRE_C_CORE_DIR}/include)

enable_testing()

add_executable(test_loopback_cpp tests/test_loopback.cpp ${CALLWIRE_CORE_SOURCES})
target_include_directories(test_loopback_cpp PRIVATE include/callwire ${CALLWIRE_C_CORE_DIR}/include)
target_link_libraries(test_loopback_cpp PRIVATE Threads::Threads)
add_test(NAME cpp_loopback COMMAND test_loopback_cpp)
