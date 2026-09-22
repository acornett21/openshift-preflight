# UBI EOL Implementation Plan

## Agreed Behavior

| Outcome | Warning phase | Enforcement phase |
| --- | --- | --- |
| Supported | Pass | Pass |
| Explicitly EOL | Warning | Fail |
| Missing or conflicting data | Fail | Fail |
| Pyxis query failure | Fail | Fail |

The warning phase can still fail certification for data or query errors. Only a confirmed EOL result is non-enforcing.

Policies:

- Include `container` and `root`.
- Exclude `scratch-nonroot`, `scratch-root`, `konflux`, and operator policies.
- Use `HasSupportedRedHatBaseImage` as the check name.

## Stage 0: Query Validation

1. Add `internal/pyxis/repository_lifecycle.go`.
2. Add an exported `RepositoryLifecycleForLayers` method to `pyxisClient`.
3. Keep it separate from `CertifiedImagesContainingLayers` in `internal/pyxis/layers.go`. The existing method supports `BasedOnUbi` and returns unrelated freshness data.
4. Return dedicated lifecycle types containing image ID, matched DiffID, registry, repository, nullable EOL date, release categories, and metadata status.
5. Implement the Jira GraphQL query with:
   - `registry.access.redhat.com` filtering.
   - Excluded-layer removal and DiffID deduplication.
   - Stable pagination.
   - Outer and nested GraphQL error handling.
   - RFC3339 date validation.
   - Explicit handling of null data.
6. Do not add `certified:true`. "Certified images" describes the partner-image validation corpus, not the Red Hat base-image records.
7. Add hermetic tests in `internal/pyxis/repository_lifecycle_test.go`.
8. Evaluate immutable certified-image digests against real Pyxis data.
9. Record match coverage, aliases, lifecycle values, conflicts, latency, architecture, and expected classification.
10. Publish the results to EDPP-406 before check integration.

## Stage 0 Gate

Proceed only after stakeholders approve:

- Null `eol_date` interpretation.
- Whether `eol_date` or `release_categories` is authoritative.
- Which repository aliases are authoritative.
- The required accuracy and coverage thresholds.
- The highest-matching-DiffID heuristic.

## Stage 1: Warning Check

1. Add `internal/policy/container/has_supported_redhat_base_image.go`.
2. Add a narrow `repositoryLifecycleFinder` interface and inject the Pyxis client.
3. Inject a clock for deterministic date tests.
4. Select the highest matching input DiffID because OCI DiffIDs are base-to-top.
5. Classify all repository aliases for that selected layer.
6. Treat `now >= eol_date` as the EOL boundary.
7. Return:
   - `true, nil` for supported data.
   - `false, nil` for confirmed EOL, producing a warning.
   - An error for missing data, conflicts, malformed dates, no match, or query failure.
8. Set metadata to `check.LevelWarn`.
9. Add the check only to `container` and `root` in `internal/engine/engine.go`.
10. Update expected policy membership:
    - `container`: 11 checks.
    - `root`: 10 checks.
    - Scratch and Konflux counts remain unchanged.
11. Add check tests, engine policy tests, and wrapper count tests.
12. Update `docs/skills/preflight-check-container/SKILL.md` and `docs/CONFIG.md`.
13. Document the Pyxis dependency and disconnected-environment failure behavior.

## Stage 2: Enforcement

1. Confirm an enforcement date and minimum Preflight version.
2. Change the check from `check.LevelWarn` to `check.LevelBest`.
3. Keep policy membership unchanged.
4. Update level tests, documentation, and GitHub release notes.
5. Confirm Pyxis availability and data-quality targets before release.

## Verification

Run:

```bash
make fmt
make test
make lint
make vet
make tidy
```

Tests must cover pagination, null and malformed dates, nested errors, aliases, conflicting records, multiple matching layers, exact EOL boundaries, query failures, policy inclusion, and Konflux and scratch exclusion.
