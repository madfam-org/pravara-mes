# Luban Bridge Service

Bridge service for integrating Snapmaker machines and Luban software with PravaraMES.

## Features

### Machine Communication
- **Serial Connection**: Direct USB/Serial communication with Snapmaker machines
- **WiFi Connection**: Network-based control via Snapmaker's HTTP API
- **OctoPrint Integration**: Support for OctoPrint-managed Snapmaker printers
- **Auto-Discovery**: Automatic detection of Snapmaker machines on network and serial ports

### Luban Integration
- **CLI Interface**: Headless control of Luban for slicing and toolpath generation
- **Project Import**: Load and process Luban project files (.lbn)
- **STL Slicing**: Convert 3D models to Snapmaker-optimized G-code
- **Multi-Tool Support**: Handle printing, laser, and CNC operations

### G-Code Processing
- **Snapmaker-Specific Parsing**: Understand Snapmaker's extended G-code commands
- **Multi-Tool Analysis**: Detect and handle tool changes (3D printing, laser, CNC)
- **Enclosure Control**: Support for Snapmaker enclosure and air purifier commands
- **Validation**: Ensure G-code compatibility with Snapmaker hardware limits

### Real-Time Monitoring
- **WebSocket Updates**: Live machine status and telemetry streaming
- **Temperature Monitoring**: Track bed, nozzle, and chamber temperatures
- **Print Progress**: Real-time progress tracking with time estimates
- **Error Detection**: Immediate notification of machine errors or issues

## API Endpoints

### Project Management
- `POST /api/project/import` - Import Luban project file
- `POST /api/project/slice` - Slice STL to G-code
- `POST /api/project/toolpath` - Generate toolpath for laser/CNC
- `POST /api/project/export` - Export G-code with options

### Machine Control
- `POST /api/machine/discover` - Discover available machines
- `GET /api/machine/list` - List discovered machines
- `POST /api/machine/:id/connect` - Connect to specific machine
- `POST /api/machine/:id/disconnect` - Disconnect from machine
- `GET /api/machine/:id/status` - Get machine status
- `POST /api/machine/:id/command` - Send G-code command
- `POST /api/machine/:id/execute` - Execute G-code file

### Print Operations
- `POST /api/machine/:id/print/start` - Start print job
- `POST /api/machine/:id/print/pause` - Pause current print
- `POST /api/machine/:id/print/resume` - Resume paused print
- `POST /api/machine/:id/print/cancel` - Cancel print job

### Machine Control
- `POST /api/machine/:id/temperature` - Set bed/nozzle temperature
- `POST /api/machine/:id/home` - Home axes
- `POST /api/machine/:id/upload` - Upload file to machine

### G-Code Analysis
- `POST /api/gcode/analyze` - Analyze G-code file
- `POST /api/gcode/validate` - Validate for Snapmaker compatibility
- `POST /api/gcode/optimize` - Optimize G-code for efficiency
- `POST /api/gcode/simulate` - Generate visualization data

## WebSocket Events

Connect to `/ws?machineId=<id>` for real-time updates:

### Incoming Messages
```json
{
  "type": "status|gcode|telemetry",
  "data": {...},
  "command": "G-code command (for gcode type)"
}
```

### Outgoing Events
```json
{
  "type": "response|error|machine-update",
  "data": {...},
  "message": "Error message (for errors)"
}
```

## Configuration

### Environment Variables
```bash
# Server Configuration
PORT=4507                    # HTTP server port
LOG_LEVEL=info              # Logging level (debug|info|warn|error)

# PravaraMES Integration
PRAVARA_API_URL=http://localhost:4200
PRAVARA_API_KEY=your-api-key

# Telemetry Service
TELEMETRY_URL=http://localhost:4204

# Luban Configuration (optional)
LUBAN_PATH=/Applications/Luban.app/Contents/MacOS/Luban
```

### Machine Connection Types

#### Serial Connection
```json
{
  "type": "serial",
  "serialPath": "/dev/ttyUSB0",
  "baudRate": 115200
}
```

#### WiFi Connection
```json
{
  "type": "wifi",
  "ipAddress": "192.168.1.100",
  "apiKey": "optional-api-key"
}
```

#### OctoPrint Connection
```json
{
  "type": "octoprint",
  "ipAddress": "octopi.local",
  "apiKey": "octoprint-api-key"
}
```

## Development

### Prerequisites
- Node.js 18+
- TypeScript
- Luban software (for full functionality)
- Serial port access (for USB connections)

### Setup
```bash
# Install dependencies
npm install

# Run in development mode
npm run dev

# Build for production
npm run build

# Start production server
npm start
```

### Docker Deployment
```bash
# Build image
docker build -t pravara-luban-bridge .

# Run with docker-compose
docker-compose up -d

# For serial port access (Linux)
docker run --privileged -v /dev:/dev pravara-luban-bridge
```

### Testing
```bash
# Run tests
npm test

# Test machine discovery
curl -X POST http://localhost:4507/api/machine/discover

# Test G-code analysis
curl -X POST http://localhost:4507/api/gcode/analyze \
  -H "Content-Type: application/json" \
  -d '{"gcode": "G28\nG1 X100 Y100 Z10 F3000"}'
```

## Supported Snapmaker Features

### Machine Models
- Snapmaker Original
- Snapmaker 2.0 (A150, A250, A350)
- Snapmaker Artisan
- Snapmaker J1

### Tool Heads
- 3D Printing Module (Single/Dual Extrusion)
- Laser Module (1.6W, 10W, 20W, 40W)
- CNC Module

### Accessories
- Enclosure with door detection
- Air Purifier with filter monitoring
- Rotary Module for 4-axis CNC/Laser
- Linear Module extensions
- Emergency Stop button

### Special G-Code Commands
- `M1005` - Get machine info and model
- `M1010` - Control enclosure door and lighting
- `M1011` - Control air purifier and fan speed
- `M1012` - Control rotary module
- `M605` - Tool head switching (3D/Laser/CNC)
- `M2000` - Snapmaker-specific configuration

## Architecture

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────┐
│  PravaraMES UI  │────▶│ Luban Bridge │◀────│   Luban     │
└─────────────────┘     └──────────────┘     └─────────────┘
                               │
                    ┌──────────┴──────────┐
                    │                     │
              ┌─────▼──────┐       ┌──────▼──────┐
              │   Serial   │       │    WiFi     │
              │  (USB/COM) │       │   (HTTP)    │
              └─────┬──────┘       └──────┬──────┘
                    │                     │
              ┌─────▼──────┐       ┌──────▼──────┐
              │ Snapmaker  │       │ Snapmaker   │
              │  (Direct)  │       │ (Network)   │
              └────────────┘       └─────────────┘
```

## Security Considerations

1. **API Key Protection**: Use environment variables for API keys
2. **Serial Port Access**: Requires appropriate permissions (dialout group on Linux)
3. **Network Security**: Use firewall rules to restrict machine access
4. **File Upload Limits**: Configured to prevent DoS via large files
5. **Command Validation**: All G-code commands are validated before execution

## Troubleshooting

### Serial Connection Issues
- Check USB cable and port permissions
- Verify baud rate (usually 115200 for Snapmaker)
- Add user to dialout group: `sudo usermod -a -G dialout $USER`

### Network Discovery Failed
- Ensure machines and server are on same network
- Check firewall rules for UDP port 30000-30001
- Try manual IP connection if auto-discovery fails

### Luban Integration Problems
- Verify Luban is installed and path is correct
- Check Luban supports headless mode (v4.0+)
- Ensure sufficient disk space for temporary files

### Docker Serial Access
- Use `--privileged` flag for container
- Map device files: `-v /dev:/dev`
- May need to restart container after connecting USB

## License

MIT