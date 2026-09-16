#!/usr/bin/env python3
import os
import re
import shlex
import subprocess
import sys

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "../.."))
BOOTSTRAP_SCRIPT = os.path.join(REPO_ROOT, "scripts", "bootstrap-mihomo-acceptance.sh")

SHELL_BUILTINS_KEYWORDS = {
    "set", "echo", "exit", "return", "exec", "local", "export", "read",
    "mapfile", "cd", "trap", "shift", "command", "if", "fi", "then",
    "else", "elif", "for", "in", "do", "done", "while", "until", "case",
    "esac", "[", "[[", "]", "]]", "test", "true", "false", ":", "break", "continue",
    "kill", "wait"
}

DECLARED_SCRIPT_FUNCTIONS = {
    "verify_file", "publish_cache", "cleanup"
}

ALL_NON_EXTERNAL = SHELL_BUILTINS_KEYWORDS | DECLARED_SCRIPT_FUNCTIONS

def get_declared_required_cmds(script_path=BOOTSTRAP_SCRIPT):
    res = subprocess.run(
        ["bash", script_path, "--list-required-cmds"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=True
    )
    return set(res.stdout.strip().split())

def extract_command_substitutions(text: str):
    """Extract all $(...), <(...), >(...) substitutions, handling nesting and quotes."""
    substitutions = []
    i = 0
    n = len(text)
    while i < n - 1:
        prefix = text[i:i+2]
        if prefix in ('$(', '<(', '>('):
            start = i + 2
            depth = 1
            j = start
            in_single_quote = False
            in_double_quote = False
            while j < n and depth > 0:
                ch = text[j]
                if ch == '\\':
                    j += 2
                    continue
                if ch == "'" and not in_double_quote:
                    in_single_quote = not in_single_quote
                elif ch == '"' and not in_single_quote:
                    in_double_quote = not in_double_quote
                elif not in_single_quote and not in_double_quote:
                    if j + 1 < n and text[j:j+2] in ('$(', '<(', '>('):
                        depth += 1
                        j += 1
                    elif ch == '(':
                        depth += 1
                    elif ch == ')':
                        depth -= 1
                j += 1
            if depth == 0:
                inner = text[start:j-1]
                substitutions.append(inner)
                substitutions.extend(extract_command_substitutions(inner))
                i = j
                continue
        i += 1
    return substitutions

def _extract_from_segment(seg: str):
    found = set()
    try:
        tokens = shlex.split(seg)
    except Exception:
        tokens = seg.split()

    if not tokens:
        return found

    # Strip leading control keywords or compound syntax
    while tokens and tokens[0] in ("if", "elif", "then", "else", "!", "{", "}"):
        tokens = tokens[1:]

    # Strip leading variable assignments (e.g. FOO=1 BAR=2)
    while tokens and re.match(r'^[a-zA-Z_][a-zA-Z0-9_]*=.*$', tokens[0]):
        tokens = tokens[1:]

    # Strip leading redirections (e.g. >out, <in, 2>/dev/null)
    while tokens and (tokens[0].startswith(">") or tokens[0].startswith("<") or re.match(r'^[0-9]+[><]', tokens[0])):
        tokens = tokens[1:]

    if not tokens:
        return found

    first = tokens[0].strip("\"'()\\")
    if not first or first in ALL_NON_EXTERNAL or first.isdigit() or first.startswith("$"):
        return found

    # Skip syntax characters and inline snippets
    if "." in first or "(" in first or ")" in first or ";" in first or "=" in first:
        return found

    cmd_basename = os.path.basename(first)
    if cmd_basename not in ALL_NON_EXTERNAL:
        found.add(cmd_basename)

    return found

def extract_commands(text: str):
    """Extract invoked external command names from shell script content."""
    invoked = set()

    # 1. Process all command and process substitutions
    for sub in extract_command_substitutions(text):
        for part in re.split(r'[|;&]+', sub):
            part = part.strip()
            if not part:
                continue
            invoked.update(_extract_from_segment(part))

    # 2. Process all lines
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        # Remove comment portion: any # preceded by whitespace
        line = re.split(r'\s+#', line)[0].strip()
        if not line:
            continue
        if re.match(r'^[a-zA-Z0-9_]+\s*\(\)\s*\{', line):
            continue

        # Split line by command separators: ;, &&, ||, |, &
        segments = re.split(r'(?:;|&&|\|\||\||&)', line)
        for seg in segments:
            seg = seg.strip()
            if not seg or seg.startswith("#"):
                continue
            invoked.update(_extract_from_segment(seg))

    return invoked

def check_command_parity(declared: set, invoked: set):
    undeclared = invoked - declared
    unused_declared = declared - invoked
    return undeclared, unused_declared

def main():
    declared = get_declared_required_cmds()
    with open(BOOTSTRAP_SCRIPT, "r", encoding="utf-8") as f:
        content = f.read()

    invoked = extract_commands(content)
    undeclared, unused_declared = check_command_parity(declared, invoked)

    has_error = False
    if undeclared:
        sys.stderr.write(f"ERROR: External commands invoked but not declared in REQUIRED_CMDS: {sorted(undeclared)}\n")
        has_error = True

    if unused_declared:
        sys.stderr.write(f"ERROR: External commands declared in REQUIRED_CMDS but not invoked in script: {sorted(unused_declared)}\n")
        has_error = True

    if has_error:
        sys.exit(1)

    print(f"SUCCESS: Automated static analysis confirmed exact parity: all {len(invoked)} invoked external commands are declared and used in REQUIRED_CMDS: {sorted(invoked)}")

if __name__ == "__main__":
    main()
