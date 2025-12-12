# GOWA Manager

**A full-stack web application for managing multiple GOWA (Go WhatsApp Web Multidevice) instances with version control, real-time monitoring, and advanced configuration options.**

This project is built on top of [go-whatsapp-web-multidevice](https://github.com/aldinokemal/go-whatsapp-web-multidevice) by **Aldino Kemal**.

## Features

- 🚀 **Multiple Instance Management** - Create, configure, and manage multiple GOWA instances
- 📦 **Version Control** - Install and use different GOWA versions per instance
- 🔄 **Real-time Monitoring** - Live status updates, resource usage, and uptime tracking
- ⚙️ **Advanced Configuration** - Comprehensive CLI flags and environment variable support
- 🔐 **Authentication System** - Optional basic authentication for web UI
- 🌐 **Proxy Integration** - Built-in proxy for accessing instance web interfaces
- 🔧 **Auto-restart Service** - Automatic instance recovery after server restarts
- 📊 **Resource Monitoring** - CPU and memory usage tracking with historical data
- 🎨 **Modern UI** - React-based interface with real-time updates

## Quick Start

### Using npx (Recommended)
```bash
npx gowa-manager
```
Automatically downloads the correct binary for your platform and runs it.

### Using Install Script
```bash
curl -fsSL https://raw.githubusercontent.com/fadlee/gowa-manager/main/install.sh | bash
```
Installs to `~/.local/bin/gowa-manager` and adds to PATH.

### Access the Application
Open http://localhost:3000 in your browser (default credentials: `admin` / `password`)

<details>
<summary>Manual Binary Download</summary>

Download from [Releases](https://github.com/fadlee/gowa-manager/releases):

| Platform | Binary |
|----------|--------|
| Linux x64 | `gowa-manager-linux-x64` |
| Linux ARM64 | `gowa-manager-linux-arm64` |
| macOS Intel | `gowa-manager-macos-x64` |
| macOS Apple Silicon | `gowa-manager-macos-arm64` |
| Windows x64 | `gowa-manager-windows-x64.exe` |

```bash
chmod +x gowa-manager-*
./gowa-manager-linux-x64
```
</details>

<details>
<summary>Development Setup</summary>

```bash
# Clone the repository
git clone https://github.com/fadlee/gowa-manager.git
cd gowa-manager

# Install dependencies
bun install
cd client && bun install

# Run in development mode
bun run dev
```
</details>

## Version Management

GOWA Manager supports multiple GOWA versions, allowing you to:

### 🔄 **Automatic Version Management**
- **Auto-download**: Latest GOWA binary is automatically downloaded on first run
- **Version Storage**: Versions stored in `data/bin/versions/{version}/gowa`
- **Smart Caching**: Avoid re-downloading existing versions

### 📦 **Per-Instance Version Control**
- **Version Selection**: Choose specific GOWA version when creating instances
- **Version Editing**: Change version of existing instances (requires restart)
- **Mixed Versions**: Different instances can run different GOWA versions simultaneously

### 🛠 **Version Management API**
```bash
# List installed versions
GET /api/system/versions/installed

# List available versions from GitHub
GET /api/system/versions/available

# Install specific version
POST /api/system/versions/install
{"version": "v7.5.1"}

# Remove version
DELETE /api/system/versions/v7.5.1
```

### 📁 **Directory Structure**
```
data/
├── bin/
│   ├── versions/
│   │   ├── v7.5.1/gowa     # Specific version
│   │   ├── v7.5.0/gowa     # Another version
│   │   └── latest/         # Symlink to latest
│   └── gowa -> versions/latest/gowa  # Compatibility symlink
├── instances/              # Instance-specific data
└── gowa.db                # SQLite database
```

## Authentication

### Enable Authentication
1. **Environment Variables:**
   ```bash
   ADMIN_USERNAME=your_username
   ADMIN_PASSWORD=your_password
   ```

2. **Or create `.env` file:**
   ```env
   ADMIN_USERNAME=admin
   ADMIN_PASSWORD=securepassword
   ```

3. **Default Credentials (if not set):**
   - Username: `admin`
   - Password: `password`

### CLI Authentication
```bash
# Set credentials via CLI
bun run src/index.ts --admin-username admin --admin-password mypassword
```

## Instance Management

### Creating Instances
1. **Via Web UI**: Click "Create New Instance" button
2. **Select Version**: Choose from installed versions or install new ones
3. **Configure Settings**: Optional advanced configuration (collapsed by default)
4. **Auto-allocation**: Ports and directories automatically managed

### Instance Features
- ✅ **Lifecycle Management** - Start, stop, restart instances
- 📊 **Real-time Status** - Live monitoring with resource usage
- 🔧 **Configuration Editor** - Modify settings anytime
- 📦 **Version Switching** - Change GOWA version per instance
- 🌐 **Web Access** - Direct proxy links to instance UIs
- 🗑️ **Safe Deletion** - Cleanup processes and data

## Development Commands

### Integrated Development (Recommended)
```bash
# Single-port development with auto-rebuild
bun run dev
```

### Separate Development
```bash
# Client on :5173, server on :3000
bun run dev:all

# Server only (with auto-restart)
bun run dev:server

# Client only (Vite dev server)
bun run dev:client
```

### Production Build
```bash
# Build client and embed static files
bun run build:production

# Compile to standalone binary
bun run compile
```

### Database Management
```bash
# Reset database (clears all data)
bun run reset-db

# Run comprehensive test suite
bun run test
```

## Configuration

### Environment Variables
```env
# Authentication (optional)
ADMIN_USERNAME=admin
ADMIN_PASSWORD=password

# Data directory (optional)
DATA_DIR=./data
```

### CLI Options
```bash
# Custom data directory
bun run src/index.ts --data-dir /path/to/data

# Custom port
bun run src/index.ts --port 8080

# Authentication
bun run src/index.ts --admin-username admin --admin-password pass
```

## API Reference

### Instance Management
```bash
# List all instances
GET /api/instances

# Create new instance
POST /api/instances
{
  "name": "my-instance",
  "gowa_version": "latest",
  "config": "{...}"
}

# Update instance
PUT /api/instances/{id}
{
  "name": "updated-name",
  "gowa_version": "v7.5.1",
  "config": "{...}"
}

# Instance actions
POST /api/instances/{id}/start
POST /api/instances/{id}/stop
POST /api/instances/{id}/restart

# Get instance status
GET /api/instances/{id}/status
```

### Version Management
```bash
# List installed versions
GET /api/system/versions/installed

# List available versions
GET /api/system/versions/available?limit=10

# Install version
POST /api/system/versions/install
{"version": "v7.5.1"}

# Remove version
DELETE /api/system/versions/{version}

# Check version availability
GET /api/system/versions/{version}/available

# Get disk usage
GET /api/system/versions/usage

# Cleanup old versions
POST /api/system/versions/cleanup
{"keepCount": 3}
```

### System Information
```bash
# System status
GET /api/system/status

# Configuration
GET /api/system/config

# Port management
GET /api/system/ports/next
GET /api/system/ports/{port}/available
```

## Architecture

### Modular Backend Structure
```
src/
├── modules/
│   ├── instances/        # Instance lifecycle management
│   │   ├── service.ts   # Business logic
│   │   ├── model.ts     # API schemas
│   │   └── utils/       # Process, directory, config management
│   ├── system/          # System status, ports, versions
│   │   ├── service.ts   # System operations
│   │   ├── versions.ts  # Version management API
│   │   └── version-manager.ts  # Version business logic
│   ├── proxy/           # Request proxying with WebSocket support
│   └── auth/            # Authentication middleware
├── middlewares/         # Reusable middleware
├── types/              # TypeScript definitions
├── db.ts               # SQLite database with prepared statements
└── binary-download.ts  # Auto-download service
```

### Frontend Architecture
```
client/src/
├── components/
│   ├── VersionSelector.tsx    # Version management UI
│   ├── CreateInstanceDialog.tsx
│   ├── EditInstanceDialog.tsx
│   ├── InstanceCard.tsx
│   └── ui/                    # UI components
├── lib/
│   ├── api.ts                # API client
│   └── auth.tsx              # Authentication context
└── types/                    # TypeScript definitions
```

### Data Flow
1. **SQLite Database** stores instance metadata and configuration
2. **Elysia Modules** handle API operations and process management
3. **React Frontend** communicates via REST API with real-time updates
4. **Version Manager** handles multiple GOWA binary versions
5. **Proxy Module** forwards requests to running instances
6. **Auto-restart Service** maintains instance state across server restarts

## Troubleshooting

### Common Issues

**🔧 Instance Won't Start**
```bash
# Check if version is installed
GET /api/system/versions/{version}/available

# Install missing version
POST /api/system/versions/install {"version": "v7.5.1"}

# Check port availability
GET /api/system/ports/{port}/available
```

**📦 Version Installation Fails**
- Check internet connection
- Verify GitHub access (not behind firewall)
- Ensure sufficient disk space
- Check file permissions in data directory

**🔄 Version Change Not Applied**
- Version changes require instance restart
- Stop and start the instance after changing version
- Check instance logs for errors

**🗃️ Database Issues**
```bash
# Reset database (⚠️ deletes all data)
bun run reset-db

# Or manually delete
rm data/gowa.db
```

**🌐 Proxy/Web UI Issues**
- Ensure instance is running and healthy
- Check proxy path: `/app/{instanceKey}/`
- Verify instance port allocation

### Logs and Debugging
```bash
# Enable debug mode
DEBUG=1 bun run dev

# Check server logs
bun run dev:server

# Test API endpoints
curl http://localhost:3000/api/health
curl http://localhost:3000/api/instances
```

### File Permissions
```bash
# Fix binary permissions
chmod +x data/bin/versions/*/gowa

# Fix data directory permissions
chmod -R 755 data/
```

## Contributing

1. **Fork the repository**
2. **Create feature branch**: `git checkout -b feature/amazing-feature`
3. **Follow existing code patterns** and use TypeScript
4. **Test your changes**: `bun run test`
5. **Update documentation** if needed
6. **Submit pull request**

### Development Guidelines
- Use **prepared statements** for database queries
- Follow **modular architecture** patterns
- Add **comprehensive error handling**
- Include **TypeScript types** for all APIs
- Test both **frontend and backend** changes

## Credits

- [go-whatsapp-web-multidevice](https://github.com/aldinokemal/go-whatsapp-web-multidevice) by **Aldino Kemal** - The core WhatsApp Web multidevice binary that powers this manager

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

- **Issues**: Report bugs and feature requests via GitHub Issues
- **Documentation**: See `/docs` directory for detailed guides
- **API**: Full OpenAPI documentation available at `/api/docs` (when running)

---

**Built with**: [Bun](https://bun.sh) • [Elysia](https://elysiajs.com) • [React](https://react.dev) • [TypeScript](https://typescriptlang.org) • [TailwindCSS](https://tailwindcss.com)
```
