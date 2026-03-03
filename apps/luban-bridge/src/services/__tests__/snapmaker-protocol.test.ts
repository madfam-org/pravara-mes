import { SnapmakerProtocol } from '../snapmaker-protocol';
import winston from 'winston';
import { EventEmitter } from 'events';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock serialport -- prevent real hardware access
jest.mock('serialport', () => {
  class MockSerialPort extends EventEmitter {
    path: string;
    baudRate: number;
    isOpen = false;

    constructor(opts: { path: string; baudRate: number; autoOpen?: boolean }) {
      super();
      this.path = opts.path;
      this.baudRate = opts.baudRate;
    }

    open(cb: (err: Error | null) => void) {
      this.isOpen = true;
      cb(null);
    }

    close(cb?: () => void) {
      this.isOpen = false;
      cb?.();
    }

    write(_data: string) {
      /* no-op */
    }

    pipe(parser: any) {
      return parser;
    }
  }

  class MockReadlineParser extends EventEmitter {
    constructor(_opts?: any) {
      super();
    }
  }

  return {
    SerialPort: MockSerialPort,
    __MockSerialPort: MockSerialPort,
  };
});

jest.mock('@serialport/parser-readline', () => {
  class MockReadlineParser extends EventEmitter {
    constructor(_opts?: any) {
      super();
    }
  }
  return { ReadlineParser: MockReadlineParser };
});

// Mock axios for wifi / octoprint connections
jest.mock('axios');
import axios from 'axios';
const mockedAxios = axios as jest.Mocked<typeof axios>;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function createSilentLogger(): winston.Logger {
  return winston.createLogger({ silent: true });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SnapmakerProtocol', () => {
  let protocol: SnapmakerProtocol;
  let logger: winston.Logger;

  beforeEach(() => {
    jest.clearAllMocks();
    logger = createSilentLogger();
    protocol = new SnapmakerProtocol(logger);
  });

  // -----------------------------------------------------------------------
  // Connection management
  // -----------------------------------------------------------------------

  describe('connect', () => {
    it('should establish a serial connection and initialise machine info', async () => {
      await protocol.connect('sm-001', {
        type: 'serial',
        serialPath: '/dev/ttyUSB0',
        baudRate: 115200,
      });

      const info = protocol.getMachineInfo('sm-001');
      expect(info).toBeDefined();
      expect(info!.id).toBe('sm-001');
      expect(info!.status).toBe('idle');
      expect(info!.position).toEqual({ x: 0, y: 0, z: 0 });
      expect(info!.temperatures.bed).toEqual({ current: 0, target: 0 });
      expect(info!.temperatures.nozzle).toEqual({ current: 0, target: 0 });
    });

    it('should establish a wifi connection using the HTTP API', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          model: 'Snapmaker A350T',
          firmware: '1.14.1',
          state: 'idle',
          position: { x: 10, y: 20, z: 5 },
          temperatures: {
            bed: { current: 23, target: 0 },
            nozzle: { current: 24, target: 0 },
          },
          toolhead: 'printing',
        },
      });

      await protocol.connect('sm-wifi', {
        type: 'wifi',
        ipAddress: '192.168.1.100',
      });

      expect(mockedAxios.get).toHaveBeenCalledWith('http://192.168.1.100:8080/api/status');

      const info = protocol.getMachineInfo('sm-wifi');
      expect(info).toBeDefined();
      expect(info!.model).toBe('Snapmaker A350T');
      expect(info!.firmware).toBe('1.14.1');
      expect(info!.position).toEqual({ x: 10, y: 20, z: 5 });
    });

    it('should establish an OctoPrint connection', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          state: { flags: { printing: false } },
          temperature: {
            bed: { current: 22, target: 0 },
            tool0: { current: 23, target: 0 },
          },
        },
      });

      await protocol.connect('sm-octo', {
        type: 'octoprint',
        ipAddress: '192.168.1.200',
        apiKey: 'test-key',
      });

      const info = protocol.getMachineInfo('sm-octo');
      expect(info).toBeDefined();
      expect(info!.model).toBe('Snapmaker (OctoPrint)');
      expect(info!.status).toBe('idle');
    });

    it('should detect printing status from OctoPrint flags', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          state: { flags: { printing: true } },
          temperature: {
            bed: { current: 60, target: 60 },
            tool0: { current: 210, target: 210 },
          },
        },
      });

      await protocol.connect('sm-octo-printing', {
        type: 'octoprint',
        ipAddress: '192.168.1.201',
        apiKey: 'key-2',
      });

      expect(protocol.getMachineInfo('sm-octo-printing')!.status).toBe('printing');
    });

    it('should throw on unknown connection type', async () => {
      await expect(
        protocol.connect('sm-bad', { type: 'bluetooth' as any })
      ).rejects.toThrow('Unknown connection type: bluetooth');
    });

    it('should throw when wifi connection fails', async () => {
      mockedAxios.get.mockRejectedValueOnce(new Error('ECONNREFUSED'));

      await expect(
        protocol.connect('sm-fail', { type: 'wifi', ipAddress: '10.0.0.1' })
      ).rejects.toThrow();
    });

    it('should throw when OctoPrint connection fails', async () => {
      mockedAxios.get.mockRejectedValueOnce(new Error('ECONNREFUSED'));

      await expect(
        protocol.connect('sm-fail-octo', {
          type: 'octoprint',
          ipAddress: '10.0.0.2',
          apiKey: 'bad-key',
        })
      ).rejects.toThrow();
    });
  });

  // -----------------------------------------------------------------------
  // Command construction -- G-code helpers
  // -----------------------------------------------------------------------

  describe('command construction helpers', () => {
    beforeEach(async () => {
      // Set up a wifi connection so sendCommand uses the wifi path
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          model: 'Snapmaker',
          firmware: '1.0',
          state: 'idle',
          position: { x: 0, y: 0, z: 0 },
          temperatures: {
            bed: { current: 0, target: 0 },
            nozzle: { current: 0, target: 0 },
          },
          toolhead: 'printing',
        },
      });

      await protocol.connect('machine-1', {
        type: 'wifi',
        ipAddress: '192.168.1.50',
      });
    });

    it('homeAxes should send G28 with requested axes', async () => {
      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.homeAxes('machine-1', 'XY');

      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://192.168.1.50:8080/api/gcode',
        { command: 'G28 XY' },
        expect.any(Object)
      );
    });

    it('homeAxes defaults to XYZ when no axes specified', async () => {
      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.homeAxes('machine-1');

      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://192.168.1.50:8080/api/gcode',
        { command: 'G28 XYZ' },
        expect.any(Object)
      );
    });

    it('setTemperature sends M140 for bed', async () => {
      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.setTemperature('machine-1', 'bed', 60);

      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://192.168.1.50:8080/api/gcode',
        { command: 'M140 S60' },
        expect.any(Object)
      );

      const info = protocol.getMachineInfo('machine-1');
      expect(info!.temperatures.bed.target).toBe(60);
    });

    it('setTemperature sends M104 for nozzle', async () => {
      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.setTemperature('machine-1', 'nozzle', 210);

      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://192.168.1.50:8080/api/gcode',
        { command: 'M104 S210' },
        expect.any(Object)
      );

      const info = protocol.getMachineInfo('machine-1');
      expect(info!.temperatures.nozzle.target).toBe(210);
    });

    it('startPrint sends M23 + M24 for wifi connections', async () => {
      mockedAxios.post
        .mockResolvedValueOnce({ data: { response: 'ok' } }) // M23
        .mockResolvedValueOnce({ data: { response: 'ok' } }); // M24

      await protocol.startPrint('machine-1', 'part.gcode');

      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://192.168.1.50:8080/api/gcode',
        { command: 'M23 part.gcode' },
        expect.any(Object)
      );
      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://192.168.1.50:8080/api/gcode',
        { command: 'M24' },
        expect.any(Object)
      );

      expect(protocol.getMachineInfo('machine-1')!.status).toBe('printing');
    });

    it('pausePrint sends M25 and updates status', async () => {
      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.pausePrint('machine-1');

      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://192.168.1.50:8080/api/gcode',
        { command: 'M25' },
        expect.any(Object)
      );
      expect(protocol.getMachineInfo('machine-1')!.status).toBe('paused');
    });

    it('resumePrint sends M24 and updates status', async () => {
      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.resumePrint('machine-1');

      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://192.168.1.50:8080/api/gcode',
        { command: 'M24' },
        expect.any(Object)
      );
      expect(protocol.getMachineInfo('machine-1')!.status).toBe('printing');
    });

    it('cancelPrint sends M0, resets status and clears progress', async () => {
      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.cancelPrint('machine-1');

      const info = protocol.getMachineInfo('machine-1')!;
      expect(info.status).toBe('idle');
      expect(info.progress).toBeUndefined();
    });
  });

  // -----------------------------------------------------------------------
  // sendCommand
  // -----------------------------------------------------------------------

  describe('sendCommand', () => {
    it('should throw when machine is not connected', async () => {
      await expect(
        protocol.sendCommand('nonexistent', 'G28')
      ).rejects.toThrow('Machine nonexistent not connected');
    });

    it('should send command via wifi and return response', async () => {
      // connect first
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          model: 'SM',
          state: 'idle',
          position: { x: 0, y: 0, z: 0 },
          temperatures: { bed: { current: 0, target: 0 }, nozzle: { current: 0, target: 0 } },
          toolhead: 'printing',
        },
      });
      await protocol.connect('wifi-cmd', { type: 'wifi', ipAddress: '10.0.0.5' });

      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok T:22.5' } });

      const result = await protocol.sendCommand('wifi-cmd', 'M105');
      expect(result).toBe('ok T:22.5');
    });

    it('should throw when wifi command fails', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          model: 'SM',
          state: 'idle',
          position: { x: 0, y: 0, z: 0 },
          temperatures: { bed: { current: 0, target: 0 }, nozzle: { current: 0, target: 0 } },
          toolhead: 'printing',
        },
      });
      await protocol.connect('wifi-err', { type: 'wifi', ipAddress: '10.0.0.6' });

      mockedAxios.post.mockRejectedValueOnce(new Error('Network error'));

      await expect(
        protocol.sendCommand('wifi-err', 'G28')
      ).rejects.toThrow('WiFi command failed');
    });

    it('should send command via OctoPrint endpoint', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          state: { flags: { printing: false } },
          temperature: {
            bed: { current: 0, target: 0 },
            tool0: { current: 0, target: 0 },
          },
        },
      });
      await protocol.connect('octo-cmd', {
        type: 'octoprint',
        ipAddress: '10.0.0.7',
        apiKey: 'abc',
      });

      mockedAxios.post.mockResolvedValueOnce({ data: {} });

      const result = await protocol.sendCommand('octo-cmd', 'M105');
      expect(result).toBe('ok');

      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://10.0.0.7/api/printer/command',
        { command: 'M105' },
        { headers: { 'X-Api-Key': 'abc' } }
      );
    });

    it('should throw when OctoPrint command fails', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          state: { flags: { printing: false } },
          temperature: {
            bed: { current: 0, target: 0 },
            tool0: { current: 0, target: 0 },
          },
        },
      });
      await protocol.connect('octo-err', {
        type: 'octoprint',
        ipAddress: '10.0.0.8',
        apiKey: 'xyz',
      });

      mockedAxios.post.mockRejectedValueOnce(new Error('Printer offline'));

      await expect(
        protocol.sendCommand('octo-err', 'G28')
      ).rejects.toThrow('OctoPrint command failed');
    });
  });

  // -----------------------------------------------------------------------
  // executeGCode
  // -----------------------------------------------------------------------

  describe('executeGCode', () => {
    beforeEach(async () => {
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          model: 'SM',
          state: 'idle',
          position: { x: 0, y: 0, z: 0 },
          temperatures: { bed: { current: 0, target: 0 }, nozzle: { current: 0, target: 0 } },
          toolhead: 'printing',
        },
      });
      await protocol.connect('exec-machine', { type: 'wifi', ipAddress: '10.0.0.10' });
    });

    it('should execute each non-empty, non-comment line', async () => {
      mockedAxios.post
        .mockResolvedValueOnce({ data: { response: 'ok' } })
        .mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.executeGCode('exec-machine', 'G28\n; comment\n\nG1 X50 Y50');

      expect(mockedAxios.post).toHaveBeenCalledTimes(2);
    });

    it('should skip empty lines and comments', async () => {
      mockedAxios.post.mockResolvedValueOnce({ data: { response: 'ok' } });

      await protocol.executeGCode('exec-machine', '; only a comment\n\n  \nG28');

      expect(mockedAxios.post).toHaveBeenCalledTimes(1);
    });
  });

  // -----------------------------------------------------------------------
  // Response parsing (handleResponse via the event emitter path)
  // -----------------------------------------------------------------------

  describe('handleResponse - response parsing', () => {
    beforeEach(async () => {
      // Use serial connection so we can feed data through the parser
      await protocol.connect('parse-machine', {
        type: 'serial',
        serialPath: '/dev/ttyUSB1',
      });
    });

    it('should parse temperature responses (T: / B:)', () => {
      const updateHandler = jest.fn();
      protocol.on('machine-update', updateHandler);

      // Access handleResponse via the prototype -- it is private, but we need
      // to validate parsing. We invoke it through the class internals.
      (protocol as any).handleResponse('parse-machine', 'T:210.5 /215 B:60.2 /60');

      const info = protocol.getMachineInfo('parse-machine')!;
      expect(info.temperatures.nozzle.current).toBe(210.5);
      expect(info.temperatures.nozzle.target).toBe(215);
      expect(info.temperatures.bed.current).toBe(60.2);
      expect(info.temperatures.bed.target).toBe(60);

      expect(updateHandler).toHaveBeenCalledWith(
        expect.objectContaining({ id: 'parse-machine' })
      );
    });

    it('should parse position responses (X: Y: Z:)', () => {
      (protocol as any).handleResponse('parse-machine', 'X:100.50 Y:200.25 Z:10.00');

      const info = protocol.getMachineInfo('parse-machine')!;
      expect(info.position).toEqual({ x: 100.5, y: 200.25, z: 10 });
    });

    it('should parse negative position values', () => {
      (protocol as any).handleResponse('parse-machine', 'X:-5.0 Y:-10.0 Z:0.0');

      const info = protocol.getMachineInfo('parse-machine')!;
      expect(info.position).toEqual({ x: -5, y: -10, z: 0 });
    });

    it('should parse SD printing progress', () => {
      (protocol as any).handleResponse('parse-machine', 'SD printing byte 5000/10000');

      const info = protocol.getMachineInfo('parse-machine')!;
      expect(info.progress).toBeDefined();
      expect(info.progress!.percent).toBe(50);
    });

    it('should handle progress at 100%', () => {
      (protocol as any).handleResponse('parse-machine', 'SD printing byte 10000/10000');

      const info = protocol.getMachineInfo('parse-machine')!;
      expect(info.progress!.percent).toBe(100);
    });

    it('should ignore responses for unknown machines', () => {
      // Should not throw
      expect(() => {
        (protocol as any).handleResponse('unknown-id', 'T:100 /100 B:50 /50');
      }).not.toThrow();
    });

    it('should not update temperatures for non-temperature responses', () => {
      const info = protocol.getMachineInfo('parse-machine')!;
      const originalNozzle = { ...info.temperatures.nozzle };

      (protocol as any).handleResponse('parse-machine', 'ok');

      expect(info.temperatures.nozzle).toEqual(originalNozzle);
    });

    it('should not update position for non-position responses', () => {
      const info = protocol.getMachineInfo('parse-machine')!;
      const originalPosition = { ...info.position };

      (protocol as any).handleResponse('parse-machine', 'ok');

      expect(info.position).toEqual(originalPosition);
    });

    it('should handle malformed temperature data gracefully', () => {
      // Does not match the regex -- fields should stay unchanged
      (protocol as any).handleResponse('parse-machine', 'T:garbage /bad B:? /?');

      const info = protocol.getMachineInfo('parse-machine')!;
      expect(info.temperatures.nozzle.current).toBe(0);
    });
  });

  // -----------------------------------------------------------------------
  // disconnect
  // -----------------------------------------------------------------------

  describe('disconnect', () => {
    it('should remove machine info after serial disconnect', async () => {
      await protocol.connect('dc-serial', {
        type: 'serial',
        serialPath: '/dev/ttyACM0',
      });

      expect(protocol.getMachineInfo('dc-serial')).toBeDefined();

      await protocol.disconnect('dc-serial');

      expect(protocol.getMachineInfo('dc-serial')).toBeUndefined();
    });

    it('should remove machine info after wifi disconnect', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        data: {
          model: 'SM',
          state: 'idle',
          position: { x: 0, y: 0, z: 0 },
          temperatures: { bed: { current: 0, target: 0 }, nozzle: { current: 0, target: 0 } },
          toolhead: 'printing',
        },
      });
      await protocol.connect('dc-wifi', { type: 'wifi', ipAddress: '10.0.0.20' });

      await protocol.disconnect('dc-wifi');

      expect(protocol.getMachineInfo('dc-wifi')).toBeUndefined();
    });

    it('should silently handle disconnect of unknown machine', async () => {
      await expect(protocol.disconnect('nope')).resolves.toBeUndefined();
    });
  });

  // -----------------------------------------------------------------------
  // getAllMachines
  // -----------------------------------------------------------------------

  describe('getAllMachines', () => {
    it('should return an empty array when no machines connected', () => {
      expect(protocol.getAllMachines()).toEqual([]);
    });

    it('should list all connected machines', async () => {
      await protocol.connect('m1', { type: 'serial', serialPath: '/dev/ttyUSB0' });
      await protocol.connect('m2', { type: 'serial', serialPath: '/dev/ttyUSB1' });

      const machines = protocol.getAllMachines();
      expect(machines).toHaveLength(2);
      expect(machines.map((m) => m.id).sort()).toEqual(['m1', 'm2']);
    });
  });
});
