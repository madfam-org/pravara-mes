import { SerialPort } from 'serialport';
import { ReadlineParser } from '@serialport/parser-readline';
import { EventEmitter } from 'events';
import winston from 'winston';
import axios from 'axios';

interface MachineInfo {
  id: string;
  model: string;
  firmware: string;
  status: 'idle' | 'printing' | 'paused' | 'error' | 'offline';
  position: { x: number; y: number; z: number };
  temperatures: {
    bed: { current: number; target: number };
    nozzle: { current: number; target: number };
    chamber?: { current: number; target: number };
  };
  progress?: {
    percent: number;
    timeElapsed: number;
    timeRemaining: number;
  };
  toolhead: 'printing' | 'laser' | 'cnc';
}

interface ConnectionOptions {
  type: 'serial' | 'wifi' | 'octoprint';
  serialPath?: string;
  ipAddress?: string;
  apiKey?: string;
  baudRate?: number;
}

export class SnapmakerProtocol extends EventEmitter {
  private logger: winston.Logger;
  private connections: Map<string, SerialPort | any> = new Map();
  private machines: Map<string, MachineInfo> = new Map();
  private parsers: Map<string, ReadlineParser> = new Map();

  constructor(logger: winston.Logger) {
    super();
    this.logger = logger;
  }

  async connect(machineId: string, options: ConnectionOptions): Promise<void> {
    this.logger.info('Connecting to Snapmaker', { machineId, type: options.type });

    switch (options.type) {
      case 'serial':
        await this.connectSerial(machineId, options);
        break;
      case 'wifi':
        await this.connectWifi(machineId, options);
        break;
      case 'octoprint':
        await this.connectOctoprint(machineId, options);
        break;
      default:
        throw new Error(`Unknown connection type: ${options.type}`);
    }
  }

  private async connectSerial(machineId: string, options: ConnectionOptions): Promise<void> {
    const port = new SerialPort({
      path: options.serialPath!,
      baudRate: options.baudRate || 115200,
      autoOpen: false
    });

    return new Promise((resolve, reject) => {
      port.open((err) => {
        if (err) {
          this.logger.error('Serial connection failed', { machineId, error: err.message });
          reject(err);
          return;
        }

        const parser = port.pipe(new ReadlineParser({ delimiter: '\n' }));
        this.connections.set(machineId, port);
        this.parsers.set(machineId, parser);

        // Handle incoming data
        parser.on('data', (data: string) => {
          this.handleResponse(machineId, data);
        });

        // Initialize machine info
        this.machines.set(machineId, {
          id: machineId,
          model: 'Unknown',
          firmware: 'Unknown',
          status: 'idle',
          position: { x: 0, y: 0, z: 0 },
          temperatures: {
            bed: { current: 0, target: 0 },
            nozzle: { current: 0, target: 0 }
          },
          toolhead: 'printing'
        });

        // Get machine info
        this.sendCommand(machineId, 'M1005');

        this.logger.info('Serial connection established', { machineId });
        resolve();
      });
    });
  }

  private async connectWifi(machineId: string, options: ConnectionOptions): Promise<void> {
    // WiFi connection via HTTP API
    const baseUrl = `http://${options.ipAddress}:8080/api`;

    try {
      // Test connection
      const response = await axios.get(`${baseUrl}/status`);

      this.connections.set(machineId, {
        type: 'wifi',
        baseUrl,
        apiKey: options.apiKey
      });

      this.machines.set(machineId, {
        id: machineId,
        model: response.data.model || 'Snapmaker',
        firmware: response.data.firmware || 'Unknown',
        status: response.data.state || 'idle',
        position: response.data.position || { x: 0, y: 0, z: 0 },
        temperatures: response.data.temperatures || {
          bed: { current: 0, target: 0 },
          nozzle: { current: 0, target: 0 }
        },
        toolhead: response.data.toolhead || 'printing'
      });

      this.logger.info('WiFi connection established', { machineId, ip: options.ipAddress });

      // Start polling for status updates
      this.startStatusPolling(machineId);
    } catch (error) {
      this.logger.error('WiFi connection failed', { machineId, error });
      throw error;
    }
  }

  private async connectOctoprint(machineId: string, options: ConnectionOptions): Promise<void> {
    const baseUrl = `http://${options.ipAddress}/api`;
    const headers = { 'X-Api-Key': options.apiKey };

    try {
      // Test connection and get printer info
      const response = await axios.get(`${baseUrl}/printer`, { headers });

      this.connections.set(machineId, {
        type: 'octoprint',
        baseUrl,
        headers
      });

      this.machines.set(machineId, {
        id: machineId,
        model: 'Snapmaker (OctoPrint)',
        firmware: 'OctoPrint',
        status: response.data.state.flags.printing ? 'printing' : 'idle',
        position: { x: 0, y: 0, z: 0 },
        temperatures: {
          bed: response.data.temperature.bed || { current: 0, target: 0 },
          nozzle: response.data.temperature.tool0 || { current: 0, target: 0 }
        },
        toolhead: 'printing'
      });

      this.logger.info('OctoPrint connection established', { machineId });

      // Start polling
      this.startStatusPolling(machineId);
    } catch (error) {
      this.logger.error('OctoPrint connection failed', { machineId, error });
      throw error;
    }
  }

  async disconnect(machineId: string): Promise<void> {
    const connection = this.connections.get(machineId);
    if (!connection) return;

    if (connection instanceof SerialPort) {
      return new Promise((resolve) => {
        connection.close(() => {
          this.connections.delete(machineId);
          this.machines.delete(machineId);
          this.parsers.delete(machineId);
          resolve();
        });
      });
    } else {
      // WiFi or OctoPrint connection
      this.connections.delete(machineId);
      this.machines.delete(machineId);
    }
  }

  async sendCommand(machineId: string, command: string): Promise<string> {
    const connection = this.connections.get(machineId);
    if (!connection) {
      throw new Error(`Machine ${machineId} not connected`);
    }

    this.logger.debug('Sending command', { machineId, command });

    if (connection instanceof SerialPort) {
      return this.sendSerialCommand(connection, command);
    } else if (connection.type === 'wifi') {
      return this.sendWifiCommand(connection, command);
    } else if (connection.type === 'octoprint') {
      return this.sendOctoprintCommand(connection, command);
    }

    throw new Error('Unknown connection type');
  }

  private sendSerialCommand(port: SerialPort, command: string): Promise<string> {
    return new Promise((resolve, reject) => {
      let response = '';
      const timeout = setTimeout(() => {
        reject(new Error('Command timeout'));
      }, 5000);

      const parser = this.parsers.get(Array.from(this.connections.keys())
        .find(key => this.connections.get(key) === port)!);

      if (!parser) {
        reject(new Error('Parser not found'));
        return;
      }

      const listener = (data: string) => {
        response += data + '\n';
        if (data.includes('ok') || data.includes('error')) {
          clearTimeout(timeout);
          parser.off('data', listener);
          resolve(response);
        }
      };

      parser.on('data', listener);
      port.write(command + '\n');
    });
  }

  private async sendWifiCommand(connection: any, command: string): Promise<string> {
    try {
      const response = await axios.post(
        `${connection.baseUrl}/gcode`,
        { command },
        { headers: { 'X-Api-Key': connection.apiKey } }
      );
      return response.data.response || 'ok';
    } catch (error) {
      throw new Error(`WiFi command failed: ${error}`);
    }
  }

  private async sendOctoprintCommand(connection: any, command: string): Promise<string> {
    try {
      const response = await axios.post(
        `${connection.baseUrl}/printer/command`,
        { command },
        { headers: connection.headers }
      );
      return 'ok';
    } catch (error) {
      throw new Error(`OctoPrint command failed: ${error}`);
    }
  }

  async executeGCode(machineId: string, gcode: string): Promise<void> {
    const lines = gcode.split('\n').filter(line =>
      line && !line.startsWith(';') && line.trim() !== ''
    );

    for (const line of lines) {
      await this.sendCommand(machineId, line);

      // Add delay for certain commands
      if (line.startsWith('G28') || line.startsWith('G29')) {
        await new Promise(resolve => setTimeout(resolve, 1000));
      }
    }
  }

  async uploadFile(machineId: string, filename: string, content: string): Promise<void> {
    const connection = this.connections.get(machineId);
    if (!connection) {
      throw new Error(`Machine ${machineId} not connected`);
    }

    if (connection.type === 'octoprint') {
      const formData = new FormData();
      formData.append('file', new Blob([content]), filename);

      await axios.post(
        `${connection.baseUrl}/files/local`,
        formData,
        { headers: { ...connection.headers, 'Content-Type': 'multipart/form-data' } }
      );
    } else if (connection.type === 'wifi') {
      await axios.post(
        `${connection.baseUrl}/files/upload`,
        { filename, content },
        { headers: { 'X-Api-Key': connection.apiKey } }
      );
    } else {
      // For serial, we need to use M28/M29 commands
      await this.sendCommand(machineId, `M28 ${filename}`);
      await this.executeGCode(machineId, content);
      await this.sendCommand(machineId, 'M29');
    }
  }

  async startPrint(machineId: string, filename: string): Promise<void> {
    const connection = this.connections.get(machineId);
    if (!connection) {
      throw new Error(`Machine ${machineId} not connected`);
    }

    if (connection.type === 'octoprint') {
      await axios.post(
        `${connection.baseUrl}/files/local/${filename}`,
        { command: 'select', print: true },
        { headers: connection.headers }
      );
    } else {
      await this.sendCommand(machineId, `M23 ${filename}`);
      await this.sendCommand(machineId, 'M24');
    }

    const machine = this.machines.get(machineId);
    if (machine) {
      machine.status = 'printing';
    }
  }

  async pausePrint(machineId: string): Promise<void> {
    await this.sendCommand(machineId, 'M25');
    const machine = this.machines.get(machineId);
    if (machine) {
      machine.status = 'paused';
    }
  }

  async resumePrint(machineId: string): Promise<void> {
    await this.sendCommand(machineId, 'M24');
    const machine = this.machines.get(machineId);
    if (machine) {
      machine.status = 'printing';
    }
  }

  async cancelPrint(machineId: string): Promise<void> {
    await this.sendCommand(machineId, 'M0');
    const machine = this.machines.get(machineId);
    if (machine) {
      machine.status = 'idle';
      machine.progress = undefined;
    }
  }

  async homeAxes(machineId: string, axes: string = 'XYZ'): Promise<void> {
    await this.sendCommand(machineId, `G28 ${axes}`);
  }

  async setTemperature(
    machineId: string,
    target: 'bed' | 'nozzle',
    temperature: number
  ): Promise<void> {
    const command = target === 'bed'
      ? `M140 S${temperature}`
      : `M104 S${temperature}`;

    await this.sendCommand(machineId, command);

    const machine = this.machines.get(machineId);
    if (machine) {
      machine.temperatures[target].target = temperature;
    }
  }

  getMachineInfo(machineId: string): MachineInfo | undefined {
    return this.machines.get(machineId);
  }

  getAllMachines(): MachineInfo[] {
    return Array.from(this.machines.values());
  }

  private handleResponse(machineId: string, response: string) {
    const machine = this.machines.get(machineId);
    if (!machine) return;

    // Parse temperature responses
    if (response.startsWith('T:')) {
      const tempMatch = response.match(/T:([\d.]+) \/(\d+) B:([\d.]+) \/(\d+)/);
      if (tempMatch) {
        machine.temperatures.nozzle.current = parseFloat(tempMatch[1]);
        machine.temperatures.nozzle.target = parseInt(tempMatch[2]);
        machine.temperatures.bed.current = parseFloat(tempMatch[3]);
        machine.temperatures.bed.target = parseInt(tempMatch[4]);
      }
    }

    // Parse position responses
    if (response.includes('X:')) {
      const posMatch = response.match(/X:([-\d.]+) Y:([-\d.]+) Z:([-\d.]+)/);
      if (posMatch) {
        machine.position = {
          x: parseFloat(posMatch[1]),
          y: parseFloat(posMatch[2]),
          z: parseFloat(posMatch[3])
        };
      }
    }

    // Parse progress
    if (response.startsWith('SD printing byte')) {
      const progressMatch = response.match(/(\d+)\/(\d+)/);
      if (progressMatch) {
        const percent = (parseInt(progressMatch[1]) / parseInt(progressMatch[2])) * 100;
        machine.progress = {
          percent,
          timeElapsed: 0,
          timeRemaining: 0
        };
      }
    }

    this.emit('machine-update', machine);
  }

  private startStatusPolling(machineId: string) {
    setInterval(async () => {
      try {
        await this.updateMachineStatus(machineId);
      } catch (error) {
        this.logger.error('Status polling error', { machineId, error });
      }
    }, 2000); // Poll every 2 seconds
  }

  private async updateMachineStatus(machineId: string) {
    const connection = this.connections.get(machineId);
    const machine = this.machines.get(machineId);
    if (!connection || !machine) return;

    if (connection.type === 'wifi') {
      const response = await axios.get(`${connection.baseUrl}/status`);
      Object.assign(machine, response.data);
    } else if (connection.type === 'octoprint') {
      const response = await axios.get(`${connection.baseUrl}/printer`, {
        headers: connection.headers
      });

      if (response.data.temperature) {
        machine.temperatures = {
          bed: response.data.temperature.bed || { current: 0, target: 0 },
          nozzle: response.data.temperature.tool0 || { current: 0, target: 0 }
        };
      }

      machine.status = response.data.state.flags.printing ? 'printing' : 'idle';
    } else {
      // Serial connection - send M105 for temperature
      await this.sendCommand(machineId, 'M105');
      await this.sendCommand(machineId, 'M114'); // Position
    }

    this.emit('machine-update', machine);
  }
}