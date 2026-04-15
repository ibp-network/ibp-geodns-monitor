# IBP GeoDNS Monitor

Health-check service for the IBP GeoDNS ecosystem. This repository builds the monitor daemon that schedules infrastructure checks, publishes status changes through the shared NATS layer, and exposes official health snapshots over HTTP for downstream consumers such as GeoDNS.

## What This Repo Contains

- Process entrypoint: `src/IBPMonitor.go`
- Monitor API: `src/api/api.go`
- Scheduler and check modules: `src/monitor/`
- Sample config: `docs/monitor.conf`
- Sample systemd unit: `docs/systemd/ibpmonitor.systemd`
- Schema reference: `docs/mysql/db.sql`

This repository does **not** implement the PowerDNS remote backend or the `/dns` API. Those belong to the DNS service, not this monitor binary.

## Features

- Reload-aware worker queue for recurring checks
- Site checks: ICMP ping
- Domain checks: TLS certificate validation
- Endpoint checks:
  - Substrate WebSocket RPC
  - Ethereum JSON-RPC
- Status proposal flow via `github.com/ibp-network/ibp-geodns-libs`
- HTTP results endpoint for the current official monitor snapshot

## Runtime Dependencies

- Go 1.24.x for local builds
- Reachable NATS cluster
- MaxMind database path and license credentials
- Remote IBP config URLs for members/services/static metadata
- Optional MySQL settings when required by the shared library stack

## Configuration

The monitor reads a JSON config file via `-config`. The binary defaults to `ibpmonitor.json`.

A full sample lives at `docs/monitor.conf` and is JSON despite the `.conf` filename. A common local workflow is:

1. Create `config/ibpmonitor.json`
2. Start from `docs/monitor.conf`
3. Replace placeholder credentials, node IDs, and filesystem paths

Important sections:

- `System`: working directory, log level, remote config URLs
- `Nats`: node identity and cluster credentials
- `Maxmind`: GeoIP database path and license info
- `MonitorApi`: listen address/port for this binary
- `CheckWorkers`: queue concurrency and worker separation interval
- `Checks`: enabled site/domain/endpoint checks and their options

`DnsApi` may still appear in the shared config schema for ecosystem compatibility, but this monitor binary serves only `MonitorApi`.

## HTTP API

### `GET /results`

Returns the current official monitor snapshot grouped into:

- `SiteResults`
- `DomainResults`
- `EndpointResults`

Each result contains the check identity, IP version, and the latest member observations with timestamps and any check data captured by the monitor.

## Build

```bash
make build
```

or

```bash
go build -o bin/ibp-monitor ./src/IBPMonitor.go
```

The build output is `bin/ibp-monitor`.

## Test

```bash
go test ./...
```

## Run

```bash
bin/ibp-monitor -config config/ibpmonitor.json
```

`make run` performs the same startup after verifying that `config/ibpmonitor.json` exists.

## Docker

```bash
make docker-build
docker run --rm \
  -v "$PWD/config:/opt/ibp-geodns-monitor/config" \
  ibp-geodns-monitor:dev
```

The container entrypoint is `/usr/local/bin/ibp-monitor` and defaults to `/opt/ibp-geodns-monitor/config/ibpmonitor.json`.

## Deployment

A sample systemd unit is provided at `docs/systemd/ibpmonitor.systemd`.

Typical Linux layout:

- Binary: `/usr/local/bin/ibp-monitor`
- Working directory: `/opt/ibp-geodns-monitor`
- Config: `/opt/ibp-geodns-monitor/config/ibpmonitor.json`

## Repository Layout

- `src/IBPMonitor.go`: process bootstrap and shared library initialization
- `src/api/`: `/results` HTTP API
- `src/monitor/`: queue, worker manager, and health-check implementations
- `docs/`: sample config, systemd unit, and schema reference

## Notes

- Consumers depend on `/results`, so keep that route and payload stable when extending checks.
- Shared types and consensus/state helpers live in `github.com/ibp-network/ibp-geodns-libs`.

## License

See `LICENSE` in the repository root.