"""Capacity, slot accounting and rate limiting — Requirements 7 and 6.3.

These tests own the failure mode the design review flagged as most severe: a
saturated server that queues silently instead of rejecting, leaving the client
blocked for the whole task timeout (ten minutes in production).

Every client here is built with max_retries=0 — the SDK retries 429 by default,
which would turn "rejected immediately" and "queued for ten minutes" into the
same observable behaviour.
"""

import concurrent.futures
import time

import openai
import pytest

pytestmark = pytest.mark.fake_only

MARKER_HANG = "__hang__"


def test_saturated_server_rejects_immediately(spawn_server, model):
    """Req 7.2 and 6.3: 429 with Retry-After: 30, without queuing."""
    server = spawn_server(MAX_CONCURRENT_TASKS="1", TASK_TIMEOUT="20s")
    client = server.client()

    # Occupy the only slot with a request that will hang until its timeout.
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        pool.submit(
            lambda: client.chat.completions.create(
                model=model, messages=[{"role": "user", "content": MARKER_HANG}]
            )
        )
        time.sleep(1.0)  # let it acquire the slot

        start = time.monotonic()
        with pytest.raises(openai.RateLimitError) as excinfo:
            client.chat.completions.create(
                model=model, messages=[{"role": "user", "content": "rejected"}]
            )
        elapsed = time.monotonic() - start

    assert excinfo.value.response.headers.get("retry-after") == "30"
    error = excinfo.value.body
    assert error["type"] == "server_error"
    assert "capacity" in error["message"].lower()

    # The whole point of Req 7.2: no queuing.
    assert elapsed < 5.0, (
        f"429 took {elapsed:.1f}s — the request queued for the task timeout "
        "instead of being rejected immediately"
    )


def test_streaming_rejection_is_json_not_sse(spawn_server, model):
    """A rejected stream must fail before SSE headers commit the response.

    Once Content-Type: text/event-stream is sent, no status code can follow, so
    the client would see an empty successful stream instead of an error.
    """
    server = spawn_server(MAX_CONCURRENT_TASKS="1", TASK_TIMEOUT="20s")
    client = server.client()

    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        pool.submit(
            lambda: client.chat.completions.create(
                model=model, messages=[{"role": "user", "content": MARKER_HANG}]
            )
        )
        time.sleep(1.0)

        with pytest.raises(openai.RateLimitError) as excinfo:
            list(
                client.chat.completions.create(
                    model=model,
                    messages=[{"role": "user", "content": "rejected"}],
                    stream=True,
                )
            )

    assert "application/json" in excinfo.value.response.headers["content-type"]


def test_capacity_recovers_after_requests_finish(spawn_server, model):
    """Req 7.3 and design Property 4: slots come back on every path."""
    server = spawn_server(MAX_CONCURRENT_TASKS="2", TASK_TIMEOUT="10s")
    client = server.client()

    for _ in range(6):
        resp = client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": "hi"}]
        )
        assert resp.choices[0].message.content

    # A leaked slot would show up as a 429 on this last request.
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": "still working"}]
    )
    assert resp.choices[0].message.content


def test_parallel_requests_all_succeed_within_capacity(spawn_server, model):
    """Concurrent traffic inside the configured limit must not interfere."""
    server = spawn_server(MAX_CONCURRENT_TASKS="4", TASK_TIMEOUT="20s")
    client = server.client()

    def one(i):
        return client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": f"request {i}"}]
        )

    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        responses = list(pool.map(one, range(4)))

    assert len({r.id for r in responses}) == 4, "responses share an id"
    for r in responses:
        assert r.choices[0].message.content


def test_streaming_disconnect_frees_capacity(spawn_server, model):
    """Req 7.3/7.4: abandoning a stream must release the slot.

    A leak here is invisible until the server wedges in production, so the test
    hangs up mid-stream and then proves the freed slot is reusable.
    """
    server = spawn_server(MAX_CONCURRENT_TASKS="1", CHUNK_DELAY="200ms", TASK_TIMEOUT="20s")
    client = server.client()

    stream = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": "__slow__"}], stream=True
    )
    next(iter(stream))  # consume one chunk
    stream.close()  # hang up

    # The slot must return; poll briefly since cleanup is asynchronous.
    deadline = time.monotonic() + 10
    last_error = None
    while time.monotonic() < deadline:
        try:
            resp = client.chat.completions.create(
                model=model, messages=[{"role": "user", "content": "after disconnect"}]
            )
            assert resp.choices[0].message.content
            return
        except openai.RateLimitError as exc:  # capacity not back yet
            last_error = exc
            time.sleep(0.25)

    pytest.fail(f"capacity never recovered after client disconnect: {last_error}")


def test_rate_limiter_uses_openai_error_format(spawn_server, model):
    """Req 6.3: the limiter's 429 must be parseable by the SDK too."""
    server = spawn_server(RATE_LIMIT="1")
    client = server.client()

    with pytest.raises(openai.RateLimitError) as excinfo:
        for _ in range(10):
            client.chat.completions.create(
                model=model, messages=[{"role": "user", "content": "hi"}]
            )

    assert excinfo.value.response.headers.get("retry-after")
    assert excinfo.value.body["type"] == "server_error"
