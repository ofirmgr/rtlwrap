# rtlwrap agent notes

- Never commit or push without explicit human approval.
- Read the README before changes and rerun `go test ./...` for the current checkout.

## Agent memory

Durable repository knowledge lives in [docs/agents/project-memory.md](docs/agents/project-memory.md).
Every agent, including subagents, reads it before work and records new durable repo facts there
during the same task, never in a tool-private memory store.
