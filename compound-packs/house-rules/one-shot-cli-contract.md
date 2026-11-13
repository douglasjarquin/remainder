---
title: Preserve the one-shot CLI contract and honest unavailable behavior
applies_when:
  - adding a provider command or changing report output
  - touching credential, cache, or network access paths
tags: [cli, providers, verification]
---

Remainder is a one-shot CLI: it binds no port, starts no service, and writes no telemetry.
Help, version, validation, and ordinary offline tests perform no credential, cache, network, or provider access.
An explicitly selected provider report uses the account/source-bound cache and performs at most one bounded read-only credential read and provider request when a refresh is required.
When a provider cannot be read, report it as unavailable instead of fabricating data.
Future provider commands must preserve this contract.
