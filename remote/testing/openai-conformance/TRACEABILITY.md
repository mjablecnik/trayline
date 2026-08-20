# Requirements traceability

Maps every acceptance criterion in `.kiro/specs/openai-compatible-api/requirements.md`
to the tests that verify it.

Legend — `go:` tests live in `remote/api/*_test.go`, `py:` in this directory.

## Req 1 — Chat Completions Endpoint

| AC | Covered by |
|---|---|
| 1.1 | go: `TestIntegration_NonStreaming_HappyPath` · py: `test_basic_completion_shape` |
| 1.2 | go: `TestIntegration_IgnoredParams`, `TestIntegration_ValidationMatrix/{too_many_messages,model_name_too_long}` · py: `test_ignored_parameters_are_accepted`, `test_oversized_messages_array_is_rejected` |
| 1.3 | go: `TestIntegration_NonStreaming_HappyPath` · py: `test_basic_completion_shape` |
| 1.4 | go: `TestGenerateCompletionID_Charset`, `_Uniqueness` · py: `test_ids_are_unique_across_requests` |
| 1.5 | go: `TestOpenAIChatResponse_JSONShape`, `TestIntegration_NonStreaming_HappyPath` · py: `test_basic_completion_shape` |
| 1.6 | go: `TestIntegration_NonStreaming_ComposedPrompt` · py: `test_multi_turn_conversation_keeps_role_labels` |
| 1.7 | go: `TestIntegration_ValidationMatrix` · py: `test_validation_errors_identify_the_bad_field` |
| 1.8 | go: `TestIntegration_UnknownModel` · py: `test_unknown_model_raises_not_found` |

## Req 2 — Streaming via SSE

| AC | Covered by |
|---|---|
| 2.1 | go: `TestSSEWriter_Headers`, `TestSSEWriter_FlushesEveryFrame` · py: `test_sse_wire_format`, `test_chunks_arrive_incrementally` |
| 2.2 | go: `TestSSEWriter_ChunkShape` · py: `test_stream_id_is_stable`, `test_sse_wire_format` |
| 2.3 | go: `TestIntegration_Streaming_Ordering` · py: `test_chunk_order_is_preserved` |
| 2.4 | go: `TestSSEWriter_WriteDone` · py: `test_exactly_one_finish_reason` |
| 2.5 | go: `TestSSEWriter_ExactlyOneDone` · py: `test_sse_wire_format`, `test_empty_output_still_terminates` |
| 2.6 | go: `TestSSEWriter_RoleOnlyInFirstChunk` · py: `test_role_appears_only_in_first_chunk` |
| 2.7 | go: `TestIntegration_Streaming_ContainerCrash`, `TestSSEWriter_ErrorBeforeAnyContent` · py: `test_mid_stream_crash_terminates_cleanly` |

## Req 3 — Model Listing

| AC | Covered by |
|---|---|
| 3.1 | go: `TestIntegration_ModelsEndpoints/list` · py: `test_list_models_wire_shape` |
| 3.2 | go: `TestIntegration_ModelsEndpoints/list`, `TestModelRegistry_CreatedTimestamp` · py: `test_list_models_returns_model_objects` |
| 3.3 | go: `TestIntegration_ModelsEndpoints/get_single` · py: `test_retrieve_known_model` |
| 3.4 | go: `TestIntegration_ModelsEndpoints/get_unknown` · py: `test_retrieve_unknown_model_is_404` |
| 3.5 | go: `TestIntegration_ModelsEndpoints/empty_registry_lists_nothing` · py: `test_empty_registry_lists_empty_array` |

## Req 4 — Model Name Resolution

| AC | Covered by |
|---|---|
| 4.1 | go: `TestModelRegistry_DefaultsMatchSpec` · py: `test_model_name_maps_to_agent_and_variant`, `test_default_variant_is_empty` |
| 4.2 | go: `TestModelRegistry_ResolveCaseInsensitive`, `TestIntegration_ModelResolutionIsCaseInsensitive` · py: `test_model_name_is_case_insensitive` |
| 4.3 | go: `TestIntegration_UnknownModel` · py: `test_unknown_model_raises_not_found` |
| 4.4 | go: `TestModelRegistry_MalformedConfig` · py: `test_custom_registry_is_honoured` |
| 4.5 | go: `TestModelRegistry_MalformedConfig` · py: `test_empty_registry_lists_empty_array` |

## Req 5 — Authentication

| AC | Covered by |
|---|---|
| 5.1 | go: `TestIntegration_Auth` · py: `test_invalid_token_reports_invalid_api_key` |
| 5.2 | go: `TestIntegration_Auth/missing_header…`, `TestWriteOpenAIError_NullParamAndCode` · py: `test_missing_token_raises_authentication_error`, `test_missing_header_wire_shape` |
| 5.3 | go: `TestIntegration_Auth/invalid_token…` · py: `test_invalid_token_reports_invalid_api_key` |
| 5.4 | go: `TestIntegration_Auth/models_endpoints…` · py: `test_models_endpoints_require_auth` |

## Req 6 — Error Response Format

| AC | Covered by |
|---|---|
| 6.1 | go: `TestWriteOpenAIError_NullParamAndCode`, `_PopulatedParamAndCode` · py: every error-path test asserts the shape |
| 6.2 | go: `TestIntegration_ValidationMatrix` · py: `test_validation_errors_identify_the_bad_field` |
| 6.3 | go: `TestCapacity_ReturnsFastWhenSaturated`, `TestIntegration_RateLimitErrorFormat` · py: `test_saturated_server_rejects_immediately`, `test_rate_limiter_uses_openai_error_format` |
| 6.4 | go: `TestIntegration_NonStreaming_AgentFailure` · py: `test_agent_failure_is_a_500_server_error` |
| 6.5 | go: `TestIntegration_Auth` · py: `test_missing_header_wire_shape` |

## Req 7 — Concurrency and Slot Management

| AC | Covered by |
|---|---|
| 7.1 | go: `TestCapacity_NoSelfDeadlock`, `TestCapacity_SlotsAreReleased` |
| 7.2 | go: `TestCapacity_ReturnsFastWhenSaturated`, `TestCapacity_StreamingReturnsFastWhenSaturated` · py: `test_saturated_server_rejects_immediately`, `test_streaming_rejection_is_json_not_sse` |
| 7.3 | go: `TestCapacity_SlotsAreReleased`, `TestIntegration_Streaming_ClientDisconnect` · py: `test_capacity_recovers_after_requests_finish`, `test_streaming_disconnect_frees_capacity` |
| 7.4 | go: `TestIntegration_Streaming_ClientDisconnect` · py: `test_streaming_disconnect_frees_capacity` |

## Req 8 — Non-streaming Response Structure

| AC | Covered by |
|---|---|
| 8.1 | go: `TestIntegration_NonStreaming_HappyPath` · py: `test_usage_is_present_and_consistent` |
| 8.2 | go: `TestEstimateTokens_Unicode`, `TestEstimateTokens_NonNegative` · py: `test_unicode_token_estimate_counts_characters` |
| 8.3 | go: `TestIntegration_NonStreaming_EmptyOutput`, `TestOpenAIChatResponse_EmptyContentSerialises` · py: `test_empty_agent_output_yields_empty_string` |

## Req 9 — Request Validation

| AC | Covered by |
|---|---|
| 9.1–9.4 | go: `TestIntegration_ValidationMatrix` (one subtest per case) · py: `test_validation_errors_identify_the_bad_field` (parametrised) |
| 9.5 | go: `TestIntegration_ValidRolesAccepted`, `TestIntegration_DeveloperRole` · py: `test_all_valid_roles_are_accepted`, `test_still_unknown_roles_are_rejected` |

## Req 10 — Ignored Parameters

| AC | Covered by |
|---|---|
| 10.1 | go: `TestIntegration_IgnoredParams/all_documented…` · py: `test_ignored_parameters_are_accepted` |
| 10.2 | go: `TestIntegration_NGreaterThanOne` · py: `test_n_greater_than_one_returns_one_choice` |
| 10.3 | go: `TestIntegration_IgnoredParams/unknown_parameters…` · py: `test_unknown_parameters_are_ignored` |
| 10.4 | go: `TestIntegration_IgnoredParams/wrong_types…` · py: `test_wrong_types_on_ignored_parameters_are_tolerated` |

## Req 11 — Multi-turn Conversation Handling

| AC | Covered by |
|---|---|
| 11.1 | go: `TestComposeMessages_Table` · py: `test_system_message_goes_to_the_system_parameter`, `test_multiple_system_messages_are_joined` |
| 11.2 | go: `TestComposeMessages_Table` · py: `test_multi_turn_conversation_keeps_role_labels` |
| 11.3 | go: `TestComposeMessages_Table` · py: `test_system_message_goes_to_the_system_parameter` |
| 11.4 | go: `TestComposeMessages_Table` · py: `test_no_system_message_yields_empty_system` |
| 11.5 | go: `TestComposeMessages_Table` · py: `test_single_user_message_is_passed_through` |
| 11.6 | go: `TestComposeMessages_Table` · py: `test_adjacent_same_role_messages_are_kept_separate` |

## Req 12 — Existing API Preservation

| AC | Covered by |
|---|---|
| 12.1 | go: `TestIntegration_ExistingEndpointsUnchanged`, `TestIntegration_Auth/existing_endpoints…` — plus the pre-existing suite passing unchanged |
| 12.2 | go: `TestIntegration_*` all run through the production `NewRouter` |
| 12.3 | go: `TestIntegration_Auth`, `TestIntegration_RateLimitErrorFormat` |

## Design properties

| Property | Covered by |
|---|---|
| 1 — Model resolution idempotency | go: `TestModelRegistry_ResolveIdempotent` |
| 2 — Single choice invariant | go: `TestIntegration_NGreaterThanOne`, `TestSSEWriter_ChunkShape` |
| 3 — SSE ordering | go: `TestIntegration_Streaming_Ordering` · py: `test_chunk_order_is_preserved` |
| 4 — Slot balance | go: `TestCapacity_SlotsAreReleased` |
| 5 — Stream termination | go: `TestSSEWriter_ExactlyOneDone` · py: `test_exactly_one_finish_reason` |
| 6 — Backward compatibility | go: `TestIntegration_ExistingEndpointsUnchanged` |
| 7 — Token estimation consistency | go: `TestEstimateTokens_*` · py: `test_usage_is_present_and_consistent` |

## Open items — decisions, not spec violations

Behaviours outside the specification that affect real-world client
compatibility. Each has a passing test pinning current behaviour and an `xfail`
test describing the improvement.

| # | Behaviour today | Status | Test |
|---|---|---|---|
| 1 | Unrouted `/v1/` paths (`/v1/embeddings`) return Go's mux default — a plain-text 404 rather than an OpenAI error body | **Accepted.** The SDK still raises `NotFoundError`; only the message is unhelpful | `test_unimplemented_v1_endpoints_use_openai_error_format` (xfail) |
| 2 | `stream_options.include_usage` is accepted but emits no usage chunk | **Accepted.** Undefined by the spec; callers get no stream token counts | `test_stream_options_include_usage_is_accepted` |
| 3 | CORS allows only the dashboard origin | **Accepted.** Browser-side SDK use is blocked — arguably correct, since the token would be exposed to the page | `test_cors_preflight_behaviour_is_pinned` |

Resolved since the first review:

| Was | Now | Test |
|---|---|---|
| `content` had to be a string; typed-part arrays were a 400 | Text parts accepted and newline-joined; non-text parts rejected with a message naming the type | `test_client_compat.py` · go: `TestIntegration_ContentParts`, `TestOpenAIMessage_UnmarshalContentForms` |
| Role `developer` was rejected as unknown | Normalised to `system` | `test_client_compat.py` · go: `TestIntegration_DeveloperRole` |
| `messages` and model name lengths were uncapped (Req 1.2) | 128 messages / 256 characters enforced | go: `TestIntegration_ValidationMatrix` |

## Not covered

| Area | Why |
|---|---|
| Real agent execution | Requires Docker, credentials and credits — `test_live.py`, run manually |
| Sustained load / soak | Needs a dedicated environment; `k6`/`hey` against a real deployment |
| Third-party client interop (Cline, Continue, Open WebUI) | Manual, one-off — worth doing now that content parts and the `developer` role are supported |
