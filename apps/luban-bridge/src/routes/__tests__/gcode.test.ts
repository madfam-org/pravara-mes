import express from 'express';
import request from 'supertest';
import winston from 'winston';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

// The G-code routes take uploads through multer (2.x since #45). These tests
// drive real multipart requests through the router so the multer contract
// the routes rely on stays covered: `dest` + `limits` + `fileFilter` and
// `upload.single('gcode')`, the uploaded file landing under UPLOAD_DIR, and
// its removal after it is read.
//
// gcode.ts resolves UPLOAD_DIR from GCODE_UPLOAD_DIR when the module loads,
// so the env var is set before the module is required.
const UPLOAD_DIR = fs.mkdtempSync(path.join(os.tmpdir(), 'luban-gcode-test-'));
process.env.GCODE_UPLOAD_DIR = UPLOAD_DIR;
const { gcodeRouter } = require('../gcode') as typeof import('../gcode');

const SAMPLE_GCODE = ['G28', 'G1 X10 Y10 Z0.2 F1500 E1', 'G1 X20 Y10 E2', 'M104 S0'].join('\n');

function createMockAnalyzer() {
  return {
    analyze: jest.fn().mockReturnValue({ totalLines: 4, commands: 4 }),
    validateGCode: jest.fn().mockReturnValue({ valid: true, errors: [] }),
    optimizeGCode: jest.fn().mockImplementation((gcode: string) => gcode),
  };
}

function createApp() {
  const analyzer = createMockAnalyzer();
  const app = express();
  app.use(express.json());
  app.use('/api/gcode', gcodeRouter(analyzer as any, winston.createLogger({ silent: true })));
  return { app, analyzer };
}

async function waitForEmptyUploadDir(timeoutMs = 2000): Promise<string[]> {
  const deadline = Date.now() + timeoutMs;
  let entries = fs.readdirSync(UPLOAD_DIR);
  while (entries.length > 0 && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 20));
    entries = fs.readdirSync(UPLOAD_DIR);
  }
  return entries;
}

afterAll(() => {
  fs.rmSync(UPLOAD_DIR, { recursive: true, force: true });
});

describe('POST /api/gcode/analyze (multipart upload)', () => {
  it('reads an uploaded .gcode file, analyzes its content and deletes it', async () => {
    const { app, analyzer } = createApp();

    const res = await request(app)
      .post('/api/gcode/analyze')
      .attach('gcode', Buffer.from(SAMPLE_GCODE), 'part.gcode');

    expect(res.status).toBe(200);
    expect(res.body).toEqual({ totalLines: 4, commands: 4 });
    expect(analyzer.analyze).toHaveBeenCalledTimes(1);
    expect(analyzer.analyze).toHaveBeenCalledWith(SAMPLE_GCODE);
    expect(await waitForEmptyUploadDir()).toEqual([]);
  });

  it.each(['part.gco', 'PART.NC'])('accepts the %s extension', async (filename) => {
    const { app, analyzer } = createApp();

    const res = await request(app)
      .post('/api/gcode/analyze')
      .attach('gcode', Buffer.from(SAMPLE_GCODE), filename);

    expect(res.status).toBe(200);
    expect(analyzer.analyze).toHaveBeenCalledWith(SAMPLE_GCODE);
  });

  it('rejects a file whose extension is not G-code before any analysis', async () => {
    const { app, analyzer } = createApp();

    const res = await request(app)
      .post('/api/gcode/analyze')
      .attach('gcode', Buffer.from(SAMPLE_GCODE), 'notes.txt');

    expect(res.status).toBe(500);
    expect(analyzer.analyze).not.toHaveBeenCalled();
    expect(fs.readdirSync(UPLOAD_DIR)).toEqual([]);
  });

  it('falls back to a JSON body when no file is uploaded', async () => {
    const { app, analyzer } = createApp();

    const res = await request(app).post('/api/gcode/analyze').send({ gcode: SAMPLE_GCODE });

    expect(res.status).toBe(200);
    expect(analyzer.analyze).toHaveBeenCalledWith(SAMPLE_GCODE);
  });

  it('returns 400 when neither a file nor a body is provided', async () => {
    const { app, analyzer } = createApp();

    const res = await request(app).post('/api/gcode/analyze').send({});

    expect(res.status).toBe(400);
    expect(res.body).toEqual({ error: 'No G-code provided' });
    expect(analyzer.analyze).not.toHaveBeenCalled();
  });
});

describe('POST /api/gcode/validate (multipart upload)', () => {
  it('validates the uploaded file content', async () => {
    const { app, analyzer } = createApp();

    const res = await request(app)
      .post('/api/gcode/validate')
      .attach('gcode', Buffer.from(SAMPLE_GCODE), 'part.gcode');

    expect(res.status).toBe(200);
    expect(res.body).toEqual({ valid: true, errors: [] });
    expect(analyzer.validateGCode).toHaveBeenCalledWith(SAMPLE_GCODE);
  });
});
