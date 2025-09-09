# Builder stage using Bun Alpine
FROM oven/bun:1-alpine AS builder

WORKDIR /app

# Copy package files
COPY package.json bun.lock ./
COPY client/package.json ./client/

# Install dependencies
RUN bun install && cd client && bun install

# Copy source code
COPY . .

# Compile to binary
RUN bun run compile

# Final stage using Alpine 3.20
FROM alpine:3.20

# Install ffmpeg and ca-certificates
RUN apk add --no-cache ffmpeg ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/gowa-manager .

# Create data/bin directory
RUN mkdir -p data/bin

EXPOSE 3000

ENV PORT=3000
ENV ADMIN_USERNAME=admin
ENV ADMIN_PASSWORD=password
ENV DATA_DIR=/app/data

# Start the application
CMD ["./gowa-manager"]
