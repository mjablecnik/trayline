"""Request shapes real OpenAI-compatible clients send.

The specification defines `content` as a string and allows only the roles
`system`, `user` and `assistant`. In practice the OpenAI SDKs and the popular
clients built on them (Cline, Continue, LangChain, LibreChat, Open WebUI) emit
structured content arrays and the newer `developer` role. Both are accepted;
these tests are the contract.
"""

import json

import openai
import pytest

MODEL = "kiro"
MARKER_ECHO = "__echo__"


def parts(*texts):
    return [{"type": "text", "text": t} for t in texts]


# --- Structured content parts ----------------------------------------------


def test_content_parts_are_accepted(client):
    resp = client.chat.completions.create(
        model=MODEL, messages=[{"role": "user", "content": parts("Hello")}]
    )

    assert resp.choices[0].message.content is not None


def test_content_parts_match_the_plain_string_form(client):
    """A single text part must behave exactly like the equivalent string."""
    as_string = client.chat.completions.create(
        model=MODEL, messages=[{"role": "user", "content": "Hello"}]
    )
    as_parts = client.chat.completions.create(
        model=MODEL, messages=[{"role": "user", "content": parts("Hello")}]
    )

    assert as_parts.choices[0].message.content == as_string.choices[0].message.content


@pytest.mark.fake_only
def test_multiple_text_parts_are_flattened(client):
    """Parts are joined with newlines before reaching the agent."""
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": parts("first line", MARKER_ECHO)}],
    )
    payload = json.loads(resp.choices[0].message.content)

    assert payload["prompt"] == f"first line\n{MARKER_ECHO}"


@pytest.mark.fake_only
def test_content_parts_work_in_a_multi_turn_conversation(client):
    """Mixed string and parts messages must compose into one transcript."""
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "user", "content": "What is a goroutine?"},
            {"role": "assistant", "content": parts("A lightweight thread.")},
            {"role": "user", "content": parts(MARKER_ECHO)},
        ],
    )
    payload = json.loads(resp.choices[0].message.content)

    assert payload["prompt"] == (
        "User:\nWhat is a goroutine?\n\n"
        "Assistant:\nA lightweight thread.\n\n"
        f"User:\n{MARKER_ECHO}"
    )


def test_content_parts_stream(client):
    """The parts form must work on the streaming path too."""
    chunks = list(
        client.chat.completions.create(
            model=MODEL,
            messages=[{"role": "user", "content": parts("Hello")}],
            stream=True,
        )
    )

    assert chunks
    assert chunks[-1].choices[0].finish_reason == "stop"


def test_image_parts_are_rejected_with_a_useful_message(client):
    """The CLI agents cannot see images.

    Rejecting is deliberate: answering as though an attachment had been read
    would be a worse failure than a clear error.
    """
    with pytest.raises(openai.BadRequestError) as excinfo:
        client.chat.completions.create(
            model=MODEL,
            messages=[
                {
                    "role": "user",
                    "content": [
                        {"type": "text", "text": "what is this?"},
                        {"type": "image_url", "image_url": {"url": "http://example.com/x.png"}},
                    ],
                }
            ],
        )

    error = excinfo.value.body
    assert error["type"] == "invalid_request_error"
    assert "image_url" in error["message"]


def test_empty_parts_array_is_a_validation_error(raw):
    resp = raw.post(
        "/v1/chat/completions",
        json={"model": MODEL, "messages": [{"role": "user", "content": []}]},
    )

    assert resp.status_code == 400
    assert resp.json()["error"]["param"] == "messages[0]"


def test_unicode_survives_part_flattening(client):
    resp = client.chat.completions.create(
        model=MODEL, messages=[{"role": "user", "content": parts("Příliš žluťoučký kůň 🐴")}]
    )

    assert resp.choices[0].message.content is not None


# --- The developer role -----------------------------------------------------


def test_developer_role_is_accepted(client):
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "developer", "content": "Be brief"},
            {"role": "user", "content": "hi"},
        ],
    )

    assert resp.choices[0].message.content is not None


@pytest.mark.fake_only
def test_developer_role_becomes_the_system_prompt(client):
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "developer", "content": "You are terse"},
            {"role": "user", "content": MARKER_ECHO},
        ],
    )
    payload = json.loads(resp.choices[0].message.content)

    assert payload["system"] == "You are terse"
    assert payload["prompt"] == MARKER_ECHO


@pytest.mark.fake_only
def test_developer_and_system_messages_combine(client):
    """Both spellings must land in the system prompt, in array order."""
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "system", "content": "First rule"},
            {"role": "developer", "content": "Second rule"},
            {"role": "user", "content": MARKER_ECHO},
        ],
    )
    payload = json.loads(resp.choices[0].message.content)

    assert payload["system"] == "First rule\nSecond rule"


@pytest.mark.fake_only
def test_developer_role_with_content_parts(client):
    """The two features must compose."""
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "developer", "content": parts("Be brief")},
            {"role": "user", "content": MARKER_ECHO},
        ],
    )
    payload = json.loads(resp.choices[0].message.content)

    assert payload["system"] == "Be brief"


def test_still_unknown_roles_are_rejected(raw):
    """Widening the role set must not turn it into an accept-anything list."""
    resp = raw.post(
        "/v1/chat/completions",
        json={
            "model": MODEL,
            "messages": [{"role": "wizard", "content": "nope"}],
        },
    )

    assert resp.status_code == 400
    assert resp.json()["error"]["param"] == "messages[0].role"
