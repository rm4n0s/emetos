"""Emetos's unittest runner (SPEC 10.32, Python provider).

  python emetos_unittest.py discover [-s DIR] [-p PATTERN] [-t TOPDIR]
  python emetos_unittest.py run [-s DIR] [-p PATTERN] [-t TOPDIR] [ID ...]

It prints JSON lines on the original standard output, each after the marker ``@@EMETOS@@ `` (the lines are those of emetos_pytest.py):

  {"type": "discovery", "tests": [{"id", "file", "line", "classes", "func", "param", "module"}]}
  {"type": "collect_error", "file", "message", "text", "line"}   (a module that could not be imported)
  {"type": "start", "id"}
  {"type": "result", "id", "outcome", "duration", "messages", "output"}
  {"type": "done", "exitstatus"}

Written for Emetos from what vscode-python's python_files/unittestadapter reports; the source of vscode-python was not at hand.
"""

import argparse
import inspect
import json
import os
import re
import sys
import time
import traceback
import unittest

MARK = "@@EMETOS@@ "
_OUT = os.fdopen(os.dup(1), "w", encoding="utf-8", errors="replace")
_RANK = {"passed": 0, "skipped": 1, "failed": 2, "errored": 3}
_LIMIT = 4000


def emit(event):
    _OUT.write(MARK + json.dumps(event, ensure_ascii=False, default=str) + "\n")
    _OUT.flush()


def is_library(path):
    p = str(path).replace("\\", "/")
    return (
        p.startswith("<")
        or "/site-packages/" in p
        or "/dist-packages/" in p
        or "/unittest/" in p
        or "/lib/python" in p
        or "/lib64/python" in p
    )


def flatten(suite):
    for test in suite:
        if isinstance(test, unittest.TestSuite):
            yield from flatten(test)
        else:
            yield test


def source_line(obj):
    try:
        return inspect.getsourcelines(inspect.unwrap(obj))[1]
    except (OSError, TypeError, ValueError):
        return 0


def describe(test):
    cls = type(test)
    module = sys.modules.get(cls.__module__)
    path = getattr(module, "__file__", None)
    if not path:
        try:
            path = inspect.getsourcefile(cls)
        except TypeError:
            path = None
    method = getattr(test, "_testMethodName", None) or test.id().rsplit(".", 1)[-1]
    fn = getattr(cls, method, None)
    line = source_line(fn) if fn is not None else 0
    return {
        "id": test.id(),
        "file": os.path.abspath(path) if path else "",
        "line": line,
        "classes": [{"name": cls.__name__, "line": source_line(cls)}],
        "func": method,
        "param": "",
        "module": cls.__module__,
    }


def exception_text(exc):
    return "".join(traceback.format_exception(type(exc), exc, exc.__traceback__))


FILE_LINE = re.compile(r'File "([^"]+)", line (\d+)')


def failed_import(test, start_dir):
    # The loader keeps the ImportError, whose text holds the traceback of the import.
    exc = getattr(test, "_exception", None)
    name = getattr(test, "_testMethodName", "")
    text = str(exc) if exc is not None else name
    lines = [l for l in text.splitlines() if l.strip()]
    path = os.path.join(start_dir, *name.split(".")) + ".py"
    line = 0
    for m in reversed(list(FILE_LINE.finditer(text))):
        if not is_library(m.group(1)):
            path, line = m.group(1), int(m.group(2))
            break
    emit(
        {
            "type": "collect_error",
            "file": os.path.abspath(path),
            "line": line,
            "message": lines[-1].strip() if lines else name,
            "text": text,
        }
    )


def make_loader(args):
    top = os.path.abspath(args.top_level_dir or args.start_dir)
    if top not in sys.path:
        sys.path.insert(0, top)
    cwd = os.getcwd()
    if cwd not in sys.path:
        sys.path.insert(0, cwd)
    return unittest.TestLoader(), top


def discover_suite(args):
    loader, top = make_loader(args)
    return loader.discover(args.start_dir, pattern=args.pattern, top_level_dir=args.top_level_dir)


def discover(args):
    tests = []
    for test in flatten(discover_suite(args)):
        if isinstance(test, unittest.loader._FailedTest):
            failed_import(test, os.path.abspath(args.start_dir))
        else:
            tests.append(describe(test))
    emit({"type": "discovery", "rootdir": os.getcwd(), "tests": tests})
    emit({"type": "done", "exitstatus": 0})
    return 0


class Result(unittest.TestResult):
    def __init__(self):
        super().__init__()
        self.started = {}
        self.records = {}

    def startTest(self, test):
        super().startTest(test)
        self.started[test.id()] = time.perf_counter()
        self.records[test.id()] = {"outcome": None, "messages": []}
        emit({"type": "start", "id": test.id()})

    def record(self, test, outcome, err=None, reason=None, label=None):
        rec = self.records.setdefault(test.id(), {"outcome": None, "messages": []})
        if outcome != "passed":
            rec["messages"].append(self.message(test, err, reason, label))
        if rec["outcome"] is None or _RANK[outcome] > _RANK[rec["outcome"]]:
            rec["outcome"] = outcome

    def message(self, test, err, reason, label):
        m = {}
        if err is None:
            m["message"] = reason or ""
            return m
        etype, value, tb = err
        try:
            m["text"] = self._exc_info_to_string(err, test)
        except Exception:
            m["text"] = exception_text(value)
        lines = traceback.format_exception_only(etype, value)
        text = "".join(lines).strip()
        m["message"] = (label + ": " if label else "") + (text if len(text) <= _LIMIT else text[:_LIMIT] + "...")
        frames = []
        for fs in reversed(traceback.extract_tb(tb)):
            if is_library(fs.filename):
                continue
            frames.append({"file": os.path.abspath(fs.filename), "line": fs.lineno, "name": fs.name})
            if len(frames) >= 30:
                break
        m["frames"] = frames
        if frames:
            m["file"], m["line"] = frames[0]["file"], frames[0]["line"]
        return m

    def addSuccess(self, test):
        super().addSuccess(test)
        self.record(test, "passed")

    def addFailure(self, test, err):
        super().addFailure(test, err)
        self.record(test, "failed", err)

    def addError(self, test, err):
        super().addError(test, err)
        if isinstance(test, unittest.suite._ErrorHolder):
            # setUpClass, setUpModule and the like: no test started, the holder names the class or the module
            self.holder(test, err)
        else:
            self.record(test, "errored", err)

    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        self.record(test, "skipped", reason=reason)

    def addExpectedFailure(self, test, err):
        super().addExpectedFailure(test, err)
        self.record(test, "skipped", reason="expected failure")

    def addUnexpectedSuccess(self, test):
        super().addUnexpectedSuccess(test)
        self.record(test, "failed", reason="unexpected success")

    def addSubTest(self, test, subtest, err):
        super().addSubTest(test, subtest, err)
        if err is not None:
            kind = "failed" if issubclass(err[0], test.failureException) else "errored"
            self.record(test, kind, err, label=str(subtest))

    def holder(self, test, err):
        emit(
            {
                "type": "result",
                "id": test.id(),
                "holder": True,
                "outcome": "errored",
                "duration": 0,
                "messages": [self.message(test, err, None, None)],
                "output": "",
            }
        )

    def stopTest(self, test):
        rec = self.records.pop(test.id(), None)
        took = time.perf_counter() - self.started.pop(test.id(), time.perf_counter())
        super().stopTest(test)
        if rec is not None:
            emit(
                {
                    "type": "result",
                    "id": test.id(),
                    "outcome": rec["outcome"] or "passed",
                    "duration": took,
                    "messages": rec["messages"],
                    "output": "",
                }
            )


def run(args):
    if args.ids:
        loader, top = make_loader(args)
        suite = loader.loadTestsFromNames(args.ids)
    else:
        suite = discover_suite(args)
    runnable = unittest.TestSuite()
    for test in flatten(suite):
        if isinstance(test, unittest.loader._FailedTest):
            failed_import(test, os.path.abspath(args.start_dir))
        else:
            runnable.addTest(test)
    result = Result()
    runnable.run(result)
    status = 0 if result.wasSuccessful() else 1
    emit({"type": "done", "exitstatus": status})
    return status


def main(argv):
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=["discover", "run"])
    parser.add_argument("-s", "--start-directory", dest="start_dir", default=".")
    parser.add_argument("-p", "--pattern", default="test*.py")
    parser.add_argument("-t", "--top-level-directory", dest="top_level_dir", default=None)
    parser.add_argument("ids", nargs="*")
    args = parser.parse_intermixed_args(argv)
    return discover(args) if args.command == "discover" else run(args)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
