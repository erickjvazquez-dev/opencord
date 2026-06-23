# Reaching your Opencord from anywhere (free, no port-forwarding)

Opencord runs as a **single origin** — one nginx (`:3000` in the Docker stack) serves the web
client at `/` and proxies `/api/` and the `/ws` WebSocket to the Go backend. The web client only
ever talks to **that same origin** (relative `/api` requests, a WebSocket derived from
`location.host`/`location.protocol`, invites shared as plain **codes**, not URLs). So you can put
**any reverse tunnel in front of `http://localhost:3000`** and a friend on another computer can
reach your server **with zero Opencord configuration and no router port-forwarding** — the same way
the managed Railway deploy fronts it over HTTPS today.

Everything below is **free and self-hostable** (Rule A — nothing behind a paywall). Pick whichever
fits; they all point a tunnel at your single origin.

> **Before you expose it publicly — set a real `JWT_SECRET`.** With the insecure dev default anyone
> can forge auth tokens (the server logs a loud warning at boot). Generate one and set it:
> ```bash
> export JWT_SECRET=$(openssl rand -hex 32)
> ```
> Optionally tighten `CORS_ORIGIN` from `*` to your tunnel URL once you know it.

The examples assume the default Docker stack is up (`docker compose up --build`, serving
`http://localhost:3000`). For the hot-reload dev stack, front the Vite origin (`:5173`) instead.

---

## Option A — Cloudflare Tunnel (`cloudflared`) — easiest, instant HTTPS

Free; gives you an HTTPS URL with no account for a quick share, or a stable named tunnel with a free
Cloudflare account. Outbound-only (no inbound ports opened).

**Quick, ephemeral URL (no account):**
```bash
cloudflared tunnel --url http://localhost:3000
# → prints https://<random>.trycloudflare.com — share that; it proxies to your Opencord
```

**Stable named tunnel (free Cloudflare account + your domain):**
```bash
cloudflared tunnel login
cloudflared tunnel create opencord
# route a hostname to the tunnel, then run it pointing at the origin:
cloudflared tunnel route dns opencord chat.example.com
cloudflared tunnel run --url http://localhost:3000 opencord
```
Trade-off: relies on Cloudflare (a third party). It's opt-in and free; Opencord needs nothing.

## Option B — `frp` (Fast Reverse Proxy) — purest self-host

Run the relay (`frps`) on **any public host you control** (a cheap VPS), and the client (`frpc`) on
the machine running Opencord. No third party — you own both ends. (`frp` is open-source, Go.)

`frpc.toml` on the Opencord machine:
```toml
serverAddr = "YOUR_VPS_PUBLIC_IP"
serverPort = 7000            # frps bind port
auth.token = "a-shared-secret"

[[proxies]]
name = "opencord"
type = "http"
localPort = 3000             # Opencord's single origin
customDomains = ["chat.example.com"]   # pointed at your VPS
```
Put TLS in front on the VPS (frps `vhostHTTPSPort` + a cert, or an nginx/Caddy in front of frps).
Alternatives with the same model: **zrok**, **Piko**, **Wireport** (all OSS, self-hostable).

## Option C — Tailscale — best for a trusted friend group

A mesh VPN: you and your friends each install a Tailscale client and join the same tailnet; no relay
to host (Tailscale's DERP servers handle fallback). Fully self-hostable coordination via **headscale**
if you don't want Tailscale's control plane.

```bash
tailscale up
tailscale serve http://localhost:3000     # share within your tailnet
# friends on the tailnet open http://<your-machine-name>:3000
```
Trade-off: every participant installs a client (vs. just opening a URL).

---

## Notes

- **Invites are tunnel-agnostic.** Opencord shares invite **codes**, not absolute links — a friend
  pastes the code into *Join server* on whatever URL they reached your instance at. No per-tunnel
  rewriting needed.
- **WebSockets work over the tunnel.** The bundled nginx upgrades `/ws` (`Upgrade`/`Connection`
  headers); Cloudflare, frp `type=http`, and Tailscale all carry WebSockets. The client picks
  `wss://` automatically when reached over HTTPS.
- **Voice/screen-share across NATs.** WebRTC media is peer-to-peer and may need a TURN relay behind
  symmetric NATs — Opencord ships an optional self-hostable coturn (`docker compose --profile turn`,
  see the README). The tunnel only carries the signaling (over `/ws`), not the media.
- **Degrades gracefully.** With no tunnel, Opencord is reachable on your LAN at `http://<host>:3000`
  or via manual port-forwarding — exactly as before. Tunneling is purely additive (Rule A).
