# upstream-tracker

Tooling for tracking commits on the [Dire Wolf](https://github.com/wb2osz/direwolf) upstream and deciding whether each one needs to be ported to Samoyed.

## Files

**`config`** — defines three variables: the upstream repo URL, the git remote name (`upstream`), and the branch to track (`dev`).

**`last-seen`** — stores a single commit SHA: the most recent upstream commit that has been processed. This is the high-water mark used by `sync`.

**`queue`** — the ledger of upstream commits. Each line is `<sha> <status> <summary>`, where status is `pending`, `applied`, or `skipped`.

**`sync`** (script) — fetches the upstream remote, finds all commits newer than `last-seen`, appends any not already in `queue` as `pending`, then advances `last-seen` to the latest upstream HEAD.

**`next`** (script) — shows the first `pending` commit in the queue (`git show`), then prompts you to mark it `[a]pplied`, `[s]kipped`, or quit.

## Workflow

1. Run `./upstream-tracker/sync` periodically to pull in new Dire Wolf commits.
2. Work through them with `./upstream-tracker/next`, marking each as:
   - `applied` — the change has already been incorporated into Samoyed
   - `skipped` — the change is not relevant or not wanted
