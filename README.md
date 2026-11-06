# tf-module-api-diff

[![ci](https://github.com/moveeeax/tf-module-api-diff/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/tf-module-api-diff/actions/workflows/ci.yml)
[![go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

Diffs the public API of a Terraform module between two git refs and tells you
the real semver severity of the change.

A module's public API is its `variable` and `output` blocks: the inputs callers
must set, their types and defaults, and the values other modules wire onward.
Nothing in the Terraform toolchain checks that contract across versions, so a
dropped default or a `type` narrowed from `any` to `string` ships as a patch
release and breaks whoever runs `terraform init` next.

`tf-module-api-diff` reads the contract at both refs, classifies every
difference, and fails CI when the tag you are about to push undersells what you
changed.

```console
$ tf-module-api-diff --from v1.1.0 --to HEAD
v1.1.0 -> HEAD
severity: major

SEVERITY  CHANGE                    TARGET                    DETAIL
MAJOR     OUTPUT_SENSITIVE_ADDED    output.flow_log_role_arn  consumers passing it to a non-sensitive context now error
MAJOR     VARIABLE_BECAME_REQUIRED  var.cidr_block            default "10.0.0.0/16" was dropped, so the caller must now set it
MAJOR     VARIABLE_REMOVED          var.enable_nat            callers that set it will fail with an unknown variable error
MAJOR     VARIABLE_TYPE_NARROWED    var.flow_log_destination  any -> object({ bucket_arn = string, kms_key_id = optional(string) })

4 major, 0 minor, 0 patch
tf-module-api-diff: major change exceeds --fail-on major: OUTPUT_SENSITIVE_ADDED on output.flow_log_role_arn
```

## Install

```console
$ go install github.com/moveeeax/tf-module-api-diff/cmd/tf-module-api-diff@latest
```

Or build from a clone: `go build -o tf-module-api-diff ./cmd/tf-module-api-diff`.
Stdlib only, no Terraform binary, no provider download, no network.

## Usage

```console
$ tf-module-api-diff --from v1.3.0 --to HEAD
$ tf-module-api-diff --dir modules/vpc --from v2.1.0 --to HEAD --expect minor
$ tf-module-api-diff --from-dir ./old --to-dir ./new --format markdown
$ tf-module-api-diff --dir modules/vpc --from latest-tag --tag-prefix vpc/ --to HEAD --suggest-version
```

| flag | meaning |
| --- | --- |
| `--dir` | module path inside the repository, default `.` |
| `--from`, `--to` | git refs: tags, branches or commits; `--from latest-tag` picks the previous release tag |
| `--tag-prefix` | release tag prefix such as `vpc/`, for `latest-tag` and `--suggest-version`; default empty |
| `--suggest-version` | print the next release tag after applying the computed severity |
| `--from-dir`, `--to-dir` | two plain directories instead of refs |
| `--format` | `table`, `json` or `markdown` |
| `--expect` | the bump you intend to publish; replaces `--fail-on` when set |
| `--fail-on` | `major`, `minor`, `patch` or `never`, default `major` |

Exit codes: `0` the change is within what the gate allows, `1` the gate
rejected it, `2` a usage error or unreadable HCL.

## How it works

Both sides are read as text and compared structurally.

1. **Read the API.** A small HCL scanner walks the `.tf` files tracking brace
   depth, quoted strings with `${...}` interpolation, comments and heredocs. It
   does not evaluate anything, so a type like
   `map(object({ cidr = string, public = optional(bool, false) }))` survives
   intact and no provider needs installing. Out come every `variable` (type,
   default present or absent, `nullable`, `sensitive`, validation count,
   description) and every `output` (`sensitive`, description).
2. **Compare the type constraints.** Type expressions are parsed into a tiny
   AST and compared for caller compatibility rather than equality. The question
   is whether every value a caller could legally pass before is still accepted:
   `string -> any` widens, `any -> string` narrows, an object attribute added
   as `optional()` widens, added as required narrows, removed narrows.
3. **Classify.** Each difference gets a severity from the caller's point of
   view. Major means a consumer who was happy before can now fail without
   changing their own code.

| severity | changes |
| --- | --- |
| major | variable removed; variable became required; type narrowed; `nullable` turned off; output removed; output turned sensitive; new variable with no default |
| minor | new optional variable; new output; type widened; variable became optional; validation added; sensitivity flipped on a variable |
| patch | default value changed; description changed; validation removed |

A changed default value is deliberately patch and deliberately always listed:
it alters behaviour for callers who never set the variable, but it does not
break a single existing call.

Comparing refs needs no checkout. Blobs are read through `git ls-tree` and
`git show`, so the working tree is untouched and a dirty repo is fine.

## Reading the module without a checkout

`--dir` is a path inside the repository, not a path in your working tree, so
the module only has to exist at the two refs:

```console
$ tf-module-api-diff --dir modules/vpc --from v2.1.0 --to v2.2.0
```

Only `.tf` files directly in that directory count. A nested directory is a
separate module with its own API.

## Previous tag and next version

Release workflows usually spend a step finding the last tag. `--from
latest-tag` does it: it takes the highest release tag reachable from `--to`,
leaving out tags on the `--to` commit itself, so the answer is the same before
and after you tag the release. A release tag is `--tag-prefix`, an optional
`v` and `MAJOR.MINOR.PATCH`. Pre-release tags such as `v1.3.0-rc.1` and tags
under other prefixes are skipped. Versions compare as numbers, so `v1.10.0`
beats `v1.9.0`.

Monorepos that tag each module separately pass the prefix:

```console
$ tf-module-api-diff --dir modules/vpc --from latest-tag --tag-prefix vpc/ --to HEAD --suggest-version
modules/vpc: vpc/v1.2.3 -> HEAD
severity: minor
next version: vpc/v1.3.0
...
```

`--suggest-version` bumps the `--from` tag by the computed severity: major
resets to `X+1.0.0` (also on `0.x`, since a break is a break for callers),
minor to `X.Y+1.0`, patch, including "nothing changed", to `X.Y.Z+1`. The prefix
and the `v` are kept. It needs `--from` to be a release tag under the prefix,
either given directly or found by `latest-tag`; a branch name or `--from-dir`
is a usage error. The JSON report carries it as `next_version`, and `from`
holds the tag `latest-tag` resolved to.

## GitHub Action

```yaml
name: module-api
on: pull_request

jobs:
  api-diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0  # the action needs both refs present

      - uses: moveeeax/tf-module-api-diff@v0
        id: api
        with:
          dir: modules/vpc
          from: ${{ github.event.pull_request.base.ref }}
          to: HEAD
          fail-on: major

      - run: echo "this PR is a ${{ steps.api.outputs.severity }} change"
```

The action appends the markdown report to the job summary and exposes
`severity`, `previous-tag`, `next-version` and `exit-code` as outputs. `from`
defaults to `latest-tag`. To gate a release instead, declare the bump you are
publishing and let the tool disagree:

```yaml
      - uses: moveeeax/tf-module-api-diff@v0
        with:
          to: HEAD
          expect: minor
```

Or let it pick the tag. With `suggest-version: true` the action is the whole
release step for a module in a monorepo:

```yaml
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0  # tags and history for latest-tag

      - uses: moveeeax/tf-module-api-diff@v0
        id: api
        with:
          dir: modules/vpc
          tag-prefix: vpc/
          suggest-version: true
          fail-on: never

      - run: |
          git tag "${{ steps.api.outputs.next-version }}"
          git push origin "${{ steps.api.outputs.next-version }}"
```

## Examples

[`examples/`](examples/) holds four revisions of the same fictional VPC module,
one per severity, used both as documentation and as the test fixtures:

```console
$ tf-module-api-diff --from-dir examples/vpc-v1.0.0 --to-dir examples/vpc-v1.1.0
examples/vpc-v1.0.0 -> examples/vpc-v1.1.0
severity: minor

SEVERITY  CHANGE                   TARGET                  DETAIL
MINOR     OUTPUT_ADDED             output.nat_gateway_ids  new output, nothing to break
MINOR     VARIABLE_ADDED_OPTIONAL  var.single_nat_gateway  new input with default false
MINOR     VARIABLE_TYPE_WIDENED    var.subnets             map(object({ cidr = string, public = optional(bool) })) -> map(object({ cidr = string, public = optional(bool), az = optional(string) }))

0 major, 3 minor, 0 patch
```

## Limits

Only literal `true` counts as `true` for `sensitive` and `nullable`; a computed
expression there is not something this tool evaluates. A primitive swap such as
`number -> string` is reported as narrowing even though Terraform would convert
some of those values, because the safe reading of a changed primitive is that
callers should look. Provider version constraints in `required_providers` are
not part of the diff yet.

## Development

```console
$ go test ./...
$ go vet ./... && gofmt -l .
```

## License

MIT
