# Changelog

All notable changes to Yoink will be documented here. Releases use semantic versioning.

## [0.2.0] - 2026-11-04

### Added

- `Options` and `ParseWithOptions` for per-call flat, depth-limited, or unlimited
  recursive composition. Existing `Parse` and the CLI without new flags remain flat.
- CLI `-depth 1`, positive integer depths, and `-depth all` selection, including
  validation and unchanged stdin, stdout, and output-file behavior.
- Active-ancestry cycle detection for unlimited recursion, including canonical
  local paths and symlink aliases, without rejecting reuse in independent branches
  or terminating range-selected self-inclusion.
- Shared per-call operation, input-byte, output-byte, and timeout budgets;
  `ErrLimitExceeded`, `LimitError`, and `ErrCycle` support normal error handling.
- Recursive, compatibility, CLI, subprocess survival, HTTP, resource-boundary,
  graph fuzz, and deep-chain tests, plus realistic source-relative public fixtures.

### Changed

- Recursive built-in inclusions retain their source identity and original line
  numbers. Nested local paths resolve from the containing file; remote-relative
  URLs resolve from the containing document's final HTTP(S) response URL.
  Loaded local symlinks resolve nested paths from the canonical target directory;
  caller-owned root readers retain the directory of their supplied source name.
- Address selection, full-line rounding, OMIT removal, and included edge-blank
  trimming happen before nested expansion. Excluded directives do not execute.
- Recursive work uses a bounded worker pool and explicit queue, with iterative
  final output assembly to avoid uncontrolled call-stack growth and repeated
  intermediate expanded-document copies.
- Finite depths leave directives at the boundary literal. Unlimited mode has finite
  default budgets; all started workers still complete before return on errors,
  cancellation, cycles, and exhausted limits.
- Existing custom parser interfaces remain supported. Generated custom-command
  output is inserted literally and is not reinterpreted as source input.

The default flat API retains the v0.1.0 resource behavior. Recursion defaults to
10,000 command operations, 32 MiB cumulative input, 32 MiB final output, and a
30-second cooperative timeout. Yoink remains a general-purpose filesystem and
HTTP(S) composition library, not an access sandbox. Noncooperative callbacks,
readers, or filesystem calls can delay return; custom parser allocations remain
that parser's responsibility.

## [0.1.0] - 2026-11-04

### Added

- First formally versioned release of Yoink's existing Go library and CLI: local
  and HTTP(S) file inclusion, sam-style addresses, OMIT filtering, flat composition,
  concurrent processing with source ordering, and custom parser registration.
- Optional `ContextParser` interface for cooperative cancellation, alongside the
  existing `Parser`, `ParseFunc`, and unchanged `Parse` function signature.
- Regression, subprocess host-survival, public API integration, CLI, registry race,
  worker-lifetime, HTTP cancellation, and address fuzz tests.
- CI formatting, race detector, vet, and build checks on stable Go.

### Changed

- Parsing failures return an empty result and wrapped errors with source location.
  Callers can inspect underlying errors with `errors.Is` and `errors.As`.
- Every started parser worker completes before `Parse` returns. Worker failures
  cancel sibling work. Legacy callbacks that ignore cancellation can delay return;
  arbitrary blocked readers and filesystem calls also require their operation to finish.
- HTTP includes use context-aware requests, cancellable concurrency waits and delays,
  close response bodies, and reject non-2xx responses. Existing request throttling
  and HTTP(S) inclusion remain available.
- Local includes require regular files; path and network access remain the embedding
  application's responsibility. No HTTP timeout or response size cap is imposed.
- CLI output files are opened after successful composition, preserving existing
  output on parse failure; output write errors produce a nonzero exit.

### Fixed

- Library-level fatal process termination on parser failures and cancellation.
- Panics from slash addresses ending in an escape and backward character offsets
  at EOF; backward offsets now respect UTF-8 characters.
- Unbounded stack growth from long comma-separated address expressions.
- Early completion that left parser workers running after an error or cancellation.
- Unsynchronized parser registry lookup during concurrent registration and parsing;
  each document now uses a synchronized registry snapshot.
- Silent truncation of long included lines, and numeric selection of a final line
  without a trailing newline.

[0.2.0]: https://github.com/tedla-brandsema/yoink/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/tedla-brandsema/yoink/releases/tag/v0.1.0
