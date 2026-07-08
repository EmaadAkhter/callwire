cmake_minimum_required(VERSION 3.16)
project(CallwireCore C)

set(CMAKE_C_STANDARD 99)
set(CMAKE_C_STANDARD_REQUIRED ON)

# Option to build with TLS support (default ON).
option(CALLWIRE_WITH_TLS "Build with TLS/mTLS support using OpenSSL" ON)

# Include directories.
include_directories(${CMAKE_CURRENT_SOURCE_DIR}/include)

# Source files.
set(CALLWIRE_SOURCES
    src/framing.c
    src/codec.c
    src/client.c
    src/server.c
    src/errors.c
    src/convenience.c
)

if(CALLWIRE_WITH_TLS)
    # tls.c is a stub (OpenSSL integration not yet implemented) — excluded
    # from the default build until it does real work. Re-enable once tls.c
    # has a working implementation (see IMPLEMENTATION_STATUS.md).
    # list(APPEND CALLWIRE_SOURCES src/tls.c)
    # find_package(OpenSSL REQUIRED)
    # add_compile_definitions(CALLWIRE_WITH_TLS)
endif()

# mpack_stub.h is header-only (no third_party/mpack.c to compile).
include_directories(${CMAKE_CURRENT_SOURCE_DIR}/third_party)

find_package(Threads REQUIRED)

# Static library.
add_library(callwire_core_static STATIC ${CALLWIRE_SOURCES})
target_include_directories(callwire_core_static PUBLIC include)
target_link_libraries(callwire_core_static PUBLIC Threads::Threads)
if(CALLWIRE_WITH_TLS)
    target_link_libraries(callwire_core_static PUBLIC OpenSSL::SSL OpenSSL::Crypto)
endif()
set_target_properties(callwire_core_static PROPERTIES OUTPUT_NAME callwire_core)

# Shared library.
add_library(callwire_core_shared SHARED ${CALLWIRE_SOURCES})
target_include_directories(callwire_core_shared PUBLIC include)
target_link_libraries(callwire_core_shared PUBLIC Threads::Threads)
if(CALLWIRE_WITH_TLS)
    target_link_libraries(callwire_core_shared PUBLIC OpenSSL::SSL OpenSSL::Crypto)
endif()
set_target_properties(callwire_core_shared PROPERTIES OUTPUT_NAME callwire_core)

# callwire init CLI — scans the repo for worker directories and generates
# callwire.toml, matching the CLI shipped with every other SDK.
add_executable(callwire src/cli.c)

# Install targets.
install(TARGETS callwire_core_static callwire_core_shared callwire
        LIBRARY DESTINATION lib
        ARCHIVE DESTINATION lib
        RUNTIME DESTINATION bin)
install(FILES include/callwire.h DESTINATION include)

# Export CMake package config so downstream projects can find_package(CallwireCore).
set(CALLWIRE_CORE_INCLUDE_DIRS "${CMAKE_CURRENT_SOURCE_DIR}/include")
set(CALLWIRE_CORE_LIBRARY_DIRS "${CMAKE_CURRENT_BINARY_DIR}")
configure_file(CallwireCoreConfig.cmake.in CallwireCoreConfig.cmake @ONLY)
install(FILES "${CMAKE_CURRENT_BINARY_DIR}/CallwireCoreConfig.cmake" DESTINATION lib/cmake/CallwireCore)

# Basic smoke tests (optional).
enable_testing()

add_executable(test_framing tests/test_framing.c src/framing.c src/errors.c)
target_include_directories(test_framing PRIVATE include)
add_test(NAME framing COMMAND test_framing)

add_executable(test_codec tests/test_codec.c src/codec.c)
target_include_directories(test_codec PRIVATE include)
add_test(NAME codec COMMAND test_codec)

add_executable(test_loopback tests/test_loopback.c src/codec.c src/framing.c src/client.c src/server.c src/errors.c)
target_include_directories(test_loopback PRIVATE include)
target_link_libraries(test_loopback PRIVATE Threads::Threads)
add_test(NAME loopback COMMAND test_loopback)

add_executable(test_convenience tests/test_convenience.c src/codec.c src/framing.c src/client.c src/server.c src/errors.c src/convenience.c)
target_include_directories(test_convenience PRIVATE include)
target_link_libraries(test_convenience PRIVATE Threads::Threads)
add_test(NAME convenience COMMAND test_convenience)
