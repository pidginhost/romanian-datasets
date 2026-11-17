# Romanian datasets

Reference datasets about Romania, built and kept current by PidginHost. Free for anyone to download and use.

The public copy is https://github.com/pidginhost/romanian-datasets.

## What is here

| Dataset | Content | Source |
| --- | --- | --- |
| `public-holidays` | The days on which no work is done by law, from 2024 | Labour Code, article 139 (1) |

More datasets follow, each after its source and licence are confirmed.

## How to read the data

1. Download `index.json`. It lists every dataset with its revision, the day it last changed, and its files.
2. Download the files it names. Each path is relative to `index.json`.
3. Check each file against the `sha256` in the index. Do not use a file that does not match.

A file is JSON Lines: UTF-8, one JSON object on each line, each line ended by a line feed. The `fields` of a file in the index say what each field holds.

A dataset's `revision` starts at 1 and grows by one each time its content changes. To stay current, read `index.json` again and download the datasets whose revision grew.

`"format": 1` is the layout described here. It changes only when a reader has to change.

```
index.json
datasets/
  public-holidays/
    holidays.jsonl
```

## Licence

- The data is under Creative Commons Attribution 4.0 International: see `LICENSE-DATA`. Credit "PidginHost, romanian-datasets". Each dataset in the index names its own source and that source's licence.
- The code is under the MIT licence: see `LICENSE`.

## Build

Needs the Go version in `go.mod`.

```
go test ./...
go run ./cmd/datasets build
go run ./cmd/datasets verify
```

`build` writes every dataset and `index.json`. Run twice, it changes nothing the second time. `verify` checks the files against the index.

A scheduled CI job runs `build` and commits to `main` only when something changed. It needs the CI variable `DATASETS_PUSH_TOKEN`, a project access token with `write_repository`, masked and protected.

## Add a dataset

1. Open an issue with the source, its address and its licence. The licence must allow republishing.
2. Add a package under `internal/` that returns a `dataset.Dataset`, with tests on recorded input.
3. Add it to `build` in `cmd/datasets/main.go`.
4. Run `build` and commit the files it writes together with the code.

Rules for contributors are in `AGENTS.md`.
