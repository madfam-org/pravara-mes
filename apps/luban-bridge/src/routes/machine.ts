import { Router } from 'express';
import winston from 'winston';
import { SnapmakerProtocol } from '../services/snapmaker-protocol';
import { MachineDiscovery } from '../services/machine-discovery';

export function machineRouter(
  protocol: SnapmakerProtocol,
  discovery: MachineDiscovery,
  logger: winston.Logger
): Router {
  const router = Router();

  // Discover machines
  router.post('/discover', async (req, res) => {
    const { enableNetwork = true, enableSerial = true, octoprintHosts = [] } = req.body;

    try {
      await discovery.startDiscovery({
        enableNetwork,
        enableSerial,
        octoprintHosts
      });

      // Wait a bit for discovery to complete
      setTimeout(() => {
        const machines = discovery.getMachines();
        res.json({ machines });
      }, 3000);
    } catch (error) {
      logger.error('Discovery failed', error);
      res.status(500).json({ error: 'Failed to discover machines' });
    }
  });

  // List discovered machines
  router.get('/list', (req, res) => {
    const machines = discovery.getMachines();
    res.json({ machines });
  });

  // Connect to machine
  router.post('/:machineId/connect', async (req, res) => {
    const { machineId } = req.params;
    const machine = discovery.getMachine(machineId);

    if (!machine) {
      return res.status(404).json({ error: 'Machine not found' });
    }

    try {
      const connectionOptions = {
        type: machine.connectionType,
        serialPath: machine.serialPath,
        ipAddress: machine.ip,
        baudRate: 115200
      };

      await protocol.connect(machineId, connectionOptions as any);
      res.json({ status: 'connected', machineId });
    } catch (error) {
      logger.error('Connection failed', error);
      res.status(500).json({ error: 'Failed to connect to machine' });
    }
  });

  // Disconnect from machine
  router.post('/:machineId/disconnect', async (req, res) => {
    const { machineId } = req.params;

    try {
      await protocol.disconnect(machineId);
      res.json({ status: 'disconnected', machineId });
    } catch (error) {
      logger.error('Disconnection failed', error);
      res.status(500).json({ error: 'Failed to disconnect from machine' });
    }
  });

  // Get machine status
  router.get('/:machineId/status', (req, res) => {
    const { machineId } = req.params;
    const machineInfo = protocol.getMachineInfo(machineId);

    if (!machineInfo) {
      return res.status(404).json({ error: 'Machine not connected' });
    }

    res.json(machineInfo);
  });

  // Send G-code command
  router.post('/:machineId/command', async (req, res) => {
    const { machineId } = req.params;
    const { command } = req.body;

    if (!command) {
      return res.status(400).json({ error: 'No command provided' });
    }

    try {
      const response = await protocol.sendCommand(machineId, command);
      res.json({ response });
    } catch (error) {
      logger.error('Command failed', error);
      res.status(500).json({ error: 'Failed to send command' });
    }
  });

  // Execute G-code file
  router.post('/:machineId/execute', async (req, res) => {
    const { machineId } = req.params;
    const { gcode } = req.body;

    if (!gcode) {
      return res.status(400).json({ error: 'No G-code provided' });
    }

    try {
      await protocol.executeGCode(machineId, gcode);
      res.json({ status: 'executing' });
    } catch (error) {
      logger.error('Execution failed', error);
      res.status(500).json({ error: 'Failed to execute G-code' });
    }
  });

  // Upload file to machine
  router.post('/:machineId/upload', async (req, res) => {
    const { machineId } = req.params;
    const { filename, content } = req.body;

    if (!filename || !content) {
      return res.status(400).json({ error: 'Filename and content required' });
    }

    try {
      await protocol.uploadFile(machineId, filename, content);
      res.json({ status: 'uploaded', filename });
    } catch (error) {
      logger.error('Upload failed', error);
      res.status(500).json({ error: 'Failed to upload file' });
    }
  });

  // Print control endpoints
  router.post('/:machineId/print/start', async (req, res) => {
    const { machineId } = req.params;
    const { filename } = req.body;

    try {
      await protocol.startPrint(machineId, filename);
      res.json({ status: 'printing', filename });
    } catch (error) {
      logger.error('Print start failed', error);
      res.status(500).json({ error: 'Failed to start print' });
    }
  });

  router.post('/:machineId/print/pause', async (req, res) => {
    const { machineId } = req.params;

    try {
      await protocol.pausePrint(machineId);
      res.json({ status: 'paused' });
    } catch (error) {
      logger.error('Print pause failed', error);
      res.status(500).json({ error: 'Failed to pause print' });
    }
  });

  router.post('/:machineId/print/resume', async (req, res) => {
    const { machineId } = req.params;

    try {
      await protocol.resumePrint(machineId);
      res.json({ status: 'printing' });
    } catch (error) {
      logger.error('Print resume failed', error);
      res.status(500).json({ error: 'Failed to resume print' });
    }
  });

  router.post('/:machineId/print/cancel', async (req, res) => {
    const { machineId } = req.params;

    try {
      await protocol.cancelPrint(machineId);
      res.json({ status: 'cancelled' });
    } catch (error) {
      logger.error('Print cancel failed', error);
      res.status(500).json({ error: 'Failed to cancel print' });
    }
  });

  // Temperature control
  router.post('/:machineId/temperature', async (req, res) => {
    const { machineId } = req.params;
    const { target, temperature } = req.body;

    if (!target || temperature === undefined) {
      return res.status(400).json({ error: 'Target and temperature required' });
    }

    try {
      await protocol.setTemperature(machineId, target, temperature);
      res.json({ status: 'set', target, temperature });
    } catch (error) {
      logger.error('Temperature set failed', error);
      res.status(500).json({ error: 'Failed to set temperature' });
    }
  });

  // Home axes
  router.post('/:machineId/home', async (req, res) => {
    const { machineId } = req.params;
    const { axes = 'XYZ' } = req.body;

    try {
      await protocol.homeAxes(machineId, axes);
      res.json({ status: 'homing', axes });
    } catch (error) {
      logger.error('Homing failed', error);
      res.status(500).json({ error: 'Failed to home axes' });
    }
  });

  // Test connection
  router.get('/:machineId/test', async (req, res) => {
    const { machineId } = req.params;

    try {
      const connected = await discovery.testConnection(machineId);
      res.json({ connected, machineId });
    } catch (error) {
      logger.error('Connection test failed', error);
      res.status(500).json({ error: 'Failed to test connection' });
    }
  });

  return router;
}