"""POST /v1/chat/completions without streaming — Requirements 1, 8, 10.

The SDK is the oracle here: `client.chat.completions.create()` validates the
response against its own ChatCompletion model, so a shape error surfaces as an
exception rather than a subtly wrong assertion.
"""

import json

import pytest

MARKER_EMPTY = "__empty__"
MARKER_UTF8 = "__utf8__"
MARKER_BIG = "__big__"
MARKER_ANSI = "__ansi__"


def test_basic_completion_shape(client, model):
    """Req 1.1, 1.3, 1.4, 1.5, 8.3."""
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": "Hello"}]
    )

    assert resp.id.startswith("chatcmpl-")
    assert len(resp.id) >= 24
    assert resp.object == "chat.completion"
    assert isinstance(resp.created, int) and resp.created > 0
    assert resp.model == model, "response must echo the requested model name"

    assert len(resp.choices) == 1
    choice = resp.choices[0]
    assert choice.index == 0
    assert choice.message.role == "assistant"
    assert choice.message.content is not None
    assert choice.finish_reason == "stop"


def test_usage_is_present_and_consistent(client, model):
    """Req 8.1: usage must be present and internally consistent."""
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": "Hello there, agent"}]
    )

    usage = resp.usage
    assert usage is not None, "usage object missing"
    assert usage.prompt_tokens >= 0
    assert usage.completion_tokens >= 0
    assert usage.total_tokens == usage.prompt_tokens + usage.completion_tokens


def test_ids_are_unique_across_requests(client, model):
    """Req 1.4: each response carries its own id."""
    ids = {
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": f"request {i}"}]
        ).id
        for i in range(5)
    }
    assert len(ids) == 5


def test_model_name_is_case_insensitive(client):
    """Req 4.2."""
    for name in ("kiro", "KIRO", "Kiro"):
        resp = client.chat.completions.create(
            model=name, messages=[{"role": "user", "content": "hi"}]
        )
        assert resp.model == name, "the response echoes the requested spelling"


@pytest.mark.fake_only
def test_empty_agent_output_yields_empty_string(client, model):
    """Req 8.3: no output is an empty string, never a missing field.

    SDK users write `resp.choices[0].message.content.strip()`; a null there is
    an AttributeError in their code, not ours.
    """
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": MARKER_EMPTY}]
    )

    assert resp.choices[0].message.content == ""
    assert resp.choices[0].finish_reason == "stop"
    assert resp.usage.completion_tokens == 0


@pytest.mark.fake_only
def test_unicode_round_trips_intact(client, model):
    """Multi-byte output must survive JSON encoding unmangled."""
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": MARKER_UTF8}]
    )

    content = resp.choices[0].message.content
    assert "Příliš žluťoučký kůň" in content
    assert "🐴" in content


@pytest.mark.fake_only
def test_unicode_token_estimate_counts_characters(client, model):
    """Req 8.2 defines the estimate in characters, not bytes.

    Byte-based counting inflates non-ASCII text by 2–4x, which makes the usage
    numbers useless for exactly the users who need them most.
    """
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": MARKER_UTF8}]
    )

    content = resp.choices[0].message.content
    expected = (len(content) + 2) // 4
    assert resp.usage.completion_tokens == expected


@pytest.mark.fake_only
def test_large_output_is_returned_whole(client, model):
    """A megabyte-scale answer must not be truncated or corrupted."""
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": MARKER_BIG}]
    )

    content = resp.choices[0].message.content
    assert len(content) > 500_000
    assert content.startswith("lorem ipsum")


@pytest.mark.fake_only
def test_ansi_escapes_are_stripped(client):
    """kiro writes to a TTY; its escape sequences must not reach API clients."""
    resp = client.chat.completions.create(
        model="kiro", messages=[{"role": "user", "content": MARKER_ANSI}]
    )

    assert "\x1b[" not in resp.choices[0].message.content


def test_ignored_parameters_are_accepted(client, model):
    """Req 10.1: standard OpenAI knobs must not cause errors."""
    resp = client.chat.completions.create(
        model=model,
        messages=[{"role": "user", "content": "hi"}],
        temperature=0.7,
        top_p=0.9,
        max_tokens=256,
        stop=["END", "STOP"],
        presence_penalty=0.5,
        frequency_penalty=0.5,
        user="conformance-suite",
    )

    assert resp.choices[0].message.content is not None


def test_n_greater_than_one_returns_one_choice(client, model):
    """Req 10.2 and design Property 2."""
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": "hi"}], n=3
    )

    assert len(resp.choices) == 1
    assert resp.choices[0].index == 0


def test_unknown_parameters_are_ignored(raw, model):
    """Req 10.3: forward-compatibility with newer SDK releases.

    Sent raw because the SDK would strip unknown keyword arguments.
    """
    resp = raw.post(
        "/v1/chat/completions",
        json={
            "model": model,
            "messages": [{"role": "user", "content": "hi"}],
            "seed": 42,
            "response_format": {"type": "text"},
            "parallel_tool_calls": False,
            "some_future_field": {"nested": [1, 2, 3]},
        },
    )

    assert resp.status_code == 200, resp.text


def test_wrong_types_on_ignored_parameters_are_tolerated(raw, model):
    """Req 10.4.

    Go's json decoder aborts the whole body on the first type mismatch, so a
    single quirky client field would otherwise 400 an otherwise-valid request.
    """
    resp = raw.post(
        "/v1/chat/completions",
        json={
            "model": model,
            "messages": [{"role": "user", "content": "hi"}],
            "temperature": "hot",
            "top_p": "high",
            "max_tokens": "many",
            "n": "three",
            "presence_penalty": [1],
            "frequency_penalty": True,
            "user": 42,
        },
    )

    assert resp.status_code == 200, resp.text


def test_response_content_type_is_json(raw, model):
    """Req 6.1 / 1.1."""
    resp = raw.post(
        "/v1/chat/completions",
        json={"model": model, "messages": [{"role": "user", "content": "hi"}]},
    )

    assert resp.headers["content-type"].startswith("application/json")
    json.loads(resp.text)  # must parse
