cmake_minimum_required(VERSION 3.14)
project(inagent VERSION 2.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# Optimization flags for minimal binary
set(CMAKE_CXX_FLAGS_RELEASE "-Os -ffunction-sections -fdata-sections")
if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
    set(CMAKE_EXE_LINKER_FLAGS_RELEASE "-Wl,--gc-sections")
endif()

# Source files
set(SOURCES
    src/main.cpp
    src/config/app_config.cpp
    src/model/types.cpp
    src/template/var_params.cpp
    src/template/render.cpp
    src/config_merge/merger.cpp
    src/daemon/daemon.cpp
    src/task/engine.cpp
    src/task/cron.cpp
    src/log/logger.cpp
    src/signal/signal_handler.cpp
    src/status/reporter.cpp
    src/util/fs.cpp
    src/util/string_util.cpp
    src/util/time_util.cpp
)

add_executable(inagent ${SOURCES})

target_include_directories(inagent PRIVATE
    ${CMAKE_SOURCE_DIR}/src
    ${CMAKE_SOURCE_DIR}/third_party/nlohmann/single_include
    ${CMAKE_SOURCE_DIR}/third_party/toml11/include
)

# yaml-cpp as static lib
set(YAML_CPP_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(YAML_CPP_BUILD_TOOLS OFF CACHE BOOL "" FORCE)
set(YAML_BUILD_SHARED_LIBS OFF CACHE BOOL "" FORCE)
add_subdirectory(third_party/yaml-cpp)
target_link_libraries(inagent PRIVATE yaml-cpp)

# pthread and atomic
target_link_libraries(inagent PRIVATE pthread)

# Static linking for release builds
if(STATIC_LINK)
    target_link_libraries(inagent PRIVATE -static)
    target_link_options(inagent PRIVATE -static)
endif()

# Strip binary for release
if(CMAKE_BUILD_TYPE STREQUAL "Release")
    add_custom_command(TARGET inagent POST_BUILD
        COMMAND ${CMAKE_STRIP} $<TARGET_FILE:inagent>
        COMMENT "Stripping inagent binary"
    )
endif()
