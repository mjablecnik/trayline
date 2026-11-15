"""Protocol-level tests with a fake Codex executable; no login/network needed."""
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import tempfile
import unittest

BRIDGE = Path(__file__).with_name("codex-chat.py")
FAKE = r'''#!/usr/bin/env python3
import json, os, signal, sys, time
args = sys.argv[1:]
prompt = sys.stdin.read()
with open(os.environ["CALLS"], "a") as f:
    f.write(json.dumps({"args": args, "prompt": prompt}) + "\n")
print(json.dumps({"type": "thread.started", "thread_id": "thread-123"}), flush=True)
if prompt == "wait":
    time.sleep(30)
if prompt == "fail":
    print(json.dumps({"type": "turn.failed", "error": {"message": "limit reached"}}), flush=True)
    sys.exit(1)
if prompt == "crash":
    print("diagnostic failure", file=sys.stderr)
    sys.exit(2)
print(json.dumps({"type": "item.completed", "item": {"type": "command_execution", "text": "PRIVATE TOOL OUTPUT"}}), flush=True)
print(json.dumps({"type": "item.completed", "item": {"type": "agent_message", "text": "reply:" + prompt}}), flush=True)
print(json.dumps({"type": "turn.completed", "usage": {}}), flush=True)
'''


class ChatProtocolTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        root = Path(self.temp.name)
        executable = root / "codex"
        executable.write_text(FAKE)
        executable.chmod(0o755)
        self.calls = root / "calls.jsonl"
        env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"], CALLS=str(self.calls))
        self.proc = subprocess.Popen(
            [sys.executable, str(BRIDGE), "--model", "my-model", "--system", 'Quoted "rules"\nnext line'],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env,
            text=True, bufsize=1,
        )

    def tearDown(self):
        self.proc.terminate()
        self.proc.communicate(timeout=5)
        self.temp.cleanup()

    def send(self, message):
        self.proc.stdin.write(json.dumps(message) + "\n")
        self.proc.stdin.flush()

    def read(self):
        # Use a single unbuffered byte-at-a-time line reader so select also
        # detects timeouts when no second protocol message arrives.
        line = bytearray()
        while not line.endswith(b"\n"):
            ready, _, _ = select.select([self.proc.stdout], [], [], 5)
            self.assertTrue(ready, "bridge output timed out")
            char = os.read(self.proc.stdout.fileno(), 1)
            self.assertTrue(char, "bridge exited unexpectedly")
            line.extend(char)
        return json.loads(line)

    def turn(self, prompt):
        self.send({"type": "user", "message": {"content": prompt}})
        messages = []
        while True:
            msg = self.read()
            messages.append(msg)
            if msg["type"] == "result":
                return messages

    def test_initialization_multiturn_and_argument_safety(self):
        self.send({"type": "control_request", "request_id": "init_test"})
        self.assertEqual(self.read()["type"], "control_response")
        for prompt in ['--flag-like "prompt"\n$(touch never)', "follow-up"]:
            messages = self.turn(prompt)
            self.assertEqual(messages[0]["message"]["content"][0]["text"], "reply:" + prompt)
            self.assertEqual(messages[-1]["subtype"], "success")
            self.assertNotIn("PRIVATE", json.dumps(messages))
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()]
        self.assertNotIn("resume", calls[0]["args"])
        self.assertIn("resume", calls[1]["args"])
        self.assertIn("thread-123", calls[1]["args"])
        self.assertIn("my-model", calls[0]["args"])
        self.assertIn('developer_instructions="Quoted \\"rules\\"\\nnext line"', calls[0]["args"])
        self.assertEqual(calls[0]["prompt"], '--flag-like "prompt"\n$(touch never)')

    def test_failed_turn_and_recovery(self):
        failed = self.turn("fail")
        self.assertEqual([m["type"] for m in failed], ["error", "result"])
        self.assertEqual(failed[0]["message"], "limit reached")
        self.assertEqual(self.turn("retry")[-1]["subtype"], "success")

    def test_nonzero_exit_without_event_is_an_error(self):
        messages = self.turn("crash")
        self.assertIn("diagnostic failure", messages[0]["message"])
        self.assertEqual(messages[-1]["subtype"], "error")

    def test_interrupt_preserves_bridge_and_thread(self):
        self.send({"type": "user", "message": {"content": "wait"}})
        # Wait for the child to start before sending the interrupt.
        import time
        deadline = time.monotonic() + 5
        while not self.calls.exists() and time.monotonic() < deadline:
            time.sleep(0.01)
        self.assertTrue(self.calls.exists())
        time.sleep(0.05)
        self.proc.send_signal(signal.SIGINT)
        self.assertEqual(self.read()["type"], "error")
        self.assertEqual(self.read()["type"], "result")
        self.assertEqual(self.turn("after interrupt")[-1]["subtype"], "success")


if __name__ == "__main__":
    unittest.main()
