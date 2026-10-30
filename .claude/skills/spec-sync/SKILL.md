---
name: spec-sync
description: Use when the Orchestra API may have changed — you have been told the specs drifted, `make openapi` changed a committed spec, TestEverySpecOperationIsClassified or TestEveryWrittenPropertyIsClassified fails, or you are asked whether the API has new resources, data sources or parameters to add. Decides what each change means for the provider and applies it.
---

# Keeping the provider in step with the API

When the published Orchestra API changes, this skill decides what that means for the
provider.

## 1. See what changed

Run `make openapi` and read `git diff internal/apicontract`, then `go test ./internal/apicontract/`.
The classification tests name every new endpoint and every new property of a request body the
provider sends. Prose changes need nothing; enum changes are already regenerated.

If the fetch fails, the spec has moved. The docs site once split its one spec into
`engine.yaml`, `agents.yaml` and `admin.yaml`. The docs MCP's `/openapi/api/` listing
names the current files; fix `SPEC_BASE` in the Makefile.

## 2. Decide each item

**New endpoint.** Ask what it is, not what it is called:

| It is… | Do |
|---|---|
| Declarative configuration the workspace owns, with CRUD | A resource |
| A settings singleton (GET + PATCH, no create or delete) | A resource. Say in the docs what destroy does: remove from state, leave the settings. |
| Read-only workspace data that other configuration references (IDs) | A data source |
| Run history, logs, incidents, audit, engine state | Exclude: it changes on every run, so state would diff forever |
| A one-shot command (start, cancel, mute, merge) | Exclude: applying it twice is not the same as once |
| A static catalogue of integrations or jobs | Exclude: `internal/enums` already generates it |
| In the `admin` spec | Exclude, or `candidate_*`: the provider does not call that API |

Excluding means an entry in `internal/apicontract/coverage.yaml` under a category that
explains **why**. When the honest answer is "not yet", use a `candidate_*` category and
say what building it would take. Do not build speculatively; a resource nobody asked
for is surface to maintain.

**Constraint changed** (`maxLength`, `maximum`, `pattern`): add or adjust the
plan-time validator. The API already rejects the value, so this only moves the error
from apply to plan.

**Shape changed** (a list became a union, a type moved): see "Modelling the API in
Terraform" in CLAUDE.md. Terraform has no unions, so use sibling attributes merged on the
wire. This is the one that breaks silently: a monitor's `groupBy` became a union and
the provider could no longer read monitors that used it.

**New property on a schema the provider writes**: model it. Otherwise an update sends
the body without it and may drop a value set elsewhere. `go test` enforces this:
TestEveryWrittenPropertyIsClassified fails until the property is sent, as a JSON tag on
the wire struct named in the failure or a key in the map literal that builds the body,
or is listed under `properties` in `coverage.yaml`. Exclude only what the provider must
not send, such as a git-only field; "not yet" goes in `candidate_properties`, and on a
PUT body it is a bug waiting for an apply.

**Removed endpoint or enum value**: breaking. Fix the callers, and say so in CHANGELOG.

## 3. Build it

Follow the workflow in CLAUDE.md. These each cost a CI round-trip before:

- **Make the stub apply the server's defaults first.** The API fills in declared defaults
  on the way out, and a stub that echoes the request hides every resulting
  "inconsistent result after apply". Reproduce the failure locally before fixing it.
- **Check defaults against the live API.** The spec said `minBaselineSeconds` was 0; the API
  returned 10. Where the server defaults a field, make it `Computed`.
- **An acceptance test is required.** CI runs them against a real workspace on every PR.
- **When you implement an endpoint or property listed in `coverage.yaml`, delete its entry.** The
  classification test fails if you do not, which is how it stays honest.

## 4. Check

```bash
go build ./... && go vet ./... && go test ./...
make docs
```

Add a CHANGELOG entry for anything a user can see.
