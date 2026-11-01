A small CMake project for the tests of the C and C++ pack (step 24b.3). compile_commands.json is what CMake writes
(CMAKE_EXPORT_COMPILE_COMMANDS); it holds absolute paths, so the tests make it from compile_commands.json.in for the folder they copy
the project to (@DIR@).
