"""Shared fixtures for the OpenAI-compatible API conformance suite.

Two run modes:

* **fake** (default) — the suite builds and starts ``cmd/fake-openai-server``,
  which runs the production router with only the agent execution layer stubbed.
  Deterministic, needs no Docker, costs nothing.
* **live** (``--base-url``) — the suite runs against a real Trayline server with
  real agents. Tests that depend on scripted agent behaviour are skipped.

The client is always constructed with ``max_retries=0``. The OpenAI SDK retries
429 and 5xx by default, which would silently paper over exactly the failures
these tests exist to catch.
"""

from __future__ import annotations

import contextlib
import os
import socket
import subprocess
import time
from pathlib import Path

import httpx
import pytest
from openai import OpenAI

REPO_REMOTE_DIR = Path(__file__).resolve().parents[2]
DEFAULT_TOKEN = "test-token"


def pytest_addoption(parser):
    parser.addoption(
        "--base-url",
        action="store",
        default=None,
        help="Run against an already-running server (live mode) instead of the fake server.",
    )
    parser.addoption(
        "--token",
        action="store",
        default=None,
        help="API token to authenticate with (default: test-token, or $API_TOKEN in live mode).",
    )


def pytest_configure(config):
    config.addinivalue_line(
        "markers", "fake_only: requires the scripted fake agent (skipped in live mode)"
    )
    config.addinivalue_line(
        "markers", "live: only meaningful against a real server with real agents"
    )


def pytest_collection_modifyitems(config, items):
    live = config.getoption("--base-url") is not None
    skip_fake = pytest.mark.skip(reason="requires the scripted fake agent")
    skip_live = pytest.mark.skip(reason="live mode only (pass --base-url)")
    for item in items:
        if live and "fake_only" in item.keywords:
            item.add_marker(skip_fake)
        if not live and "live" in item.keywords:
            item.add_marker(skip_live)


def _free_port() -> int:
    with contextlib.closing(socket.socket()) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


@pytest.fixture(scope="session")
def fake_server_binary(pytestconfig) -> Path | None:
    """Build cmd/fake-openai-server once per session (skipped in live mode)."""
    if pytestconfig.getoption("--base-url"):
        return None

    out = Path(os.environ.get("FAKE_SERVER_BIN", "")) if os.environ.get("FAKE_SERVER_BIN") else None
    if out and out.exists():
        return out

    out = REPO_REMOTE_DIR / ".fake-openai-server"
    result = subprocess.run(
        ["go", "build", "-o", str(out), "./cmd/fake-openai-server"],
        cwd=REPO_REMOTE_DIR,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        pytest.fail(f"failed to build fake server:\n{result.stdout}\n{result.stderr}")
    return out


class ServerHandle:
    def __init__(self, url: str, token: str, process: subprocess.Popen | None):
        self.url = url
        self.token = token
        self.process = process

    def client(self, **kwargs) -> OpenAI:
        params = {
            "base_url": f"{self.url}/v1",
            "api_key": self.token,
            # See module docstring: retries would mask the very failures under test.
            "max_retries": 0,
            "timeout": 60.0,
        }
        params.update(kwargs)
        return OpenAI(**params)


def _wait_until_healthy(url: str, process: subprocess.Popen, timeout: float = 20.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if process.poll() is not None:
            out, err = process.communicate()
            raise RuntimeError(f"fake server exited early:\n{out}\n{err}")
        try:
            if httpx.get(f"{url}/health", timeout=1.0).status_code == 200:
                return
        except httpx.HTTPError:
            time.sleep(0.1)
    raise RuntimeError(f"fake server at {url} did not become healthy within {timeout}s")


@pytest.fixture(scope="session")
def spawn_server(fake_server_binary, pytestconfig):
    """Factory starting an isolated fake server with custom environment.

    Used by tests that need a specific server configuration (a single task slot,
    a tight rate limit, an empty model registry) without disturbing the shared
    session server.
    """
    if pytestconfig.getoption("--base-url"):
        pytest.skip("custom server configuration is unavailable in live mode")

    processes: list[subprocess.Popen] = []

    def _spawn(**env_overrides) -> ServerHandle:
        port = _free_port()
        url = f"http://127.0.0.1:{port}"
        token = DEFAULT_TOKEN
        env = {**os.environ, "API_TOKEN": token, "RATE_LIMIT": "10000", **env_overrides}
        proc = subprocess.Popen(
            [str(fake_server_binary), "-addr", f"127.0.0.1:{port}"],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        processes.append(proc)
        _wait_until_healthy(url, proc)
        return ServerHandle(url, token, proc)

    yield _spawn

    for proc in processes:
        proc.terminate()
        with contextlib.suppress(subprocess.TimeoutExpired):
            proc.wait(timeout=5)


@pytest.fixture(scope="session")
def server(pytestconfig, fake_server_binary) -> ServerHandle:
    """The server under test: the shared fake instance, or a live deployment."""
    base_url = pytestconfig.getoption("--base-url")
    if base_url:
        token = pytestconfig.getoption("--token") or os.environ.get("API_TOKEN")
        if not token:
            pytest.fail("live mode needs --token or $API_TOKEN")
        return ServerHandle(base_url.rstrip("/").removesuffix("/v1"), token, None)

    port = _free_port()
    url = f"http://127.0.0.1:{port}"
    token = pytestconfig.getoption("--token") or DEFAULT_TOKEN
    env = {**os.environ, "API_TOKEN": token, "RATE_LIMIT": "10000", "TASK_TIMEOUT": "10s"}
    proc = subprocess.Popen(
        [str(fake_server_binary), "-addr", f"127.0.0.1:{port}"],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    _wait_until_healthy(url, proc)

    handle = ServerHandle(url, token, proc)
    yield handle

    proc.terminate()
    with contextlib.suppress(subprocess.TimeoutExpired):
        proc.wait(timeout=5)


@pytest.fixture
def client(server: ServerHandle) -> OpenAI:
    """An OpenAI SDK client pointed at the server under test."""
    return server.client()


@pytest.fixture
def raw(server: ServerHandle) -> httpx.Client:
    """Raw HTTP client, for assertions the SDK abstracts away (wire format, headers)."""
    with httpx.Client(
        base_url=server.url,
        headers={"Authorization": f"Bearer {server.token}"},
        timeout=60.0,
    ) as c:
        yield c


@pytest.fixture
def model(pytestconfig) -> str:
    """Model name used by tests that just need *a* working model."""
    return os.environ.get("CONFORMANCE_MODEL", "kiro")
