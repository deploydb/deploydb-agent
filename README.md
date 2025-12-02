# DeployDB Agent

The official agent for [DeployDB](https://deploydb.com) - PostgreSQL deployment, monitoring, and maintenance.

## What it Does

The DeployDB agent runs on your server and:

- **Deploys** fresh PostgreSQL installations via PGDG repositories
- **Monitors** PostgreSQL metrics (connections, disk, memory, replication lag)
- **Executes** maintenance commands (VACUUM, ANALYZE, REINDEX)
- **Reports** to the DeployDB control plane via secure WebSocket

## Installation

### Deploy New PostgreSQL

```bash
curl -sSL https://get.deploydb.com | sudo bash -s -- --token=YOUR_TOKEN
```

This will:
1. Detect your OS (Ubuntu, Debian, RHEL, Rocky, Alma, Fedora, macOS)
2. Install PostgreSQL from official PGDG repositories
3. Configure and start the database
4. Connect to DeployDB for monitoring

### Monitor Existing PostgreSQL

```bash
# Download the agent
curl -LO https://github.com/deploydb/deploydb-agent/releases/latest/download/deploydb-agent-linux-amd64
chmod +x deploydb-agent-linux-amd64
sudo mv deploydb-agent-linux-amd64 /usr/local/bin/deploydb-agent

# Create config file
sudo mkdir -p /etc/deploydb
sudo tee /etc/deploydb/config.yaml <<EOF
control_plane:
  url: wss://app.deploydb.com/cable
  token: YOUR_SERVER_TOKEN

postgres:
  host: localhost
  port: 5432
  user: postgres
  database: postgres

metrics:
  interval: 30s
EOF

# Start the agent
sudo deploydb-agent run --config=/etc/deploydb/config.yaml
```

## Supported Platforms

| OS | Versions | Architecture |
|----|----------|--------------|
| Ubuntu | 20.04, 22.04, 24.04 | amd64, arm64 |
| Debian | 11, 12 | amd64, arm64 |
| RHEL/Rocky/Alma | 8, 9 | amd64, arm64 |
| Fedora | 40+ | amd64, arm64 |
| macOS | 12+ | amd64 (Intel), arm64 (Apple Silicon) |

## Commands

```bash
# Deploy PostgreSQL (requires root)
sudo deploydb-agent bootstrap --token=YOUR_TOKEN

# Start monitoring daemon
deploydb-agent run --config=/etc/deploydb/config.yaml

# Show version
deploydb-agent version

# Show help
deploydb-agent help
```

## Configuration

```yaml
# /etc/deploydb/config.yaml

control_plane:
  url: wss://app.deploydb.com/cable
  token: ddb_xxxxxxxxxxxxxxxxxxxx

postgres:
  host: localhost
  port: 5432
  user: postgres
  database: postgres
  # password: optional, uses peer auth by default

metrics:
  interval: 30s  # How often to collect and send metrics

log_level: info  # debug, info, warn, error
```

## Building from Source

```bash
# Clone the repo
git clone https://github.com/deploydb/deploydb-agent.git
cd deploydb-agent

# Build for current platform
make build

# Build for all platforms
make build-all

# Run tests
make test
```

## Security

- All communication uses TLS (wss://)
- Server tokens are unique per server and can be rotated
- Commands are cryptographically signed (Ed25519)
- The agent never stores database credentials
- Source code is fully auditable

## License

MIT License - see [LICENSE](LICENSE)

## Links

- [DeployDB Website](https://deploydb.com)
- [Documentation](https://docs.deploydb.com)
- [Report Issues](https://github.com/deploydb/deploydb-agent/issues)
