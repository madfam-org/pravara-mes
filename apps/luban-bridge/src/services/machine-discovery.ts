import { EventEmitter } from 'events';
import * as dgram from 'dgram';
import * as net from 'net';
import axios from 'axios';
import winston from 'winston';
import { SerialPort } from 'serialport';

interface DiscoveredMachine {
  id: string;
  name: string;
  model: string;
  ip?: string;
  port?: number;
  serialPath?: string;
  connectionType: 'wifi' | 'serial' | 'octoprint';
  firmware?: string;
  capabilities?: string[];
  lastSeen: Date;
}

interface DiscoveryOptions {
  enableNetwork?: boolean;
  enableSerial?: boolean;
  networkTimeout?: number;
  serialPorts?: string[];
  octoprintHosts?: string[];
}

export class MachineDiscovery extends EventEmitter {
  private logger: winston.Logger;
  private machines: Map<string, DiscoveredMachine> = new Map();
  private discoverySocket: dgram.Socket | null = null;
  private isScanning = false;

  constructor(logger: winston.Logger) {
    super();
    this.logger = logger;
  }

  async startDiscovery(options: DiscoveryOptions = {}): Promise<void> {
    if (this.isScanning) {
      this.logger.warn('Discovery already in progress');
      return;
    }

    this.isScanning = true;
    this.logger.info('Starting machine discovery', options);

    const promises: Promise<void>[] = [];

    if (options.enableNetwork !== false) {
      promises.push(this.discoverNetworkMachines(options));
    }

    if (options.enableSerial !== false) {
      promises.push(this.discoverSerialMachines(options));
    }

    if (options.octoprintHosts && options.octoprintHosts.length > 0) {
      promises.push(this.discoverOctoprintMachines(options.octoprintHosts));
    }

    await Promise.all(promises);
    this.isScanning = false;
  }

  private async discoverNetworkMachines(options: DiscoveryOptions): Promise<void> {
    // UDP broadcast discovery for Snapmaker machines
    return new Promise((resolve) => {
      this.discoverySocket = dgram.createSocket('udp4');

      const timeout = setTimeout(() => {
        this.discoverySocket?.close();
        resolve();
      }, options.networkTimeout || 5000);

      this.discoverySocket.on('message', async (msg, rinfo) => {
        const message = msg.toString();
        this.logger.debug('Discovery response', { from: rinfo.address, message });

        // Parse Snapmaker discovery response
        if (message.includes('Snapmaker') || message.includes('model:')) {
          const machine = await this.parseNetworkDiscovery(message, rinfo.address);
          if (machine) {
            this.addMachine(machine);
          }
        }
      });

      this.discoverySocket.on('listening', () => {
        this.discoverySocket!.setBroadcast(true);
        const message = Buffer.from('discover');

        // Send discovery to common ports
        const ports = [8080, 30000, 30001];
        ports.forEach(port => {
          this.discoverySocket!.send(message, port, '255.255.255.255');
        });

        // Also try mDNS/Bonjour style discovery
        this.discoverMDNS();
      });

      this.discoverySocket.bind();
    });
  }

  private async parseNetworkDiscovery(message: string, ip: string): Promise<DiscoveredMachine | null> {
    try {
      // Try to extract machine info from discovery response
      const modelMatch = message.match(/model:([\w\d-]+)/);
      const nameMatch = message.match(/name:([^,]+)/);
      const idMatch = message.match(/id:([^,]+)/);

      const machine: DiscoveredMachine = {
        id: idMatch ? idMatch[1] : `snapmaker_${ip.replace(/\./g, '_')}`,
        name: nameMatch ? nameMatch[1] : `Snapmaker at ${ip}`,
        model: modelMatch ? modelMatch[1] : 'Unknown',
        ip,
        port: 8080,
        connectionType: 'wifi',
        lastSeen: new Date()
      };

      // Try to get more details via HTTP API
      try {
        const response = await axios.get(`http://${ip}:8080/api/version`, { timeout: 2000 });
        if (response.data) {
          machine.firmware = response.data.firmware;
          machine.capabilities = response.data.capabilities;
        }
      } catch (error) {
        // API might not be available
      }

      return machine;
    } catch (error) {
      this.logger.error('Error parsing network discovery', { error, message });
      return null;
    }
  }

  private async discoverMDNS(): Promise<void> {
    // Simplified mDNS discovery for Snapmaker printers
    // They typically advertise as _octoprint._tcp or _snapmaker._tcp

    const socket = dgram.createSocket('udp4');

    const query = Buffer.from([
      0x00, 0x00, // Transaction ID
      0x00, 0x00, // Flags
      0x00, 0x01, // Questions
      0x00, 0x00, // Answer RRs
      0x00, 0x00, // Authority RRs
      0x00, 0x00, // Additional RRs
      // Query for _snapmaker._tcp.local
      0x0a, 0x5f, 0x73, 0x6e, 0x61, 0x70, 0x6d, 0x61, 0x6b, 0x65, 0x72,
      0x04, 0x5f, 0x74, 0x63, 0x70,
      0x05, 0x6c, 0x6f, 0x63, 0x61, 0x6c,
      0x00,
      0x00, 0x0c, // Type: PTR
      0x00, 0x01  // Class: IN
    ]);

    socket.send(query, 5353, '224.0.0.251', () => {
      socket.close();
    });
  }

  private async discoverSerialMachines(options: DiscoveryOptions): Promise<void> {
    let ports: string[] = options.serialPorts || [];

    if (ports.length === 0) {
      // Auto-detect serial ports
      try {
        const availablePorts = await SerialPort.list();
        ports = availablePorts
          .filter(port =>
            port.manufacturer?.includes('Snapmaker') ||
            port.vendorId === '2341' || // Arduino
            port.vendorId === '1a86' || // CH340
            port.path.includes('USB') ||
            port.path.includes('ACM')
          )
          .map(port => port.path);
      } catch (error) {
        this.logger.error('Error listing serial ports', error);
        return;
      }
    }

    for (const portPath of ports) {
      try {
        const machine = await this.probeSerialPort(portPath);
        if (machine) {
          this.addMachine(machine);
        }
      } catch (error) {
        this.logger.debug('Failed to probe serial port', { port: portPath, error });
      }
    }
  }

  private async probeSerialPort(portPath: string): Promise<DiscoveredMachine | null> {
    return new Promise((resolve) => {
      const port = new SerialPort({
        path: portPath,
        baudRate: 115200,
        autoOpen: false
      });

      const timeout = setTimeout(() => {
        port.close();
        resolve(null);
      }, 3000);

      port.open((err) => {
        if (err) {
          clearTimeout(timeout);
          resolve(null);
          return;
        }

        let response = '';

        port.on('data', (data) => {
          response += data.toString();

          // Check if this is a Snapmaker or compatible printer
          if (response.includes('Snapmaker') ||
              response.includes('Marlin') ||
              response.includes('ok')) {

            clearTimeout(timeout);

            const machine: DiscoveredMachine = {
              id: `serial_${portPath.replace(/[\/\\]/g, '_')}`,
              name: `Snapmaker on ${portPath}`,
              model: 'Snapmaker',
              serialPath: portPath,
              connectionType: 'serial',
              lastSeen: new Date()
            };

            // Try to extract model from firmware string
            const modelMatch = response.match(/Snapmaker\s+(\w+)/);
            if (modelMatch) {
              machine.model = modelMatch[1];
            }

            port.close(() => {
              resolve(machine);
            });
          }
        });

        // Send M105 to get temperature (most printers respond to this)
        port.write('M105\n');
        // Also try M115 for firmware info
        port.write('M115\n');
      });
    });
  }

  private async discoverOctoprintMachines(hosts: string[]): Promise<void> {
    const promises = hosts.map(async (host) => {
      try {
        // Normalize host (add http if missing)
        if (!host.startsWith('http')) {
          host = `http://${host}`;
        }

        const response = await axios.get(`${host}/api/version`, { timeout: 3000 });

        if (response.data) {
          const machine: DiscoveredMachine = {
            id: `octoprint_${host.replace(/[^a-zA-Z0-9]/g, '_')}`,
            name: `OctoPrint at ${host}`,
            model: 'OctoPrint',
            ip: new URL(host).hostname,
            port: parseInt(new URL(host).port) || 80,
            connectionType: 'octoprint',
            firmware: response.data.server,
            capabilities: ['octoprint'],
            lastSeen: new Date()
          };

          // Try to get printer profile
          try {
            const printerResponse = await axios.get(`${host}/api/printerprofiles`);
            if (printerResponse.data?.profiles) {
              const profile = Object.values(printerResponse.data.profiles)[0] as any;
              if (profile?.model) {
                machine.model = profile.model;
              }
            }
          } catch (error) {
            // Printer profile might not be accessible without API key
          }

          this.addMachine(machine);
        }
      } catch (error) {
        this.logger.debug('Failed to discover OctoPrint', { host, error });
      }
    });

    await Promise.all(promises);
  }

  private addMachine(machine: DiscoveredMachine) {
    const existing = this.machines.get(machine.id);
    if (existing) {
      // Update last seen time
      existing.lastSeen = new Date();
    } else {
      this.machines.set(machine.id, machine);
      this.emit('machine-discovered', machine);
      this.logger.info('Machine discovered', {
        id: machine.id,
        name: machine.name,
        type: machine.connectionType
      });
    }
  }

  getMachines(): DiscoveredMachine[] {
    return Array.from(this.machines.values());
  }

  getMachine(id: string): DiscoveredMachine | undefined {
    return this.machines.get(id);
  }

  removeStaleMachines(maxAge: number = 30000): void {
    const now = Date.now();
    const stale: string[] = [];

    this.machines.forEach((machine, id) => {
      if (now - machine.lastSeen.getTime() > maxAge) {
        stale.push(id);
      }
    });

    stale.forEach(id => {
      const machine = this.machines.get(id);
      this.machines.delete(id);
      this.emit('machine-lost', machine);
      this.logger.info('Machine lost', { id, name: machine?.name });
    });
  }

  async testConnection(machineId: string): Promise<boolean> {
    const machine = this.machines.get(machineId);
    if (!machine) return false;

    try {
      switch (machine.connectionType) {
        case 'wifi':
          const response = await axios.get(`http://${machine.ip}:${machine.port}/api/status`, {
            timeout: 3000
          });
          return response.status === 200;

        case 'serial':
          // Quick serial port test
          return new Promise((resolve) => {
            const port = new SerialPort({
              path: machine.serialPath!,
              baudRate: 115200,
              autoOpen: false
            });

            port.open((err) => {
              if (err) {
                resolve(false);
              } else {
                port.close(() => resolve(true));
              }
            });
          });

        case 'octoprint':
          const octoprintResponse = await axios.get(
            `http://${machine.ip}:${machine.port}/api/version`,
            { timeout: 3000 }
          );
          return octoprintResponse.status === 200;

        default:
          return false;
      }
    } catch (error) {
      this.logger.debug('Connection test failed', { machineId, error });
      return false;
    }
  }

  stopDiscovery(): void {
    if (this.discoverySocket) {
      this.discoverySocket.close();
      this.discoverySocket = null;
    }
    this.isScanning = false;
  }
}