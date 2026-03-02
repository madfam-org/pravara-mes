# Build stage
FROM node:18-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./
COPY tsconfig.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY src ./src

# Build TypeScript
RUN npm run build

# Production stage
FROM node:18-alpine

WORKDIR /app

# Install required system dependencies for SerialPort
RUN apk add --no-cache \
    python3 \
    make \
    g++ \
    linux-headers \
    udev \
    avahi \
    avahi-tools

# Copy package files
COPY package*.json ./

# Install production dependencies only
RUN npm ci --only=production && \
    npm rebuild serialport --build-from-source

# Copy built application
COPY --from=builder /app/dist ./dist

# Create directories
RUN mkdir -p logs uploads temp luban-workspace

# Create non-root user
RUN addgroup -g 1001 -S nodejs && \
    adduser -S nodejs -u 1001 && \
    chown -R nodejs:nodejs /app

# Add user to dialout group for serial port access
RUN addgroup nodejs dialout

USER nodejs

# Environment variables
ENV NODE_ENV=production \
    PORT=4507 \
    LOG_LEVEL=info

EXPOSE 4507

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD node -e "require('http').get('http://localhost:4507/health', (r) => {r.statusCode === 200 ? process.exit(0) : process.exit(1)})"

CMD ["node", "dist/index.js"]