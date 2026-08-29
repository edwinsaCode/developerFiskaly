# =====================================================
# Stage: dev — Next.js dev server dengan hot-reload
# Source di-mount sebagai volume; npm install dijalankan
# saat container start agar package-lock.json di-generate.
# =====================================================
FROM node:20-alpine AS dev
WORKDIR /app
RUN apk --no-cache add libc6-compat
EXPOSE 3000
# `dev:container`, BUKAN `dev`: di dalam container server harus mendengar di
# 0.0.0.0 agar port mapping bekerja, sementara `npm run dev` di host sengaja
# dikunci ke 127.0.0.1. Paparan ke LAN tetap tidak terjadi karena compose hanya
# memetakan "127.0.0.1:3000:3000".
CMD ["sh", "-c", "npm install && npm run dev:container"]

# =====================================================
# Stage: deps — install production dependencies
# =====================================================
FROM node:20-alpine AS deps
WORKDIR /app
RUN apk --no-cache add libc6-compat
COPY package.json package-lock.json ./
RUN npm ci

# =====================================================
# Stage: builder — build Next.js standalone
# NEXT_PUBLIC_API_URL harus di-set saat build production.
# =====================================================
FROM node:20-alpine AS builder
WORKDIR /app
RUN apk --no-cache add libc6-compat
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# =====================================================
# Stage: prod — standalone Next.js, image minimal
# =====================================================
FROM node:20-alpine AS prod
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
