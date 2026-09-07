# e2e-net-sdk

[MIT licensed](./LICENSE). Unofficial and not affiliated with E2E Networks
Ltd. — their own [`e2e-cli`](https://pypi.org/project/e2e-cli/) ships no
license at all, so there's no compatibility constraint here, just a plain
permissive choice.

Unofficial Go SDK for E2E Networks, generated from their published OpenAPI specs
and split into one Go module per service, mirroring how `aws-sdk-go-v2` is laid out
(each service gets its own module: `service/ec2`, `service/s3`, etc.) rather than
one giant client package.

Two APIs are covered:

- **`myaccount/`** — the account/infra console: compute, storage, network, database,
  Kubernetes, billing. Source: `specs/e2e.openapi.json`.
- **`tir/`** — E2E's AI/GPU platform (TIR): notebooks, datasets, model serving,
  fine-tuning, RAG. Source: `specs/e2e-tir.openapi.json`.

Neither spec is authored by this repo. Both are E2E's own published documents;
this repo's job is turning them into buildable Go code and keeping that
reproducible as E2E updates them.

## Releasing

Go has no version field in `go.mod` -- module versions come from git tags,
and since this is a multi-module repo (no root module, 34 independent
`go.mod` files), each module needs its own `<module-path>/vX.Y.Z` tag for
`go get` to resolve it, alongside one bare `vX.Y.Z` tag for the GitHub
Release page itself.

Don't tag by hand. Run the **Release** workflow (Actions tab -> Release ->
Run workflow, or `gh workflow run release.yml -f version=0.2.0`) with a
MAJOR.MINOR.PATCH version. It builds and verifies every module
(`make all`), tags all 34 modules plus the bare tag, pushes them, and
creates the GitHub Release with notes generated from commits since the
last release.

## Layout

```
myaccount/
  ec2/              compute, AMIs, security groups, elastic IPs, key pairs, EBS, VPC
  s3/                object storage
  eks/               kubernetes
  rds/               DBaaS
  autoscaling/       auto scaling groups
  elasticloadbalancingv2/
  cloudwatch/        monitoring, alerts
  ecr/               container registry
  ... (26 modules total, see `make list-modules`)
tir/
  compute/           notebooks / instances
  storage/           datasets, SFS, PFS, vector DB
  models/            model repository, endpoints, playground, GenAI API, fine-tuning
  clusters/          training clusters, private clusters
  rag/               knowledge base, chat assistant
  pipelines/         pipelines, runs, schedules
  network/           reserve IPs, security groups, gateway
  other/             IAM, plans & pricing, SKU, data syncer, alerts
scripts/             the regeneration pipeline (see below)
specs/               raw + patched OpenAPI specs, checked in
```

Each module (`myaccount/ec2`, `tir/models`, etc.) is its own Go module with its
own `go.mod` — import only what you use:

```
go get github.com/go-batteries/e2e-net-sdk/myaccount/ec2
go get github.com/go-batteries/e2e-net-sdk/tir/models
```

## Regenerating

Everything under a module's `client.gen.go` is generated. Nothing in this repo
hand-edits generated code — when E2E's spec has a bug, the fix is a declarative
patch entry in `scripts/patches/`, not a hand edit to the output.

```
cp ~/Downloads/e2e.openapi.json specs/e2e.openapi.json     # cloud/MyAccount spec
cp ~/Downloads/openapi.json     specs/e2e-tir.openapi.json  # TIR/GPU spec
./scripts/generate.sh
```

Pipeline, per API:

1. **`scripts/fix_spec.py`** applies `scripts/patches/{myaccount,tir}.json` — every
   known bug in E2E's spec (undeclared path params, type/enum mismatches, a
   self-referential `oneOf` schema) with a description of what's wrong and why.
   These are why `oapi-codegen` can't just be pointed at the raw download.
2. **`scripts/split_spec.py`** splits the patched spec into one `openapi.json` per
   module, using the classifier in `scripts/classify_service.py`.
3. **`oapi-codegen`** generates `client.gen.go` for each module.
4. `gofmt`, `go build`, `go vet` run on each module; the script stops at the
   first failure.

**Finding new bugs without creating any resources:** `scripts/find_bugs.py`
calls every safe (no-path-param) GET list endpoint against your live
account, diffs the JSON against the fixed spec's declared types, and
auto-appends properly-formed entries to `scripts/patches/myaccount.json`
for anything that doesn't match -- the same process used by hand to find
the bugs already patched, now scripted:

```
E2E_API_KEY=... E2E_AUTH_TOKEN=... E2E_PROJECT_ID=58489 python3 scripts/find_bugs.py --dry-run
```

Drop `--dry-run` to actually write the new entries, then review the diff
in `scripts/patches/myaccount.json` before running `scripts/generate.sh`.
GET-only, creates nothing, costs nothing.

Re-running is safe: `go.mod` is only written the first time a module directory
appears, never touched again, and only `client.gen.go` / `openapi.json` are
regenerated.

## Local build

```
make fmt-check   # gofmt, no writes
make vet
make lint        # golangci-lint, installed automatically if missing
make build
make all         # all of the above
make list-modules
```

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>): <description>`,
type one of `feat fix docs style refactor perf test build ci chore revert`.

```
make hooks   # installs a local commit-msg hook that rejects non-conforming messages
```

Enforced in CI either way (`.github/workflows/commitlint.yml`), on every push and PR to `main`.

## Status

CI (`.github/workflows/ci.yml`) runs `fmt-check`, `vet`, `lint`, `build` on every
push and PR to `main`.

Not yet wired up: automatic spec refresh / versioning against E2E's published
spec (tracked separately), and usage examples per module (`example/` is a
placeholder).
