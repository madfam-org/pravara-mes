import { spawn, ChildProcess } from 'child_process';
import * as path from 'path';
import * as fs from 'fs/promises';
import { EventEmitter } from 'events';
import winston from 'winston';

interface LubanOptions {
  lubanPath?: string;
  workspace?: string;
  tempDir?: string;
}

interface SliceResult {
  gcodeFile: string;
  printTime: number;
  filamentUsed: number;
  layerHeight: number;
  layerCount: number;
  boundingBox: {
    min: { x: number; y: number; z: number };
    max: { x: number; y: number; z: number };
  };
}

interface ProjectInfo {
  name: string;
  type: 'printing' | 'laser' | 'cnc';
  material: string;
  machine: string;
  settings: Record<string, any>;
}

export class LubanCLI extends EventEmitter {
  private logger: winston.Logger;
  private lubanProcess: ChildProcess | null = null;
  private options: LubanOptions;

  constructor(logger: winston.Logger, options: LubanOptions = {}) {
    super();
    this.logger = logger;
    this.options = {
      lubanPath: options.lubanPath || process.env.LUBAN_PATH || '/Applications/Luban.app/Contents/MacOS/Luban',
      workspace: options.workspace || path.join(process.cwd(), 'luban-workspace'),
      tempDir: options.tempDir || path.join(process.cwd(), 'temp')
    };
  }

  async initialize(): Promise<void> {
    // Create workspace directories if they don't exist
    await fs.mkdir(this.options.workspace!, { recursive: true });
    await fs.mkdir(this.options.tempDir!, { recursive: true });

    // Verify Luban is accessible
    try {
      await fs.access(this.options.lubanPath!);
      this.logger.info('Luban CLI initialized', { path: this.options.lubanPath });
    } catch (error) {
      this.logger.error('Luban executable not found', { path: this.options.lubanPath });
      throw new Error(`Luban not found at ${this.options.lubanPath}`);
    }
  }

  async sliceSTL(stlFile: string, profileId: string = 'snapmaker-a350'): Promise<SliceResult> {
    this.logger.info('Slicing STL file', { stlFile, profileId });

    const outputFile = path.join(
      this.options.tempDir!,
      `${path.basename(stlFile, '.stl')}_${Date.now()}.gcode`
    );

    return new Promise((resolve, reject) => {
      const args = [
        '--headless',
        '--slice',
        stlFile,
        '--output', outputFile,
        '--profile', profileId,
        '--workspace', this.options.workspace!
      ];

      const luban = spawn(this.options.lubanPath!, args);
      let stdout = '';
      let stderr = '';

      luban.stdout.on('data', (data) => {
        stdout += data.toString();
        this.emit('output', data.toString());
      });

      luban.stderr.on('data', (data) => {
        stderr += data.toString();
        this.emit('error', data.toString());
      });

      luban.on('close', async (code) => {
        if (code !== 0) {
          reject(new Error(`Luban slicing failed with code ${code}: ${stderr}`));
          return;
        }

        // Parse the output for metadata
        const result: SliceResult = await this.parseSliceOutput(outputFile, stdout);
        resolve(result);
      });
    });
  }

  private async parseSliceOutput(gcodeFile: string, output: string): Promise<SliceResult> {
    // Parse Luban output for print details
    const timeMatch = output.match(/Print time:\s*([\d.]+)\s*hours?/);
    const filamentMatch = output.match(/Filament used:\s*([\d.]+)\s*m/);
    const layersMatch = output.match(/Layer count:\s*(\d+)/);
    const heightMatch = output.match(/Layer height:\s*([\d.]+)\s*mm/);

    // Read G-code to extract bounding box
    const gcodeContent = await fs.readFile(gcodeFile, 'utf-8');
    const boundingBox = this.extractBoundingBox(gcodeContent);

    return {
      gcodeFile,
      printTime: timeMatch ? parseFloat(timeMatch[1]) * 60 : 0,
      filamentUsed: filamentMatch ? parseFloat(filamentMatch[1]) : 0,
      layerHeight: heightMatch ? parseFloat(heightMatch[1]) : 0.2,
      layerCount: layersMatch ? parseInt(layersMatch[1]) : 0,
      boundingBox
    };
  }

  private extractBoundingBox(gcode: string): SliceResult['boundingBox'] {
    const lines = gcode.split('\n');
    let minX = Infinity, minY = Infinity, minZ = Infinity;
    let maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;

    for (const line of lines) {
      if (line.startsWith('G0') || line.startsWith('G1')) {
        const xMatch = line.match(/X([-\d.]+)/);
        const yMatch = line.match(/Y([-\d.]+)/);
        const zMatch = line.match(/Z([-\d.]+)/);

        if (xMatch) {
          const x = parseFloat(xMatch[1]);
          minX = Math.min(minX, x);
          maxX = Math.max(maxX, x);
        }
        if (yMatch) {
          const y = parseFloat(yMatch[1]);
          minY = Math.min(minY, y);
          maxY = Math.max(maxY, y);
        }
        if (zMatch) {
          const z = parseFloat(zMatch[1]);
          minZ = Math.min(minZ, z);
          maxZ = Math.max(maxZ, z);
        }
      }
    }

    return {
      min: { x: minX, y: minY, z: minZ },
      max: { x: maxX, y: maxY, z: maxZ }
    };
  }

  async generateToolpath(
    projectFile: string,
    toolType: 'printing' | 'laser' | 'cnc'
  ): Promise<string> {
    this.logger.info('Generating toolpath', { projectFile, toolType });

    const outputFile = path.join(
      this.options.tempDir!,
      `toolpath_${Date.now()}.nc`
    );

    return new Promise((resolve, reject) => {
      const args = [
        '--headless',
        '--generate-toolpath',
        projectFile,
        '--tool', toolType,
        '--output', outputFile,
        '--workspace', this.options.workspace!
      ];

      const luban = spawn(this.options.lubanPath!, args);
      let stderr = '';

      luban.stderr.on('data', (data) => {
        stderr += data.toString();
      });

      luban.on('close', (code) => {
        if (code !== 0) {
          reject(new Error(`Toolpath generation failed: ${stderr}`));
          return;
        }
        resolve(outputFile);
      });
    });
  }

  async importProject(projectPath: string): Promise<ProjectInfo> {
    this.logger.info('Importing Luban project', { projectPath });

    // Extract and parse the Luban project file (.lbn)
    const tempDir = path.join(this.options.tempDir!, `project_${Date.now()}`);
    await fs.mkdir(tempDir, { recursive: true });

    // Luban projects are zip files
    const unzipper = require('unzipper');
    const stream = require('fs').createReadStream(projectPath)
      .pipe(unzipper.Extract({ path: tempDir }));

    return new Promise((resolve, reject) => {
      stream.on('close', async () => {
        try {
          // Read project configuration
          const configPath = path.join(tempDir, 'project.json');
          const configContent = await fs.readFile(configPath, 'utf-8');
          const config = JSON.parse(configContent);

          const projectInfo: ProjectInfo = {
            name: config.name || 'Untitled',
            type: config.machineType || 'printing',
            material: config.material || 'PLA',
            machine: config.machine || 'Snapmaker A350',
            settings: config.settings || {}
          };

          resolve(projectInfo);
        } catch (error) {
          reject(error);
        }
      });

      stream.on('error', reject);
    });
  }

  async exportGCode(
    projectFile: string,
    options: {
      format?: 'gcode' | 'nc';
      includeSupports?: boolean;
      includeThumbnail?: boolean;
    } = {}
  ): Promise<string> {
    this.logger.info('Exporting G-code from project', { projectFile, options });

    const outputFile = path.join(
      this.options.tempDir!,
      `export_${Date.now()}.${options.format || 'gcode'}`
    );

    return new Promise((resolve, reject) => {
      const args = [
        '--headless',
        '--export',
        projectFile,
        '--output', outputFile,
        '--format', options.format || 'gcode'
      ];

      if (options.includeSupports) args.push('--include-supports');
      if (options.includeThumbnail) args.push('--include-thumbnail');

      const luban = spawn(this.options.lubanPath!, args);
      let stderr = '';

      luban.stderr.on('data', (data) => {
        stderr += data.toString();
      });

      luban.on('close', (code) => {
        if (code !== 0) {
          reject(new Error(`Export failed: ${stderr}`));
          return;
        }
        resolve(outputFile);
      });
    });
  }

  async cleanup(): Promise<void> {
    // Clean up temporary files
    try {
      const files = await fs.readdir(this.options.tempDir!);
      const now = Date.now();
      const maxAge = 24 * 60 * 60 * 1000; // 24 hours

      for (const file of files) {
        const filePath = path.join(this.options.tempDir!, file);
        const stats = await fs.stat(filePath);

        if (now - stats.mtime.getTime() > maxAge) {
          await fs.unlink(filePath);
          this.logger.debug('Cleaned up old file', { file });
        }
      }
    } catch (error) {
      this.logger.error('Cleanup error:', error);
    }
  }

  async shutdown(): Promise<void> {
    if (this.lubanProcess) {
      this.lubanProcess.kill();
      this.lubanProcess = null;
    }
    await this.cleanup();
  }
}