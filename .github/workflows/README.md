# GitHub Actions

These workflows keep checks, releases, and small maintenance tasks in one place.

# Required secrets

`LINEA_BOT_CLIENT_ID`

`LINEA_BOT_PRIVATE_KEY`

These let the Linea GitHub App open pull requests and publish releases that can trigger follow-up workflows.

# Workflows

`ci.yml`

Runs the app checks on pull requests and pushes.

`release.yml`

Runs when a `v*` tag is pushed.

Builds the Apple Silicon binary, packages the macOS DMG, writes checksums, and publishes the GitHub release.

`formula-sha.yml`

Runs after a release is published.

Updates `Formula/linea.rb` with the release source SHA and opens a pull request.

Can also be run by hand with a tag.

`pages.yml`

Publishes the public site.

`release-drafter.yml`

Keeps a draft release ready from merged pull requests.

`renovate.yml`

Runs dependency update checks.

`labeler.yml`

Adds labels to pull requests.

`auto-merge.yml`

Merges approved dependency pull requests when checks pass.

`stale.yml`

Marks inactive issues and pull requests.

# Release path

1. Push a version tag, for example `v0.1.2`.
2. `release.yml` publishes the release assets.
3. `formula-sha.yml` opens the formula SHA pull request.
4. Merge the formula pull request.
5. Run `make release-check`.
