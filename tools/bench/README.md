# Benchmark harness for T038

The harness prepares experiments and imports evidence offline using local
files and Git. The bundled corpus consists of
30 independently authored tasks on three synthetic Go modules. It includes
interface, field and type changes, behavior fixes, contract preservation,
local renames, a tracked edit and an untracked caller, an unavailable module
behind a build tag, and renames with 1,025 callers. Only the missing-dependency
case adds that module requirement and the tagged source to its checkout.

## Plan and materialize

Use Python 3 and an approved local Git binary. Choose the corpus, model and
permissions before observing engine output. Supply a JSON settings document
with these keys.

~~~json
{
  "host": "local planned host",
  "model": "approved model name",
  "model_revision": "recorded version",
  "reasoning": "recorded setting",
  "permission_profile": "same local fixture rights for every arm",
  "goos": "linux",
  "goarch": "amd64",
  "toolchain": "go1.27.4",
  "machine": "recorded CPU and memory",
  "filesystem": "recorded filesystem",
  "completion_budget_ms": 900000,
  "common_tools": ["read", "search", "git", "gopls"]
}
~~~

List the tools available to the baseline. Arm A uses those tools with MCE4D
disabled. B adds map and locate queries. C also uses the context pack.
Every arm can use the original semantic tools. Keep model/version,
reasoning, prompt, dependencies, permissions and completion budget fixed.
The plan shuffles 270 run IDs by seed by default and records the exact
prompt, expected references, source fingerprint, profile, dependency state,
file count and byte count. Preserve the plan before any measurement.

~~~sh
python3 -B tools/bench/main.py plan --seed 53038 --settings settings.json > plan.json
python3 -B tools/bench/main.py prepare --plan plan.json --run-id relay-02-A1 \
  --output /owned/runs/relay-02-A1-attempt1 --git /usr/bin/git --git-sha256 <approved-sha256>
~~~

Follow the order of the runs array. Its IDs are stable across seeds. Use a
fresh directory under a caller-owned parent for each attempt, including
retries with `--attempt 2` and so on. Preparation creates a separate local
Git repository with a reproducible starting commit and reapplies the exact
dirty seed for that task. It writes `receipt.json` after verifying the
source and Git status. Give the participant the prompt and settings in
`task.json`, and keep the plan's oracle with the reviewer. MCE4D can be
registered separately for B and C using disposable state. The participant
and reviewer handle edits and tests in their prepared worktree.

## Import observations

The importer consumes normalized JSON, independent of host export versions.
Use the host's authoritative per-attempt totals and retain the usage export
privately. Include tool schemas, cached input and every contributing turn.
Use disjoint attempt totals so cumulative samples do not overlap. Input
totals already include cached input.

The record contains `schema_version: 1`, `plan_sha256`, `usage_source` and
`runs`. Use `{"kind":"host_export","artifact_sha256":"<sha256>"}` for a
real usage export, `synthetic_fixture` with a fixture artifact digest for
importer checks, or `{"kind":"unavailable","artifact_sha256":null}`.

| Per-run key | Value |
|---|---|
| `run_id`, `starting_revision`, `input_sha256` | From the preparation receipt. Starting revision must agree across the repo's runs. |
| `attempts` | Chronological attempts including failures, cancellations and timeouts. Acceptance terminates a run. Missing planned runs remain unreported. |

Each attempt requires the fields below. Use null for missing optional
observations. Counts and durations are nonnegative integers.

| Attempt key | Value |
|---|---|
| `outcome` | `accepted`, `failed`, `timeout` or `cancelled` |
| `elapsed_ms` | Full attempt wall-clock time |
| `review` | Null or `review_id`, `critical_omissions`, `false_edits`. Acceptance requires independent review with both counts zero. The reviewer checks the task oracle and required tests. |
| `usage` | Null or `input_tokens`, `cached_input_tokens`, `output_tokens`. The cached split may be null. |
| `context_proxy` | Null or `tool_payload_bytes`, `lines`, `files`, `read_calls`. Individual values may be null. |
| `index` | Null or `full_ms`, `noop_ms`, `change_ms`, `bytes_hashed`, `storage_bytes`, `peak_tree_rss_bytes`. Individual values may be null. |
| `queries` | Array of `operation`, `temperature`, `outcome`, `elapsed_ms`, `freshness_ms`, `required_pending`. The last two values may be null. |

Query operations are `locate`, `context`, `expand`, `validate`, `rename`,
`read`, `search` and `semantic`. Use `cold` for the first query with empty
query caches and `warm` for a repeat after priming. Hold the checkout and
index constant and record the index build separately. Query outcomes are
`ok`, `failed`, `timeout` and `cancelled`. Timings are grouped by arm,
operation and cache temperature. p50/p95 use successful queries; the report
retains individual failed timings and outcome counts. Use the same priming
method and measurement boundaries in each arm.

~~~sh
python3 -B tools/bench/main.py report --plan plan.json --observations observations.json > report.json
~~~

The report lists all planned runs and counts retries and terminal failures.
Tokens per accepted task includes input and output from all attempts when
the cohort is complete and has full usage. A/C savings compare total input
across accepted pairs from the same task and repeat, include retry usage,
and expose counts of ineligible pairs. Missing usage stays null and byte
proxies stay in their original units. Reports keep source, patches,
host transcripts and credentials out of the observations schema.

Preparing the corpus and validating these calculations is T038. Actual
measurements and target judgments belong to T039. T053's scale workloads
remain available in `scale_fixture.py`.

Run the importer and planning checks with `python3 -B tools/bench/test_bench.py`.
Set `MCE4D_BENCH_GIT` and `MCE4D_BENCH_GIT_SHA256` to the approved local
executable's absolute path and SHA-256 to include the worktree test.

## Native scale measurements for T039

Use the protected Linux acceptance broker in
`tests/acceptance/native_broker.py` through the local run guard and private
PID/mount/network namespaces documented in the acceptance README. Use the
same approved product binaries and isolated synthetic state for each trial.
The `scale` suite runs the S27 large workloads `M` and `L`, with cohort
selected by `MCE4D_SCALE_COHORT`. The `bench` suite adds repeated query
measurements on the small cohorts `S16` and `S` (128 files). A bounded
index refusal is recorded with the count of published and staging
generations. Query, update and freshness results for that scope stay
`NOT_RUN` if index publication was refused.

Latency includes the bound native CLI process and response. The first
query of each operation is cold for the measured CLI sequence, immediately
after provisioning/indexing or the preceding phase. Warm queries repeat
the identical request against the same generation. This process leaves OS
disk caches in place. Snapshot context and full workspace validation are
separate operations. Failed queries stay in the observations and out of
successful-query p50/p95. The sampler reports peak observed process-tree
RSS and kernel process write counters observed before exit as a lower
bound. Storage is reported as allocated bytes and logical bytes; captured
logical source bytes are recorded separately from total bytes hashed.

The fixed A/B/C retrieval probe measures delivered bytes for one authored
symbol oracle. It exercises direct grep/read, locate plus direct read, and
locate plus snapshot context on the same source. These local transport
counts are separate from host input/output tokens and host task success.

## C078 beyond the L file-count boundary

Run the same protected Linux broker with `MCE4D_ACCEPTANCE_SUITE=bench` and
`MCE4D_SCALE_COHORT=XL150K`. The independently authored workload contains
150,000 files. Keep the profile's default 100,000-file limit and record the
actual configuration. The pinned packaged CLI must refuse within its
deadline, preserve the source, and publish zero generations. The only query
after refusal checks that status reports a missing index. The evidence records
latency to refusal, sampled tree RSS, sampled write-byte floor, and allocated
state. Workload queries and incremental updates remain `NOT_RUN`.
Use the 30-task T038 plan and an authoritative usage export when a host
comparison has been authorized.

Run `bench-retention` separately for the wall-clock TTL fixture. Its
12-second pack TTL and 60-second rename TTL belong to that disposable
profile. The ordinary measurement profile uses the existing 900-second
default. The existing
`gc`, `restore`, `typed-graph` and `typed-mutations` suites provide
generation, lock wait, query plan and independent equivalence evidence.
The `bench-context` fixture measures first-page delivery and token-bound
live validation over a persistent MCP session. A paginated first page
keeps its reported pending count. Its timings use a distinct workload
and transport from the repeated native CLI samples.
