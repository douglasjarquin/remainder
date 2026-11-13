---
title: "feat: AgentReady audit remediation for the repository surface"
date: 2026-11-13
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-plan-bootstrap
execution: code
---

# feat: AgentReady audit remediation for the repository surface

## Goal Capsule

**Objective:** The repository presents itself to AI agents the way the AgentReady open standard measures, and issue #53 carries a scored audit with a before/after.
A reader or agent landing on the repository finds a curated `llms.txt` index, linked discovery files, and fully language-tagged code fences.

**Means:** Add `llms.txt` at the repository root and repair the two markdown gaps the audit found in `README.md` (KTD1–KTD3).

**Authority:** The sum brief for issue #53 is the controlling instruction; AgentReady spec v2.0.1 supplies the criteria; `AGENTS.md` and `VERIFY.md` supply repo constraints.

**Stop conditions:** Stop if a failing MUST cannot be fixed without editing `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `GROK.md`, or `.compound-engineering/` — issue #54 owns those.

**Execution profile:** One pull request against `main` from `maintain/issue-53-agentready`, using the sum pull-request template; the PR may say `Fixes #53` and is never merged by the agent.

## Product Contract

### Summary

Remainder is a local one-shot Go quota CLI with no deployed site.
The AgentReady rubric is a website rubric, so most FIND/READ criteria are N/A for this product; the applicable gaps are a missing `llms.txt`, discovery files not linked from `README.md`, and one untagged fenced code block.
The remediation closes the applicable SHOULDs without touching quota behavior, credentials, providers, or the CLI contract.

### Problem Frame

Issue #53 asks for a scored AgentReady audit of this project plus remediation of the gaps.
Because the product is a repository-hosted CLI rather than a website, the honest audit marks site-dependent criteria N/A and fixes what a repository can actually ship.

### Requirements

- R1. A scored audit covering every criterion listed in issue #53 with Pass / Partial / Fail / N/A, evidence, an overall score, and a before/after; posted as exactly one comment on issue #53.
- R2. Fix failing MUSTs first, then SHOULDs; MAYs are optional.
- R3. Ship `llms.txt` at the repository root (AR-READ-07): a lean, described index of the repository's key first-party pages.
- R4. Link the repository's discovery files (`llms.txt`, `AGENTS.md`) from `README.md` (AR-READ-06).
- R5. Every fenced code block in `README.md` carries a language tag (AR-READ-05).
- R6. No change to quota behavior, credentials, providers, or the CLI contract; no edits to `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `GROK.md`, or anything under `.compound-engineering/`.
- R7. Commit only on `maintain/issue-53-agentready`, push only that branch with an explicit refspec, and open a PR against `main` using `/Users/douglasjarquin/github/sum/templates/pull-request.md`.
- R8. Run `mise run verify` and record the result.

### Success Criteria

- `curl https://raw.githubusercontent.com/douglasjarquin/remainder/main/llms.txt` would serve the index after merge.
- `rg -n '^```$' README.md` finds no untagged opening fences.
- The issue-53 comment records the before/after scores.

### Scope Boundaries

Out of scope: any website, host, dashboard, or new product surface; changes to provider collection, cache, or CLI behavior; files owned by issue #54.

### Key Decisions

- **Treat the GitHub repository as the first-party surface** (`session-settled:user-directed`, rejected: scoring the repo against a nonexistent deployed site or building one — the brief forbids new product surface). Governs R1, R2.
- **Remediation touches only repository markdown/metadata files** (`session-settled:user-directed`, rejected: editing `AGENTS.md` or `.compound-engineering/` — issue #54 owns them). Governs R3, R4, R5, R6.

## Planning Contract

### Key Technical Decisions

- KTD1 (`session-settled:user-directed`): Site-dependent criteria are scored N/A with a one-line reason rather than forced into Pass/Fail; the GitHub repo page and `raw.githubusercontent.com` are the reachable first-party surface, already verified to return 200 to an agent user-agent and real 404s for missing paths.
- KTD2 (`session-settled:user-directed`): No behavior change — the diff is documentation and metadata only, so no feature-map update is needed under `VERIFY.md`; the report states that evidence requirement as not applicable.
- KTD3: `llms.txt` uses relative links to `README.md`, `docs/`, `VERIFY.md`, `CONTRIBUTING.md`, and `skills/remainder/` so it works equally on the GitHub blob view and on `raw.githubusercontent.com`.

### Assumptions

- AR-READ-10 is retired in AgentReady spec v2.0.1 and is scored N/A as retired.
- The repository ships `skills/remainder/` as an agent-facing skill; it is listed in `llms.txt` as evidence of the act surface.
- AR-ACT-10 passes because the product is itself a CLI; AR-ACT-03 (MCP server) is N/A per the spec's "build the one that fits" guidance.

## Implementation Units

### U1. Ship root llms.txt

**Goal:** Give agents a curated, described index of the repository's key pages (AR-READ-07).

**Requirements:** R3

**Dependencies:** none

**Files:** `llms.txt` (create)

**Approach:** Follow the spec's informative `llms.txt` shape: an `# remainder` heading, a one-line blockquote description, then sections listing the README, the `docs/remainder/` contract pages, provider feature maps, `VERIFY.md`, `CONTRIBUTING.md`, and `skills/remainder/SKILL.md`, each as a markdown link with a short description.
Use repo-relative links so the file resolves under both the GitHub blob view and `raw.githubusercontent.com`.
Each full sentence goes on its own line per the maintainer's markdown convention.

**Test scenarios:** `Test expectation: none — documentation-only addition.`

**Verification:** File exists at repo root; every link resolves to a tracked path in the checkout.

### U2. Repair README agent-readiness gaps

**Goal:** Link discovery files and language-tag the remaining untagged fence (AR-READ-06, AR-READ-05).

**Requirements:** R4, R5

**Dependencies:** U1 (llms.txt must exist to be linked)

**Files:** `README.md` (modify)

**Approach:**

1. Add `llms.txt` and `AGENTS.md` entries to the existing Documentation list, matching its format.
2. Change the untagged fence around the "How it works" ASCII diagram (lines ~59–66) to ```` ```text ````.

**Test scenarios:** `Test expectation: none — documentation-only edits.`

**Verification:** No `^```$` opening fences remain in `README.md`; the new links resolve to tracked paths.

## Verification Contract

- `mise run verify` (formatting, vet, race-enabled tests, dependency contract, CGO-free build).
- `rg -n '^```$' README.md` returns only closing fences.
- `git status` shows only `llms.txt` and `README.md` changed.
- No user-visible CLI behavior changes; the VERIFY.md scenario-evidence requirement is not applicable.

## Definition of Done

- `llms.txt` present at repo root with working relative links.
- `README.md` links `llms.txt` and `AGENTS.md`; all its code fences are language-tagged.
- `mise run verify` passes.
- No edits to `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `GROK.md`, or `.compound-engineering/`; no stray experimental files in the diff.
