import express from 'express';
import request from 'supertest';
import winston from 'winston';
import { machineRouter } from '../machine';

// ---------------------------------------------------------------------------
// Mocks -- SnapmakerProtocol and MachineDiscovery
// ---------------------------------------------------------------------------

function createMockProtocol() {
  return {
    connect: jest.fn().mockResolvedValue(undefined),
    disconnect: jest.fn().mockResolvedValue(undefined),
    sendCommand: jest.fn().mockResolvedValue('ok'),
    executeGCode: jest.fn().mockResolvedValue(undefined),
    uploadFile: jest.fn().mockResolvedValue(undefined),
    startPrint: jest.fn().mockResolvedValue(undefined),
    pausePrint: jest.fn().mockResolvedValue(undefined),
    resumePrint: jest.fn().mockResolvedValue(undefined),
    cancelPrint: jest.fn().mockResolvedValue(undefined),
    homeAxes: jest.fn().mockResolvedValue(undefined),
    setTemperature: jest.fn().mockResolvedValue(undefined),
    getMachineInfo: jest.fn().mockReturnValue(undefined),
    getAllMachines: jest.fn().mockReturnValue([]),
  };
}

function createMockDiscovery() {
  return {
    startDiscovery: jest.fn().mockResolvedValue(undefined),
    getMachines: jest.fn().mockReturnValue([]),
    getMachine: jest.fn().mockReturnValue(undefined),
    testConnection: jest.fn().mockResolvedValue(false),
    removeStaleMachines: jest.fn(),
    stopDiscovery: jest.fn(),
  };
}

function createSilentLogger(): winston.Logger {
  return winston.createLogger({ silent: true });
}

// ---------------------------------------------------------------------------
// Test app factory
// ---------------------------------------------------------------------------

function createApp(
  protocolOverrides: Partial<ReturnType<typeof createMockProtocol>> = {},
  discoveryOverrides: Partial<ReturnType<typeof createMockDiscovery>> = {}
) {
  const protocol = { ...createMockProtocol(), ...protocolOverrides };
  const discovery = { ...createMockDiscovery(), ...discoveryOverrides };
  const logger = createSilentLogger();

  const app = express();
  app.use(express.json());
  app.use('/api/machine', machineRouter(protocol as any, discovery as any, logger));

  return { app, protocol, discovery };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Machine Routes', () => {
  // -----------------------------------------------------------------------
  // GET /api/machine/list
  // -----------------------------------------------------------------------

  describe('GET /api/machine/list', () => {
    it('should return an empty list when no machines are discovered', async () => {
      const { app } = createApp();

      const res = await request(app).get('/api/machine/list');

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ machines: [] });
    });

    it('should return all discovered machines', async () => {
      const machines = [
        { id: 'sm-001', name: 'Snapmaker A350', model: 'A350', connectionType: 'wifi' },
        { id: 'sm-002', name: 'Snapmaker A250', model: 'A250', connectionType: 'serial' },
      ];

      const { app } = createApp({}, { getMachines: jest.fn().mockReturnValue(machines) });

      const res = await request(app).get('/api/machine/list');

      expect(res.status).toBe(200);
      expect(res.body.machines).toHaveLength(2);
      expect(res.body.machines[0].id).toBe('sm-001');
      expect(res.body.machines[1].id).toBe('sm-002');
    });
  });

  // -----------------------------------------------------------------------
  // POST /api/machine/:machineId/connect
  // -----------------------------------------------------------------------

  describe('POST /api/machine/:machineId/connect', () => {
    it('should return 404 when machine is not found', async () => {
      const { app } = createApp();

      const res = await request(app).post('/api/machine/unknown-id/connect');

      expect(res.status).toBe(404);
      expect(res.body.error).toBe('Machine not found');
    });

    it('should connect to a discovered wifi machine', async () => {
      const machine = {
        id: 'sm-wifi',
        name: 'WiFi Snapmaker',
        model: 'A350',
        ip: '192.168.1.100',
        connectionType: 'wifi',
      };

      const { app, protocol } = createApp(
        {},
        { getMachine: jest.fn().mockReturnValue(machine) }
      );

      const res = await request(app).post('/api/machine/sm-wifi/connect');

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'connected', machineId: 'sm-wifi' });
      expect(protocol.connect).toHaveBeenCalledWith(
        'sm-wifi',
        expect.objectContaining({
          type: 'wifi',
          ipAddress: '192.168.1.100',
        })
      );
    });

    it('should connect to a discovered serial machine', async () => {
      const machine = {
        id: 'sm-serial',
        name: 'Serial Snapmaker',
        model: 'A250',
        serialPath: '/dev/ttyUSB0',
        connectionType: 'serial',
      };

      const { app, protocol } = createApp(
        {},
        { getMachine: jest.fn().mockReturnValue(machine) }
      );

      const res = await request(app).post('/api/machine/sm-serial/connect');

      expect(res.status).toBe(200);
      expect(protocol.connect).toHaveBeenCalledWith(
        'sm-serial',
        expect.objectContaining({
          type: 'serial',
          serialPath: '/dev/ttyUSB0',
        })
      );
    });

    it('should return 500 when connection fails', async () => {
      const machine = {
        id: 'sm-fail',
        name: 'Failing Machine',
        model: 'X',
        connectionType: 'wifi',
        ip: '10.0.0.1',
      };

      const { app } = createApp(
        { connect: jest.fn().mockRejectedValue(new Error('ECONNREFUSED')) },
        { getMachine: jest.fn().mockReturnValue(machine) }
      );

      const res = await request(app).post('/api/machine/sm-fail/connect');

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to connect to machine');
    });
  });

  // -----------------------------------------------------------------------
  // POST /api/machine/:machineId/disconnect
  // -----------------------------------------------------------------------

  describe('POST /api/machine/:machineId/disconnect', () => {
    it('should disconnect from a machine', async () => {
      const { app, protocol } = createApp();

      const res = await request(app).post('/api/machine/sm-001/disconnect');

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'disconnected', machineId: 'sm-001' });
      expect(protocol.disconnect).toHaveBeenCalledWith('sm-001');
    });

    it('should return 500 when disconnect fails', async () => {
      const { app } = createApp({
        disconnect: jest.fn().mockRejectedValue(new Error('Port busy')),
      });

      const res = await request(app).post('/api/machine/sm-001/disconnect');

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to disconnect from machine');
    });
  });

  // -----------------------------------------------------------------------
  // GET /api/machine/:machineId/status
  // -----------------------------------------------------------------------

  describe('GET /api/machine/:machineId/status', () => {
    it('should return 404 when machine is not connected', async () => {
      const { app } = createApp();

      const res = await request(app).get('/api/machine/sm-001/status');

      expect(res.status).toBe(404);
      expect(res.body.error).toBe('Machine not connected');
    });

    it('should return machine info when connected', async () => {
      const machineInfo = {
        id: 'sm-001',
        model: 'Snapmaker A350',
        firmware: '1.14.1',
        status: 'idle',
        position: { x: 0, y: 0, z: 0 },
        temperatures: {
          bed: { current: 22, target: 0 },
          nozzle: { current: 23, target: 0 },
        },
        toolhead: 'printing',
      };

      const { app } = createApp({
        getMachineInfo: jest.fn().mockReturnValue(machineInfo),
      });

      const res = await request(app).get('/api/machine/sm-001/status');

      expect(res.status).toBe(200);
      expect(res.body.id).toBe('sm-001');
      expect(res.body.model).toBe('Snapmaker A350');
      expect(res.body.temperatures.bed.current).toBe(22);
    });
  });

  // -----------------------------------------------------------------------
  // POST /api/machine/:machineId/command
  // -----------------------------------------------------------------------

  describe('POST /api/machine/:machineId/command', () => {
    it('should return 400 when no command is provided', async () => {
      const { app } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/command')
        .send({});

      expect(res.status).toBe(400);
      expect(res.body.error).toBe('No command provided');
    });

    it('should send a G-code command and return the response', async () => {
      const { app, protocol } = createApp({
        sendCommand: jest.fn().mockResolvedValue('ok T:210.0 /210 B:60.0 /60'),
      });

      const res = await request(app)
        .post('/api/machine/sm-001/command')
        .send({ command: 'M105' });

      expect(res.status).toBe(200);
      expect(res.body.response).toBe('ok T:210.0 /210 B:60.0 /60');
      expect(protocol.sendCommand).toHaveBeenCalledWith('sm-001', 'M105');
    });

    it('should return 500 when command execution fails', async () => {
      const { app } = createApp({
        sendCommand: jest.fn().mockRejectedValue(new Error('Machine not connected')),
      });

      const res = await request(app)
        .post('/api/machine/sm-001/command')
        .send({ command: 'G28' });

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to send command');
    });
  });

  // -----------------------------------------------------------------------
  // POST /api/machine/:machineId/execute
  // -----------------------------------------------------------------------

  describe('POST /api/machine/:machineId/execute', () => {
    it('should return 400 when no gcode is provided', async () => {
      const { app } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/execute')
        .send({});

      expect(res.status).toBe(400);
      expect(res.body.error).toBe('No G-code provided');
    });

    it('should execute G-code and return executing status', async () => {
      const { app, protocol } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/execute')
        .send({ gcode: 'G28\nG1 X100 Y100 Z10' });

      expect(res.status).toBe(200);
      expect(res.body.status).toBe('executing');
      expect(protocol.executeGCode).toHaveBeenCalledWith('sm-001', 'G28\nG1 X100 Y100 Z10');
    });

    it('should return 500 when execution fails', async () => {
      const { app } = createApp({
        executeGCode: jest.fn().mockRejectedValue(new Error('Execution error')),
      });

      const res = await request(app)
        .post('/api/machine/sm-001/execute')
        .send({ gcode: 'G28' });

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to execute G-code');
    });
  });

  // -----------------------------------------------------------------------
  // POST /api/machine/:machineId/upload
  // -----------------------------------------------------------------------

  describe('POST /api/machine/:machineId/upload', () => {
    it('should return 400 when filename is missing', async () => {
      const { app } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/upload')
        .send({ content: 'G28' });

      expect(res.status).toBe(400);
      expect(res.body.error).toBe('Filename and content required');
    });

    it('should return 400 when content is missing', async () => {
      const { app } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/upload')
        .send({ filename: 'test.gcode' });

      expect(res.status).toBe(400);
      expect(res.body.error).toBe('Filename and content required');
    });

    it('should upload file successfully', async () => {
      const { app, protocol } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/upload')
        .send({ filename: 'part.gcode', content: 'G28\nG1 X50 Y50' });

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'uploaded', filename: 'part.gcode' });
      expect(protocol.uploadFile).toHaveBeenCalledWith('sm-001', 'part.gcode', 'G28\nG1 X50 Y50');
    });

    it('should return 500 when upload fails', async () => {
      const { app } = createApp({
        uploadFile: jest.fn().mockRejectedValue(new Error('Upload error')),
      });

      const res = await request(app)
        .post('/api/machine/sm-001/upload')
        .send({ filename: 'part.gcode', content: 'G28' });

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to upload file');
    });
  });

  // -----------------------------------------------------------------------
  // Print control endpoints
  // -----------------------------------------------------------------------

  describe('print control', () => {
    it('POST /print/start should start printing', async () => {
      const { app, protocol } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/print/start')
        .send({ filename: 'part.gcode' });

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'printing', filename: 'part.gcode' });
      expect(protocol.startPrint).toHaveBeenCalledWith('sm-001', 'part.gcode');
    });

    it('POST /print/start should return 500 on failure', async () => {
      const { app } = createApp({
        startPrint: jest.fn().mockRejectedValue(new Error('Start failed')),
      });

      const res = await request(app)
        .post('/api/machine/sm-001/print/start')
        .send({ filename: 'x.gcode' });

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to start print');
    });

    it('POST /print/pause should pause printing', async () => {
      const { app, protocol } = createApp();

      const res = await request(app).post('/api/machine/sm-001/print/pause');

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'paused' });
      expect(protocol.pausePrint).toHaveBeenCalledWith('sm-001');
    });

    it('POST /print/resume should resume printing', async () => {
      const { app, protocol } = createApp();

      const res = await request(app).post('/api/machine/sm-001/print/resume');

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'printing' });
      expect(protocol.resumePrint).toHaveBeenCalledWith('sm-001');
    });

    it('POST /print/cancel should cancel printing', async () => {
      const { app, protocol } = createApp();

      const res = await request(app).post('/api/machine/sm-001/print/cancel');

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'cancelled' });
      expect(protocol.cancelPrint).toHaveBeenCalledWith('sm-001');
    });
  });

  // -----------------------------------------------------------------------
  // POST /api/machine/:machineId/temperature
  // -----------------------------------------------------------------------

  describe('POST /api/machine/:machineId/temperature', () => {
    it('should return 400 when target is missing', async () => {
      const { app } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/temperature')
        .send({ temperature: 60 });

      expect(res.status).toBe(400);
      expect(res.body.error).toBe('Target and temperature required');
    });

    it('should return 400 when temperature is missing', async () => {
      const { app } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/temperature')
        .send({ target: 'bed' });

      expect(res.status).toBe(400);
      expect(res.body.error).toBe('Target and temperature required');
    });

    it('should set bed temperature', async () => {
      const { app, protocol } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/temperature')
        .send({ target: 'bed', temperature: 60 });

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'set', target: 'bed', temperature: 60 });
      expect(protocol.setTemperature).toHaveBeenCalledWith('sm-001', 'bed', 60);
    });

    it('should set nozzle temperature', async () => {
      const { app, protocol } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/temperature')
        .send({ target: 'nozzle', temperature: 210 });

      expect(res.status).toBe(200);
      expect(protocol.setTemperature).toHaveBeenCalledWith('sm-001', 'nozzle', 210);
    });

    it('should accept temperature of 0 (turn off heater)', async () => {
      const { app, protocol } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/temperature')
        .send({ target: 'bed', temperature: 0 });

      expect(res.status).toBe(200);
      expect(protocol.setTemperature).toHaveBeenCalledWith('sm-001', 'bed', 0);
    });
  });

  // -----------------------------------------------------------------------
  // POST /api/machine/:machineId/home
  // -----------------------------------------------------------------------

  describe('POST /api/machine/:machineId/home', () => {
    it('should home all axes by default', async () => {
      const { app, protocol } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/home')
        .send({});

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'homing', axes: 'XYZ' });
      expect(protocol.homeAxes).toHaveBeenCalledWith('sm-001', 'XYZ');
    });

    it('should home specific axes when requested', async () => {
      const { app, protocol } = createApp();

      const res = await request(app)
        .post('/api/machine/sm-001/home')
        .send({ axes: 'Z' });

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'homing', axes: 'Z' });
      expect(protocol.homeAxes).toHaveBeenCalledWith('sm-001', 'Z');
    });

    it('should return 500 when homing fails', async () => {
      const { app } = createApp({
        homeAxes: jest.fn().mockRejectedValue(new Error('Motor error')),
      });

      const res = await request(app)
        .post('/api/machine/sm-001/home')
        .send({});

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to home axes');
    });
  });

  // -----------------------------------------------------------------------
  // GET /api/machine/:machineId/test
  // -----------------------------------------------------------------------

  describe('GET /api/machine/:machineId/test', () => {
    it('should return connection test result (true)', async () => {
      const { app } = createApp(
        {},
        { testConnection: jest.fn().mockResolvedValue(true) }
      );

      const res = await request(app).get('/api/machine/sm-001/test');

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ connected: true, machineId: 'sm-001' });
    });

    it('should return connection test result (false)', async () => {
      const { app } = createApp(
        {},
        { testConnection: jest.fn().mockResolvedValue(false) }
      );

      const res = await request(app).get('/api/machine/sm-001/test');

      expect(res.status).toBe(200);
      expect(res.body).toEqual({ connected: false, machineId: 'sm-001' });
    });

    it('should return 500 when test throws', async () => {
      const { app } = createApp(
        {},
        { testConnection: jest.fn().mockRejectedValue(new Error('Network error')) }
      );

      const res = await request(app).get('/api/machine/sm-001/test');

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to test connection');
    });
  });

  // -----------------------------------------------------------------------
  // POST /api/machine/discover
  // -----------------------------------------------------------------------

  describe('POST /api/machine/discover', () => {
    it('should trigger discovery and return machines', async () => {
      jest.useFakeTimers();

      const discoveredMachines = [
        { id: 'sm-disc-1', name: 'Discovered 1', model: 'A350', connectionType: 'wifi' },
      ];

      const { app, discovery } = createApp(
        {},
        {
          startDiscovery: jest.fn().mockResolvedValue(undefined),
          getMachines: jest.fn().mockReturnValue(discoveredMachines),
        }
      );

      const resPromise = request(app)
        .post('/api/machine/discover')
        .send({ enableNetwork: true, enableSerial: false });

      // Advance past the 3-second setTimeout in the route handler
      jest.advanceTimersByTime(3500);

      const res = await resPromise;

      expect(res.status).toBe(200);
      expect(res.body.machines).toHaveLength(1);
      expect(res.body.machines[0].id).toBe('sm-disc-1');
      expect(discovery.startDiscovery).toHaveBeenCalledWith(
        expect.objectContaining({
          enableNetwork: true,
          enableSerial: false,
        })
      );

      jest.useRealTimers();
    });

    it('should return 500 when discovery throws', async () => {
      const { app } = createApp(
        {},
        {
          startDiscovery: jest.fn().mockRejectedValue(new Error('Discovery error')),
        }
      );

      const res = await request(app)
        .post('/api/machine/discover')
        .send({});

      expect(res.status).toBe(500);
      expect(res.body.error).toBe('Failed to discover machines');
    });
  });
});
