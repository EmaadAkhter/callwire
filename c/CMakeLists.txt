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
)

if(CALLWIRE_WITH_TLS)
    list(APPEND CALLWIRE_SOURCES src/tls.c)
    find_package(OpenSSL REQUIRED)
    add_compile_definitions(CALLWIRE_WITH_TLS)
endif()

# Vendored mpack (single-file msgpack implementation).
set(CALLWIRE_SOURCES ${CALLWIRE_SOURCES} third_party/mpack.c)
include_directories(${CMAKE_CURRENT_SOURCE_DIR}/third_party)

# Static library.
add_library(callwire_core_static STATIC ${CALLWIRE_SOURCES})
target_include_directories(callwire_core_static PUBLIC include)
if(CALLWIRE_WITH_TLS)
    target_link_libraries(callwire_core_static PUBLIC OpenSSL::SSL OpenSSL::Crypto)
endif()
set_target_properties(callwire_core_static PROPERTIES OUTPUT_NAME callwire_core)

# Shared library.
add_library(callwire_core_shared SHARED ${CALLWIRE_SOURCES})
target_include_directories(callwire_core_shared PUBLIC include)
if(CALLWIRE_WITH_TLS)
    target_link_libraries(callwire_core_shared PUBLIC OpenSSL::SSL OpenSSL::Crypto)
endif()
set_target_properties(callwire_core_shared PROPERTIES OUTPUT_NAME callwire_core)

# Install targets.
install(TARGETS callwire_core_static callwire_core_shared
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
