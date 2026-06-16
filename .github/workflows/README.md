# GitHub Actions

Updated 17 Jun 2026.

# Required secrets

`LINEA_BOT_CLIENT_ID`

`LINEA_BOT_PRIVATE_KEY`

Used by the Linea GitHub App.

# App permissions

Checks: read and write

Contents: read and write

Issues: read and write

Pull requests: read and write

Dependabot alerts: read

# Workflows

`android.yml`

Builds the Android debug APK.
Runs on pull requests touching `android/` or `backend/`.

`app.yml`

Publishes the app check.

`issue-response.yml`

Comments on newly opened issues from the Linea bot.

`pr-check.yml`

Builds, packages, and verifies the DMG on pull requests.

`pr-ready.yml`

Updates one pull request comment with the check result.

`pr-body.yml`

Replaces literal `\n` in pull request bodies with real line breaks.

`ci.yml`

Runs the app checks on pull requests and pushes.

`release.yml`

Runs when a `v*` tag is pushed.

Builds the Apple Silicon binary, packages the macOS DMG, builds iOS `.app` (skips without blocking the release on failure), builds Android debug APK, writes checksums, updates `Formula/linea.rb` and `package.json`, publishes the GitHub release, and publishes to npm.

`nightly.yml`

Builds a dated prerelease.

Does not update the Homebrew formula.

`formula-sha.yml`

Runs after a release is published.

Updates `Formula/linea.rb` with the release source SHA and opens a pull request.

Skips prereleases.

Can also be run by hand with a tag.

`release-install-check.yml`

Runs when the Homebrew formula changes on `main`.

Checks the published formula install path with `make install-check`.

Can also be run by hand.

`pages.yml`

Publishes the public site.

`release-drafter.yml`

Keeps a draft release ready from merged pull requests.

`renovate.yml`

Runs dependency update checks.

`labeler.yml`

Adds labels to pull requests.

`auto-merge.yml`

Enables auto-merge for Dependabot and Renovate pull requests.

Branch protection still decides when a pull request can merge.

`stale.yml`

Marks inactive issues and pull requests.
Uses Linea bot token for comments.

# Release path

1. Push a version tag, for example `v0.1.2`.
2. `release.yml` builds, updates `Formula/linea.rb` and `package.json` on a branch, opens a version update PR, creates the release, and publishes to npm.
3. Merge the version update PR. `release-install-check.yml` runs when the formula changes on `main`.
4. Run `make release-check` for a local end-to-end release check.
