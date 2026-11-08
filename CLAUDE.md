# CLAUDE.md

Notes for coding agents working in go-imagefy: a Go library (module
`github.com/anatolykoptev/go-imagefy`, Go 1.26) for editorial image search, license assessment,
validation, de-duplication and LLM classification. Other services import it, so the exported API
is a contract. Every document is indexed in [docs/INDEX.md](docs/INDEX.md).

## Layout

One flat root package `imagefy`, no `cmd/`. `imagefy.go` holds `Config` and the interfaces
(`Cache`, `Classifier`, `SearchProvider`). Search providers are `provider*.go`, `openverse.go`,
`pexels.go`, `orchestrator.go`; the pipeline is `find.go`, `search.go`, `validate*.go`, `dedup.go`;
license logic is `license.go`, `metadata*.go`, `cclicense.go`, `assessment.go`; the deterministic
reject gates are `placeholder.go`, `flatimage.go`, `screen.go`; LLM classification is `classify*.go`
and `prefilter.go`. `docs/ARCHITECTURE.md` explains the layers. `vendor/` is tracked.

## Gate

CI (`.github/workflows/ci.yml`) runs:

```sh
GOWORK=off go test -race -count=1 -coverprofile=coverage.out ./...   # matrix: Go 1.24 and 1.25
golangci-lint run                                                    # golangci-lint v2.10.1, Go 1.25
```

`make test`, `make lint` and `make build` run the same with `GOWORK=off`. A docs-only PR (Markdown,
`docs/`, licence, templates) skips both jobs through the `docs-scope` job in the same workflow;
that block is canonical across the fleet and compared byte for byte, so do not edit it here.
After any `go.mod` change run `go mod vendor` and commit `vendor/`. There is no release
automation: a release is a hand-pushed `v0.x.y` tag and [CHANGELOG.md](CHANGELOG.md) is edited by hand.

## Invariants that are easy to break

- **Classification degrades open.** A nil classifier, an unavailable model or an error accepts the
  image (`IsRealPhoto` is true for class `""`). Do not turn an error into a rejection.
- **Cost tiers order the pipeline**: pure logic first (license, URL patterns, placeholder and flat
  gates, dedup), the LLM last. `PreClassify` skips the LLM for safe sources; keep new cheap
  checks ahead of the model call.
- **License verdicts are conservative**: a blocked stock domain or stock metadata outranks a safe
  signal only where `AssessLicense` says so, and every verdict carries its signal list.
- **`ReverseCheck` is optional** and returns a zero result when `OxBrowserURL` is empty or on any
  error.
- **Log values from the network are sanitised** (`sanitizeLogValue` strips CR and LF); keep that for
  any new log line that includes a URL or provider string.
- **Use `GOWORK=off`** for every Go command.
- **This repository is public.** No hostnames, operator paths, account names, tokens or personal
  data in any tracked file, tests and fixtures included.
