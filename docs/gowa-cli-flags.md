# Gowa CLI Flags Documentation

This application is from clone https://github.com/aldinokemal/go-whatsapp-web-multidevice

## Usage
```bash
gowa rest [flags]
```

## Available Flags

### Required Flags (Configurable via UI)

#### Account Validation
- **Flag**: `--account-validation <true/false>`
- **Default**: `true`
- **Description**: Enable or disable account validation
- **Example**: `--account-validation=true`
- **UI**: Toggle switch

#### Basic Authentication
- **Flag**: `-b, --basic-auth <username:password>`
- **Multiple**: Yes (comma-separated pairs)
- **Description**: Basic auth credentials for API access
- **Example**: `--basic-auth=user1:pass1 --basic-auth=user2:pass2`
- **UI**: Dynamic list with username/password inputs

#### OS Name
- **Flag**: `--os <string>`
- **Default**: `"GowaManager"`
- **Description**: Custom OS name identifier
- **Example**: `--os="Chrome"`
- **UI**: Text input field

#### Webhooks
- **Flag**: `-w, --webhook <url>`
- **Multiple**: Yes
- **Description**: Forward events to webhook URLs
- **Example**: `--webhook="https://api.example.com/webhook"`
- **UI**: Dynamic list with URL inputs

### Additional Flags

#### Auto Mark Read
- **Flag**: `--auto-mark-read <true/false>`
- **Description**: Automatically mark incoming messages as read
- **Example**: `--auto-mark-read=true`
- **UI**: Toggle switch

#### Auto Reply
- **Flag**: `--autoreply <string>`
- **Description**: Automatic reply message for incoming messages
- **Example**: `--autoreply="Don't reply this message"`
- **UI**: Text input field

#### Base Path
- **Flag**: `--base-path <string>`
- **Description**: Base path for subpath deployment
- **Example**: `--base-path="/gowa"`
- **UI**: Text input field

#### Debug Mode
- **Flag**: `-d, --debug <true/false>`
- **Description**: Enable debug logging
- **Example**: `--debug=true`
- **UI**: Toggle switch

#### Webhook Secret
- **Flag**: `--webhook-secret <string>`
- **Default**: `"secret"`
- **Description**: Secret key to secure webhook requests
- **Example**: `--webhook-secret="super-secret-key"`
- **UI**: Password input field

### System Flags (Not configurable via UI)

#### Port
- **Flag**: `-p, --port <number>`
- **Default**: `3000`
- **Description**: Port number (automatically managed by system)
- **Example**: `--port=8080`

#### Database URI
- **Flag**: `--db-uri <string>`
- **Default**: `"file:storages/whatsapp.db?_foreign_keys=on"`
- **Description**: Database connection string

#### Database Keys URI
- **Flag**: `--db-keys-uri <string>`
- **Description**: Separate database for keys storage

## UI Implementation

The GowaManager provides an intuitive web interface for configuring these flags:

### Flag Configuration Mode
- **Toggle Switch**: For boolean flags (account-validation, auto-mark-read, debug)
- **Text Inputs**: For string values (os, auto-reply, base-path, webhook-secret)
- **Dynamic Lists**: For multiple values (basic-auth pairs, webhooks)
- **Password Fields**: For sensitive data (passwords, secrets)

### JSON Configuration Mode
- Advanced users can directly edit the JSON configuration
- Supports all flags plus custom environment variables
- Real-time validation

## Example Configurations

### Basic Setup
```json
{
  "args": ["rest", "--port=PORT"],
  "flags": {
    "accountValidation": true,
    "os": "GowaManager",
    "debug": false
  }
}
```

### Production Setup with Webhooks
```json
{
  "args": ["rest", "--port=PORT"],
  "flags": {
    "accountValidation": true,
    "os": "ProductionServer",
    "basicAuth": [
      {"username": "admin", "password": "secure123"},
      {"username": "api", "password": "api-key-456"}
    ],
    "webhooks": [
      "https://api.company.com/whatsapp-webhook",
      "https://backup.company.com/webhook"
    ],
    "webhookSecret": "super-secret-production-key",
    "autoMarkRead": true,
    "basePath": "/whatsapp"
  }
}
```

### Development Setup
```json
{
  "args": ["rest", "--port=PORT"],
  "flags": {
    "accountValidation": false,
    "os": "DevEnvironment",
    "debug": true,
    "autoReply": "This is a development bot - messages may not be processed",
    "webhooks": ["http://localhost:3001/webhook"]
  }
}
```

## CLI Command Generation

Based on the configuration above, the system automatically generates the appropriate CLI commands:

### Basic Setup Command
```bash
gowa rest --port=3000 --account-validation=true --os=GowaManager
```

### Production Setup Command
```bash
gowa rest --port=3000 \
  --account-validation=true \
  --os=ProductionServer \
  --basic-auth=admin:secure123 \
  --basic-auth=api:api-key-456 \
  --webhook=https://api.company.com/whatsapp-webhook \
  --webhook=https://backup.company.com/webhook \
  --webhook-secret=super-secret-production-key \
  --auto-mark-read=true \
  --base-path=/whatsapp
```

### Development Setup Command
```bash
gowa rest --port=3000 \
  --account-validation=false \
  --os=DevEnvironment \
  --debug=true \
  --autoreply="This is a development bot - messages may not be processed" \
  --webhook=http://localhost:3001/webhook
```
