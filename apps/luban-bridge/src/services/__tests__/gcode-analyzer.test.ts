import { GCodeAnalyzer } from '../gcode-analyzer';
import winston from 'winston';

describe('GCodeAnalyzer', () => {
  let analyzer: GCodeAnalyzer;
  let logger: winston.Logger;

  beforeEach(() => {
    logger = winston.createLogger({
      silent: true // Disable logging during tests
    });
    analyzer = new GCodeAnalyzer(logger);
  });

  describe('analyze', () => {
    it('should parse basic movement commands', () => {
      const gcode = `
        G28 ; Home all axes
        G1 X100 Y100 Z10 F3000
        G1 X150 Y150 Z10 E5
        M104 S210 ; Set nozzle temperature
        M140 S60 ; Set bed temperature
      `;

      const result = analyzer.analyze(gcode);

      expect(result.commands).toBeGreaterThan(0);
      expect(result.temperatures.nozzle).toBe(210);
      expect(result.temperatures.bed).toBe(60);
      expect(result.boundingBox.max.x).toBe(150);
      expect(result.boundingBox.max.y).toBe(150);
    });

    it('should detect Snapmaker-specific commands', () => {
      const gcode = `
        M1010 S1 ; Enable enclosure
        M1011 S255 ; Set air purifier to max
        M1012 ; Enable rotary module
        M605 S1 ; Switch to laser module
      `;

      const result = analyzer.analyze(gcode);

      expect(result.features.hasEnclosureControl).toBe(true);
      expect(result.snapmakerSpecific.enclosureDoor).toBe(true);
      expect(result.snapmakerSpecific.airPurifier).toBe(true);
      expect(result.snapmakerSpecific.rotaryModule).toBe(true);
      expect(result.toolhead).toBe('laser');
    });

    it('should calculate filament usage', () => {
      const gcode = `
        G1 X100 Y100 E10
        G1 X150 Y150 E20
        G1 X200 Y200 E30
      `;

      const result = analyzer.analyze(gcode);

      expect(result.filamentUsed).toBe(30);
    });

    it('should detect layer changes', () => {
      const gcode = `
        G1 X100 Y100 Z0.2
        G1 X150 Y150 Z0.2
        G1 X100 Y100 Z0.4 ; Layer 2
        G1 X150 Y150 Z0.4
        G1 X100 Y100 Z0.6 ; Layer 3
      `;

      const result = analyzer.analyze(gcode);

      expect(result.layerCount).toBe(3);
      expect(result.layerHeight).toBeCloseTo(0.2);
    });

    it('should detect print features', () => {
      const gcode = `
        ; SUPPORT START
        G1 X100 Y100 Z0.2
        ; SUPPORT END
        ; RAFT
        G1 X0 Y0 Z0.1
        ; BRIM
        G1 X200 Y200 Z0.2
      `;

      const result = analyzer.analyze(gcode);

      expect(result.features.hasSupports).toBe(true);
      expect(result.features.hasRaft).toBe(true);
      expect(result.features.hasBrim).toBe(true);
    });
  });

  describe('validateGCode', () => {
    it('should validate correct G-code', () => {
      const gcode = `
        G28 ; Home
        M104 S210
        M140 S60
        G1 X100 Y100 Z10
        M104 S0 ; Turn off nozzle
        M140 S0 ; Turn off bed
        M84 ; Disable motors
      `;

      const validation = analyzer.validateGCode(gcode);

      expect(validation.valid).toBe(true);
      expect(validation.errors).toHaveLength(0);
    });

    it('should detect missing start code', () => {
      const gcode = `
        G1 X100 Y100 Z10
        M104 S0
        M140 S0
        M84
      `;

      const validation = analyzer.validateGCode(gcode);

      expect(validation.valid).toBe(false);
      expect(validation.errors).toContain('Missing start G-code sequence');
    });

    it('should detect build volume violations', () => {
      const gcode = `
        G28
        G1 X400 Y400 Z400 ; Exceeds A350 build volume
        M104 S0
        M140 S0
        M84
      `;

      const validation = analyzer.validateGCode(gcode);

      expect(validation.valid).toBe(false);
      expect(validation.errors).toContain('Print exceeds Snapmaker A350 build volume');
    });

    it('should detect temperature limit violations', () => {
      const gcode = `
        G28
        M104 S300 ; Exceeds max nozzle temp
        M140 S120 ; Exceeds max bed temp
        M104 S0
        M140 S0
        M84
      `;

      const validation = analyzer.validateGCode(gcode);

      expect(validation.valid).toBe(false);
      expect(validation.errors).toContain('Nozzle temperature exceeds Snapmaker maximum (275°C)');
      expect(validation.errors).toContain('Bed temperature exceeds Snapmaker maximum (110°C)');
    });
  });

  describe('optimizeGCode', () => {
    it('should remove redundant commands', () => {
      const gcode = `
        G1 X100 Y100
        G1 X100 Y100
        G1 X150 Y150
        G1 X150 Y150
      `;

      const optimized = analyzer.optimizeGCode(gcode);
      const lines = optimized.split('\n').filter(line => line.trim());

      expect(lines).toHaveLength(2);
      expect(lines[0]).toBe('G1 X100 Y100');
      expect(lines[1]).toBe('G1 X150 Y150');
    });

    it('should limit excessive precision', () => {
      const gcode = 'G1 X100.123456789 Y100.987654321 Z10.111111111';
      const optimized = analyzer.optimizeGCode(gcode);

      expect(optimized).toContain('X100.1234');
      expect(optimized).toContain('Y100.9876');
      expect(optimized).toContain('Z10.1111');
    });

    it('should preserve important comments', () => {
      const gcode = `
        ; LAYER:0
        ; TIME:1234
        ; This is comment 1
        ; This is comment 2
        ; This is comment 3
        ; This is comment 4
        ; This is comment 5
        G1 X100 Y100
      `;

      const optimized = analyzer.optimizeGCode(gcode);

      expect(optimized).toContain('LAYER:0');
      expect(optimized).toContain('TIME:1234');
    });
  });
});