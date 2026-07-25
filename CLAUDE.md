## Git

Never run `git commit` (or anything that creates a commit, e.g. `git rebase --continue`, `git cherry-pick` without `-n`). Stage or leave changes ready, but the operator always makes the commit themselves.

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) (`feat:`, `fix:`, `refactor:`, etc.). When drafting a suggested commit message for the operator, use this format.

## Agent skills

### Coding standards

Google Go Style Guide by default, with repo-specific overrides. See `CODING_STANDARDS.md`.

### Issue tracker

Issues live as markdown files under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical labels (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
