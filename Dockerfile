# Use Bun Debian image for glibc compatibility
FROM oven/bun:1-debian AS base

RUN apt-get update && apt-get install -y unzip ffmpeg ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY package.json bun.lock ./
COPY client/package.json ./client/

RUN bun install
RUN cd client && bun install

COPY . .

RUN bun run build:client

RUN mkdir -p data/bin

EXPOSE 3000

ENV NODE_ENV=production
ENV PORT=3000

ENV ADMIN_USERNAME=admin
ENV ADMIN_PASSWORD=password

# Start the application
CMD ["bun", "start"]
