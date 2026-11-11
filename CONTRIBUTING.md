# Contributing to go-imagefy

Changes are welcome when they keep the exported API stable and are covered by tests.

## Development

Requires Go 1.26 and golangci-lint v2.10.1.

```sh
make test     # GOWORK=off go test -race -count=1 ./...
make lint     # GOWORK=off golangci-lint run ./...
make build    # GOWORK=off go build ./...
```

CI (`.github/workflows/ci.yml`) runs the tests with the race detector on Go 1.24 and 1.25, and
golangci-lint v2.10.1. If those pass locally they pass there. A pull request that changes only
documents skips them. Dependencies are vendored: after changing `go.mod` run `go mod vendor` and
commit `vendor/`.

## Commits

Conventional Commits (`feat:`, `fix:`, `refactor:`, `perf:`, `test:`, `chore:`, `ci:`, `docs:`) with
the PR number appended by the merge. There is no release-please: a maintainer tags `v0.x.y` by hand
and updates [CHANGELOG.md](CHANGELOG.md), so leave the changelog and tags alone.

## Pull requests

- Branch from `master`, keep the change focused, and fill in the pull-request template.
- Add tests for behaviour changes; a new reject gate needs a positive and a negative fixture.
- Update [docs/INDEX.md](docs/INDEX.md) when you add or rename a document.
- Do not add secrets, hostnames or personal data to any file; the repository is public.

## Reporting problems

Bugs and feature requests go through the issue templates. Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md).
