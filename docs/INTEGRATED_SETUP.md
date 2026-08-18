# Integrated Development Setup

This document describes the integrated development workflow where the Go server serves both the API and the client application.

## Overview

- **Server**: `http://localhost:3000` (API + embedded static files)
- **Client**: Built by Vite into `public/`, then copied to `internal/static/web/` for Go embedding

## How It Works

1. **Static File Serving**: Go serves embedded client assets via `go:embed` in `internal/static/`
2. **Build**: Vite builds client to `public/`, then assets are copied to `internal/static/web/`
3. **API Routes**: All API endpoints remain under `/api/` prefix
4. **Client App**: Served from the root path `/`

## Project Structure

```
├── client/                 # React client source
│   ├── src/               # React components, etc.
│   └── vite.config.ts     # Builds to ../public
├── public/                # Client build output (gitignored)
│   ├── index.html
│   └── assets/
├── internal/static/web/   # Go embed directory (gitignored except .gitkeep)
└── cmd/gowa-manager-go/   # Go server entrypoint
```

## Development Commands

### Frontend Development (Vite dev server)
```bash
bun run dev
```
Starts Vite dev server on `http://localhost:5173` with hot module reload.

### Go Backend Development
```bash
bun run dev:go
# OR: go run ./cmd/gowa-manager-go
```

### Build for Production
```bash
bun run build:production
```
This builds the client and compiles the Go binary with embedded static assets.

### Manual asset embedding
```bash
bun run build:client && cp -r public/* internal/static/web/
go build -o gowa-manager-go ./cmd/gowa-manager-go
```

Or use the build script:
```bash
bun run scripts/build-go.ts
```

## Access Points

- **Main App**: `http://localhost:3000/` (React UI)
- **API Health**: `http://localhost:3000/api/health`
- **API Ready**: `http://localhost:3000/api/ready`
- **Instances API**: `http://localhost:3000/api/instances`
- **Proxy**: `http://localhost:3000/app/{key}/`

## Benefits

1. **Single Port**: Everything runs on `localhost:3000`
2. **No CORS Issues**: Client and API on same origin
3. **Production-like**: Similar to production deployment
4. **Simple Deployment**: One binary serves everything
