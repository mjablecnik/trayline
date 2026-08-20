"""Real-world SDK quirks the specification does not cover.

These are not spec violations — they are behaviours that decide whether popular
OpenAI-compatible clients (Cline, Continue, LangChain, LibreChat, Open WebUI)
work against this server out of the box.

Where an area is settled it has a plain test; where the behaviour is a pinned
decision rather than a requirement, the test says so. Anything marked ``xfail``
documents an improvement that has not been made — an xfail that starts passing
is reported as XPASS, so implementing it is a visible event, not silent drift.

Content-parts arrays and the ``developer`` role graduated out of this file into
test_client_compat.py once they were supported. See TRACEABILITY.md.
"""

import pytest

MODEL = "kiro"
SIMPLE = [{"role": "user", "content": "hi"}]


# --- Unimplemented /v1/ endpoints ------------------------------------------
# A client probing for capabilities should get a parseable error, not an empty
# body from the mux's default handler.


@pytest.mark.parametrize(
    "method,path",
    [
        ("POST", "/v1/embeddings"),
        ("POST", "/v1/completions"),
        ("GET", "/v1/models/"),
        ("POST", "/v1/chat/completions/extra"),
    ],
)
def test_unimplemented_v1_endpoints_return_a_status_not_a_hang(raw, method, path):
    resp = raw.request(method, path, json={})

    assert resp.status_code in (404, 405), f"{method} {path} → {resp.status_code}"


@pytest.mark.xfail(reason="unrouted /v1/ paths fall through to the mux default, not the OpenAI error format")
@pytest.mark.parametrize("path", ["/v1/embeddings", "/v1/completions"])
def test_unimplemented_v1_endpoints_use_openai_error_format(raw, path):
    resp = raw.post(path, json={})

    body = resp.json()
    assert "error" in body
    assert body["error"]["type"]


# --- Method handling --------------------------------------------------------


def test_get_on_completions_is_rejected_cleanly(raw):
    resp = raw.get("/v1/chat/completions")

    assert resp.status_code in (404, 405)


# --- CORS -------------------------------------------------------------------
# Browser-side use of the OpenAI SDK requires permissive CORS. The server allows
# only the configured dashboard origin, which is a defensible choice (the API
# token would be exposed to the page anyway) — pinned here so a change is
# deliberate.


def test_cors_preflight_behaviour_is_pinned(raw):
    resp = raw.request(
        "OPTIONS",
        "/v1/chat/completions",
        headers={
            "Origin": "https://example.com",
            "Access-Control-Request-Method": "POST",
        },
    )

    allowed = resp.headers.get("access-control-allow-origin")
    assert allowed in (None, "", "https://example.com"), (
        f"unexpected CORS policy for a third-party origin: {allowed!r}"
    )


# --- Robustness -------------------------------------------------------------


def test_oversized_messages_array_is_rejected(raw):
    """Req 1.2 caps the conversation at 128 messages."""
    messages = [{"role": "user", "content": f"m{i}"} for i in range(500)]

    resp = raw.post("/v1/chat/completions", json={"model": MODEL, "messages": messages})

    assert resp.status_code == 400, resp.text
    error = resp.json()["error"]
    assert error["type"] == "invalid_request_error"
    assert error["param"] == "messages"


def test_messages_at_the_limit_are_accepted(raw):
    """The boundary itself must be inside the accepted range, not outside."""
    messages = [{"role": "user", "content": f"m{i}"} for i in range(128)]

    resp = raw.post("/v1/chat/completions", json={"model": MODEL, "messages": messages})

    assert resp.status_code == 200, resp.text


def test_very_long_model_name_is_rejected_cleanly(raw):
    """Req 1.2 caps the model name at 256 characters."""
    resp = raw.post(
        "/v1/chat/completions", json={"model": "x" * 5000, "messages": SIMPLE}
    )

    assert resp.status_code == 400
    error = resp.json()["error"]
    assert error["type"] == "invalid_request_error"
    assert error["param"] == "model"


def test_deeply_nested_junk_does_not_crash_the_server(raw):
    """Malformed input must never take the process down (Req 10.3)."""
    payload = {"model": MODEL, "messages": SIMPLE, "junk": {}}
    node = payload["junk"]
    for _ in range(200):
        node["n"] = {}
        node = node["n"]

    resp = raw.post("/v1/chat/completions", json=payload)
    assert resp.status_code in (200, 400)

    # The server must still be answering afterwards.
    assert raw.get("/health").status_code == 200


def test_health_endpoint_needs_no_auth(raw):
    resp = raw.get("/health", headers={"Authorization": ""})

    assert resp.status_code == 200
