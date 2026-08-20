"""GET /v1/models and /v1/models/{id} — Requirement 3.

These go through the SDK rather than raw HTTP wherever possible: if the SDK
deserialises the response into its own model objects without complaining, the
schema is right by construction.
"""

import openai
import pytest


def test_list_models_returns_model_objects(client):
    """Req 3.1, 3.2."""
    page = client.models.list()
    models = list(page)

    assert models, "no models returned"
    for m in models:
        assert m.id, "model has an empty id"
        assert m.object == "model"
        assert isinstance(m.created, int) and m.created > 0
        assert m.owned_by


def test_list_models_wire_shape(raw):
    """Req 3.1: `object: "list"` with a `data` array — the SDK hides this."""
    resp = raw.get("/v1/models")

    assert resp.status_code == 200
    assert resp.headers["content-type"].startswith("application/json")
    body = resp.json()
    assert body["object"] == "list"
    assert isinstance(body["data"], list)


def test_retrieve_known_model(client):
    """Req 3.3."""
    known = list(client.models.list())[0]

    fetched = client.models.retrieve(known.id)

    assert fetched.id == known.id
    assert fetched.object == "model"
    assert fetched.created == known.created
    assert fetched.owned_by == known.owned_by


def test_retrieve_unknown_model_is_404(client):
    """Req 3.4: the error must name the model the caller asked for."""
    with pytest.raises(openai.NotFoundError) as excinfo:
        client.models.retrieve("no-such-model-xyz")

    error = excinfo.value.body
    assert error["type"] == "invalid_request_error"
    assert "no-such-model-xyz" in error["message"]


@pytest.mark.fake_only
def test_empty_registry_lists_empty_array(spawn_server):
    """Req 3.5 / 4.5: a misconfigured registry degrades to an empty list."""
    server = spawn_server(OPENAI_MODELS="this-is-not-a-valid-entry")
    client = server.client()

    assert list(client.models.list()) == []


@pytest.mark.fake_only
def test_custom_registry_is_honoured(spawn_server):
    """Req 4.4: model mappings are configurable without code changes."""
    server = spawn_server(OPENAI_MODELS="my-model:claude:sonnet,other:kiro:")
    client = server.client()

    ids = {m.id for m in client.models.list()}
    assert ids == {"my-model", "other"}


def test_model_ids_are_usable_for_completions(client):
    """Every advertised model must actually accept a completion request.

    A registry that lists models the completions endpoint then rejects is the
    kind of inconsistency SDK users hit immediately.
    """
    for m in client.models.list():
        resp = client.chat.completions.create(
            model=m.id, messages=[{"role": "user", "content": "hi"}]
        )
        assert resp.choices[0].message.content is not None
