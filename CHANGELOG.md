# Changelog

All notable changes to Yoink will be documented here. Releases use semantic versioning.

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

[0.1.0]: https://github.com/tedla-brandsema/yoink/releases/tag/v0.1.0
