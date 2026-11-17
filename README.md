# Romanian datasets

Reference datasets about Romania, built and kept current by PidginHost. Free for anyone to download and use.

The public copy is https://github.com/pidginhost/romanian-datasets.

## What is here

| Dataset | Content | Source | Kept current |
| --- | --- | --- | --- |
| `bnr-currencies` | The currencies with a reference rate of the National Bank of Romania, the units each rate is given for, and since when | BNR yearly rate files | By hand, when BNR changes its list |
| `caen` | The classification of economic activities CAEN, Rev.2 and Rev.3, and the links between the classes of the two | ONRC nomenclatures on data.gov.ro; Eurostat correspondence table NACE Rev.2 to Rev.2.1 | By hand, when INS changes CAEN |
| `countries` | Countries and territories with their ISO 3166-1 codes, names in Romanian and English, whether they are in the European Union, and the code an electronic invoice uses for them | Publications Office of the European Union | By hand, when the table changes |
| `efactura` | The codes the e-Factura system of ANAF accepts for each check of a code list, the CIUS-RO limits on invoice types and VAT point date codes, and the county and sector codes of a Romanian address | Ministry of Finance, e-Factura validation package | Monthly |
| `en16931-codes` | The code lists of the European standard on e-invoicing: units, currencies, VAT categories and exemption reasons, payment means, document types, address schemes and the others, with the period of each code; the countries are in `countries` | European Commission, EN 16931 genericode archives | Monthly |
| `public-holidays` | The days on which no work is done by law, as rules that give them for any year | Labour Code, article 139 (1) | By hand, when the law changes |
| `siruta` | The administrative-territorial units of Romania: counties, municipalities, towns, communes, localities and Bucharest sectors, with their SIRUTA codes, county letters and development regions | National Institute of Statistics (INS); names with Romanian letters from the INS copy on data.gov.ro | Monthly |
| `vat-numbers` | How a VAT number looks in each European jurisdiction, and which are in the European Union for VAT | vatnode/eu-vat-rates-data, VIES | By hand, when a format changes |
| `vat-rates` | The VAT rates in force in each European jurisdiction | vatnode/eu-vat-rates-data (European Commission TEDB), VIES | Weekly |

More datasets follow, each after its source and licence are confirmed.

## How to read the data

1. Download `index.json`. It lists every dataset with its revision, the day it last changed, how often it is rebuilt, and its files.
2. Download the files it names. Each path is relative to `index.json`.
3. Check each file against the `sha256` in the index. Do not use a file that does not match.

A file is JSON Lines: UTF-8, one JSON object on each line, each line ended by a line feed. The `fields` of a file in the index say what each field holds.

A dataset's `revision` starts at 1 and grows by one each time its content changes. To stay current, read `index.json` again and download the datasets whose revision grew.

`"format": 1` is the layout described here. It changes only when a reader has to change.

```
index.json
datasets/
  bnr-currencies/
    currencies.jsonl
  caen/
    activities.jsonl
    correspondence.jsonl
    versions.jsonl
  countries/
    countries.jsonl
  efactura/
    accepted-codes.jsonl
    subdivisions.jsonl
  en16931-codes/
    allowance-reasons.jsonl
    charge-reasons.jsonl
    currencies.jsonl
    document-types.jsonl
    electronic-address-schemes.jsonl
    identifier-schemes.jsonl
    item-schemes.jsonl
    mime-codes.jsonl
    payment-means.jsonl
    reference-qualifiers.jsonl
    text-subjects.jsonl
    units.jsonl
    vat-categories.jsonl
    vat-exemption-reasons.jsonl
  public-holidays/
    rules.jsonl
  siruta/
    counties.jsonl
    regions.jsonl
    types.jsonl
    units.jsonl
  vat-numbers/
    numbers.jsonl
  vat-rates/
    rates.jsonl
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

`build` writes every dataset and `index.json`. Run twice, it changes nothing the second time. `verify` checks the files against the index. `list` names every dataset with its frequency.

## How the datasets stay current

Each dataset has a frequency, set by how often its source changes:

| Frequency | Who rebuilds it |
| --- | --- |
| `daily`, `weekly`, `monthly`, `yearly` | The scheduled CI job of that frequency: `datasets build -frequency <f>`. It commits to `main` only when something changed. |
| `manual` | A person, when the source changes: `datasets build -only <name>`, then a merge request with the result. For data that changes rarely. |

A build of one frequency leaves every other dataset as it is in the index.

The scheduled job needs:

- one pipeline schedule on `main` for each frequency in use, with the variable `DATASETS_FREQUENCY` set to that frequency;
- the CI variable `DATASETS_PUSH_TOKEN`, a project access token with `write_repository`, masked and protected.

## Public copy

Every pipeline on `main` ends with `mirror:github`, which publishes `main` to GitHub the way `pidginhost/csm` does: with force, as the author PidginHost, without the CI files. It uses the group variable `GITHUB_TOKEN`. Changes are made here, never on GitHub.

## Add a dataset

1. Open an issue with the source, its address and its licence. The licence must allow republishing.
2. Choose its frequency from how often the source changes. Data that changes rarely is `manual`.
3. Add a package under `internal/` that returns a `dataset.Dataset`, with tests on recorded input.
4. Add it to `builders` in `cmd/datasets/main.go`.
5. Run `build -only <name>` and commit the files it writes together with the code. `verify` fails while a listed dataset is not in the index.
6. If no dataset had that frequency before, add its pipeline schedule.

Rules for contributors are in `AGENTS.md`.
