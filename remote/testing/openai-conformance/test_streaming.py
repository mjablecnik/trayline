"""Streaming chat completions over SSE — Requirement 2.

Every test consumes the stream through the OpenAI SDK, so a malformed chunk or a
missing terminator fails as an SDK error rather than a silently wrong string.
One test drops to raw HTTP to assert the wire framing the SDK normalises away.
"""

import time

import pytest

MARKER_SLOW = "__slow__"
MARKER_CRASH = "__crash__"
MARKER_EMPTY = "__empty__"
MARKER_ANSI = "__ansi__"
MARKER_UTF8 = "__utf8__"


def collect(stream):
    """Drain a stream into (chunks, reassembled text)."""
    chunks = list(stream)
    text = "".join(
        c.choices[0].delta.content
        for c in chunks
        if c.choices and c.choices[0].delta and c.choices[0].delta.content
    )
    return chunks, text


def test_stream_yields_chunks_and_terminates(client, model):
    """Req 2.1–2.5, consumed the way an SDK user would."""
    stream = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": "Hello"}], stream=True
    )
    chunks, text = collect(stream)

    assert len(chunks) >= 2, "expected incremental delivery, not one lump"
    assert text, "stream produced no content"
    for c in chunks:
        assert c.object == "chat.completion.chunk"
        assert len(c.choices) == 1
        assert c.choices[0].index == 0


def test_stream_id_is_stable(client, model):
    """Req 2.2: every chunk of one response shares its id."""
    chunks, _ = collect(
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": "Hello"}], stream=True
        )
    )

    ids = {c.id for c in chunks}
    assert len(ids) == 1, f"chunks carry different ids: {ids}"
    assert ids.pop().startswith("chatcmpl-")


def test_role_appears_only_in_first_chunk(client, model):
    """Req 2.6."""
    chunks, _ = collect(
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": "Hello"}], stream=True
        )
    )

    assert chunks[0].choices[0].delta.role == "assistant"
    for i, c in enumerate(chunks[1:], start=1):
        assert c.choices[0].delta.role is None, f"chunk {i} repeats the role"


def test_exactly_one_finish_reason(client, model):
    """Req 2.4 and design Property 5."""
    chunks, _ = collect(
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": "Hello"}], stream=True
        )
    )

    finishes = [c for c in chunks if c.choices[0].finish_reason is not None]
    assert len(finishes) == 1, f"{len(finishes)} chunks carry finish_reason, want 1"
    assert finishes[0].choices[0].finish_reason == "stop"
    assert finishes[-1] is chunks[-1], "the finish chunk must be last"
    assert not finishes[0].choices[0].delta.content


def test_streaming_matches_non_streaming_content(client, model):
    """The two modes must not disagree about what the agent said."""
    prompt = [{"role": "user", "content": "Hello"}]

    blocking = client.chat.completions.create(model=model, messages=prompt)
    _, streamed = collect(
        client.chat.completions.create(model=model, messages=prompt, stream=True)
    )

    assert streamed.strip() == blocking.choices[0].message.content.strip()


def test_sse_wire_format(raw, model):
    """Req 2.1, 2.2, 2.5 at the byte level, below the SDK's parsing."""
    with raw.stream(
        "POST",
        "/v1/chat/completions",
        json={"model": model, "messages": [{"role": "user", "content": "hi"}], "stream": True},
    ) as resp:
        assert resp.status_code == 200
        assert resp.headers["content-type"] == "text/event-stream"
        assert resp.headers["cache-control"] == "no-cache"
        body = "".join(resp.iter_text())

    assert body.endswith("data: [DONE]\n\n"), repr(body[-64:])
    assert body.count("data: [DONE]") == 1

    frames = [line for line in body.split("\n\n") if line]
    for frame in frames:
        assert frame.startswith("data: "), repr(frame)


@pytest.mark.fake_only
def test_chunks_arrive_incrementally(spawn_server, model):
    """Req 2.1: chunks must be flushed as produced, not buffered to the end.

    With a 120ms server-side gap between chunks, a client that receives
    everything at once is proof of buffering somewhere in the chain.
    """
    server = spawn_server(CHUNK_DELAY="120ms")
    client = server.client()

    start = time.monotonic()
    arrivals = []
    for chunk in client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": MARKER_SLOW}], stream=True
    ):
        if chunk.choices[0].delta.content:
            arrivals.append(time.monotonic() - start)

    assert len(arrivals) >= 3
    spread = arrivals[-1] - arrivals[0]
    assert spread > 0.2, f"all chunks arrived within {spread:.3f}s — output was buffered"


@pytest.mark.fake_only
def test_chunk_order_is_preserved(spawn_server):
    """Design Property 3: no reordering or coalescing."""
    server = spawn_server()
    client = server.client()

    _, text = collect(
        client.chat.completions.create(
            model="claude-sonnet",
            messages=[{"role": "user", "content": MARKER_SLOW}],
            stream=True,
        )
    )

    assert text == "chunk 1chunk 2chunk 3chunk 4chunk 5"


@pytest.mark.fake_only
def test_mid_stream_crash_terminates_cleanly(client, model):
    """Req 2.7: a container that dies mid-stream still ends the SSE stream.

    The SDK must see an ordinary end-of-stream, not a truncated connection —
    otherwise every consumer needs custom recovery code.
    """
    chunks, text = collect(
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": MARKER_CRASH}], stream=True
        )
    )

    assert "partial one" in text, "chunks produced before the crash must be delivered"
    finishes = [c for c in chunks if c.choices[0].finish_reason is not None]
    assert len(finishes) == 1
    assert finishes[0].choices[0].finish_reason == "stop"


@pytest.mark.fake_only
def test_empty_output_still_terminates(raw, model):
    """An agent that says nothing must not leave the client hanging."""
    with raw.stream(
        "POST",
        "/v1/chat/completions",
        json={"model": model, "messages": [{"role": "user", "content": MARKER_EMPTY}], "stream": True},
    ) as resp:
        body = "".join(resp.iter_text())

    assert body.endswith("data: [DONE]\n\n")
    assert body.count("data: [DONE]") == 1


@pytest.mark.fake_only
def test_streaming_strips_ansi(client):
    """kiro's TTY escapes must not leak into deltas either."""
    _, text = collect(
        client.chat.completions.create(
            model="kiro", messages=[{"role": "user", "content": MARKER_ANSI}], stream=True
        )
    )

    assert "\x1b[" not in text
    assert "green" in text


@pytest.mark.fake_only
def test_streaming_unicode_intact(client, model):
    """Multi-byte characters must not be split across chunk boundaries."""
    _, text = collect(
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": MARKER_UTF8}], stream=True
        )
    )

    assert "Příliš žluťoučký kůň" in text
    assert "🐴" in text


@pytest.mark.fake_only
def test_stream_start_failure_is_a_json_error(client, model):
    """A container that never starts must fail as a status code, not as a
    half-open stream: once SSE headers go out, no error can be reported."""
    import openai

    with pytest.raises(openai.APIStatusError) as excinfo:
        list(
            client.chat.completions.create(
                model=model, messages=[{"role": "user", "content": "__error__"}], stream=True
            )
        )

    assert excinfo.value.status_code == 500
    assert excinfo.value.body["type"] == "server_error"


def test_stream_options_include_usage_is_accepted(raw, model):
    """SDK clients commonly request usage on streams.

    The spec does not define this, so the contract asserted here is the minimum
    one: the request must not fail. Whether a usage chunk is emitted is a
    product decision recorded in TRACEABILITY.md.
    """
    with raw.stream(
        "POST",
        "/v1/chat/completions",
        json={
            "model": model,
            "messages": [{"role": "user", "content": "hi"}],
            "stream": True,
            "stream_options": {"include_usage": True},
        },
    ) as resp:
        assert resp.status_code == 200
        body = "".join(resp.iter_text())

    assert body.endswith("data: [DONE]\n\n")
