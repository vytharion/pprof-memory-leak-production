# Codebase rules — pprof-memory-leak-production

This is the **code** half of the article workspace. Everything in this
directory is git-tracked + pushed to `github.com/vytharion/pprof-memory-leak-production`. Stay
inside this directory when writing code; the article MDX lives next
door in `docs/` and is owned by a different phase.

## Identity

```bash
git config user.name "vytharion"
git config user.email "vytharion@users.noreply.github.com"
gh auth switch --user vytharion  # never use the operator's personal account
```

## Commit cadence (tutorial / series articles)

For each step N of the tutorial:

1. **commit-start** (BEFORE writing code) — empty commit recording
   intent:
   ```bash
   git commit --allow-empty -m "step N: <short title>" -m "<plain-language plan>"
   ```
2. **write-code** — implement the step. Create files. Run tests
   (`pytest`, `cargo test`, whatever fits the stack). Tests must PASS
   before you move on.
3. **commit-end** — capture final state of the step:
   ```bash
   git add -A
   git commit -m "step N complete: <one-line summary>"
   ```

For wiki / intro articles, the codebase/ folder is OPTIONAL. Skip it
entirely if `article_kind == "intro"`.

## Test discipline

- Every step that introduces new behavior MUST add a test for it.
- Tests run via the project's standard runner (`pytest`, `cargo test`,
  `bun test`, etc).
- The article in `docs/0N-step.mdx` must show the test command + its
  passing output (or the failing output before the fix, then passing
  after).
- Don't fake test output. Run the command. Paste the real result.

## Nested control flow — NON-NEGOTIABLE

- Maximum 2 levels of `if/elif/else` nesting inside a function.
- ZERO nested `try/except` blocks.
- Functions exceeding these thresholds → break them into smaller helpers
  BEFORE committing.

## Comments

- Default: write no comments. Names should explain WHAT.
- Comment only the WHY for non-obvious invariants — a hidden constraint,
  a subtle ordering requirement, a workaround for a specific bug.
- Inline comments max 3 lines. No multi-paragraph context-history.
- Don't reference the current article, fix, or callers — those belong
  in the commit message / docs, not the code.

## Privacy

- The git author + remote is `vytharion`. NEVER push under the
  operator's personal account.
- Do NOT hardcode operator-private literals (real email, real domain,
  internal hostnames). Use placeholders + `os.environ.get(...)` defaults.
- Do NOT reference the source monorepo's structure in code comments.

## Tech stack hints (per niche)

The `article_kind` + `niche` already gives you most of the context.
Quick reference:
- `python` niches → uv + pytest + ruff + mypy strict.
- `rust` niches → cargo workspace + clippy -D warnings.
- `claudeplugins` → markdown skill + Python hooks; pytest for hook tests.
- `aiagent` → claude-agent-sdk Python; pytest with stubbed query.
- `typescript` → bun (not npm); tsconfig strict.

Pick the smallest set of dependencies needed. Do NOT pull in a framework
when stdlib + 1 package suffices.
