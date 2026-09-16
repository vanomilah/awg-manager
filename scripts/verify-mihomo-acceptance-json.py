#!/usr/bin/env python3
import argparse
import json
import sys

ALLOWED_ACTIONS = {"run", "pause", "cont", "pass", "fail", "skip", "output", "start"}

# Test states
TEST_INIT = "INIT"
TEST_RUNNING = "RUNNING"
TEST_PAUSED = "PAUSED"
TEST_PASSED = "PASSED"
TEST_FAILED = "FAILED"
TEST_SKIPPED = "SKIPPED"

# Package states
PKG_INIT = "INIT"
PKG_STARTED = "STARTED"
PKG_PASSED = "PASSED"
PKG_FAILED = "FAILED"
PKG_SKIPPED = "SKIPPED"


def verify_acceptance_json(json_path: str, target_test: str) -> int:
    events = []
    try:
        with open(json_path, "r", encoding="utf-8", errors="replace") as f:
            for line_no, line in enumerate(f, start=1):
                line = line.strip()
                if not line:
                    continue
                try:
                    ev = json.loads(line)
                except Exception as e:
                    sys.stderr.write(f"ERROR: Malformed JSON on line {line_no} in {json_path}: {e}\n")
                    return 1
                if not isinstance(ev, dict):
                    sys.stderr.write(f"ERROR: Line {line_no} is not a JSON object\n")
                    return 1
                act = ev.get("Action")
                if act not in ALLOWED_ACTIONS:
                    sys.stderr.write(f"ERROR: Unknown or unsupported action '{act}' on line {line_no}\n")
                    return 1
                events.append(ev)
    except Exception as e:
        sys.stderr.write(f"ERROR: Failed to read test json at {json_path}: {e}\n")
        return 1

    if not events:
        sys.stderr.write(f"ERROR: Empty event stream in {json_path}\n")
        return 1

    # Collision check and package resolution before dispatch
    test_packages = set()
    for ev in events:
        if ev.get("Test") == target_test:
            pkg = ev.get("Package")
            if pkg:
                test_packages.add(pkg)

    if len(test_packages) == 0:
        sys.stderr.write(f"ERROR: Target test '{target_test}' did not emit any events in {json_path}\n")
        return 1

    if len(test_packages) > 1:
        sys.stderr.write(
            f"ERROR: Test name collision: '{target_test}' appears in multiple packages: {sorted(test_packages)}\n"
        )
        return 1

    target_package = next(iter(test_packages))

    test_state = TEST_INIT
    pkg_state = PKG_INIT
    test_outputs = []

    for ev in events:
        action = ev.get("Action")
        ev_test = ev.get("Test")
        ev_pkg = ev.get("Package")

        # Events for the target test
        if ev_test == target_test:
            if test_state in (TEST_PASSED, TEST_FAILED, TEST_SKIPPED):
                sys.stderr.write(
                    f"ERROR: Target test received '{action}' after transitioning to terminal state '{test_state}'\n"
                )
                return 1

            if action == "run":
                if test_state != TEST_INIT:
                    sys.stderr.write(f"ERROR: Target test emitted duplicate 'run' (current state '{test_state}')\n")
                    return 1
                test_state = TEST_RUNNING
            elif action == "pause":
                if test_state != TEST_RUNNING:
                    sys.stderr.write(f"ERROR: Target test emitted 'pause' while in state '{test_state}'\n")
                    return 1
                test_state = TEST_PAUSED
            elif action == "cont":
                if test_state != TEST_PAUSED:
                    sys.stderr.write(f"ERROR: Target test emitted 'cont' while in state '{test_state}'\n")
                    return 1
                test_state = TEST_RUNNING
            elif action == "output":
                if test_state not in (TEST_RUNNING, TEST_PAUSED):
                    sys.stderr.write(f"ERROR: Target test output received before 'run'\n")
                    return 1
                if "Output" in ev:
                    test_outputs.append(ev["Output"])
            elif action == "pass":
                if test_state != TEST_RUNNING:
                    sys.stderr.write(f"ERROR: Target test 'pass' received while in state '{test_state}'\n")
                    return 1
                test_state = TEST_PASSED
            elif action == "fail":
                if test_state != TEST_RUNNING:
                    sys.stderr.write(f"ERROR: Target test 'fail' received while in state '{test_state}'\n")
                    return 1
                test_state = TEST_FAILED
            elif action == "skip":
                if test_state != TEST_RUNNING:
                    sys.stderr.write(f"ERROR: Target test 'skip' received while in state '{test_state}'\n")
                    return 1
                test_state = TEST_SKIPPED
            elif action == "start":
                sys.stderr.write(f"ERROR: 'start' action is not permitted for test events\n")
                return 1

        # Package-level events for target package
        elif not ev_test and ev_pkg == target_package:
            if pkg_state in (PKG_PASSED, PKG_FAILED, PKG_SKIPPED):
                sys.stderr.write(
                    f"ERROR: Package received '{action}' after transitioning to terminal state '{pkg_state}'\n"
                )
                return 1

            if action == "start":
                if pkg_state != PKG_INIT:
                    sys.stderr.write(f"ERROR: Package emitted duplicate 'start' (current state '{pkg_state}')\n")
                    return 1
                pkg_state = PKG_STARTED
            elif action == "output":
                pass
            elif action in ("pass", "fail", "skip"):
                # Precondition: target test MUST be in terminal state before package terminal
                if test_state not in (TEST_PASSED, TEST_FAILED, TEST_SKIPPED):
                    sys.stderr.write(
                        f"ERROR: Package terminal event '{action}' received before target test finished (test state: '{test_state}')\n"
                    )
                    return 1

                if action == "pass":
                    pkg_state = PKG_PASSED
                elif action == "fail":
                    pkg_state = PKG_FAILED
                elif action == "skip":
                    pkg_state = PKG_SKIPPED
            elif action in ("run", "pause", "cont"):
                sys.stderr.write(f"ERROR: Action '{action}' is not permitted for package-level events\n")
                return 1

    # Final assertion of terminal outcomes
    if test_state == TEST_FAILED:
        for out in test_outputs:
            sys.stderr.write(out)
        sys.stderr.write(f"ERROR: Target test '{target_test}' failed!\n")
        return 1

    if test_state == TEST_SKIPPED:
        for out in test_outputs:
            sys.stderr.write(out)
        sys.stderr.write(f"ERROR: Target test '{target_test}' was skipped!\n")
        return 1

    if test_state != TEST_PASSED:
        sys.stderr.write(f"ERROR: Target test '{target_test}' did not complete with 'pass' (state: '{test_state}')\n")
        return 1

    if pkg_state == PKG_FAILED:
        sys.stderr.write(f"ERROR: Target package '{target_package}' failed!\n")
        return 1

    if pkg_state == PKG_SKIPPED:
        sys.stderr.write(f"ERROR: Target package '{target_package}' was skipped!\n")
        return 1

    if pkg_state != PKG_PASSED:
        sys.stderr.write(f"ERROR: Target package '{target_package}' did not complete with 'pass' (state: '{pkg_state}')\n")
        return 1

    print(f"SUCCESS: Target test '{target_test}' and package '{target_package}' passed all lifecycle checks.")
    return 0


def main():
    parser = argparse.ArgumentParser(description="Verify go test -json output for Mihomo acceptance")
    parser.add_argument("--json", required=True, help="Path to go test JSON output file")
    parser.add_argument("--test", required=True, help="Name of the target test function")
    args = parser.parse_args()

    sys.exit(verify_acceptance_json(args.json, args.test))


if __name__ == "__main__":
    main()
