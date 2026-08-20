"""Authentication, validation and error format — Requirements 5, 6, 9.

Errors are asserted through the SDK's exception hierarchy. That is the property
that actually matters to an integrator: a wrong status code or a non-OpenAI body
shows up as the wrong exception type (or none at all), not as a cosmetic
difference.
"""

import openai
import pytest


# --- Authentication (Req 5) ------------------------------------------------


def test_missing_token_raises_authentication_error(server, model):
    """Req 5.2: a request with no Authorization header must 401.

    The SDK refuses to construct a client without an api_key, so the header is
    cleared via default_headers instead.
    """
    client = server.client(default_headers={"Authorization": ""})

    with pytest.raises(openai.AuthenticationError) as excinfo:
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": "hi"}]
        )

    error = excinfo.value.body
    assert error["type"] == "invalid_request_error"
    assert error["param"] is None
    assert error["code"] is None


def test_missing_header_wire_shape(raw, model):
    """Req 5.2 at the wire level."""
    resp = raw.post(
        "/v1/chat/completions",
        json={"model": model, "messages": [{"role": "user", "content": "hi"}]},
        headers={"Authorization": ""},
    )

    assert resp.status_code == 401
    error = resp.json()["error"]
    assert error["type"] == "invalid_request_error"
    assert error["message"]
    assert "param" in error and error["param"] is None
    assert "code" in error and error["code"] is None


def test_invalid_token_reports_invalid_api_key(server, model):
    """Req 5.3."""
    client = server.client(api_key="definitely-not-the-token")

    with pytest.raises(openai.AuthenticationError) as excinfo:
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": "hi"}]
        )

    error = excinfo.value.body
    assert error["type"] == "invalid_request_error"
    assert error["code"] == "invalid_api_key"


def test_models_endpoints_require_auth(server):
    """Req 5.4."""
    client = server.client(api_key="definitely-not-the-token")

    with pytest.raises(openai.AuthenticationError):
        list(client.models.list())
    with pytest.raises(openai.AuthenticationError):
        client.models.retrieve("kiro")


def test_malformed_authorization_scheme_is_rejected(raw, model):
    """Anything other than a Bearer token is a 401, not a 500."""
    for header in ("Basic dXNlcjpwYXNz", "Bearer", "token abc", "bearer lowercase"):
        resp = raw.post(
            "/v1/chat/completions",
            json={"model": model, "messages": [{"role": "user", "content": "hi"}]},
            headers={"Authorization": header},
        )
        assert resp.status_code == 401, f"{header!r} produced {resp.status_code}"
        assert resp.json()["error"]["type"] == "invalid_request_error"


# --- Validation (Req 9) ----------------------------------------------------


@pytest.mark.parametrize(
    "body,want_param",
    [
        ({"messages": [{"role": "user", "content": "hi"}]}, "model"),  # Req 9.1
        ({"model": "", "messages": [{"role": "user", "content": "hi"}]}, "model"),  # Req 9.1
        ({"model": "kiro"}, "messages"),  # Req 9.2
        ({"model": "kiro", "messages": []}, "messages"),  # Req 9.2
        ({"model": "kiro", "messages": [{"content": "hi"}]}, "messages[0]"),  # Req 9.3
        ({"model": "kiro", "messages": [{"role": "user"}]}, "messages[0]"),  # Req 9.3
        (
            {
                "model": "kiro",
                "messages": [
                    {"role": "user", "content": "ok"},
                    {"role": "wizard", "content": "nope"},
                ],
            },
            "messages[1].role",
        ),  # Req 9.4
    ],
)
def test_validation_errors_identify_the_bad_field(raw, body, want_param):
    resp = raw.post("/v1/chat/completions", json=body)

    assert resp.status_code == 400, resp.text
    error = resp.json()["error"]
    assert error["type"] == "invalid_request_error"
    assert error["param"] == want_param
    assert error["message"]


def test_validation_error_surfaces_as_bad_request_error(client):
    """The SDK must classify it as BadRequestError, not a generic failure."""
    with pytest.raises(openai.BadRequestError):
        client.chat.completions.create(model="kiro", messages=[])


def test_malformed_json_body(raw):
    resp = raw.post(
        "/v1/chat/completions",
        content=b'{"model": "kiro", "messages": [',
        headers={"Content-Type": "application/json"},
    )

    assert resp.status_code == 400
    assert resp.json()["error"]["type"] == "invalid_request_error"


def test_empty_body(raw):
    resp = raw.post(
        "/v1/chat/completions", content=b"", headers={"Content-Type": "application/json"}
    )

    assert resp.status_code == 400
    assert resp.json()["error"]["type"] == "invalid_request_error"


def test_all_valid_roles_are_accepted(client, model):
    """Req 9.5."""
    resp = client.chat.completions.create(
        model=model,
        messages=[
            {"role": "system", "content": "Be brief"},
            {"role": "user", "content": "Question"},
            {"role": "assistant", "content": "Answer"},
            {"role": "user", "content": "Follow-up"},
        ],
    )

    assert resp.choices[0].message.content is not None


# --- Model resolution errors (Req 1.8, 4.3) --------------------------------


def test_unknown_model_raises_not_found(client):
    with pytest.raises(openai.NotFoundError) as excinfo:
        client.chat.completions.create(
            model="gpt-4o", messages=[{"role": "user", "content": "hi"}]
        )

    error = excinfo.value.body
    assert error["type"] == "invalid_request_error"
    assert error["code"] == "model_not_found"
    assert "gpt-4o" in error["message"]


# --- Server errors (Req 6.4) -----------------------------------------------


@pytest.mark.fake_only
def test_agent_failure_is_a_500_server_error(client, model):
    with pytest.raises(openai.InternalServerError) as excinfo:
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": "__fail__"}]
        )

    error = excinfo.value.body
    assert error["type"] == "server_error"
    assert error["param"] is None
    assert error["code"] is None


@pytest.mark.fake_only
def test_agent_timeout_is_a_500_server_error(spawn_server, model):
    """The design's task-timeout rule: a hung agent must not hang the client."""
    server = spawn_server(TASK_TIMEOUT="1s")
    client = server.client()

    with pytest.raises(openai.InternalServerError) as excinfo:
        client.chat.completions.create(
            model=model, messages=[{"role": "user", "content": "__hang__"}]
        )

    assert excinfo.value.body["type"] == "server_error"
    assert "timed out" in excinfo.value.body["message"].lower()
