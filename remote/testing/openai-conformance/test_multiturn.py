"""Conversation composition — Requirement 11.

How the messages array becomes an agent prompt is invisible from outside, so
these tests use the fake agent's `__echo__` mode, which returns the composed
(system, prompt) pair it was invoked with. Without that, Req 11 can only be
verified by reading the code.
"""

import json

import pytest

MARKER_ECHO = "__echo__"

pytestmark = pytest.mark.fake_only


def echo(client, model, messages):
    resp = client.chat.completions.create(model=model, messages=messages)
    return json.loads(resp.choices[0].message.content)


def test_single_user_message_is_passed_through(client, model):
    """Req 11.5: no role labels are added around a lone question."""
    payload = echo(client, model, [{"role": "user", "content": MARKER_ECHO}])

    assert payload["prompt"] == MARKER_ECHO
    assert payload["system"] == ""


def test_system_message_goes_to_the_system_parameter(client, model):
    """Req 11.1, 11.3: system content is separate from the conversation."""
    payload = echo(
        client,
        model,
        [
            {"role": "system", "content": "You are a Go expert"},
            {"role": "user", "content": MARKER_ECHO},
        ],
    )

    assert payload["system"] == "You are a Go expert"
    assert "You are a Go expert" not in payload["prompt"]


def test_multiple_system_messages_are_joined(client, model):
    """Req 11.1: newline-joined, in array order."""
    payload = echo(
        client,
        model,
        [
            {"role": "system", "content": "First rule"},
            {"role": "system", "content": "Second rule"},
            {"role": "user", "content": MARKER_ECHO},
        ],
    )

    assert payload["system"] == "First rule\nSecond rule"


def test_multi_turn_conversation_keeps_role_labels(client, model):
    """Req 11.2: the agent receives the history with attribution intact."""
    payload = echo(
        client,
        model,
        [
            {"role": "user", "content": "What is a goroutine?"},
            {"role": "assistant", "content": "A lightweight thread."},
            {"role": "user", "content": MARKER_ECHO},
        ],
    )

    assert payload["prompt"] == (
        "User:\nWhat is a goroutine?\n\n"
        "Assistant:\nA lightweight thread.\n\n"
        f"User:\n{MARKER_ECHO}"
    )


def test_adjacent_same_role_messages_are_kept_separate(client, model):
    """Req 11.6."""
    payload = echo(
        client,
        model,
        [
            {"role": "user", "content": "First"},
            {"role": "user", "content": MARKER_ECHO},
        ],
    )

    assert payload["prompt"] == f"User:\nFirst\n\nUser:\n{MARKER_ECHO}"


def test_no_system_message_yields_empty_system(client, model):
    """Req 11.4."""
    payload = echo(
        client,
        model,
        [
            {"role": "user", "content": "Q"},
            {"role": "assistant", "content": "A"},
            {"role": "user", "content": MARKER_ECHO},
        ],
    )

    assert payload["system"] == ""


def test_model_name_maps_to_agent_and_variant(client):
    """Req 4.1: the public model name selects agent + model variant."""
    payload = echo(client, "claude-sonnet", [{"role": "user", "content": MARKER_ECHO}])

    assert payload["agent"] == "claude"
    assert payload["model"] == "sonnet"


def test_default_variant_is_empty(client):
    """Req 4.1: a name without a variant uses the agent's own default."""
    payload = echo(client, "kiro", [{"role": "user", "content": MARKER_ECHO}])

    assert payload["agent"] == "kiro"
    assert payload["model"] == ""


def test_long_conversation_is_composed_in_order(client, model):
    """Order must survive a realistic multi-turn history."""
    messages = []
    for i in range(10):
        messages.append({"role": "user", "content": f"question {i}"})
        messages.append({"role": "assistant", "content": f"answer {i}"})
    messages.append({"role": "user", "content": MARKER_ECHO})

    payload = echo(client, model, messages)

    positions = [payload["prompt"].index(f"question {i}") for i in range(10)]
    assert positions == sorted(positions), "turns were reordered"
