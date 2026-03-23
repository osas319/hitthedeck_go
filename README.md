# Krew3D - Go Server

High-performance multiplayer pirate game server written in Go with native WebSocket.

## Why Go instead of Node.js?
- No Socket.io overhead - raw WebSocket with gorilla/websocket  
- Goroutines handle thousands of concurrent connections efficiently
- No garbage collection pauses (compared to Node.js event loop blocks)
- Single binary deployment - no node_modules

## Local Dev
```bash
go build -o krew3d .
./krew3d
# Open http://localhost:3000
```

## Deploy to Render
1. Push to GitHub
2. Render → New Web Service → connect repo
3. Render auto-detects Go
4. Build: `go build -ldflags="-s -w" -o krew3d .`
5. Start: `./krew3d`
6. Or just use the render.yaml (auto-configured)

## Features
- 3 ship types (Rowboat, War Galleon, Trade Schooner)
- Trading system with cargo
- Mining on small islands  
- Entity interpolation for smooth movement
- Free-look camera (right-click)
- Mouse aim + click to fire
