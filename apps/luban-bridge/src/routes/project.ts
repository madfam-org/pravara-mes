import { Router } from 'express';
import multer from 'multer';
import * as path from 'path';
import winston from 'winston';
import { LubanCLI } from '../services/luban-cli';

const upload = multer({
  dest: 'uploads/',
  limits: { fileSize: 100 * 1024 * 1024 }, // 100MB limit
  fileFilter: (req, file, cb) => {
    const ext = path.extname(file.originalname).toLowerCase();
    if (['.lbn', '.stl', '.obj', '.3mf'].includes(ext)) {
      cb(null, true);
    } else {
      cb(new Error('Invalid file type'));
    }
  }
});

export function projectRouter(lubanCLI: LubanCLI, logger: winston.Logger): Router {
  const router = Router();

  // Import Luban project
  router.post('/import', upload.single('project'), async (req, res) => {
    if (!req.file) {
      return res.status(400).json({ error: 'No project file provided' });
    }

    try {
      const projectInfo = await lubanCLI.importProject(req.file.path);
      res.json(projectInfo);
    } catch (error) {
      logger.error('Project import failed', error);
      res.status(500).json({ error: 'Failed to import project' });
    }
  });

  // Slice STL to G-code
  router.post('/slice', upload.single('model'), async (req, res) => {
    if (!req.file) {
      return res.status(400).json({ error: 'No model file provided' });
    }

    const { profileId = 'snapmaker-a350' } = req.body;

    try {
      const result = await lubanCLI.sliceSTL(req.file.path, profileId);
      res.json(result);
    } catch (error) {
      logger.error('Slicing failed', error);
      res.status(500).json({ error: 'Failed to slice model' });
    }
  });

  // Generate toolpath
  router.post('/toolpath', upload.single('project'), async (req, res) => {
    if (!req.file) {
      return res.status(400).json({ error: 'No project file provided' });
    }

    const { toolType = 'printing' } = req.body;

    try {
      const toolpathFile = await lubanCLI.generateToolpath(req.file.path, toolType);
      res.json({ toolpathFile });
    } catch (error) {
      logger.error('Toolpath generation failed', error);
      res.status(500).json({ error: 'Failed to generate toolpath' });
    }
  });

  // Export G-code
  router.post('/export', upload.single('project'), async (req, res) => {
    if (!req.file) {
      return res.status(400).json({ error: 'No project file provided' });
    }

    const { format = 'gcode', includeSupports, includeThumbnail } = req.body;

    try {
      const exportFile = await lubanCLI.exportGCode(req.file.path, {
        format,
        includeSupports: includeSupports === 'true',
        includeThumbnail: includeThumbnail === 'true'
      });

      res.json({ exportFile });
    } catch (error) {
      logger.error('Export failed', error);
      res.status(500).json({ error: 'Failed to export G-code' });
    }
  });

  return router;
}