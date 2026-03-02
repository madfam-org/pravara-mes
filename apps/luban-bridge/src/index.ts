import express from 'express';
import cors from 'cors';
import { WebSocketServer } from 'ws';
import winston from 'winston';
import dotenv from 'dotenv';
import { LubanCLI } from './services/luban-cli';
import { SnapmakerProtocol } from './services/snapmaker-protocol';
import { GCodeAnalyzer } from './services/gcode-analyzer';
import { MachineDiscovery } from './services/machine-discovery';
import { projectRouter } from './routes/project';
import { machineRouter } from './routes/machine';
import { gcodeRouter } from './routes/gcode';

dotenv.config();

// Configure logger
const logger = winston.createLogger({
  level: process.env.LOG_LEVEL || 'info',
  format: winston.format.combine(
    winston.format.timestamp(),
    winston.format.json()
  ),
  transports: [
    new winston.transports.Console({
      format: winston.format.simple()
    }),
    new winston.transports.File({
      filename: 'logs/luban-bridge.log'
    })
  ]
});

// Initialize services
const lubanCLI = new LubanCLI(logger);
const snapmakerProtocol = new SnapmakerProtocol(logger);
const gcodeAnalyzer = new GCodeAnalyzer(logger);
const machineDiscovery = new MachineDiscovery(logger);

// Create Express app
const app = express();
const PORT = process.env.PORT || 4507;

// Middleware
app.use(cors());
app.use(express.json({ limit: '50mb' }));
app.use(express.urlencoded({ extended: true, limit: '50mb' }));

// Health check
app.get('/health', (req, res) => {
  res.json({
    status: 'healthy',
    service: 'luban-bridge',
    version: '1.0.0',
    timestamp: new Date().toISOString()
  });
});

// API Routes
app.use('/api/project', projectRouter(lubanCLI, logger));
app.use('/api/machine', machineRouter(snapmakerProtocol, machineDiscovery, logger));
app.use('/api/gcode', gcodeRouter(gcodeAnalyzer, logger));

// Start HTTP server
const server = app.listen(PORT, () => {
  logger.info(`Luban Bridge server running on port ${PORT}`);
});

// WebSocket server for real-time machine communication
const wss = new WebSocketServer({
  server,
  path: '/ws'
});

wss.on('connection', (ws, req) => {
  const machineId = new URL(req.url!, `http://${req.headers.host}`).searchParams.get('machineId');
  logger.info(`WebSocket connection established for machine: ${machineId}`);

  // Handle machine telemetry
  ws.on('message', async (data) => {
    try {
      const message = JSON.parse(data.toString());

      switch (message.type) {
        case 'status':
          // Forward status to main system
          await forwardMachineStatus(machineId!, message.data);
          break;

        case 'gcode':
          // Execute G-code command
          const response = await snapmakerProtocol.executeGCode(
            machineId!,
            message.command
          );
          ws.send(JSON.stringify({ type: 'response', data: response }));
          break;

        case 'telemetry':
          // Process telemetry data
          await processTelemetry(machineId!, message.data);
          break;

        default:
          logger.warn(`Unknown message type: ${message.type}`);
      }
    } catch (error) {
      logger.error('WebSocket message error:', error);
      ws.send(JSON.stringify({
        type: 'error',
        message: 'Failed to process message'
      }));
    }
  });

  ws.on('close', () => {
    logger.info(`WebSocket connection closed for machine: ${machineId}`);
  });

  ws.on('error', (error) => {
    logger.error(`WebSocket error for machine ${machineId}:`, error);
  });
});

// Helper functions
async function forwardMachineStatus(machineId: string, status: any) {
  // Forward to main PravaraMES system
  try {
    const response = await fetch(`${process.env.PRAVARA_API_URL}/v1/machines/${machineId}/status`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${process.env.PRAVARA_API_KEY}`
      },
      body: JSON.stringify(status)
    });

    if (!response.ok) {
      logger.error(`Failed to forward machine status: ${response.statusText}`);
    }
  } catch (error) {
    logger.error('Error forwarding machine status:', error);
  }
}

async function processTelemetry(machineId: string, telemetry: any) {
  // Process and forward telemetry
  const processed = {
    machine_id: machineId,
    timestamp: new Date().toISOString(),
    ...telemetry,
    // Add Snapmaker-specific telemetry processing
    module_type: telemetry.module || 'unknown',
    tool_head: telemetry.toolHead || 'unknown',
    enclosure_status: telemetry.enclosure || {}
  };

  // Forward to telemetry worker
  try {
    await fetch(`${process.env.TELEMETRY_URL}/telemetry`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(processed)
    });
  } catch (error) {
    logger.error('Error forwarding telemetry:', error);
  }
}

// Graceful shutdown
process.on('SIGTERM', () => {
  logger.info('SIGTERM signal received: closing HTTP server');
  server.close(() => {
    logger.info('HTTP server closed');
  });
  wss.close(() => {
    logger.info('WebSocket server closed');
  });
});

export { logger };