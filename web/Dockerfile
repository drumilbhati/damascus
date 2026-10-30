# -------------------------------------------------------------
# Stage 1: Build production assets using Node
# -------------------------------------------------------------
FROM node:20-alpine AS build

WORKDIR /app

# Install dependencies using clean install
COPY package*.json ./
RUN npm ci

# Copy application source code
COPY . .

# Build Vite React SPA into static assets in /app/dist
RUN npm run build

# -------------------------------------------------------------
# Stage 2: Serve static assets with Nginx
# -------------------------------------------------------------
FROM nginx:alpine

# Default configuration environment variables
ENV PORT=80
ENV BACKEND_URL=http://localhost:8080/api/
ENV NGINX_ENVSUBST_FILTER="^(BACKEND_URL|PORT)$"

# Copy custom nginx configuration template for runtime envsubst
COPY nginx.conf.template /etc/nginx/templates/default.conf.template

# Copy compiled SPA static assets from build stage
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
