import winston from 'winston';

interface GCodeCommand {
  type: string;
  code: string;
  params: Record<string, number>;
  comment?: string;
  line: number;
}

interface AnalysisResult {
  totalLines: number;
  commands: number;
  comments: number;
  printTime: number; // minutes
  layerCount: number;
  layerHeight: number;
  filamentUsed: number; // mm
  boundingBox: {
    min: { x: number; y: number; z: number };
    max: { x: number; y: number; z: number };
  };
  temperatures: {
    bed: number;
    nozzle: number;
    chamber?: number;
  };
  speeds: {
    travel: number;
    print: number;
    retract: number;
  };
  features: {
    hasSupports: boolean;
    hasRaft: boolean;
    hasBrim: boolean;
    hasWipeSequence: boolean;
    hasToolChange: boolean;
    hasEnclosureControl: boolean;
  };
  toolhead: 'printing' | 'laser' | 'cnc';
  snapmakerSpecific: {
    moduleType?: string;
    enclosureDoor?: boolean;
    airPurifier?: boolean;
    rotaryModule?: boolean;
  };
}

export class GCodeAnalyzer {
  private logger: winston.Logger;

  constructor(logger: winston.Logger) {
    this.logger = logger;
  }

  analyze(gcode: string): AnalysisResult {
    const lines = gcode.split('\n');
    const result: AnalysisResult = {
      totalLines: lines.length,
      commands: 0,
      comments: 0,
      printTime: 0,
      layerCount: 0,
      layerHeight: 0.2,
      filamentUsed: 0,
      boundingBox: {
        min: { x: Infinity, y: Infinity, z: Infinity },
        max: { x: -Infinity, y: -Infinity, z: -Infinity }
      },
      temperatures: {
        bed: 0,
        nozzle: 0
      },
      speeds: {
        travel: 0,
        print: 0,
        retract: 0
      },
      features: {
        hasSupports: false,
        hasRaft: false,
        hasBrim: false,
        hasWipeSequence: false,
        hasToolChange: false,
        hasEnclosureControl: false
      },
      toolhead: 'printing',
      snapmakerSpecific: {}
    };

    let currentPosition = { x: 0, y: 0, z: 0, e: 0 };
    let lastPosition = { ...currentPosition };
    let currentSpeed = 0;
    let isExtruding = false;
    let currentLayer = 0;

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i].trim();
      if (!line) continue;

      // Parse comments
      if (line.startsWith(';')) {
        result.comments++;
        this.parseComment(line, result);
        continue;
      }

      // Parse command
      const command = this.parseGCodeLine(line, i);
      if (!command) continue;

      result.commands++;

      // Track position and movement
      switch (command.type) {
        case 'G0': // Rapid move
        case 'G1': // Linear move
          lastPosition = { ...currentPosition };

          if ('X' in command.params) {
            currentPosition.x = command.params.X;
            result.boundingBox.min.x = Math.min(result.boundingBox.min.x, currentPosition.x);
            result.boundingBox.max.x = Math.max(result.boundingBox.max.x, currentPosition.x);
          }
          if ('Y' in command.params) {
            currentPosition.y = command.params.Y;
            result.boundingBox.min.y = Math.min(result.boundingBox.min.y, currentPosition.y);
            result.boundingBox.max.y = Math.max(result.boundingBox.max.y, currentPosition.y);
          }
          if ('Z' in command.params) {
            currentPosition.z = command.params.Z;
            result.boundingBox.min.z = Math.min(result.boundingBox.min.z, currentPosition.z);
            result.boundingBox.max.z = Math.max(result.boundingBox.max.z, currentPosition.z);

            // Detect layer change
            if (currentPosition.z > lastPosition.z) {
              currentLayer++;
              result.layerHeight = currentPosition.z - lastPosition.z;
            }
          }
          if ('E' in command.params) {
            const extrusionAmount = command.params.E - currentPosition.e;
            if (extrusionAmount > 0) {
              result.filamentUsed += extrusionAmount;
              isExtruding = true;
            } else {
              isExtruding = false;
            }
            currentPosition.e = command.params.E;
          }
          if ('F' in command.params) {
            currentSpeed = command.params.F;
            if (isExtruding) {
              result.speeds.print = Math.max(result.speeds.print, currentSpeed);
            } else {
              result.speeds.travel = Math.max(result.speeds.travel, currentSpeed);
            }
          }
          break;

        case 'G10': // Retract
          result.speeds.retract = Math.max(result.speeds.retract, currentSpeed);
          break;

        case 'G28': // Home
          currentPosition = { x: 0, y: 0, z: 0, e: 0 };
          break;

        case 'M104': // Set extruder temperature
        case 'M109': // Set extruder temperature and wait
          if ('S' in command.params) {
            result.temperatures.nozzle = command.params.S;
          }
          break;

        case 'M140': // Set bed temperature
        case 'M190': // Set bed temperature and wait
          if ('S' in command.params) {
            result.temperatures.bed = command.params.S;
          }
          break;

        case 'M1010': // Snapmaker enclosure control
          result.features.hasEnclosureControl = true;
          result.snapmakerSpecific.enclosureDoor = true;
          break;

        case 'M1011': // Snapmaker air purifier
          result.snapmakerSpecific.airPurifier = true;
          break;

        case 'M1012': // Snapmaker rotary module
          result.snapmakerSpecific.rotaryModule = true;
          break;

        case 'M605': // Snapmaker tool head change
          result.features.hasToolChange = true;
          this.detectToolhead(command, result);
          break;

        case 'M106': // Fan on
          if (command.params.P === 2) {
            // Chamber fan for Snapmaker
            result.snapmakerSpecific.enclosureDoor = true;
          }
          break;

        case 'T0':
        case 'T1': // Tool change
          result.features.hasToolChange = true;
          break;
      }
    }

    result.layerCount = currentLayer;

    // Estimate print time based on movements and speeds
    result.printTime = this.estimatePrintTime(result);

    // Detect features from patterns
    this.detectPrintFeatures(result, lines);

    return result;
  }

  private parseGCodeLine(line: string, lineNumber: number): GCodeCommand | null {
    // Remove comments from the line
    const commentIndex = line.indexOf(';');
    const code = commentIndex >= 0 ? line.substring(0, commentIndex).trim() : line.trim();
    const comment = commentIndex >= 0 ? line.substring(commentIndex + 1).trim() : undefined;

    if (!code) return null;

    // Match G-code pattern
    const match = code.match(/^([GM])(\d+)(.*)/);
    if (!match) return null;

    const type = match[1] + match[2];
    const params: Record<string, number> = {};

    // Parse parameters
    const paramString = match[3];
    const paramMatches = paramString.matchAll(/([A-Z])([-\d.]+)/g);

    for (const paramMatch of paramMatches) {
      params[paramMatch[1]] = parseFloat(paramMatch[2]);
    }

    return {
      type,
      code,
      params,
      comment,
      line: lineNumber
    };
  }

  private parseComment(comment: string, result: AnalysisResult) {
    // Parse Snapmaker-specific comments
    if (comment.includes('LAYER:')) {
      const match = comment.match(/LAYER:(\d+)/);
      if (match) {
        result.layerCount = Math.max(result.layerCount, parseInt(match[1]) + 1);
      }
    }

    if (comment.includes('TIME:')) {
      const match = comment.match(/TIME:(\d+)/);
      if (match) {
        result.printTime = parseInt(match[1]) / 60; // Convert seconds to minutes
      }
    }

    if (comment.includes('Filament used:')) {
      const match = comment.match(/Filament used: ([\d.]+)m/);
      if (match) {
        result.filamentUsed = parseFloat(match[1]) * 1000; // Convert m to mm
      }
    }

    if (comment.includes('SUPPORT')) {
      result.features.hasSupports = true;
    }

    if (comment.includes('RAFT')) {
      result.features.hasRaft = true;
    }

    if (comment.includes('BRIM')) {
      result.features.hasBrim = true;
    }

    if (comment.includes('WIPE')) {
      result.features.hasWipeSequence = true;
    }

    // Snapmaker module detection
    if (comment.includes('Module:')) {
      const match = comment.match(/Module: (\w+)/);
      if (match) {
        result.snapmakerSpecific.moduleType = match[1];
        if (match[1].toLowerCase().includes('laser')) {
          result.toolhead = 'laser';
        } else if (match[1].toLowerCase().includes('cnc')) {
          result.toolhead = 'cnc';
        }
      }
    }
  }

  private detectToolhead(command: GCodeCommand, result: AnalysisResult) {
    // Snapmaker M605 command for tool head type
    if (command.params.S === 0) {
      result.toolhead = 'printing';
    } else if (command.params.S === 1) {
      result.toolhead = 'laser';
    } else if (command.params.S === 2) {
      result.toolhead = 'cnc';
    }
  }

  private detectPrintFeatures(result: AnalysisResult, lines: string[]) {
    // Look for patterns that indicate specific features
    for (const line of lines) {
      if (line.includes('support') || line.includes('SUPPORT')) {
        result.features.hasSupports = true;
      }
      if (line.includes('raft') || line.includes('RAFT')) {
        result.features.hasRaft = true;
      }
      if (line.includes('brim') || line.includes('BRIM')) {
        result.features.hasBrim = true;
      }
      if (line.includes('wipe') || line.includes('WIPE')) {
        result.features.hasWipeSequence = true;
      }
    }
  }

  private estimatePrintTime(result: AnalysisResult): number {
    // Simple time estimation based on filament used and speeds
    if (result.printTime > 0) {
      return result.printTime; // Use embedded time if available
    }

    // Estimate based on filament and average speed
    const avgSpeed = (result.speeds.print + result.speeds.travel) / 2 || 3000;
    const speedMmPerMin = avgSpeed / 60;

    // Rough estimation: filament length / speed + overhead
    const printTime = (result.filamentUsed / speedMmPerMin) * 1.2; // 20% overhead

    return Math.round(printTime);
  }

  validateGCode(gcode: string): { valid: boolean; errors: string[] } {
    const errors: string[] = [];
    const lines = gcode.split('\n');
    let hasEndCode = false;
    let hasStartCode = false;

    for (let i = 0; i < Math.min(50, lines.length); i++) {
      if (lines[i].includes('G28') || lines[i].includes('M104') || lines[i].includes('M140')) {
        hasStartCode = true;
        break;
      }
    }

    for (let i = Math.max(0, lines.length - 50); i < lines.length; i++) {
      if (lines[i].includes('M104 S0') || lines[i].includes('M140 S0') || lines[i].includes('M84')) {
        hasEndCode = true;
        break;
      }
    }

    if (!hasStartCode) {
      errors.push('Missing start G-code sequence');
    }

    if (!hasEndCode) {
      errors.push('Missing end G-code sequence');
    }

    // Check for Snapmaker-specific requirements
    const analysis = this.analyze(gcode);

    if (analysis.boundingBox.max.x > 320 || analysis.boundingBox.max.y > 350 || analysis.boundingBox.max.z > 330) {
      errors.push('Print exceeds Snapmaker A350 build volume');
    }

    if (analysis.temperatures.nozzle > 275) {
      errors.push('Nozzle temperature exceeds Snapmaker maximum (275°C)');
    }

    if (analysis.temperatures.bed > 110) {
      errors.push('Bed temperature exceeds Snapmaker maximum (110°C)');
    }

    return {
      valid: errors.length === 0,
      errors
    };
  }

  optimizeGCode(gcode: string): string {
    const lines = gcode.split('\n');
    const optimized: string[] = [];
    let lastCommand = '';
    let consecutiveComments = 0;

    for (const line of lines) {
      const trimmed = line.trim();

      // Remove redundant commands
      if (trimmed === lastCommand && !trimmed.startsWith(';')) {
        continue;
      }

      // Limit consecutive comments
      if (trimmed.startsWith(';')) {
        consecutiveComments++;
        if (consecutiveComments > 3 && !trimmed.includes('LAYER') && !trimmed.includes('TIME')) {
          continue;
        }
      } else {
        consecutiveComments = 0;
      }

      // Remove unnecessary precision
      const optimizedLine = trimmed.replace(/(\d+\.\d{4})\d+/g, '$1');

      optimized.push(optimizedLine);
      lastCommand = trimmed;
    }

    return optimized.join('\n');
  }
}