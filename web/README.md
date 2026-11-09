# Damascus Web UI

Professional dark-themed observability and chaos control dashboard inspired by Apache SkyWalking.

## 🚀 Running with Docker

### Build the Docker Image:
```bash
docker build -t damascus-ui ./web
```

### Run the Container:
```bash
docker run -d --name damascus-ui \
  -p 3000:80 \
  -e BACKEND_URL=http://host.docker.internal:8080/api/ \
  damascus-ui
```

### Or using Docker Compose:
```bash
docker compose -f docker-compose.web.yaml up -d
```

The standalone Compose file maps the UI to [http://localhost:4000](http://localhost:4000). The full stack (`docker compose up -d --build` from the repository root) maps it to [http://localhost:3000](http://localhost:3000).

If the page is blank after an update, rebuild and recreate the dashboard container, then reload the page:

```bash
docker compose up -d --build --force-recreate damascus-ui
```

If Docker reports that port 3000 is already allocated, use an available host port (for example, 3001) and open that port instead:

```bash
WEB_PORT=3001 docker compose up -d --build --force-recreate damascus-ui
```

Open [http://localhost:3001](http://localhost:3001). To keep this port for future `docker compose` commands, set `WEB_PORT=3001` in the root `.env` file.

## 🛠️ Local Development

```bash
cd web
npm install
npm run dev
```

Build production static bundle:
```bash
npm run build
```

