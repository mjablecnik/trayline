# Repository guidance

When adding an AI agent or extending agent support, read and follow [the agent integration guide](docs/ADDING_AN_AGENT.md). Review every integration surface and use its completion checklist; a CLI/executor change alone does not enable YAML pipelines, API endpoints, chat, or the dashboard.

Update the guide in the same change if integration points move or new surfaces are introduced. Report automated checks separately from live provider verification and identify any unsupported or untested surface.

See [CLAUDE.md](CLAUDE.md) for the repository layout, dependency direction rules, and build/test commands.
