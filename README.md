# Leo

<img src="assets/leo.jpg" alt="Leo the pug" width="160" align="right">

Leo the Pug (`leo-the-pug[bot]`) watches pug-sh's public repos. It keeps everything that needs a person on one board, [Inbox](https://github.com/orgs/pug-sh/projects): every open issue and pull request, security alerts, unreleased changes and proto syncs. It also does the chores that repeat across the SDKs, starting with the proto pin bump.

Leo never merges anything, and it only edits what it created (its issues, pull requests and comments) plus Inbox items. It covers the public repos in pug-sh that aren't archived or forks. Every run recomputes from GitHub, so a rerun or a missed run does no harm.

## Commands

| Command | Runs | Does |
|---|---|---|
| `leo inbox` | every 15 minutes | Adds every open issue and non-draft pull request to Inbox, fills empty fields, unarchives items reopened since they were archived, and keeps CI current on pull requests. |
| `leo security` | every 15 minutes | Opens one issue per repo and advisory whose Dependabot or OSV-Scanner alerts no pull request covers, an hour after the oldest alert. Closes it once pull requests cover the alerts or none is open. |
| `leo release` | every 15 minutes | Keeps one reminder per repo of the changes on main that ship but aren't released, once the oldest is a day old. Closes it when a release covers main. |
| `leo protos plan` | from pug's buf-ci, daily | Lists the SDKs whose `proto/` differs from `buf.build/pugsh/pug:main`. |
| `leo protos sync` | per drifted SDK | Pushes the SDK's codegen to `leo/sync-protos` and opens or updates a `Sync protos to <commit>` pull request. |
| `leo setup` | by hand | Creates Inbox if it's missing and adds any missing fields, options and views. |

Every command takes `--dry-run`, which prints each write instead of making it, and the text of every issue and pull request it would open. The token comes from `GH_TOKEN`.

If a person closes one of Leo's issues, Leo leaves it closed. It opens a new one only when something changes after the close: a shipped commit for a release reminder, a new alert for an advisory, or a new pug commit for a sync failure.

## Inbox

| Field | Values | Set by |
|---|---|---|
| Status | Todo, In Progress, Done | Leo sets Todo when it's empty, then the built-in workflows and you |
| Priority | High, Normal | Leo, when it's empty |
| Kind | Issue, PR, Dependabot, Security, Release, Protos | Leo, when it's empty |
| CI | Passing, Failing, Pending | Leo, on every run, for pull requests |

Leo only fills empty fields, so what you set by hand stays. Priority is High for security items, failed proto syncs, and anything opened by someone who isn't an org member, a collaborator or a bot. Kind takes the first rule that matches:

1. **Security**: Leo's security issues, and Dependabot pull requests that fix a Dependabot alert.
2. **Protos**: Leo's `leo/sync-protos` pull requests and its "Proto sync failed" issues.
3. **Release**: Leo's release reminders.
4. **Dependabot**: any other pull request by Dependabot.
5. **PR** or **Issue**: everything else.

Items stay in their repos, so closing or merging works the same from either place. To dismiss an item, archive it: Leo never adds an item back, unless its issue or pull request is reopened after the item was archived.

## Proto syncs

An SDK takes part when it has the `pug-sdk` topic and its Makefile has:

- a `PROTO_COMMIT := <commit>` line
- the `--path` arguments in `PROTO_PATHS` or in the `sync-protos` recipe
- vendored protos in `proto/`
- a `protos` or `codegen` target

Each SDK only drifts, and only gets a pull request, when a proto its `--path` list covers changes.

`sync.yml` runs in two jobs, because SDK installs and builds run third-party code that could read any secret in its job. The codegen job has no secrets and a read-only token: it sets `PROTO_COMMIT`, runs `make sync-protos` and the codegen target, and uploads the diff and the end of its log. The push job runs no SDK tooling. With an App token for that one SDK, it commits the diff through the API, so the commit is signed. If the branch has commits from someone else, it comments on the pull request instead. If main changed `.github/workflows/` since the branch's base, GitHub refuses the force-push, because the App has no Workflows permission. Leo then recreates the branch and links the new pull request from the old one. A failure in either job opens a `Proto sync failed` issue in the SDK, which Leo closes after the next successful sync.

The codegen job picks a toolchain from the files at the SDK's root, copied from the SDKs' codegen CI jobs. Each also gets buf 1.73.0, except gradle, whose Makefile builds its own.

| Root file | Toolchain | Setup | Codegen |
|---|---|---|---|
| `Package.swift` | swift | the SDK's `Tools/linux.Dockerfile` container, with `.build` cached | `make docker-protos` |
| `gradlew` | gradle | Gradle and the runner's JDK | `make protos` |
| `pubspec.yaml` | flutter | Flutter stable, `flutter pub get`, protoc, `protoc_plugin` 25.0.0 | `make protos` |
| `go.mod` | go | Go stable | `make protos` |
| `Gemfile` | ruby | Ruby 3.4 with bundler, protoc at the Makefile's `PROTOC_VERSION` | `make protos` |
| `bun.lock` | bun | bun, `bun install --frozen-lockfile` | `make protos` or `make codegen` |

A new SDK in one of these languages only needs the `pug-sdk` topic. A new language needs a setup block in `sync.yml` and a rule in `DetectToolchain`. When an SDK changes its codegen toolchain in CI, change `sync.yml` or its `codegen` entry in `leo.yaml` to match.

## leo.yaml

Per-repo overrides:

- `unshipped`: paths whose changes don't reach users, for `leo release`. `.github/` never ships, and neither do lockfiles in `pug-sdk` repos.
- `codegen`: the `bun`, `node` and `protoc` versions for an SDK's codegen job, where its CI differs from `sync.yml`'s defaults.
- `skip_protos`: leaves an SDK out of proto syncs, for while a branch of its own carries one.

## Workflows

| File | Runs on | Does |
|---|---|---|
| `ci.yml` | push and pull request | `go vet`, `go test ./...` and a gofmt check |
| `inbox.yml` | every 15 minutes, or by hand | `leo security`, `leo release` and `leo inbox`, never two runs at once. Run it by hand with "setup" checked to run `leo setup` first. |
| `protos.yml` | pug's buf-ci, daily, or by hand | `leo protos plan`, then `sync.yml` for each drifted SDK. A newer run cancels an older one. |
| `sync.yml` | calls from `protos.yml` | the codegen and push jobs for one SDK |

Each workflow gets an installation token from `actions/create-github-app-token` with the `LEO_CLIENT_ID` variable and the `LEO_PRIVATE_KEY` secret. The workflows that hold a token build Leo without the Actions cache, since the codegen job could write to it.

GitHub turns off a public repo's scheduled workflows after 60 days with no activity. pug's buf-ci turns both back on each time it starts `protos.yml`, with this job after its BSR push:

```yaml
  leo:
    needs: buf
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    permissions: {}
    steps:
      - uses: actions/create-github-app-token@v3
        id: token
        with:
          client-id: ${{ vars.LEO_CLIENT_ID }}
          private-key: ${{ secrets.LEO_PRIVATE_KEY }}
          owner: pug-sh
          repositories: leo
          permission-actions: write

      - env:
          GH_TOKEN: ${{ steps.token.outputs.token }}
        run: |
          gh api --method PUT repos/pug-sh/leo/actions/workflows/inbox.yml/enable
          gh api --method PUT repos/pug-sh/leo/actions/workflows/protos.yml/enable
          gh workflow run protos.yml --repo pug-sh/leo
```

The REST call succeeds when a workflow is already on, where `gh workflow enable` fails.

## One-time setup

1. **Register the App.** In the org's Settings, under Developer settings, then GitHub Apps, choose New.
   - Name it `Leo the Pug`, set the homepage to `https://github.com/pug-sh/leo`, turn the webhook off, and allow installing it only on this account.
   - Repository permissions, read and write: Contents, Pull requests, Issues and Actions.
   - Repository permissions, read only: Checks, Commit statuses, Code scanning alerts and Dependabot alerts.
   - Organization permissions: Projects read and write, Members read.
   - Leave Workflows unset, so the key can't edit CI in every repo. `leo protos sync` works without it.
   - Upload [pug.sh's logo](https://pug.sh/logo.svg) as the logo, as a PNG trimmed to the pug with a transparent background, and set the badge background color to white. The App page draws the logo at under half the badge's width, so a photo reads poorly there.
   - Generate a private key, and install the App on pug-sh with All repositories.
2. **Add credentials.** Create an org variable `LEO_CLIENT_ID` with the App's Client ID and an org secret `LEO_PRIVATE_KEY` with the `.pem` file, and share both with `leo` and `pug`.
3. **Create the board.** Run the Inbox workflow by hand with "setup" checked. Then, in Inbox, turn on the built-in workflows Item reopened and Auto-archive items, with the filter `is:closed updated:<@today-2w`. Set the Board view's columns to Status and its swimlanes to Priority, and group the By repo view by Repository. The API can't set either.

## Running locally

```sh
gh auth refresh -s project
go build ./cmd/leo
GH_TOKEN=$(gh auth token) ./leo inbox --dry-run
```

`leo protos plan` needs buf; pass `--buf` if it isn't on your `PATH`. Local runs act as you rather than as Leo, so use `--dry-run`: Leo only trusts issues that `leo-the-pug[bot]` opened.

## Development

`go test ./...` covers Kind and Priority, picking the release tag, unreleased commits on a fixture git repo, the Makefiles of the current SDKs, grouping alerts by advisory, and the commands themselves, run in dry-run mode against recorded API responses.

The code is in `cmd/leo` and `internal/`: `gh` for GitHub (REST through go-github, GraphQL through githubv4), and one package for each of `inbox`, `security`, `release` and `protos`.
