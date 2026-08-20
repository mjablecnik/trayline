"""Smoke tests against a real server running real agents.

Run with:

    ./run.sh --live --base-url https://your-host --token "$API_TOKEN"

Real agent output is non-deterministic and each run costs credits and takes
tens of seconds, so this file stays deliberately small: it asserts response
*structure* plus the weakest semantic claim that still proves a model answered.
Everything else is covered against the fake server.
"""

import openai
import pytest

pytestmark = [pytest.mark.live, pytest.mark.timeout(600)]

PING = "Reply with exactly the word PONG and nothing else."


def test_models_endpoint_lists_configured_models(client):
    models = list(client.models.list())

    assert models, "server advertises no models"
    for m in models:
        assert m.object == "model"


def test_non_streaming_completion_answers(client, model):
    resp = client.chat.completions.create(
        model=model, messages=[{"role": "user", "content": PING}]
    )

    content = resp.choices[0].message.content
    assert content, "agent returned no content"
    assert "PONG" in content.upper()
    assert resp.usage.total_tokens > 0
    assert resp.choices[0].finish_reason == "stop"


def test_streaming_completion_delivers_incrementally(client, model):
    chunks = list(
        client.chat.completions.create(
            model=model,
            messages=[{"role": "user", "content": "Count from 1 to 5, one number per line."}],
            stream=True,
        )
    )

    text = "".join(
        c.choices[0].delta.content for c in chunks if c.choices[0].delta.content
    )
    assert text.strip(), "stream produced no content"
    assert len(chunks) >= 2, "no incremental delivery"
    assert chunks[-1].choices[0].finish_reason == "stop"


def test_multi_turn_context_is_carried(client, model):
    """The agent must see earlier turns, not just the last message."""
    resp = client.chat.completions.create(
        model=model,
        messages=[
            {"role": "user", "content": "My favourite colour is heliotrope."},
            {"role": "assistant", "content": "Noted."},
            {"role": "user", "content": "What is my favourite colour? Answer with one word."},
        ],
    )

    assert "heliotrope" in resp.choices[0].message.content.lower()


def test_system_prompt_is_honoured(client, model):
    resp = client.chat.completions.create(
        model=model,
        messages=[
            {"role": "system", "content": "You always answer in exactly one word."},
            {"role": "user", "content": "Name a primary colour."},
        ],
    )

    assert resp.choices[0].message.content.strip()


def test_invalid_token_is_rejected(server, model):
    bad = server.client(api_key="definitely-not-the-token")

    with pytest.raises(openai.AuthenticationError):
        bad.chat.completions.create(model=model, messages=[{"role": "user", "content": "hi"}])


def test_unknown_model_is_rejected(client):
    with pytest.raises(openai.NotFoundError) as excinfo:
        client.chat.completions.create(
            model="gpt-4o", messages=[{"role": "user", "content": "hi"}]
        )

    assert excinfo.value.body["code"] == "model_not_found"
