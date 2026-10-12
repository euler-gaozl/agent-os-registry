# Agent OS Registry

Independent resource/version and encrypted credential service. Owns only
`public.resources` and `public.secrets`; has no Runtime, ToolHub, or Knowledge
table access. Catalog queries return bounded summaries. Skill/MCP writes are
forwarded to the configured ToolHub; startup requires `TOOLHUB_URL`.

## Build and run

Go 1.27.2, PostgreSQL 18, PowerShell 7 for the optional build helper:

```powershell
$env:GOWORK='off'
./scripts/Build.ps1
./bin/registry.exe --config D:/workspace/agent-platform/local/config/services/registry.json
```

Builds use the committed `vendor` tree with `GOPROXY=off`; no sibling repo,
private-module credentials, Node or model key is needed. Linux can use the same
script or `go build -mod=vendor ./cmd/registry`. `source-provenance.json` records
the integration source and fixed SDK commit/module checksum. Update dependencies
only from a new fixed SDK version, regenerate vendor, and review its diff.

`deploy/service.example.json` contains placeholders, not a runnable identity.
Provision distinct random service tokens; each receiver stores peer SHA-256
hashes. `SERVICE_PEERS` must include this service's token hash. Preserve the
existing `MASTER_KEY` when upgrading: replacing it loses credential readability.
Environment values override the JSON file. Process-internal configuration cannot
be resolved as business credentials. Only the three legacy provider variables
DEEPSEEK_API_KEY/LANGFUSE_PUBLIC_KEY/LANGFUSE_SECRET_KEY have environment fallback;
other named provider keys must be stored through the encrypted credential API.

Control may manage resources/credentials but cannot resolve plaintext credentials.
Execution/retrieval services can resolve named provider credentials. Futures can
read status and its publication list. This is service-level trust, not tenant RBAC.

## Migration and release

Provision the application role and its database connection separately. Schema
owner/DDL credentials are supplied only to `bin/migrate`, through
`MIGRATION_DATABASE_URL`. Run `bin/migrate --role <existing-application-role>`.
The application must have schema USAGE, and must not be its owner or have CREATE.
The migration transaction changes/grants only the two owned tables; it has lock
and statement timeouts and does not rewrite other service configuration. Empty
or incompatible databases fail startup readiness; startup never runs DDL.

Keep the old binary and configuration, run the compatible expansion migration,
replace only Registry and check authenticated `/readyz`. Rollback is a binary
rollback under the same HTTP/data contract in `deploy/release-contract.json`;
never perform destructive down migrations. Integration's Windows release manager
will own verified catalog activation; this repository is the source/build owner.

The Docker image needs only its JSON config, scoped PostgreSQL endpoint, ToolHub
URL and optional Runtime heartbeat URL. Run with read-only root, memory/CPU/PID
limits, and a private network; TLS termination/peer network policy is deployment
responsibility. `/healthz` is liveness; `/readyz` checks local schema/permissions,
not ToolHub health or model balance.

## Verification scope

`go test -mod=vendor -count=1 ./...` and `go vet -mod=vendor ./...`.
Set `TEST_DATABASE_URL` to a disposable PostgreSQL administrator connection to
execute database tests. Tests create/drop uniquely named fixture databases and
NOLOGIN roles; they do not migrate the database named in that URL. Without it,
integration tests explicitly skip. Linux CI runs real PostgreSQL and race checks;
Windows CI covers source/build portability. No test uses a real model key.

P2 is in progress: source/build extraction does not by itself prove live
independent release/rollback, whole-platform fault tolerance or scale targets.
