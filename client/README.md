# Gowa Manager Frontend

A modern React frontend for managing Gowa application instances.

## Features

- **Instance Management**: View all instances in a card-based layout
- **Real-time Status**: Live updates of instance status, PID, and uptime
- **Instance Actions**:
  - Start stopped instances
  - Stop running instances  
  - Restart running instances
  - Open proxy links to running applications
  - Edit instance name and configuration
  - Delete instances
- **Create New Instances**: Dialog for creating new instances with optional name and JSON configuration
- **Responsive Design**: Works on desktop and mobile devices

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

- `GET /api/instances` - Get all instances
- `POST /api/instances` - Create new instance
- `PUT /api/instances/:id` - Update instance
- `DELETE /api/instances/:id` - Delete instance
- `POST /api/instances/:id/start` - Start instance
- `POST /api/instances/:id/stop` - Stop instance
- `POST /api/instances/:id/restart` - Restart instance
- `GET /api/instances/:id/status` - Get instance status

## Component Structure

```
src/
├── components/
│   ├── ui/              # Reusable UI components
│   ├── InstanceList.tsx # Main instance list view
│   ├── InstanceCard.tsx # Individual instance card
│   ├── CreateInstanceDialog.tsx
│   └── EditInstanceDialog.tsx
├── lib/
│   ├── api.ts          # API client
│   └── utils.ts        # Utility functions
├── types/
│   └── index.ts        # TypeScript type definitions
├── App.tsx             # Main app component
└── main.tsx           # App entry point
```

## Instance Card Features

Each instance is displayed as a card showing:

- **Instance name and ID**
- **Current status** (running, stopped, starting, etc.)
- **Port number** (when running)
- **Process ID** (when running)  
- **Uptime** (when running)
- **Configuration** (JSON, if provided)

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