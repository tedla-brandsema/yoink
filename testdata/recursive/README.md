# Recursive composition fixture

These invented documents model a reusable Stage 1 body placed between two selected
strategy excerpts. They contain no source text from any external business repository.

`grand-vision.md` selects the Stage 1 body from `stage-1.md`, which includes
`sections/overview.md`. That file resolves `../knowledge/economy.md` relative to its
own directory and selects a supporting excerpt. Its range markers end in `OMIT`
and disappear before composition.

Missing-file directives outside selected regions must never execute. The selected
Stage 1 body appears exactly once, in the intended order. Depth 1 leaves the
`sections/overview.md` directive literal; depth 2 leaves the supporting-excerpt
directive literal. Depth 3 and unlimited depth both produce `expected.md`.

The expected file includes the CLI's final newline. The library result is the same
text without that final newline. `recursive_integration_test.go` checks the public
API against this deterministic fixture and tests host survival after failures.
