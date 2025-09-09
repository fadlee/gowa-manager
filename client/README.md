# GOWA Manager Frontend

A modern React frontend for managing multiple GOWA (Go WhatsApp Web Multidevice) instances with comprehensive version control and real-time monitoring.

## Features

- **Instance Management**: View all instances in a card-based layout
- **Version Control**: Select and install different GOWA versions per instance
- **Real-time Status**: Live updates of instance status, PID, uptime, and resource usage
- **Instance Actions**:
  - Start stopped instances
  - Stop running instances  
  - Restart running instances
  - Open proxy links to running applications
  - Edit instance name, version, and configuration
  - Delete instances with cleanup
- **Version Management**:
  - Install GOWA versions on-demand
  - Switch versions per instance
  - View installation status and size information
  - Automatic refresh after version installation
- **Create New Instances**: Minimal dialog with version selection and optional configuration
- **Collapsible Configuration**: Advanced settings hidden by default for clean UX
- **Responsive Design**: Works on desktop and mobile devices
- **Real-time Monitoring**: CPU and memory usage tracking with historical data

## Tech Stack

- **Frontend**: React 18 with TypeScript
- **Build Tool**: Vite
- **Styling**: TailwindCSS
- **UI Components**: Radix UI primitives
- **Data Fetching**: TanStack Query (React Query)
- **Icons**: Lucide React

## Development

### Prerequisites

- Bun (recommended) or Node.js
- Backend server running on `http://localhost:3000`

### Getting Started

1. Install dependencies:
   ```bash
   bun install
   ```

2. Start development server:
   ```bash
   bun run dev
   ```

3. Open your browser to `http://localhost:5173`

### Build for Production

```bash
bun run build
```

The built files will be in the `dist` directory.

## API Integration

The frontend communicates with the backend via REST API:

### Instance Management
- `GET /api/instances` - Get all instances
- `POST /api/instances` - Create new instance with version
- `PUT /api/instances/:id` - Update instance (including version)
- `DELETE /api/instances/:id` - Delete instance
- `POST /api/instances/:id/start` - Start instance
- `POST /api/instances/:id/stop` - Stop instance
- `POST /api/instances/:id/restart` - Restart instance
- `GET /api/instances/:id/status` - Get instance status

### Version Management
- `GET /api/system/versions/installed` - List installed versions
- `GET /api/system/versions/available` - List available versions
- `POST /api/system/versions/install` - Install specific version
- `DELETE /api/system/versions/:version` - Remove version
- `GET /api/system/versions/:version/available` - Check version availability

## Component Structure

```
src/
├── components/
│   ├── ui/                      # Reusable UI components (Button, Input, Dialog, etc.)
│   ├── InstanceList.tsx         # Main instance list view
│   ├── InstanceCard.tsx         # Individual instance card with version display
│   ├── CreateInstanceDialog.tsx # Instance creation with version selection
│   ├── EditInstanceDialog.tsx   # Instance editing with version switching
│   ├── VersionSelector.tsx      # Version selection and installation UI
│   ├── CliFlags/               # Configuration flag components
│   └── LoginPage.tsx           # Authentication interface
├── lib/
│   ├── api.ts                  # API client with version management
│   ├── auth.tsx                # Authentication context
│   └── utils.ts                # Utility functions
├── types/
│   └── index.ts                # TypeScript type definitions (including version types)
├── App.tsx                     # Main app component
└── main.tsx                   # App entry point
```

## Instance Card Features

Each instance is displayed as a card showing:

- **Instance name and ID**
- **Current status** (running, stopped, starting, etc.) with status indicator
- **GOWA version** (with "Latest" badge if applicable)
- **Port number** (when allocated)
- **Process ID** (when running)  
- **Uptime** (when running) with formatted display
- **Resource usage** (CPU and memory with progress bars)
- **Configuration** (collapsible JSON view)

### Actions Available:

- **Start**: Start a stopped instance
- **Stop**: Stop a running instance
- **Restart**: Restart a running instance
- **Open**: Open the proxy link to the running application
- **Edit**: Modify instance name and configuration

## Configuration

Instances can be configured with JSON parameters:

```json
{
  "port": 8080,
  "args": ["--debug", "--verbose"],
  "environment": "development"
}
```

The configuration is stored as a JSON string and can be edited through the UI.