#!/usr/bin/env python3
import os
import unittest

from scripts.tests.verify_bootstrap_commands import (
    extract_commands,
    extract_command_substitutions,
    check_command_parity,
    get_declared_required_cmds,
    BOOTSTRAP_SCRIPT
)

class TestVerifyBootstrapCommands(unittest.TestCase):
    def test_simple_command_extraction(self):
        script = "curl -fsSL https://example.com -o /tmp/file\nsha256sum /tmp/file\n"
        invoked = extract_commands(script)
        self.assertEqual(invoked, {"curl", "sha256sum"})

    def test_inline_env_assignment(self):
        # Must detect undeclared_tool even when preceded by inline environment assignments
        script = "FOO=1 undeclared_tool arg\nBAR=2 BAZ=3 another_tool -v\n"
        invoked = extract_commands(script)
        self.assertEqual(invoked, {"undeclared_tool", "another_tool"})

    def test_only_env_assignment_ignored(self):
        # Variable assignments without any command should not extract commands
        script = "FOO=1\nBAR=\"hello world\"\nBAZ='test'\n"
        invoked = extract_commands(script)
        self.assertEqual(invoked, set())

    def test_command_after_redirection(self):
        script = "<input.txt special_tool >output.txt 2>&1\n"
        invoked = extract_commands(script)
        self.assertEqual(invoked, {"special_tool"})

    def test_nested_command_substitutions(self):
        script = "echo $(outer_cmd $(inner_cmd foo))\n"
        subs = extract_command_substitutions(script)
        self.assertIn("outer_cmd $(inner_cmd foo)", subs)
        self.assertIn("inner_cmd foo", subs)
        invoked = extract_commands(script)
        self.assertEqual(invoked, {"outer_cmd", "inner_cmd"})

    def test_process_substitutions(self):
        script = "diff <(cmd1 arg) <(cmd2 arg)\n"
        subs = extract_command_substitutions(script)
        self.assertIn("cmd1 arg", subs)
        self.assertIn("cmd2 arg", subs)
        invoked = extract_commands(script)
        self.assertEqual(invoked, {"diff", "cmd1", "cmd2"})

    def test_pipeline_and_logical_operators(self):
        script = "tool1 -a | tool2 -b && tool3 -c || tool4 -d\n"
        invoked = extract_commands(script)
        self.assertEqual(invoked, {"tool1", "tool2", "tool3", "tool4"})

    def test_shell_builtins_and_functions_ignored(self):
        script = "echo hello\nexit 0\nmapfile -t arr < file\ncleanup\npublish_cache\nverify_file\nkill -TERM 123\nwait 123\n"
        invoked = extract_commands(script)
        self.assertEqual(invoked, set())

    def test_mutation_undeclared_detected(self):
        # Adding an undeclared tool to declared set must report undeclared
        declared = {"curl", "python3"}
        invoked = {"curl", "python3", "secret_unannounced_tool"}
        undeclared, unused = check_command_parity(declared, invoked)
        self.assertEqual(undeclared, {"secret_unannounced_tool"})
        self.assertEqual(unused, set())

    def test_mutation_unused_declared_detected(self):
        # Declaring a tool that is not invoked must report unused_declared
        declared = {"curl", "python3", "find"}
        invoked = {"curl", "python3"}
        undeclared, unused = check_command_parity(declared, invoked)
        self.assertEqual(undeclared, set())
        self.assertEqual(unused, {"find"})

    def test_production_script_exact_parity(self):
        declared = get_declared_required_cmds(BOOTSTRAP_SCRIPT)
        with open(BOOTSTRAP_SCRIPT, "r", encoding="utf-8") as f:
            content = f.read()
        invoked = extract_commands(content)
        undeclared, unused = check_command_parity(declared, invoked)
        self.assertEqual(undeclared, set(), f"Undeclared commands in production bootstrap: {undeclared}")
        self.assertEqual(unused, set(), f"Unused declared commands in production bootstrap: {unused}")
        self.assertEqual(declared, invoked)

if __name__ == "__main__":
    unittest.main()
