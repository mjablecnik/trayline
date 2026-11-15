#!/usr/bin/env python3
"""Adapt Codex JSONL turns to Trayline's persistent structured chat protocol.

Each follow-up resumes the exact thread returned by Codex, in the same container.
Only completed agent messages are exposed; tool output and reasoning stay private.
"""
import argparse
import json
import os
import queue
import signal
import subprocess
import sys
import tempfile
import threading


class Chat:
    def __init__(self, model="", system=""):
        self.model = model
        self.system = system
        self.thread_id = None
        self.child = None
        self.lock = threading.Lock()
        self.output_lock = threading.Lock()
        self.stopping = False

    def emit(self, message):
        with self.output_lock:
            print(json.dumps(message), flush=True)

    def interrupt(self, *_):
        with self.lock:
            if self.child is not None and self.child.poll() is None:
                try:
                    os.killpg(self.child.pid, signal.SIGINT)
                except ProcessLookupError:
                    pass

    def shutdown(self, *_):
        self.stopping = True
        with self.lock:
            if self.child is not None and self.child.poll() is None:
                try:
                    os.killpg(self.child.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
        raise SystemExit(0)

    def command(self):
        cmd = ["codex", "--no-daemon", "exec"]
        if self.thread_id:
            cmd += ["resume", self.thread_id]
        cmd += ["--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check", "--json"]
        if self.model:
            cmd += ["--model", self.model]
        if self.system:
            cmd += ["-c", "developer_instructions=" + json.dumps(self.system)]
        return cmd + ["-"]

    def turn(self, prompt):
        failed = False
        completed = False
        try:
            # A file drains diagnostics without blocking the JSONL reader or
            # mixing stderr into protocol messages; only surface it on failure.
            with tempfile.TemporaryFile(mode="w+t") as diagnostics:
                with self.lock:
                    self.child = subprocess.Popen(
                        self.command(), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=diagnostics, text=True, start_new_session=True,
                    )
                    child = self.child
                # Prompts arrive over stdin, never through a shell or argv flags.
                try:
                    child.stdin.write(prompt)
                    child.stdin.close()
                except BrokenPipeError:
                    pass
                for line in child.stdout:
                    try:
                        event = json.loads(line)
                    except (ValueError, TypeError):
                        continue
                    kind = event.get("type")
                    if kind == "thread.started":
                        self.thread_id = event.get("thread_id", self.thread_id)
                    elif kind == "item.completed":
                        item = event.get("item", {})
                        if item.get("type") == "agent_message" and item.get("text"):
                            self.emit({"type": "assistant", "message": {"content": [
                                {"type": "text", "text": item["text"]}
                            ]}})
                    elif kind == "turn.completed":
                        completed = True
                    elif kind in ("turn.failed", "error"):
                        failed = True
                        error = event.get("error") or {}
                        message = error.get("message") if isinstance(error, dict) else str(error)
                        self.emit({"type": "error", "message": message or event.get("message", "Codex turn failed")})
                code = child.wait()
                child.stdout.close()
                if not failed and (code != 0 or not completed):
                    diagnostics.seek(0)
                    detail = diagnostics.read()[-4096:].strip()
                    self.emit({"type": "error", "message": detail or f"Codex exited without completing the turn (exit {code})"})
                    failed = True
        except (OSError, ValueError) as exc:
            self.emit({"type": "error", "message": str(exc)})
            failed = True
        finally:
            with self.lock:
                self.child = None
            self.emit({"type": "result", "subtype": "error" if failed else "success"})

    def run(self, source):
        turns = queue.Queue()

        def worker():
            while True:
                prompt = turns.get()
                if prompt is None:
                    return
                self.turn(prompt)

        thread = threading.Thread(target=worker, daemon=True)
        thread.start()
        for line in source:
            try:
                message = json.loads(line)
            except ValueError:
                self.emit({"type": "error", "message": "Invalid chat JSON"})
                continue
            if message.get("type") == "control_request":
                self.emit({"type": "control_response", "request_id": message.get("request_id"),
                           "response": {"subtype": "success"}})
            elif message.get("type") == "user":
                content = message.get("message", {}).get("content", "")
                if isinstance(content, list):
                    content = "\n".join(block.get("text", "") for block in content if block.get("type") == "text")
                if content:
                    turns.put(content)
        turns.put(None)
        thread.join()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", default="")
    parser.add_argument("--system", default="")
    args = parser.parse_args()
    chat = Chat(args.model, args.system)
    signal.signal(signal.SIGINT, chat.interrupt)
    signal.signal(signal.SIGTERM, chat.shutdown)
    chat.run(sys.stdin)


if __name__ == "__main__":
    main()
