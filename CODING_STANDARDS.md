# Coding Standards

Follow the [Google Go Style Guide](https://google.github.io/styleguide/go/) by default. Where a rule below conflicts with it, the rule below wins.

Anything `golangci-lint` already enforces (see `.golangci.yaml`) is not repeated here — don't flag it in review.

## Comments

- Comments explain *why*, not *what* — the code already says what it does.
- Keep them short: one line where possible.
- Trivial declarations (e.g. a bare constructor) don't need a rationale if there isn't a real one — a short label is fine.

## Testing

- Write tests as table-driven tests (`tests := []struct{...}{...}` + `t.Run` per case), even for a single behavior with few cases.
- Use [testify](https://github.com/stretchr/testify)'s `require` for assertions that should stop the test immediately (setup, preconditions, anything that would make later assertions meaningless if false) and `assert` for assertions that should keep collecting failures.
