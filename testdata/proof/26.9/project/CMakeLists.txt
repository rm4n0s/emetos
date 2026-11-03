cmake_minimum_required(VERSION 3.16)
project(shapes LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
enable_testing()

add_library(shapes src/shapes.cpp)
target_include_directories(shapes PUBLIC src)

add_executable(shapes_test tests/shapes_test.cpp)
target_link_libraries(shapes_test shapes)
target_compile_options(shapes_test PRIVATE -fmacro-prefix-map=${CMAKE_SOURCE_DIR}/=)

add_test(NAME Shapes.Square COMMAND shapes_test square)
add_test(NAME Shapes.Perimeter COMMAND shapes_test perimeter)
add_test(NAME Shapes.Scaled COMMAND shapes_test scaled)
add_test(NAME Shapes.Describe COMMAND shapes_test describe)
add_test(NAME Display.Window COMMAND shapes_test window)

set_tests_properties(Display.Window PROPERTIES SKIP_RETURN_CODE 77)
set_tests_properties(Shapes.Square Shapes.Perimeter PROPERTIES LABELS "fast")
