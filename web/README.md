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

The UI will be accessible at [http://localhost:3000](http://localhost:3000).

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

