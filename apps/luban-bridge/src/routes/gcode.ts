import { Router } from 'express';
import multer from 'multer';
import winston from 'winston';
import { GCodeAnalyzer } from '../services/gcode-analyzer';

const upload = multer({
  dest: 'uploads/',
  limits: { fileSize: 50 * 1024 * 1024 }, // 50MB limit
  fileFilter: (req, file, cb) => {
    const ext = file.originalname.toLowerCase();
    if (ext.endsWith('.gcode') || ext.endsWith('.gco') || ext.endsWith('.nc')) {
      cb(null, true);
    } else {
      cb(new Error('Invalid file type'));
    }
  }
});

export function gcodeRouter(analyzer: GCodeAnalyzer, logger: winston.Logger): Router {
  const router = Router();

  // Analyze G-code
  router.post('/analyze', upload.single('gcode'), async (req, res) => {
    try {
      let gcodeContent: string;

      if (req.file) {
        // Read file content
        const fs = require('fs').promises;
        gcodeContent = await fs.readFile(req.file.path, 'utf-8');
      } else if (req.body.gcode) {
        gcodeContent = req.body.gcode;
      } else {
        return res.status(400).json({ error: 'No G-code provided' });
      }

      const analysis = analyzer.analyze(gcodeContent);
      res.json(analysis);
    } catch (error) {
      logger.error('G-code analysis failed', error);
      res.status(500).json({ error: 'Failed to analyze G-code' });
    }
  });

  // Validate G-code
  router.post('/validate', upload.single('gcode'), async (req, res) => {
    try {
      let gcodeContent: string;

      if (req.file) {
        const fs = require('fs').promises;
        gcodeContent = await fs.readFile(req.file.path, 'utf-8');
      } else if (req.body.gcode) {
        gcodeContent = req.body.gcode;
      } else {
        return res.status(400).json({ error: 'No G-code provided' });
      }

      const validation = analyzer.validateGCode(gcodeContent);
      res.json(validation);
    } catch (error) {
      logger.error('G-code validation failed', error);
      res.status(500).json({ error: 'Failed to validate G-code' });
    }
  });

  // Optimize G-code
  router.post('/optimize', upload.single('gcode'), async (req, res) => {
    try {
      let gcodeContent: string;

      if (req.file) {
        const fs = require('fs').promises;
        gcodeContent = await fs.readFile(req.file.path, 'utf-8');
      } else if (req.body.gcode) {
        gcodeContent = req.body.gcode;
      } else {
        return res.status(400).json({ error: 'No G-code provided' });
      }

      const optimized = analyzer.optimizeGCode(gcodeContent);

      // Analyze both original and optimized
      const originalAnalysis = analyzer.analyze(gcodeContent);
      const optimizedAnalysis = analyzer.analyze(optimized);

      res.json({
        optimized,
        stats: {
          original: {
            lines: originalAnalysis.totalLines,
            commands: originalAnalysis.commands
          },
          optimized: {
            lines: optimizedAnalysis.totalLines,
            commands: optimizedAnalysis.commands
          },
          reduction: {
            lines: originalAnalysis.totalLines - optimizedAnalysis.totalLines,
            percentage: ((1 - optimizedAnalysis.totalLines / originalAnalysis.totalLines) * 100).toFixed(2)
          }
        }
      });
    } catch (error) {
      logger.error('G-code optimization failed', error);
      res.status(500).json({ error: 'Failed to optimize G-code' });
    }
  });

  // Simulate G-code for visualization
  router.post('/simulate', async (req, res) => {
    try {
      const { gcode, material = 'PLA', nozzle_diameter = 0.4 } = req.body;

      if (!gcode) {
        return res.status(400).json({ error: 'No G-code provided' });
      }

      const analysis = analyzer.analyze(gcode);

      // Generate segments for visualization
      const segments = [];
      const layers = [];
      const lines = gcode.split('\n');

      let currentPosition = { x: 0, y: 0, z: 0, e: 0 };
      let currentLayer = 0;
      let currentLayerSegments = [];
      let lastZ = 0;

      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith(';')) continue;

        // Simple G-code parsing for visualization
        if (trimmed.startsWith('G0') || trimmed.startsWith('G1')) {
          const newPosition = { ...currentPosition };
          let hasMovement = false;

          // Parse coordinates
          const xMatch = trimmed.match(/X([-\d.]+)/);
          const yMatch = trimmed.match(/Y([-\d.]+)/);
          const zMatch = trimmed.match(/Z([-\d.]+)/);
          const eMatch = trimmed.match(/E([-\d.]+)/);
          const fMatch = trimmed.match(/F([-\d.]+)/);

          if (xMatch) {
            newPosition.x = parseFloat(xMatch[1]);
            hasMovement = true;
          }
          if (yMatch) {
            newPosition.y = parseFloat(yMatch[1]);
            hasMovement = true;
          }
          if (zMatch) {
            newPosition.z = parseFloat(zMatch[1]);
            if (newPosition.z > lastZ) {
              // Layer change
              if (currentLayerSegments.length > 0) {
                layers.push({
                  number: currentLayer,
                  height: lastZ,
                  segments: [...currentLayerSegments],
                  print_time: 0,
                  filament_used_mm: 0
                });
                currentLayerSegments = [];
                currentLayer++;
              }
              lastZ = newPosition.z;
            }
            hasMovement = true;
          }
          if (eMatch) {
            newPosition.e = parseFloat(eMatch[1]);
          }

          if (hasMovement) {
            const isExtruding = eMatch && newPosition.e > currentPosition.e;
            const segment = {
              start: { x: currentPosition.x, y: currentPosition.y, z: currentPosition.z },
              end: { x: newPosition.x, y: newPosition.y, z: newPosition.z },
              extrusion_rate: isExtruding ? 5 : 0,
              layer_height: analysis.layerHeight,
              line_width: nozzle_diameter,
              temperature: analysis.temperatures.nozzle,
              speed: fMatch ? parseFloat(fMatch[1]) : 50,
              material,
              is_retraction: eMatch && newPosition.e < currentPosition.e,
              is_prime: false,
              is_travel: !isExtruding,
              volume_deposited: isExtruding ? 0.1 : 0
            };

            segments.push(segment);
            currentLayerSegments.push(segment);
            currentPosition = newPosition;
          }
        }
      }

      // Add last layer
      if (currentLayerSegments.length > 0) {
        layers.push({
          number: currentLayer,
          height: lastZ,
          segments: currentLayerSegments,
          print_time: 0,
          filament_used_mm: 0
        });
      }

      res.json({
        segments,
        layers,
        bounding_box: analysis.boundingBox,
        stats: {
          layer_count: analysis.layerCount,
          print_time_min: analysis.printTime,
          filament_meters: analysis.filamentUsed / 1000,
          weight_grams: (analysis.filamentUsed / 1000) * 2.5, // Approximate for PLA
          volume_cm3: (analysis.filamentUsed / 1000) * 0.8
        },
        material,
        toolhead: analysis.toolhead,
        snapmaker_features: analysis.snapmakerSpecific
      });
    } catch (error) {
      logger.error('G-code simulation failed', error);
      res.status(500).json({ error: 'Failed to simulate G-code' });
    }
  });

  return router;
}