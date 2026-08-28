<p align="center">
  <img src="web/public/favicon.svg" width="96" alt="DnsCat">
</p>

<h1 align="center">DnsCat</h1>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=111111">
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-5.5-3178C6?style=flat-square&logo=typescript&logoColor=white">
  <img alt="Vite" src="https://img.shields.io/badge/Vite-5-646CFF?style=flat-square&logo=vite&logoColor=white">
  <img alt="Tailwind CSS" src="https://img.shields.io/badge/Tailwind_CSS-3-06B6D4?style=flat-square&logo=tailwindcss&logoColor=white">
  <img alt="SQLite" src="https://img.shields.io/badge/SQLite-Default-003B57?style=flat-square&logo=sqlite&logoColor=white">
  <img alt="MySQL" src="https://img.shields.io/badge/MySQL-Optional-4479A1?style=flat-square&logo=mysql&logoColor=white">
</p>

<p align="center">
  <img alt="Authoritative DNS" src="https://img.shields.io/badge/Authoritative_DNS-UDP_%2F_TCP_53-1976D2?style=flat-square">
  <img alt="DNSSEC" src="https://img.shields.io/badge/DNSSEC-ECDSA_P--256-2E7D32?style=flat-square">
  <img alt="DoH / DoT" src="https://img.shields.io/badge/DoH_%2F_DoT-RFC_8484_%2F_7858-00838F?style=flat-square">
  <img alt="GeoDNS" src="https://img.shields.io/badge/GeoDNS-Continent_%2F_Country_%2F_ASN-7B1FA2?style=flat-square">
  <img alt="Edge Cluster" src="https://img.shields.io/badge/Edge_Cluster-Multi--PoP-F57C00?style=flat-square">
  <img alt="Linux" src="https://img.shields.io/badge/Linux-amd64_%7C_arm64-FCC624?style=flat-square&logo=linux&logoColor=111111">
</p>

**English** | [简体中文](docs/README.zh-CN.md)

DnsCat is a self-hosted authoritative DNS server with a web control panel. Once you delegate a domain's nameservers to DnsCat, it answers authoritatively for the records you configure, and adds DNSSEC signing, geo-based traffic steering, origin health checking with failover, dynamic DNS, automatic ACME certificate issuance, per-query security enforcement, and a multi-node edge cluster on top of that.

The backend is written in Go with a DNS engine built on `miekg/dns`. The frontend is React and TypeScript, and its production build is served directly by the backend. State is persisted in SQLite by default or MySQL when configured, while Redis provides caching, cross-node broadcast, and FIFO persistence for security logs.

<p align="center">
  <img src="image/demo.png" alt="DnsCat control panel in light and dark themes">
</p>

## Features

| Area | What DnsCat provides |
| --- | --- |
| Authoritative serving | UDP/TCP 53, DoT (RFC 7858), and DoH (RFC 8484) entry points; in-memory zone indexing with concurrent lookup so a single node sustains high query volume. |
| Record types | `A` `AAAA` `CNAME` `TXT` `MX` `NS` `SRV` `CAA` `PTR` `SOA` `ALIAS/ANAME` (apex flattening) `HTTPS` `SVCB` `TLSA` `SSHFP` `DS` `DNSKEY`. |
| Record management | Full CRUD, enable/disable toggle, checkbox bulk actions (bulk enable / disable / delete committed in a single transaction), filtering by type and routing line, search, and pagination. |
| DNSSEC | Generates KSK (257) and ZSK (256) using ECDSA Curve P-256 with SHA-256; live RRSIG response signing, NSEC negative proof, key rotation, plus the DS record parameters and registrar instructions. |
| Geo steering | Define lines across three dimensions — continent, country (ISO 3166-1), and ASN — combined with OR semantics; seven built-in continent lines, custom lines with priority, referenced from records via `geo_line`. |
| ECS and weighting | EDNS0 Client Subnet (RFC 7871) for accurate subnet-level placement; records sharing a name can carry weights for weighted round-robin. |
| Health checking | HTTP/HTTPS status code, TCP port reachability, and ICMP echo probes; automatic failover to a backup IP after consecutive failures, automatic recovery, and on-demand probing. |
| Dynamic DNS | dyndns2 endpoint at `/nic/update`, compatible with ddclient and common router firmware; a separate credential scheme storing only SHA-256 digests, with hostname allow-lists, source IP/CIDR allow-lists, key rotation, and last-use auditing. |
| SSL certificates | ACME DNS-01 issuance and renewal across Let's Encrypt, ZeroSSL, Buypass and others, including EAB external account binding; applicant profiles, multiple key algorithms, certificate and private key download, and categorized failure diagnostics. |
| Security enforcement | Individually toggleable per-query rules: rate limiting, flood/DDoS, RRL response rate limiting, IP/ASN/country blacklists, resolver ACL, AXFR transfer restriction, abnormal QTYPE filtering, ANY policy (RFC 8482), per-zone QPS, and NXDOMAIN random-subdomain (water torture) protection. |
| Security logs | Live detail of blocked queries with filtering by rule, outcome, and keyword plus server-side pagination; a configurable retention cap (100–200,000) that rotates FIFO and persists to Redis so it survives restarts. |
| Edge cluster | A master plus lightweight edge node daemons; Redis PubSub broadcasts configuration changes while nodes pull zone snapshots over REST; nodes self-register, report heartbeats, and get a generated deployment command. |
| Node monitoring | Per-node time series for CPU, memory, disk, network throughput, live QPS, cumulative queries, and response latency, alongside uptime-style availability bars (1h to 30d). |
| Analytics | Global and per-zone QPS trends, query type distribution, country traffic breakdown, and a world heatmap over selectable 1h–30d ranges; telemetry is persisted and does not reset on restart. |
| Nameservers | A registry of authoritative NS hostnames, their binding to cluster nodes, IPv4/IPv6 glue records, and both scheduled and manual verification of public NS delegation. |
| Open API | Every management endpoint accepts an `X-API-Key` header for login-free access with the same permissions as the owning account; the panel ships an API reference page listing all endpoints, parameters, and examples. |
| Control panel | Built to the Vercel Geist design language, with dark and light themes, English and Chinese UI, responsive layout, and a mobile sidebar drawer. |

## Architecture

```text
                        [ recursive resolvers / DNS clients ]
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        │ UDP/TCP 53                │ DoH                       │ DoT 853
        ▼                           ▼                           ▼
┌────────────────┐         ┌──────────────────┐         ┌────────────────┐
│  Edge node     │         │      Master      │         │  Edge node     │
│  DNS daemon    │         │ API + panel + DNS│         │  DNS daemon    │
└───────┬────────┘         └────────┬─────────┘         └───────┬────────┘
        │                           │                           │
        └───────────┬───────────────┴───────────────┬───────────┘
                    │ heartbeats / zone snapshots   │
                    ▼                               ▼
            ┌────────────────┐              ┌────────────────┐
            │ SQLite / MySQL │              │     Redis      │
            │  configuration │              │ cache/pubsub/  │
            │                │              │ security logs  │
            └────────────────┘              └────────────────┘
```

The master serves the API, the control panel, and authoritative DNS. Edge nodes run only the DNS daemon, pulling zone snapshots and routing rules from the master over the public internet while reporting heartbeats and telemetry. Configuration changes are broadcast through Redis PubSub so edge nodes invalidate their local cache immediately.

## Installation

### One-click installer

Nothing to clone. The script asks for a language, then whether to install via native binary (systemd) or Docker, then the role — and completes the whole installation itself.

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash
```

Prefer to skip the questions? Install a master straight away — binary mode with SQLite, no Docker needed:

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash -s -- --yes --role master
```

The first run generates a random administrator password and prints it once. There is no factory default password.

When the master finishes it prints a ready-to-paste command for edge nodes with the cluster token and master URL already filled in. It looks like this:

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash -s -- --yes --role node --master-url http://203.0.113.10:8080 --node-id edge-fra-01 --cluster-token <TOKEN> --public-ip 203.0.113.20
```

Supported architectures: `linux/amd64`, `linux/386`, `linux/arm64`, `linux/armv7`.

Useful options:

| Option | Purpose |
| --- | --- |
| `--lang zh\|en` | Interface language; asked interactively when omitted. |
| `--mode binary\|docker` | Install method; asked interactively, otherwise `binary`. |
| `--role master\|node` | Install role; asked interactively, otherwise `master`. |
| `--db sqlite\|mysql` | Master database; defaults to `sqlite`. |
| `--db-dsn DSN` | Custom database connection string, overrides `--db`. |
| `--http-port` / `--dns-port` | Panel and authoritative DNS ports. |
| `--from-source` | Force a build from source, requiring Go (plus Node.js for the master). |
| `--binary-dir DIR` | Use existing binaries from a directory, skipping download and build. |
| `--upgrade` | Replace binaries and restart, preserving configuration and data. |
| `--uninstall` | Remove the service; add `--purge` to delete configuration and data too. |
| `-y, --yes` | Non-interactive, auto-confirm every remediation step. |

Downloaded binaries are verified against the release's `SHA256SUMS.txt` before installation. A mismatch aborts the install.

Every binary reports its own version:

```bash
dnscat-server --version   # dnscat-server v0.1.0 (abc1234, 2026-08-26T00:00:00Z, go1.25.0, linux/amd64)
dnscat version
```

About port 53: in binary mode the installer detects what already holds the port and steps aside where it can do so safely. `systemd-resolved` is handled by disabling its stub listener, which leaves the host's own name resolution intact. Occupants it cannot resolve safely abort the install with guidance. Docker mode never modifies host services — it aborts on conflict and prints the steps for you.

### Docker Compose

The installer handles the Docker path end to end as well — it fetches a source snapshot when you run it standalone, builds the images and starts the stack:

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash -s -- --yes --mode docker --role master
```

Images are published to GHCR for `linux/amd64` and `linux/arm64`, so the installer pulls instead of building. `armv7` is binary-install only — the Alpine base used here has no armv7 Node image for the frontend build stage.

```
ghcr.io/mengmengcode/dnscat-server
ghcr.io/mengmengcode/dnscat-node
```

Add `--from-source` to build the images on the target machine instead of pulling.

To run Compose directly, prepare `deploy/.env` first — the three secrets have no defaults, so Compose fails fast rather than silently using weak values:

```bash
cat > deploy/.env <<EOF
MYSQL_ROOT_PASSWORD=$(openssl rand -hex 24)
JWT_SECRET=$(openssl rand -hex 32)
CLUSTER_TOKEN=$(openssl rand -hex 32)
EOF

docker compose -f deploy/docker-compose.master.yml up -d --build
```

Retrieve the generated administrator password from the logs:

```bash
docker compose -f deploy/docker-compose.master.yml logs server | grep -A4 admin
```

MySQL and Redis are reachable only on the internal Compose network — they use `expose`, not `ports`, so nothing is published to the host or the public internet.

For an edge node machine, `deploy/.env` instead holds `MASTER_URL`, `NODE_ID`, `CLUSTER_TOKEN`, and `NODE_PUBLIC_IP`:

```bash
docker compose -f deploy/docker-compose.node.yml up -d --build
```

`NODE_PUBLIC_IP` must be set explicitly. When a node reaches the master through NAT or a container bridge, the source address the master observes is a private one, which would otherwise end up in the node list and in glue records.

### Endpoints

| Entry point | Address |
| --- | --- |
| Control panel | `http://<host>:8080` |
| Authoritative DNS | `<host>:53` (UDP/TCP) |
| DoH | `http://<host>:8080/dns-query` |
| DoT | `<host>:853` (disabled by default) |

> [!IMPORTANT]
> Port 8080 serves plain HTTP. Put it behind a TLS reverse proxy and restrict the source IPs with a firewall or security group before relying on it long term. Note that DoH over plain HTTP provides no confidentiality — terminate TLS in front of it for DoH to be meaningful.

### Building from source

Prerequisites: Go 1.25+, Node.js 20+, npm.

```bash
# 1. Build the frontend; output lands in web/dist and is served by the backend
cd web
npm install
npm run build
cd ..

# 2. Build the backend
go build -o bin/server ./cmd/server
go build -o bin/node   ./cmd/node
go build -o bin/dnscat ./cmd/cli

# 3. Start the master
./bin/server --config deploy/config.example.yaml
```

Binding port 53 requires privileges: run with `sudo` or grant `CAP_NET_BIND_SERVICE`.

## Configuration

Configuration is YAML, selected with `--config`; see `deploy/config.example.yaml`.

| Key | Default | Description |
| --- | --- | --- |
| `server.http_port` | `8080` | Panel and API listen port. |
| `server.host` | `0.0.0.0` | Listen address. |
| `server.mode` | `release` | Gin mode; `debug` emits verbose logs. |
| `server.trusted_proxies` | empty | Reverse proxies allowed to set `X-Forwarded-For`. Empty means no proxy is trusted and the client IP is the TCP source address. Only set this when actually running behind a proxy, otherwise `X-Forwarded-For` can be spoofed and DDNS source allow-lists bypassed. |
| `dns.udp_port` / `dns.tcp_port` | `53` | Authoritative DNS ports. |
| `dns.tls_port` | `853` | DoT port. |
| `dns.enable_tls` | `false` | Enable DoT; requires a certificate and key path. |
| `dns.enable_doh` | `true` | Enable the DoH endpoint at `/dns-query`. |
| `dns.default_ns` | — | Authoritative NS list written into newly created zones. |
| `dns.default_ttl` | `300` | Default record TTL in seconds. |
| `dns.rate_limit` | `1000` | Per-IP queries per second baseline for RRL. |
| `database.driver` | `sqlite` | `sqlite` or `mysql`. |
| `database.dsn` | `data/dnscat.db` | Data source; a file path under SQLite. |
| `redis.enabled` | `true` | When disabled, caching falls back to process memory and cross-node broadcast plus security log persistence become unavailable. |
| `redis.addr` / `password` / `db` | `redis:6379` | Redis connection parameters. |
| `dnssec.auto_sign` | `true` | Whether DNSSEC-enabled zones sign responses automatically. |
| `dnssec.algorithm` | `ECDSAP256SHA256` | Signing algorithm. |
| `cluster.secret_token` | empty | Cluster token, the sole credential edge nodes use to pull full zone snapshots. **Must be a random value.** |
| `cluster.sync_interval` | `10` | Zone snapshot sync interval in seconds. |
| `prober.enabled` | `true` | Enable origin health checking. |
| `prober.check_interval` | `15` | Probe interval in seconds. |
| `prober.timeout_sec` | `5` | Per-probe timeout in seconds. |
| `jwt_secret` | empty | Panel session signing key. **Must be a random value**; a temporary one is generated when empty, invalidating sessions on restart. |

Several keys can be overridden by environment variables, which is what the Docker deployment uses so no config file needs mounting:

| Variable | Overrides |
| --- | --- |
| `DNSCAT_HTTP_PORT` | `server.http_port` |
| `DNSCAT_DB_DSN` | `database.dsn`, switching the driver to MySQL when the DSN looks like one |
| `DNSCAT_REDIS_ADDR` | `redis.addr`, also enabling Redis |
| `DNSCAT_JWT_SECRET` | `jwt_secret` |
| `DNSCAT_CLUSTER_TOKEN` | `cluster.secret_token` |

Never commit `jwt_secret`, the cluster token, database passwords, or ACME EAB credentials to the repository.

## CLI

The `dnscat` binary built from `cmd/cli` handles day-to-day operations on the server and opens an interactive menu when run without arguments. It detects whether the install is systemd- or Compose-based and acts accordingly.

```bash
dnscat                   # interactive management menu
dnscat reset-admin       # reset administrator credentials, no old password needed
dnscat start             # start the service
dnscat restart           # restart the service
dnscat stop              # stop the service
dnscat status            # show service status and ports
dnscat update            # update the service (Compose installs rebuild in place)
```

`reset-admin` rewrites credentials directly in the database, which is the recovery path when the administrator password is lost.

## Verifying resolution

```bash
# Standard query
dig @127.0.0.1 example.com A +noall +answer

# DNSSEC signatures and DNSKEY
dig @127.0.0.1 example.com DNSKEY +dnssec +noall +answer
dig @127.0.0.1 example.com A      +dnssec +noall +answer

# ECS steering: simulate client subnets in different regions
dig @127.0.0.1 cdn.example.com +subnet=114.114.114.0/24 +noall +answer
dig @127.0.0.1 cdn.example.com +subnet=8.8.8.0/24       +noall +answer

# DoH, in the Cloudflare/Google compatible JSON format
curl -s "http://127.0.0.1:8080/dns-query?name=example.com&type=A" | jq .
```

DnsCat is authoritative-only and is not an open resolver. Names outside the zones it hosts are answered with `REFUSED`, and responses always carry `RA=0`.

## Development

```bash
# Frontend dev server, proxying to the local backend
cd web
npm install
npm run dev

# Backend
go run ./cmd/server --config deploy/config.example.yaml

# Tests and static analysis
go test ./...
go vet ./...

# Frontend type check and production build
cd web && npm run build
```

The production frontend build lives in `web/dist` and is served statically by the backend. `index.html` is returned with `Cache-Control: no-cache`, so browsers pick up newly hashed assets right after a release without a manual hard refresh.

## Project layout

```text
cmd/server/                 Master entry point: API, panel, authoritative DNS
cmd/node/                   Edge node daemon
cmd/cli/                    Server-side operations CLI
internal/api/               REST controllers (domains, records, DNSSEC, certs, nodes, security, stats)
internal/dnsengine/         DNS engine: zone store, resolution, DNSSEC, EDNS, RRL, security rules, telemetry
internal/cluster/           Cluster management: heartbeats, zone snapshot sync, change broadcast
internal/acme/              ACME client: accounts, DNS-01, issuance and renewal
internal/geo/               GeoIP lookup and continent/country/ASN line matching
internal/healthcheck/       Origin probing and failover
internal/cache/             Redis wrapper: cache, PubSub, LIST for security log FIFO
internal/database/          GORM persistence and auto-migration
internal/model/             Data entities
internal/config/            YAML configuration
internal/nsverify/          Public NS delegation verification
internal/telemetrystore/    Telemetry persistence and recovery
internal/securitystore/     Security counter persistence
internal/seclogstore/       Security log persistence in Redis
internal/sysmetrics/        Host CPU / memory / disk / network collection
web/src/pages/              Control panel pages
web/src/components/         Geist UI components
deploy/                     Dockerfiles, Compose files, config template, installer
```

## Responsible use

Authoritative DNS determines whether a domain resolves at all. Deleting a zone, disabling DNSSEC, changing authoritative nameservers, or tightening security rules can break resolution or start refusing queries. Confirm the blast radius before each change and keep a rollback path.

DNSSEC key rotation must be coordinated with the DS record in the parent zone. Rotating before the DS is updated causes validation failures that take the entire domain offline. Blacklists, ACLs, and rate limits genuinely refuse live queries, so validate thresholds on a low-traffic zone first.

Only serve domains you own or are authorized to operate.

## Contributing

Issues and pull requests are welcome. Keep changes focused, never commit credentials, keys, or real user data, and cite the relevant RFC when the change affects protocol behavior.

Before submitting:

```bash
go test ./...
go vet ./...
cd web && npm run build
```

## License

Licensed under Apache-2.0.
