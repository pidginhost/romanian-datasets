# AGENTS.md

Rules for every coding agent and contributor in this repository.
Keep this file short. Change it only by merge request.

## Project
- Public reference datasets about Romania, built by PidginHost and free for anyone (CC BY 4.0).
- This repository on the internal GitLab is the source. It is mirrored to https://github.com/pidginhost/romanian-datasets, which is what readers download from.
- A Go tool builds every dataset and the index. A scheduled CI job runs it and commits only when something changed.
- The first reader is the Conta accounting application (conta/conta, ADR-027). It reads `index.json`, then the files it names, and checks each by SHA-256.
- Everything here is public. Never commit personal data, company data, credentials or anything not meant for the whole internet.

## Datasets
- A dataset is a folder `datasets/<name>/` of JSON Lines files, listed in `index.json`. Nothing else describes it.
- Before a dataset is added, confirm its source and that the source's licence allows republishing. Record both in the dataset. No confirmed licence, no dataset.
- The same input gives the same bytes. No timestamps, random order or machine-dependent values in a file.
- A code is never removed from a dataset. It gets an end date, so old documents stay valid.
- `index.json` format 1 is a contract with readers. A change that a reader must follow needs a new format number and a decision issue.
- Files in `datasets/` and `index.json` are written by the tool only. Never edit them by hand.

## Build and test
- Needs the Go version in `go.mod`. No dependencies outside the standard library without a decision issue.
- Test: `go test ./...`. Format: `gofmt -l .` prints nothing. Vet: `go vet ./...`.
- Build the datasets: `go run ./cmd/datasets build`. Check them: `go run ./cmd/datasets verify`.
- Tests never call a real source. Use recorded responses.

## Work style
- No corner cutting, hacks, or workarounds that paper over a root cause.
- Do not widen scope past the ask. Flag adjacent breakage; do not fix it uninvited.
- Too large or risky to do properly now: stop, say so, say what it needs.

## Issues and git
- Work starts from an issue. One branch per issue, named `<issue number>-<short-name>`. Name the issue in the merge request.
- A merge needs approval from one human. An agent never approves a merge request.
- Never work on main. The scheduled job is the only thing that commits to main directly, and only data.
- Ask before: force push, deleting a remote branch or tag, `reset --hard`, discarding uncommitted work.
- Conventional Commits. Subject 50 chars or fewer. No Co-Authored-By or any other attribution trailer.

## Prose: commits, docs, issues, merge request text
- English. Plain ASCII. Short direct sentences. No em-dashes, arrows, Unicode bullets, emojis.
- Banned words: comprehensive, seamlessly, leverages, ensures, robust.
- Dataset content is not prose: names keep their Romanian letters.

## Secrets
- Never print, read, cat, or echo raw secrets: keys, passwords, tokens, certificates.
- No secrets in code, fixtures, or commit messages. The push token of the scheduled job lives only in a masked, protected CI variable.
- Exposure: stop, flag it, recommend rotation.
