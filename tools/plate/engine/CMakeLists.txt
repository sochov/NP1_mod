cmake_minimum_required(VERSION 3.16)
project(np1_plate_engine C CXX)

set(TFLITE_SOURCE_DIR "" CACHE PATH "TensorFlow source tree (pinned by build_engine.sh)")
set(JSON_INCLUDE_DIR "" CACHE PATH "Directory containing nlohmann/json.hpp")
set(TFLITE_VERSION_LABEL "unknown" CACHE STRING "Version label reported by the engine")

if(NOT EXISTS "${TFLITE_SOURCE_DIR}/tensorflow/lite/CMakeLists.txt")
  message(FATAL_ERROR "TFLITE_SOURCE_DIR must point to a TensorFlow checkout")
endif()

add_subdirectory("${TFLITE_SOURCE_DIR}/tensorflow/lite" "${CMAKE_CURRENT_BINARY_DIR}/tensorflow-lite" EXCLUDE_FROM_ALL)

add_executable(plate-engine plate_engine.cc)
set_target_properties(plate-engine PROPERTIES CXX_STANDARD 17 CXX_STANDARD_REQUIRED ON)
target_include_directories(plate-engine PRIVATE "${JSON_INCLUDE_DIR}")
target_compile_definitions(plate-engine PRIVATE TFLITE_VERSION_LABEL="${TFLITE_VERSION_LABEL}")
target_link_libraries(plate-engine PRIVATE tensorflow-lite)
