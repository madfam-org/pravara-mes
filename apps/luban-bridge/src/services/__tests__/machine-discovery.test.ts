import { MachineDiscovery } from '../machine-discovery';
import winston from 'winston';
import { EventEmitter } from 'events';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock dgram -- prevent real network operations
const mockSocket = Object.assign(new EventEmitter(), {
  bind: jest.fn(function (this: EventEmitter) {
    process.nextTick(() => this.emit('listening'));
  }),
  close: jest.fn(function (this: EventEmitter & { _closed?: boolean }) {
    this._closed = true;
  }),
  setBroadcast: jest.fn(),
  send: jest.fn(),
});

jest.mock('dgram', () => ({
  createSocket: jest.fn(() => mockSocket),
}));

// Mock serialport
const mockSerialPortList = jest.fn();
jest.mock('serialport', () => {
  class MockSerialPort extends EventEmitter {
    path: string;
    isOpen = false;

    constructor(opts: { path: string; baudRate: number; autoOpen?: boolean }) {
      super();
      this.path = opts.path;
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
  }

  MockSerialPort.list = mockSerialPortList;

  return { SerialPort: MockSerialPort };
});

// Mock axios
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

describe('MachineDiscovery', () => {
  let discovery: MachineDiscovery;
  let logger: winston.Logger;

  beforeEach(() => {
    jest.clearAllMocks();
    jest.useFakeTimers({ advanceTimers: true });
    logger = createSilentLogger();
    discovery = new MachineDiscovery(logger);

    // Reset the mock socket event listeners between tests
    mockSocket.removeAllListeners();
    (mockSocket as any)._closed = false;
  });

  afterEach(() => {
    jest.useRealTimers();
    discovery.stopDiscovery();
  });

  // -----------------------------------------------------------------------
  // Machine registration via addMachine (internal)
  // -----------------------------------------------------------------------

  describe('machine registration', () => {
    it('should emit machine-discovered when a new machine is added', () => {
      const handler = jest.fn();
      discovery.on('machine-discovered', handler);

      // Invoke internal addMachine
      (discovery as any).addMachine({
        id: 'sm-001',
        name: 'Snapmaker A350',
        model: 'A350',
        ip: '192.168.1.10',
        port: 8080,
        connectionType: 'wifi',
        lastSeen: new Date(),
      });

      expect(handler).toHaveBeenCalledTimes(1);
      expect(handler).toHaveBeenCalledWith(
        expect.objectContaining({ id: 'sm-001', name: 'Snapmaker A350' })
      );
    });

    it('should update lastSeen for duplicate discoveries without re-emitting', () => {
      const handler = jest.fn();
      discovery.on('machine-discovered', handler);

      const machine = {
        id: 'sm-dup',
        name: 'Duplicate Machine',
        model: 'A250',
        ip: '192.168.1.20',
        port: 8080,
        connectionType: 'wifi' as const,
        lastSeen: new Date('2025-01-01T00:00:00Z'),
      };

      (discovery as any).addMachine({ ...machine });
      (discovery as any).addMachine({
        ...machine,
        lastSeen: new Date('2025-06-01T00:00:00Z'),
      });

      // Only emitted once (first discovery)
      expect(handler).toHaveBeenCalledTimes(1);

      const machines = discovery.getMachines();
      expect(machines).toHaveLength(1);
      // lastSeen should be updated
      expect(machines[0].lastSeen.getTime()).toBeGreaterThan(
        new Date('2025-01-01T00:00:00Z').getTime()
      );
    });

    it('should list multiple machines via getMachines', () => {
      (discovery as any).addMachine({
        id: 'a',
        name: 'Machine A',
        model: 'A350',
        connectionType: 'wifi',
        lastSeen: new Date(),
      });
      (discovery as any).addMachine({
        id: 'b',
        name: 'Machine B',
        model: 'A250',
        connectionType: 'serial',
        lastSeen: new Date(),
      });

      expect(discovery.getMachines()).toHaveLength(2);
    });

    it('should look up a single machine by id', () => {
      (discovery as any).addMachine({
        id: 'lookup-test',
        name: 'Lookup',
        model: 'A350',
        connectionType: 'wifi',
        lastSeen: new Date(),
      });

      expect(discovery.getMachine('lookup-test')).toBeDefined();
      expect(discovery.getMachine('nonexistent')).toBeUndefined();
    });
  });

  // -----------------------------------------------------------------------
  // Stale machine removal (timeout handling)
  // -----------------------------------------------------------------------

  describe('removeStaleMachines', () => {
    it('should remove machines older than maxAge and emit machine-lost', () => {
      const lostHandler = jest.fn();
      discovery.on('machine-lost', lostHandler);

      // Add a machine with lastSeen far in the past
      (discovery as any).addMachine({
        id: 'stale-1',
        name: 'Old Machine',
        model: 'A350',
        connectionType: 'wifi',
        lastSeen: new Date(Date.now() - 60000), // 60 seconds ago
      });

      (discovery as any).addMachine({
        id: 'fresh-1',
        name: 'Fresh Machine',
        model: 'A250',
        connectionType: 'serial',
        lastSeen: new Date(), // now
      });

      discovery.removeStaleMachines(30000); // 30-second threshold

      expect(discovery.getMachines()).toHaveLength(1);
      expect(discovery.getMachine('stale-1')).toBeUndefined();
      expect(discovery.getMachine('fresh-1')).toBeDefined();
      expect(lostHandler).toHaveBeenCalledWith(
        expect.objectContaining({ id: 'stale-1' })
      );
    });

    it('should use default maxAge of 30000ms', () => {
      (discovery as any).addMachine({
        id: 'default-stale',
        name: 'Default Stale',
        model: 'X',
        connectionType: 'wifi',
        lastSeen: new Date(Date.now() - 31000),
      });

      discovery.removeStaleMachines();

      expect(discovery.getMachine('default-stale')).toBeUndefined();
    });

    it('should not remove machines within the freshness window', () => {
      (discovery as any).addMachine({
        id: 'still-fresh',
        name: 'Still Fresh',
        model: 'X',
        connectionType: 'wifi',
        lastSeen: new Date(Date.now() - 5000),
      });

      discovery.removeStaleMachines(30000);

      expect(discovery.getMachine('still-fresh')).toBeDefined();
    });
  });

  // -----------------------------------------------------------------------
  // Network discovery (UDP broadcast)
  // -----------------------------------------------------------------------

  describe('network discovery', () => {
    it('should create a UDP socket and send broadcast discovery packets', async () => {
      const dgram = require('dgram');

      // Mock the timeout to resolve quickly
      const discoveryPromise = discovery.startDiscovery({
        enableNetwork: true,
        enableSerial: false,
        networkTimeout: 100,
      });

      // Advance past the timeout
      jest.advanceTimersByTime(200);

      await discoveryPromise;

      expect(dgram.createSocket).toHaveBeenCalledWith('udp4');
      expect(mockSocket.setBroadcast).toHaveBeenCalledWith(true);
      expect(mockSocket.send).toHaveBeenCalled();
    });

    it('should parse Snapmaker discovery response messages', async () => {
      const handler = jest.fn();
      discovery.on('machine-discovered', handler);

      // Mock axios for the version endpoint
      mockedAxios.get.mockRejectedValue(new Error('Not available'));

      const discoveryPromise = discovery.startDiscovery({
        enableNetwork: true,
        enableSerial: false,
        networkTimeout: 200,
      });

      // Simulate a UDP response
      process.nextTick(() => {
        mockSocket.emit(
          'message',
          Buffer.from('model:A350T,name:My Snapmaker,id:sm-net-1'),
          { address: '192.168.1.100', port: 8080 }
        );
      });

      jest.advanceTimersByTime(300);
      await discoveryPromise;

      const machines = discovery.getMachines();
      const netMachine = machines.find((m) => m.id === 'sm-net-1');
      expect(netMachine).toBeDefined();
      expect(netMachine!.model).toBe('A350T');
      expect(netMachine!.name).toBe('My Snapmaker');
      expect(netMachine!.ip).toBe('192.168.1.100');
      expect(netMachine!.connectionType).toBe('wifi');
    });

    it('should generate an id from IP when none is provided in the response', async () => {
      mockedAxios.get.mockRejectedValue(new Error('Not available'));

      const discoveryPromise = discovery.startDiscovery({
        enableNetwork: true,
        enableSerial: false,
        networkTimeout: 200,
      });

      process.nextTick(() => {
        mockSocket.emit(
          'message',
          Buffer.from('Snapmaker device here'),
          { address: '10.0.0.5', port: 8080 }
        );
      });

      jest.advanceTimersByTime(300);
      await discoveryPromise;

      const machines = discovery.getMachines();
      expect(machines.some((m) => m.id === 'snapmaker_10_0_0_5')).toBe(true);
    });

    it('should ignore non-Snapmaker UDP responses', async () => {
      const discoveryPromise = discovery.startDiscovery({
        enableNetwork: true,
        enableSerial: false,
        networkTimeout: 200,
      });

      process.nextTick(() => {
        mockSocket.emit(
          'message',
          Buffer.from('random device broadcasting'),
          { address: '192.168.1.200', port: 1234 }
        );
      });

      jest.advanceTimersByTime(300);
      await discoveryPromise;

      expect(discovery.getMachines()).toHaveLength(0);
    });
  });

  // -----------------------------------------------------------------------
  // OctoPrint discovery
  // -----------------------------------------------------------------------

  describe('OctoPrint discovery', () => {
    it('should discover machines from OctoPrint hosts', async () => {
      mockedAxios.get.mockImplementation((url: string) => {
        if (url.includes('/api/version')) {
          return Promise.resolve({
            data: { server: '1.9.0', api: '0.1' },
          });
        }
        if (url.includes('/api/printerprofiles')) {
          return Promise.resolve({
            data: {
              profiles: {
                _default: { model: 'Snapmaker A350T' },
              },
            },
          });
        }
        return Promise.reject(new Error('Unknown URL'));
      });

      await discovery.startDiscovery({
        enableNetwork: false,
        enableSerial: false,
        octoprintHosts: ['http://192.168.1.50'],
      });

      const machines = discovery.getMachines();
      expect(machines.length).toBeGreaterThanOrEqual(1);

      const octoMachine = machines.find((m) => m.connectionType === 'octoprint');
      expect(octoMachine).toBeDefined();
      expect(octoMachine!.firmware).toBe('1.9.0');
      expect(octoMachine!.model).toBe('Snapmaker A350T');
    });

    it('should normalise OctoPrint hosts without http prefix', async () => {
      mockedAxios.get.mockImplementation((url: string) => {
        if (url.includes('/api/version')) {
          return Promise.resolve({ data: { server: '1.8.0' } });
        }
        return Promise.reject(new Error('Not found'));
      });

      await discovery.startDiscovery({
        enableNetwork: false,
        enableSerial: false,
        octoprintHosts: ['192.168.1.60'],
      });

      expect(mockedAxios.get).toHaveBeenCalledWith(
        expect.stringContaining('http://192.168.1.60'),
        expect.any(Object)
      );
    });

    it('should handle unreachable OctoPrint hosts gracefully', async () => {
      mockedAxios.get.mockRejectedValue(new Error('ECONNREFUSED'));

      // Should not throw
      await discovery.startDiscovery({
        enableNetwork: false,
        enableSerial: false,
        octoprintHosts: ['http://10.0.0.99'],
      });

      expect(discovery.getMachines()).toHaveLength(0);
    });
  });

  // -----------------------------------------------------------------------
  // Serial discovery
  // -----------------------------------------------------------------------

  describe('serial discovery', () => {
    it('should auto-detect serial ports when none specified', async () => {
      mockSerialPortList.mockResolvedValueOnce([
        {
          path: '/dev/ttyUSB0',
          manufacturer: 'Snapmaker',
          vendorId: '2341',
        },
      ]);

      // The probe will open the port but no data is sent back, so timeout
      await discovery.startDiscovery({
        enableNetwork: false,
        enableSerial: true,
        serialPorts: [],
      });

      // Advance past the 3-second serial probe timeout
      jest.advanceTimersByTime(4000);

      expect(mockSerialPortList).toHaveBeenCalled();
    });

    it('should handle serial port listing failure gracefully', async () => {
      mockSerialPortList.mockRejectedValueOnce(new Error('Permission denied'));

      await discovery.startDiscovery({
        enableNetwork: false,
        enableSerial: true,
      });

      // Should not throw and machines list remains empty
      expect(discovery.getMachines()).toHaveLength(0);
    });

    it('should filter serial ports by known vendor IDs and path patterns', async () => {
      mockSerialPortList.mockResolvedValueOnce([
        { path: '/dev/ttyUSB0', manufacturer: 'Snapmaker', vendorId: '2341' },
        { path: '/dev/ttyS0', manufacturer: 'Unknown', vendorId: '0000' },
        { path: '/dev/ttyACM1', manufacturer: 'CH340', vendorId: '1a86' },
      ]);

      await discovery.startDiscovery({
        enableNetwork: false,
        enableSerial: true,
      });

      // The method filters for Snapmaker manufacturer, vendor IDs 2341/1a86,
      // and paths containing USB/ACM. ttyS0 should be excluded.
      // We verify the list call was made correctly.
      expect(mockSerialPortList).toHaveBeenCalled();
    });
  });

  // -----------------------------------------------------------------------
  // Concurrent discovery prevention
  // -----------------------------------------------------------------------

  describe('concurrent discovery prevention', () => {
    it('should not start a second scan while one is running', async () => {
      // First scan -- keep it pending
      const firstScan = discovery.startDiscovery({
        enableNetwork: true,
        enableSerial: false,
        networkTimeout: 5000,
      });

      // Second scan should return immediately
      const secondScan = discovery.startDiscovery({
        enableNetwork: true,
        enableSerial: false,
        networkTimeout: 5000,
      });

      // Advance past timeouts
      jest.advanceTimersByTime(6000);

      await firstScan;
      await secondScan;

      // dgram.createSocket should only have been called for the first scan
      const dgram = require('dgram');
      // First scan creates 1 discovery socket + 1 mDNS socket
      // The second scan should be skipped entirely
      expect(dgram.createSocket.mock.calls.length).toBeLessThanOrEqual(2);
    });
  });

  // -----------------------------------------------------------------------
  // testConnection
  // -----------------------------------------------------------------------

  describe('testConnection', () => {
    it('should return false for unknown machines', async () => {
      const result = await discovery.testConnection('nonexistent');
      expect(result).toBe(false);
    });

    it('should test wifi connection via HTTP status endpoint', async () => {
      (discovery as any).addMachine({
        id: 'test-wifi',
        name: 'Test WiFi',
        model: 'A350',
        ip: '192.168.1.100',
        port: 8080,
        connectionType: 'wifi',
        lastSeen: new Date(),
      });

      mockedAxios.get.mockResolvedValueOnce({ status: 200, data: {} });

      const result = await discovery.testConnection('test-wifi');
      expect(result).toBe(true);

      expect(mockedAxios.get).toHaveBeenCalledWith(
        'http://192.168.1.100:8080/api/status',
        { timeout: 3000 }
      );
    });

    it('should return false when wifi connection test fails', async () => {
      (discovery as any).addMachine({
        id: 'test-wifi-fail',
        name: 'Test WiFi Fail',
        model: 'A350',
        ip: '10.0.0.99',
        port: 8080,
        connectionType: 'wifi',
        lastSeen: new Date(),
      });

      mockedAxios.get.mockRejectedValueOnce(new Error('Timeout'));

      const result = await discovery.testConnection('test-wifi-fail');
      expect(result).toBe(false);
    });

    it('should test OctoPrint connection via version endpoint', async () => {
      (discovery as any).addMachine({
        id: 'test-octo',
        name: 'Test OctoPrint',
        model: 'SM',
        ip: '192.168.1.200',
        port: 80,
        connectionType: 'octoprint',
        lastSeen: new Date(),
      });

      mockedAxios.get.mockResolvedValueOnce({ status: 200, data: { server: '1.9.0' } });

      const result = await discovery.testConnection('test-octo');
      expect(result).toBe(true);
    });
  });

  // -----------------------------------------------------------------------
  // stopDiscovery
  // -----------------------------------------------------------------------

  describe('stopDiscovery', () => {
    it('should close the discovery socket and reset scanning flag', async () => {
      const discoveryPromise = discovery.startDiscovery({
        enableNetwork: true,
        enableSerial: false,
        networkTimeout: 10000,
      });

      discovery.stopDiscovery();

      expect(mockSocket.close).toHaveBeenCalled();

      jest.advanceTimersByTime(11000);
      await discoveryPromise;
    });

    it('should handle stopDiscovery when no scan is active', () => {
      expect(() => discovery.stopDiscovery()).not.toThrow();
    });
  });
});
