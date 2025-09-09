# GOWA Version Management

GOWA Manager supports managing multiple GOWA (Go WhatsApp Web Multidevice) versions simultaneously, allowing different instances to run different versions as needed.

## Overview

The version management system provides:
- **Multiple Version Support**: Install and maintain multiple GOWA versions
- **Per-Instance Version Control**: Each instance can use a different GOWA version
- **Automatic Downloads**: On-demand installation from GitHub releases
- **Version Migration**: Safe switching between versions
- **Storage Management**: Efficient disk usage and cleanup

## Directory Structure

```
data/
├── bin/
│   ├── versions/
│   │   ├── v7.5.1/
│   │   │   └── gowa          # Specific version binary
│   │   ├── v7.5.0/
│   │   │   └── gowa          # Another version
│   │   └── latest/           # Symlink to latest version
│   └── gowa -> versions/latest/gowa  # Backward compatibility
├── instances/                 # Instance-specific data directories
└── gowa.db                   # SQLite database
```

## Version Storage

### Version Directory Structure
- **Organized Storage**: `data/bin/versions/{version}/gowa`
- **Isolation**: Each version stored in separate directory
- **Permissions**: Binaries automatically made executable
- **Platform Support**: Handles different OS/architecture combinations

### Version Resolution
- **Explicit Versions**: `v7.5.1`, `v7.5.0`, etc.
- **Latest Alias**: `latest` resolves to most recent installed version
- **Backward Compatibility**: Legacy `data/bin/gowa` path maintained

## API Reference

### Installation Management

#### List Installed Versions
```http
GET /api/system/versions/installed
```

**Response:**
```json
[
  {
    "version": "v7.5.1",
    "path": "/data/bin/versions/v7.5.1/gowa",
    "installed": true,
    "isLatest": true,
    "size": 26541394,
    "installedAt": "2024-01-15T10:30:00Z"
  }
]
```

#### List Available Versions
```http
GET /api/system/versions/available?limit=10
```

**Response:**
```json
[
  {
    "version": "latest",
    "path": "/data/bin/versions/latest/gowa",
    "installed": true,
    "isLatest": true
  },
  {
    "version": "v7.5.1",
    "path": "/data/bin/versions/v7.5.1/gowa",
    "installed": true,
    "isLatest": true
  },
  {
    "version": "v7.5.0",
    "path": "/data/bin/versions/v7.5.0/gowa",
    "installed": false,
    "isLatest": false
  }
]
```

#### Install Specific Version
```http
POST /api/system/versions/install
Content-Type: application/json

{
  "version": "v7.5.1"
}
```

**Response:**
```json
{
  "success": true,
  "message": "Successfully installed GOWA version v7.5.1"
}
```

#### Remove Version
```http
DELETE /api/system/versions/v7.5.0
```

**Response:**
```json
{
  "success": true,
  "message": "Successfully removed GOWA version v7.5.0"
}
```

#### Check Version Availability
```http
GET /api/system/versions/v7.5.1/available
```

**Response:**
```json
{
  "version": "v7.5.1",
  "available": true,
  "path": "/data/bin/versions/v7.5.1/gowa"
}
```

### Storage Management

#### Get Disk Usage
```http
GET /api/system/versions/usage
```

**Response:**
```json
{
  "v7.5.1": 26541394,
  "v7.5.0": 25832156,
  "v7.4.1": 25123098
}
```

#### Cleanup Old Versions
```http
POST /api/system/versions/cleanup
Content-Type: application/json

{
  "keepCount": 3
}
```

**Response:**
```json
{
  "success": true,
  "message": "Cleaned up 2 old versions: v7.3.0, v7.2.1",
  "removed": ["v7.3.0", "v7.2.1"]
}
```

## Instance Integration

### Creating Instances with Specific Versions

When creating instances, you can specify the GOWA version:

```http
POST /api/instances
Content-Type: application/json

{
  "name": "my-instance",
  "gowa_version": "v7.5.1",
  "config": "{\"args\":[\"rest\",\"--port=PORT\"],\"flags\":{\"accountValidation\":true}}"
}
```

### Updating Instance Versions

Change the version of existing instances:

```http
PUT /api/instances/1
Content-Type: application/json

{
  "name": "my-instance",
  "gowa_version": "v7.5.0"
}
```

**⚠️ Note**: Version changes require instance restart to take effect.

### Version Validation

Before starting instances, the system validates that the required version is installed:

- **Available**: Instance starts normally
- **Missing**: Error with installation suggestion
- **Invalid**: Clear error message

## Frontend Integration

### Version Selector Component

The `VersionSelector` component provides:
- **Dropdown Selection**: Choose from available versions
- **Installation UI**: Install missing versions on-demand
- **Status Indicators**: Visual feedback for installation status
- **Real-time Updates**: Immediate refresh after installation

### User Experience

#### Creating Instances
1. **Version Selection**: Dropdown with installed/available versions
2. **Smart Installation**: Click "Install" for missing versions
3. **Immediate Availability**: No dialog refresh needed after installation
4. **Clear Status**: Visual indicators for installation state

#### Editing Instances
1. **Current Version**: Shows instance's current version
2. **Version Change Warning**: Alerts about restart requirement
3. **Installation Support**: Can install new versions during edit
4. **Save & Restart**: Version changes applied on restart

## Installation Process

### Automatic Installation

1. **GitHub API Query**: Fetch latest release information
2. **Asset Resolution**: Find correct binary for current platform
3. **Download**: Retrieve zip archive from GitHub releases
4. **Extraction**: Extract binary to versioned directory
5. **Permissions**: Make binary executable (Unix systems)
6. **Verification**: Confirm successful installation

### Platform Support

- **macOS**: `darwin_amd64`, `darwin_arm64`
- **Linux**: `linux_amd64`, `linux_arm64`
- **Windows**: `windows_amd64` (`.exe` extension)

### Error Handling

- **Network Issues**: Graceful fallback with error messages
- **Disk Space**: Check available space before download
- **Permissions**: Handle file system permission issues
- **Invalid Versions**: Clear error for non-existent versions

## Version Migration

### Safe Migration Process

1. **Pre-check**: Validate target version is available
2. **Configuration Backup**: Preserve instance configuration
3. **Graceful Stop**: Stop instance before version change
4. **Version Switch**: Update database with new version
5. **Restart**: Start instance with new version
6. **Verification**: Confirm instance is running correctly

### Migration Scenarios

#### Upgrading Versions
```bash
# Instance running v7.5.0, upgrade to v7.5.1
1. Install v7.5.1 (if not already installed)
2. Stop instance
3. Change version to v7.5.1
4. Start instance
5. Verify functionality
```

#### Downgrading Versions
```bash
# Instance running v7.5.1, downgrade to v7.5.0
1. Ensure v7.5.0 is installed
2. Stop instance
3. Change version to v7.5.0
4. Start instance
5. Check for compatibility issues
```

## Storage Management

### Disk Usage Monitoring

- **Version Sizes**: Track disk usage per version
- **Total Usage**: Monitor cumulative storage usage
- **Cleanup Recommendations**: Suggest removal of unused versions

### Cleanup Strategies

#### Automatic Cleanup
```javascript
// Keep only latest 3 versions
POST /api/system/versions/cleanup
{
  "keepCount": 3
}
```

#### Manual Cleanup
```javascript
// Remove specific version
DELETE /api/system/versions/v7.4.1
```

### Retention Policies

1. **Latest Version**: Never automatically removed
2. **Active Versions**: Versions used by running instances protected
3. **Historical Versions**: Can be removed safely
4. **Custom Retention**: Configurable keep count

## Troubleshooting

### Common Issues

#### Version Installation Fails
```bash
# Check network connectivity
curl -I https://api.github.com/repos/aldinokemal/go-whatsapp-web-multidevice/releases/latest

# Check disk space
df -h data/

# Check permissions
ls -la data/bin/versions/
```

#### Instance Won't Start After Version Change
```bash
# Verify version is installed
GET /api/system/versions/{version}/available

# Check binary permissions
ls -la data/bin/versions/{version}/gowa

# Fix permissions if needed
chmod +x data/bin/versions/{version}/gowa
```

#### Version Selection Not Updating
- Clear browser cache
- Refresh version data in UI
- Check API connectivity
- Restart development server if needed

### Debug Commands

```bash
# List all versions
ls -la data/bin/versions/

# Check binary executability
file data/bin/versions/*/gowa

# Test version availability API
curl http://localhost:3000/api/system/versions/installed

# Verify database schema
echo ".schema instances" | sqlite3 data/gowa.db
```

## Best Practices

### Version Management
1. **Test New Versions**: Test in development before production
2. **Keep Previous Version**: Maintain previous stable version for rollback
3. **Monitor Disk Usage**: Regular cleanup of unused versions
4. **Document Changes**: Track which instances use which versions

### Migration Planning
1. **Backup Data**: Backup instance data before major version changes
2. **Staged Rollout**: Update instances gradually, not all at once
3. **Rollback Plan**: Prepare rollback strategy for issues
4. **Monitoring**: Monitor instances after version changes

### Storage Optimization
1. **Regular Cleanup**: Remove unused versions periodically
2. **Selective Installation**: Only install versions you need
3. **Monitor Growth**: Track storage usage trends
4. **Automation**: Use cleanup API for maintenance scripts

## Security Considerations

### Binary Integrity
- Downloads from official GitHub releases only
- Binary verification through file size checks
- Automatic permission setting for executables

### Access Control
- Version management requires admin authentication
- Installation operations logged for audit
- Cleanup operations require explicit confirmation

### Network Security
- HTTPS-only downloads from GitHub
- Graceful handling of network failures
- No sensitive data in version metadata

---

This version management system provides robust, flexible control over GOWA versions while maintaining simplicity for end users.
