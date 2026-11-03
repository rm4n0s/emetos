cmake_minimum_required(VERSION 3.19)
project(calc LANGUAGES C CXX)
enable_testing()

add_executable(adds src/adds.c)
add_executable(divides src/divides.cpp)
add_executable(skipped src/skipped.c)

add_test(NAME adds COMMAND adds)
add_test(NAME divides COMMAND divides 5 2)
add_test(NAME skipped COMMAND skipped)
add_test(NAME disabled COMMAND adds)
add_test(NAME math.lists COMMAND adds WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}/src)
add_test(NAME "odd[1]+x(2)" COMMAND adds)

set_tests_properties(skipped PROPERTIES SKIP_RETURN_CODE 77)
set_tests_properties(disabled PROPERTIES DISABLED TRUE)
set_tests_properties(math.lists PROPERTIES LABELS "fast;math")
