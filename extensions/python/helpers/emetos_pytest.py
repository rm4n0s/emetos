"""Emetos's pytest plugin (SPEC 10.32, Python provider).

Passed to pytest with ``-p emetos_pytest``. It reports what Emetos's Testing view needs as JSON lines on the original standard output,
each after the marker ``@@EMETOS@@ ``:

  {"type": "discovery", "rootdir": ..., "tests": [{"id", "file", "line", "classes", "func", "param"}]}   (with --collect-only)
  {"type": "collect_error", "file", "message", "text", "line"}   (a file that could not be collected)
  {"type": "start", "id"}                                        (a test starts)
  {"type": "result", "id", "outcome", "duration", "messages", "output"}   (outcome: passed, failed, errored or skipped)
  {"type": "done", "exitstatus"}

All lines are counted from 1. Everything else pytest and the tests print is not touched: it is the output of the run.

Written for Emetos from what vscode-python's python_files/vscode_pytest reports (the discovery tree, the outcome of each test, its
message and traceback); the source of vscode-python was not at hand.
"""

import json
import os
import re

import pytest

MARK = "@@EMETOS@@ "

# The original standard output, duplicated before pytest starts capturing the file descriptors, so that the events reach the parent
# even while a test is being captured.
_OUT = os.fdopen(os.dup(1), "w", encoding="utf-8", errors="replace")

_RANK = {"passed": 0, "skipped": 1, "failed": 2, "errored": 3}
_LIMIT = 4000

_state = {
    "rootdir": "",
    "discovered": False,
    "frames": {},
    "compare": {},
    "last_compare": None,
    "results": {},
}


def _emit(event):
    _OUT.write(MARK + json.dumps(event, ensure_ascii=False, default=str) + "\n")
    _OUT.flush()


def _short(value):
    try:
        text = repr(value)
    except Exception:
        text = "<unrepresentable>"
    return text if len(text) <= _LIMIT else text[:_LIMIT] + "..."


def _is_library(path):
    p = str(path).replace("\\", "/")
    return (
        p.startswith("<")
        or "/site-packages/" in p
        or "/dist-packages/" in p
        or "/_pytest/" in p
        or "/pluggy/" in p
        or "/lib/python" in p
        or "/lib64/python" in p
    )


def _absolute(path):
    path = str(path)
    if os.path.isabs(path):
        return path
    return os.path.abspath(os.path.join(_state["rootdir"] or os.getcwd(), path))


def pytest_configure(config):
    _state["rootdir"] = str(config.rootpath)


@pytest.hookimpl(tryfirst=True)
def pytest_assertrepr_compare(config, op, left, right):
    # Remember the two sides of the last failed comparison; the explanation is left to pytest.
    if op == "==":
        _state["last_compare"] = (_short(left), _short(right))
    return None


def _describe(item):
    try:
        _, line, _ = item.reportinfo()
    except Exception:
        line = None
    classes = []
    for node in item.listchain():
        if isinstance(node, pytest.Class):
            try:
                cline = node.reportinfo()[1]
            except Exception:
                cline = None
            classes.append({"name": node.name, "line": (cline + 1) if isinstance(cline, int) else 0})
    func = getattr(item, "originalname", None) or item.name
    param = item.name[len(func):] if item.name.startswith(func) else ""
    return {
        "id": item.nodeid,
        "file": _absolute(item.path),
        "line": (line + 1) if isinstance(line, int) else 0,
        "classes": classes,
        "func": func,
        "param": param,
    }


def _discovered(session):
    if _state["discovered"]:
        return
    _state["discovered"] = True
    _emit({"type": "discovery", "rootdir": _state["rootdir"], "tests": [_describe(i) for i in session.items]})


def pytest_collection_finish(session):
    if session.config.option.collectonly:
        _discovered(session)


def pytest_sessionfinish(session, exitstatus):
    if session.config.option.collectonly:
        _discovered(session)
    _emit({"type": "done", "exitstatus": int(exitstatus)})


def _error_line(text):
    # The line of the exception itself ("E   SyntaxError: ..."), not the lines of its context.
    errors = [l for l in text.splitlines() if l.startswith("E ")]
    for line in reversed(errors):
        if line.startswith("E   ") and not line[4:5].isspace():
            return line[1:].strip()
    if errors:
        return errors[-1][1:].strip()
    lines = [l for l in text.splitlines() if l.strip()]
    return lines[-1].strip() if lines else ""


_FILE_LINE = re.compile(r'File "([^"]+)", line (\d+)')


def _locate(text):
    # The last place of the user's code a traceback names.
    for m in reversed(list(_FILE_LINE.finditer(text))):
        if not _is_library(m.group(1)):
            return m.group(1), int(m.group(2))
    return None, 0


def _crash(report):
    crash = getattr(getattr(report, "longrepr", None), "reprcrash", None)
    if crash is None:
        return None, 0, None
    return getattr(crash, "path", None), getattr(crash, "lineno", 0), getattr(crash, "message", None)


def pytest_collectreport(report):
    if not report.failed:
        return
    path, line, message = _crash(report)
    text = report.longreprtext
    if not path:
        path, line = _locate(text)
    _emit(
        {
            "type": "collect_error",
            "file": _absolute(path or report.nodeid.split("::")[0]),
            "line": line or 0,
            "message": message or _error_line(text),
            "text": text,
        }
    )


def pytest_exception_interact(node, call, report):
    # The frames of the failure in the user's code (innermost first), and the sides of a failed comparison. pytest tells the report to
    # pytest_runtest_logreport first, so the messages are made when the test is finished.
    when = getattr(report, "when", None)
    if when is None or call.excinfo is None:
        return
    key = (node.nodeid, when)
    frames = []
    try:
        for entry in reversed(list(call.excinfo.traceback)):
            path = str(entry.path)
            if _is_library(path):
                continue
            frames.append({"file": _absolute(path), "line": entry.lineno + 1, "name": entry.name})
            if len(frames) >= 30:
                break
    except Exception:
        pass
    _state["frames"][key] = frames
    if isinstance(call.excinfo.value, AssertionError) and _state["last_compare"] is not None:
        _state["compare"][key] = _state["last_compare"]


def pytest_runtest_logstart(nodeid, location):
    _state["last_compare"] = None
    _state["results"][nodeid] = {"outcome": None, "duration": 0.0, "reports": [], "output": []}
    _emit({"type": "start", "id": nodeid})


def _message(report, nodeid):
    m = {}
    if report.skipped:
        lr = report.longrepr
        wasxfail = getattr(report, "wasxfail", None)
        if wasxfail is not None:
            m["message"] = "xfail" + (": " + wasxfail if wasxfail else "")
        else:
            reason = lr[2] if isinstance(lr, tuple) and len(lr) == 3 else str(lr)
            m["message"] = reason[len("Skipped: "):] if reason.startswith("Skipped: ") else reason
            if isinstance(lr, tuple) and len(lr) == 3:
                m["file"], m["line"] = _absolute(lr[0]), int(lr[1])
        return m
    text = report.longreprtext
    path, line, message = _crash(report)
    m["text"] = text
    m["message"] = message or _error_line(text)
    if path:
        m["file"], m["line"] = _absolute(path), line or 0
    key = (nodeid, report.when)
    m["frames"] = _state["frames"].pop(key, [])
    if key in _state["compare"]:
        m["actual"], m["expected"] = _state["compare"].pop(key)
    return m


def pytest_runtest_logreport(report):
    rec = _state["results"].get(report.nodeid)
    if rec is None:
        return
    rec["duration"] += report.duration
    for title, content in report.sections:
        if content:
            rec["output"].append("----- " + title + " -----\n" + content)
    outcome = None
    if report.when == "setup":
        if report.failed:
            outcome = "errored"
        elif report.skipped:
            outcome = "skipped"
    elif report.when == "call":
        outcome = "failed" if report.failed else ("skipped" if report.skipped else "passed")
    elif report.when == "teardown" and report.failed:
        outcome = "errored"
    if outcome is None:
        return
    if outcome in ("failed", "errored", "skipped"):
        rec["reports"].append(report)
    if rec["outcome"] is None or _RANK[outcome] > _RANK[rec["outcome"]]:
        rec["outcome"] = outcome


def pytest_runtest_logfinish(nodeid, location):
    rec = _state["results"].pop(nodeid, None)
    if rec is None:
        return
    _emit(
        {
            "type": "result",
            "id": nodeid,
            "outcome": rec["outcome"] or "passed",
            "duration": rec["duration"],
            "messages": [_message(r, nodeid) for r in rec["reports"]],
            "output": "\n".join(rec["output"]),
        }
    )
