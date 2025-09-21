# IBP GeoDNS v2

A distributed, geo-aware DNS load balancer with health monitoring, consensus-based failover, and usage accounting for the IBP Network.

## Overview

IBP GeoDNS v2 is a multi-repository Go system that provides:
- **Geographic DNS routing** - Routes users to nearest healthy infrastructure provider
- **Health monitoring** - Continuous checks via ping, SSL, WebSocket, and ETHRPC
- **Consensus-based failover** - NATS-powered distributed consensus for status changes  
- **Usage accounting** - Tracks DNS queries by member, country, ASN, and network
- **PowerDNS integration** - Acts as a remote backend for PowerDNS Authoritative Server

## Architecture

### Core Components

**ibp-geodns** - DNS API server
- PowerDNS remote backend implementation (POST /dns)
- MaxMind GeoIP-based routing with 30s TTL for dynamic records
- Static records (NS, TXT, SOA) from remote configuration
- ACME challenge support via HTTP fetch
- Daily usage aggregation and MySQL persistence

**ibp-geodns-monitor** - Health check orchestrator
- Performs site (ping), domain (SSL), and endpoint (WebSocket/ETHRPC) checks
- Proposes status changes via NATS, requires majority consensus
- Publishes official results after finalization
- IPv4/IPv6 aware monitoring

**ibp-geodns-libs** - Shared libraries
- Configuration management (local + remote hot-reload)
- MaxMind GeoLite2 integration with auto-updates
- MySQL data persistence
- NATS cluster communication
- Matrix alerting with deduplication

**ibp-geodns-collator** - Metrics aggregation
- Collects per-node usage statistics
- Processes finalized events for BI/reporting

**ibp-geodns-dashboard** - Web UI
- Management interface for monitoring and metrics

## Quick Start

### Prerequisites
- Go 1.24.x
- MySQL 5.7+
- NATS server cluster
- PowerDNS 4.x (for production)
- MaxMind GeoLite2 license key

### Installation

1. Clone the repository:
```bash
git clone https://github.com/ibp-network/ibp-geodns.git
cd ibp-geodns
```

2. Configure your environment:
```bash
cp config/ibpdns.json.example config/ibpdns.json
# Edit with your MySQL, NATS, and MaxMind credentials
```

3. Build the binaries:
```bash
go build -o bin/ibp-geodns ./src/IBPDns.go
go build -o bin/ibp-geodns-monitor ./src/monitor/main.go
```

4. Initialize the database:
```bash
mysql -u root -p < db/schema.sql
```

5. Start the services:
```bash
./bin/ibp-geodns -config config/ibpdns.json
./bin/ibp-geodns-monitor -config config/monitor.json
```

## Configuration

### Local Configuration (ibpdns.json)
```json
{
  "System": {
    "WorkDir": "/path/to/workdir",
    "LogLevel": "Info",
    "ConfigReloadTime": 3600,
    "MinimumOfflineTime": 900
  },
  "Nats": {
    "NodeID": "NODE-01",
    "Url": "nats://server1:4222,nats://server2:4222",
    "User": "geodns",
    "Pass": "__SET_ME__"
  },
  "Mysql": {
    "Host": "localhost",
    "Port": "3306",
    "User": "ibpdns",
    "Pass": "__SET_ME__",
    "DB": "ibpdns"
  },
  "DnsApi": {
    "ListenAddress": "0.0.0.0",
    "ListenPort": "6100",
    "MonitorAddress": "127.0.0.1",
    "MonitorPort": "6101"
  }
}
```

### Remote Configuration
Fetched from GitHub and hot-reloaded:
- **StaticDNS** - NS, TXT, and other static records
- **Members** - Infrastructure provider details and locations
- **Services** - RPC endpoints and service assignments
- **Alerts** - Matrix notification settings

## PowerDNS Integration

Configure PowerDNS to use the remote backend:

```conf
# pdns.conf
launch=remote
remote-connection-string=http:url=http://localhost:6100/dns
```

## API Endpoints

### DNS API (port 6100)
- `POST /dns` - PowerDNS remote backend protocol
  - Methods: initialize, lookup, list, getDomainInfo, getAllDomains, getAllDomainMetadata, getDomainKeys, getMemberEvents
- `GET /process?date=YYYY-MM-DD` - Trigger usage flush to MySQL

### Monitor API (port 6101)  
- `GET /results` - Current health check results (offline-only rollups)

## Health Check Types

- **site** - ICMP ping check for basic connectivity
- **domain** - SSL certificate validation for RPC/ETHRPC domains
- **endpoint** - WebSocket (RPC) or ETHRPC connection tests

## Consensus Protocol

1. Monitor performs local health checks
2. Proposes status via NATS to cluster
3. Other monitors vote on proposal
4. Status becomes official when:
   - Minimum 2 votes received AND
   - Majority of active monitors agree
5. DNS nodes use only official status for routing

## Usage Accounting

- In-memory aggregation with 5-minute flush intervals
- Tracks: date, domain, member, country, ASN, network
- IPv4/IPv6 separated statistics
- Daily aggregation via `/process` endpoint

## Development

### Adding a Health Check
```go
RegisterSiteCheck("new-check", checkFunc)
// Or for typed checks:
RegisterDomainCheckWithTypes("ssl-check", []string{"RPC"}, sslCheckFunc)
```

### Adding a Service
1. Extend Services JSON configuration
2. Update member assignments
3. Call `RebuildServiceRecords()` after config reload

### Database Migrations
Place SQL files in `db/migrations/` with format:
```
2025-09-21T120000_description.sql
```

## Monitoring

- Logs: Configurable levels (Debug, Info, Warn, Error, Fatal)
- Metrics: Via collator to MySQL for BI/dashboards
- Alerts: Matrix notifications for outages with @ mentions

## Security Notes

- Never expose secrets in configs - use `__SET_ME__` placeholders
- Member.Override=true excludes from routing
- ACME challenges limited to 512 bytes, 3 retry attempts
- SOA serial uses current UTC timestamp

## Support

For issues and questions:
- GitHub Issues: https://github.com/ibp-network/ibp-geodns/issues
- Documentation: See `docs/` folder for detailed guides