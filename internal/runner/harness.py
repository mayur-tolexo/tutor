"""Runs a student's main.py against a list of test cases and prints one JSON
result. Executed inside the sandbox by the Go runner; compatible with
Python 3.8+ so the same file works on every sandbox image and dev machine.

Usage: python3 harness.py spec.json
spec.json: {"time_limit_ms": int, "cases": [{"id", "stdin", "files": {path: text},
            "collect": [path]}]}
"""
import ast
import json
import os
import re
import shutil
import subprocess
import sys

OUTPUT_CAP = 64 * 1024
TRACEBACK_LINE = re.compile(r'File "[^"]*main\.py", line (\d+)')
LAST_EXC = re.compile(r"^(\w+(?:\.\w+)*)(?::\s?(.*))?$")


def cap(b):
    """Truncate captured output so a runaway print loop cannot flood the API."""
    if len(b) > OUTPUT_CAP:
        return b[:OUTPUT_CAP].decode("utf-8", "replace") + "\n... [output truncated]"
    return b.decode("utf-8", "replace")


def parse_error(stderr):
    """Extract the final exception type, message and main.py line from a traceback."""
    lines = [l for l in stderr.strip().splitlines() if l.strip()]
    if not lines:
        return None
    m = LAST_EXC.match(lines[-1].strip())
    if not m:
        return None
    err = {"type": m.group(1).split(".")[-1], "message": (m.group(2) or "").strip()}
    line_nums = TRACEBACK_LINE.findall(stderr)
    if line_nums:
        err["line"] = int(line_nums[-1])
    return err


class FlagVisitor(ast.NodeVisitor):
    """Collects mechanical signals about common beginner mistakes."""

    def __init__(self):
        self.calls = set()
        self.has_function = False
        self.flags = set()
        self.raw_input_names = set()
        self.numeric_use_names = set()

    def visit_FunctionDef(self, node):
        self.has_function = True
        has_print = any(
            isinstance(n, ast.Call) and isinstance(n.func, ast.Name) and n.func.id == "print"
            for n in ast.walk(node)
        )
        has_return = any(isinstance(n, ast.Return) and n.value is not None for n in ast.walk(node))
        if has_print and not has_return:
            self.flags.add("print_instead_of_return")
        self.generic_visit(node)

    visit_AsyncFunctionDef = visit_FunctionDef

    def visit_Call(self, node):
        if isinstance(node.func, ast.Name):
            self.calls.add(node.func.id)
        self.generic_visit(node)

    def visit_Assign(self, node):
        # `x = input(...)` with no int()/float() around it.
        v = node.value
        if isinstance(v, ast.Call) and isinstance(v.func, ast.Name) and v.func.id == "input":
            for t in node.targets:
                if isinstance(t, ast.Name):
                    self.raw_input_names.add(t.id)
        self.generic_visit(node)

    def visit_BinOp(self, node):
        if isinstance(node.op, ast.Div):
            self.flags.add("uses_float_division")
        if isinstance(node.op, ast.Add) and self._str_int_pair(node.left, node.right):
            self.flags.add("string_int_concat")
        for side in (node.left, node.right):
            if isinstance(side, ast.Name) and self._is_numeric(node.left if side is node.right else node.right):
                self.numeric_use_names.add(side.id)
        self.generic_visit(node)

    def visit_Compare(self, node):
        operands = [node.left] + list(node.comparators)
        for a in operands:
            if isinstance(a, ast.Name) and any(self._is_numeric(b) for b in operands if b is not a):
                self.numeric_use_names.add(a.id)
        self.generic_visit(node)

    def visit_ExceptHandler(self, node):
        if node.type is None:
            self.flags.add("bare_except")
        self.generic_visit(node)

    def visit_While(self, node):
        t = node.test
        is_true = isinstance(t, ast.Constant) and t.value is True
        if is_true and not any(isinstance(n, ast.Break) for n in ast.walk(node)):
            self.flags.add("while_true_no_break")
        self.generic_visit(node)

    def visit_For(self, node):
        if isinstance(node.target, ast.Name) and not node.target.id.startswith("_"):
            used = any(
                isinstance(n, ast.Name) and n.id == node.target.id and isinstance(n.ctx, ast.Load)
                for stmt in node.body for n in ast.walk(stmt)
            )
            if not used:
                self.flags.add("loop_variable_unused")
        self.generic_visit(node)

    @staticmethod
    def _is_numeric(node):
        return isinstance(node, ast.Constant) and isinstance(node.value, (int, float)) and not isinstance(node.value, bool)

    @classmethod
    def _str_int_pair(cls, a, b):
        def is_str(n):
            return isinstance(n, ast.Constant) and isinstance(n.value, str)

        def is_intlike(n):
            return cls._is_numeric(n) or (
                isinstance(n, ast.Call) and isinstance(n.func, ast.Name) and n.func.id in ("int", "len", "float")
            )

        return (is_str(a) and is_intlike(b)) or (is_str(b) and is_intlike(a))

    def result(self):
        if "input" not in self.calls:
            self.flags.add("no_input_call")
        if "print" not in self.calls:
            self.flags.add("no_print_call")
        if not self.has_function:
            self.flags.add("no_function_def")
        if "eval" in self.calls:
            self.flags.add("uses_eval")
        if self.raw_input_names & self.numeric_use_names:
            self.flags.add("input_not_converted")
        return sorted(self.flags)


def analyze(source):
    """Return (syntax_error, flags). A syntax error yields no flags."""
    try:
        tree = ast.parse(source, filename="main.py")
    except SyntaxError as e:
        return {"type": "SyntaxError", "message": e.msg or "invalid syntax", "line": e.lineno or 0}, []
    v = FlagVisitor()
    v.visit(tree)
    return None, v.result()


def run_case(case, source, time_limit_ms, idx):
    """Run one case in its own directory so cases cannot see each other's files."""
    d = os.path.join(os.getcwd(), "case_%d" % idx)
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "main.py"), "w", encoding="utf-8") as f:
        f.write(source)
    for rel, text in (case.get("files") or {}).items():
        p = os.path.normpath(os.path.join(d, rel))
        if not p.startswith(d):
            continue
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "w", encoding="utf-8") as f:
            f.write(text)

    out = {"id": case["id"], "stdout": "", "stderr": "", "exit_code": 0, "timed_out": False, "files": {}, "error": None}
    env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "PYTHONIOENCODING": "utf-8",
           "PYTHONDONTWRITEBYTECODE": "1", "LANG": "C.UTF-8"}
    try:
        p = subprocess.run(
            [sys.executable, "-I", "main.py"],
            input=case.get("stdin", "").encode("utf-8"),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            cwd=d, env=env, timeout=time_limit_ms / 1000.0,
        )
        out["stdout"], out["stderr"], out["exit_code"] = cap(p.stdout), cap(p.stderr), p.returncode
    except subprocess.TimeoutExpired as e:
        out["timed_out"] = True
        out["exit_code"] = -1
        out["stdout"] = cap(e.stdout or b"")
        out["stderr"] = cap(e.stderr or b"")
    if out["exit_code"] != 0 and not out["timed_out"]:
        out["error"] = parse_error(out["stderr"])
    for rel in case.get("collect") or []:
        p = os.path.normpath(os.path.join(d, rel))
        if p.startswith(d) and os.path.isfile(p):
            with open(p, "r", encoding="utf-8", errors="replace") as f:
                out["files"][rel] = f.read(OUTPUT_CAP)
    shutil.rmtree(d, ignore_errors=True)
    return out


def main():
    with open(sys.argv[1], encoding="utf-8") as f:
        spec = json.load(f)
    with open("main.py", encoding="utf-8") as f:
        source = f.read()
    syntax_error, flags = analyze(source)
    result = {"syntax_error": syntax_error, "flags": flags, "cases": []}
    # A file that does not parse fails every case the same way; skip execution.
    if syntax_error is None:
        for i, case in enumerate(spec.get("cases", [])):
            result["cases"].append(run_case(case, source, spec.get("time_limit_ms", 2000), i))
    sys.stdout.write(json.dumps(result))


if __name__ == "__main__":
    main()
