# Integrated Development Setup

This document describes the integrated development workflow where the Elysia server serves both the API and the client application.

## Overview

Instead of running the client and server on separate ports:
- **Server**: `http://localhost:3000` (API + Static Files)
- **Client**: Automatically built and served from the server root

## How It Works

1. **Static File Serving**: Elysia uses `@elysiajs/static` plugin to serve the client build files
2. **Auto-build**: Vite watches for client file changes and automatically rebuilds to `public/` directory
3. **API Routes**: All API endpoints remain under `/api/` prefix
4. **Client App**: Served from the root path `/`

## Project Structure

```
├── client/                 # React client source
│   ├── src/               # React components, etc.
│   └── vite.config.ts     # Builds to ../public
├── public/                # Client build output (served by Elysia)
│   ├── index.html
│   └── assets/
└── src/                   # Elysia server source
    └── index.ts           # Configured with static plugin
```

## Development Commands

### Start Integrated Development Mode
```bash
bun run dev:watch
```
This runs both:
- `vite build --watch` (auto-rebuilds client on changes)
- `bun --watch run src/index.ts` (auto-restarts server on changes)

### Alternative Commands
```bash
# Build client once, then start server
bun run dev:integrated

# Separate development (client on :5173, server on :3000)
bun run dev:all

# Just client development server
bun run dev:client

# Just server
bun run dev
```

## Access Points

- **Main App**: `http://localhost:3000/` (React UI)
- **API Health**: `http://localhost:3000/api/health`
- **Instances API**: `http://localhost:3000/api/instances`
- **Proxy**: `http://localhost:3000/proxy/app/{id}/`

## Configuration Details

### Elysia Static Plugin
```typescript
.use(staticPlugin({
  assets: 'public',     // Serves from ./public directory
  prefix: '/',          // Root path (not /public)
  indexHTML: true       // Serves index.html for SPA routing
}))
```

### Vite Build Configuration
```typescript
build: {
  outDir: '../public',    // Builds to server's public directory
  emptyOutDir: true,      // Cleans before build
}
```

## Benefits

1. **Single Port**: Everything runs on `localhost:3000`
2. **No CORS Issues**: Client and API on same origin
3. **Production-like**: Similar to production deployment
4. **Auto-rebuild**: Changes instantly reflected
5. **Simple Deployment**: One server serves everything

## File Watching

- **Client Changes**: Vite detects changes → rebuilds to `public/` → Elysia serves updated files
- **Server Changes**: Bun detects changes → restarts server → continues serving latest client build

This setup provides a seamless development experience with automatic rebuilds while maintaining the production deployment model.