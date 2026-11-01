The project of the real-server tests of the Python pack (build tags emetos_pyright and emetos_pylsp, SPEC §10.34 "Tests"): main.py has a
type error (line 5) and an undefined name (line 6), and imports emetoslib, a package that exists only in the .venv the test makes for its
copy of the folder (a venv holds absolute paths, so it cannot be kept here).
