# GOWA Manager

**A full-stack web application for managing multiple GOWA (Go WhatsApp Web Multidevice) instances with version control, real-time monitoring, and advanced configuration options.**

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

### Installation
```bash
# Install dependencies for both server and client
bun install
cd client && bun install
```

### Development Mode (Recommended)
```bash
# Integrated development - client builds to public/, server serves everything on :3000
bun run dev
```

### Access the Application
Open http://localhost:3000 in your browser

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
