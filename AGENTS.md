# integration-sandbox — agent collaboration rules

This file is read by any sub-agent spawned from inside `integration-sandbox/`.

## Universal rules

1. Read `.claude/CLAUDE.md` before doing anything else.
2. **No edits outside this repository.** Full stop. If a sibling repo has
   a bug, file an upstream issue against that repo — don't patch it from
   here even if you happen to have it cloned locally.
3. Use `printf` to update `PROGRESS.log` and `STATUS` at every milestone.
   The user is reading these on a phone.
4. Every script you author must:
   - start with `set -euo pipefail`
   - log to `logs/<UTC>/<scriptname>.log` via `tee`
   - exit non-zero on any failure
   - be idempotent (check-then-create, never blind create)
5. When a sub-step times out, capture `docker compose ps`, `docker ps -a`, and `docker compose logs --tail=200` for the offending service into the log bundle, then move on or stop per the failure protocol.

## Search agents (Explore)
Use `Explore` for cross-repo lookups — never search siblings yourself; the result window is small and you'll waste tokens.

## Implementation agents
The default `general-purpose` agent is fine for compose / fixture / test scaffolding. Brief it with the exact files to write and the conventions in `.claude/CLAUDE.md`. Do not delegate "figure out what to test" — that's the orchestrator's job.

## Don't
- Don't `kind`, `kubectl`, or anything Kubernetes in v1.
- Don't open the docker daemon socket inside containers (no `-v /var/run/docker.sock`).
- Don't add hostPort publishes for services that only need to talk on the docker network — minimize external surface.
- Don't introduce new ports, credentials, or xnames without updating both `.claude/CLAUDE.md` and the relevant `docs/` reference card.
