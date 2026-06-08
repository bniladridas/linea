# GitHub Actions

Updated 3 Jun 2026.

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

`app.yml`

Publishes the app check.

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

Builds the Apple Silicon binary, packages the macOS DMG, writes checksums, and publishes the GitHub release.

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

# Release path

1. Push a version tag, for example `v0.1.2`.
2. `release.yml` publishes the release assets.
3. `formula-sha.yml` opens the formula SHA pull request.
4. Merge the formula pull request.
5. `release-install-check.yml` checks the published formula install path.
6. Run `make release-check` for a local end-to-end release check.
