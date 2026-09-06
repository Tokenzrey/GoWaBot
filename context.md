# GOWA Integration Context

Reference for any backend service that wants to use this GOWA deployment as a WhatsApp bot backend
(same idea as a Telegram Bot API: your service sends messages via REST, receives incoming messages via webhook).

## Deployment

| | |
|---|---|
| Public base URL | `https://gowatokenzrey.my.id` |
| Transport | Cloudflare Tunnel (named tunnel `gowa`) → `cloudflared` sidecar → `whatsapp_go:3000` container, all in [docker-compose.yml](docker-compose.yml) |
| Auth | HTTP Basic Auth on every REST/MCP/UI route except `/health` |
| Credentials | `APP_BASIC_AUTH` in `src/.env` — get the current value from ops, do not hardcode it in code you commit |
| Multi-device | Not used yet (single WhatsApp account expected). If more than one device is ever registered, every device-scoped call below must add `X-Device-Id: <device_id>` (or `?device_id=`); with exactly one device registered it's picked automatically. |

Health check (no auth):
```bash
curl https://gowatokenzrey.my.id/health
```

## One-time setup: link the WhatsApp account

Nothing works until a device is registered and QR-scanned. From a machine with access to the deployment:

```bash
# 1. Register a device slot
curl -u admin:<password> -X POST https://gowatokenzrey.my.id/devices

# 2. Get QR code (returns PNG or JSON with QR link depending on Accept header — open in browser)
curl -u admin:<password> https://gowatokenzrey.my.id/devices/<device_id>/login

# 3. Scan with the WhatsApp phone app: Linked Devices > Link a Device
# 4. Confirm it's connected
curl -u admin:<password> https://gowatokenzrey.my.id/devices/<device_id>/status
```

Or just open `https://gowatokenzrey.my.id/` in a browser (dashboard) and use the QR flow there.

## Sending messages (bot → user)

All send endpoints are `POST`, JSON body unless noted, Basic Auth required.

### Text message

```bash
curl -u admin:<password> -X POST https://gowatokenzrey.my.id/send/message \
  -H "Content-Type: application/json" \
  -d '{
    "phone": "6289685028129@s.whatsapp.net",
    "message": "Hello from bot"
  }'
```

Response:
```json
{"code":"SUCCESS","message":"Message sent","results":{"message_id":"...","status":"..."}}
```

`phone` format: `<countrycode><number>@s.whatsapp.net` for a person, `<number>@g.us` for a group.

Optional fields on `/send/message`: `reply_message_id` (quote a message), `is_forwarded`, `duration`
(disappearing-message seconds: `0`, `86400`, `604800`, `7776000`), `mentions` (array of phone numbers or
the literal `"@everyone"` for a ghost-mention of all group participants).

### Other send types (same pattern, see [docs/openapi.yaml](docs/openapi.yaml) for full schemas)

| Endpoint | Body | Notes |
|---|---|---|
| `POST /send/image` | multipart/form-data: `phone`, `image` or `image_url`, `caption` | auto-compresses unless `compress=false` |
| `POST /send/video` | multipart/form-data: `phone`, `video` or `video_url`, `caption` | |
| `POST /send/audio` | multipart/form-data: `phone`, `audio` or `audio_url` | |
| `POST /send/file` | multipart/form-data: `phone`, `file` | any document type |
| `POST /send/sticker` | multipart/form-data: `phone`, `image` | auto-converts to WebP, resizes to 512×512 |
| `POST /send/contact` | JSON: `phone`, `contact_name`, `contact_phone` | |
| `POST /send/location` | JSON: `phone`, `latitude`, `longitude` | |
| `POST /send/link` | JSON: `phone`, `link`, `caption` | link preview |
| `POST /send/poll` | JSON: `phone`, `question`, `options[]`, `max_answer` | |
| `POST /send/chat-presence` | JSON: `phone`, `action` (`start`/`stop`) | typing indicator |

Message actions (act on an existing `message_id`):

| Endpoint | Purpose |
|---|---|
| `POST /message/:message_id/revoke` | delete for everyone |
| `POST /message/:message_id/reaction` | react with emoji |
| `POST /message/:message_id/delete` | delete for me |
| `POST /message/:message_id/update` | edit text |
| `POST /message/:message_id/read` | mark read |
| `GET /message/:message_id/download?phone=<chat_jid>` | download media. **`phone` query param is REQUIRED** — omit it and GOWA returns HTTP 400 `phone: cannot be blank`. Response is JSON `{results:{file_url,file_path,media_type}}` (not raw bytes): take `file_url`, then fetch it with the same Basic Auth. |

## Receiving messages (user → bot)

This is the Telegram-"getUpdates"-equivalent: configure a webhook and GOWA pushes events to your backend —
no polling needed.

1. Set your backend's endpoint as the webhook target:
   ```bash
   # global (all devices) — edit src/.env
   WHATSAPP_WEBHOOK=https://your-backend.example.com/gowa/webhook
   WHATSAPP_WEBHOOK_EVENTS=message,message.ack   # empty = all events
   WHATSAPP_WEBHOOK_SECRET=<pick a real secret, not the "secret" default>
   ```
   then `docker compose up -d` (env-only change, no rebuild) — or per-device via
   `PATCH /devices/:device_id/webhook` with `{"webhook_url": "..."}` if multiple bots share one GOWA instance.

2. GOWA POSTs JSON to that URL on every matching event, e.g. an incoming text:
   ```json
   {
     "event": "message",
     "device_id": "628987654321@s.whatsapp.net",
     "payload": {
       "id": "3EB0C127D7BACC83D6A1",
       "chat_id": "628123456789@s.whatsapp.net",
       "from": "628123456789@s.whatsapp.net",
       "from_name": "John Doe",
       "timestamp": "2023-10-15T10:30:00Z",
       "is_from_me": false,
       "body": "Hello, how are you?"
     }
   }
   ```
   `chat_id` is who to reply to — pass it straight back as `phone` in `/send/message`.

3. **Verify the signature** on every request before trusting the payload:
   - Header: `X-Hub-Signature-256: sha256=<hex hmac>`
   - HMAC-SHA256 of the raw request body, keyed with `WHATSAPP_WEBHOOK_SECRET`.
   - Pseudocode: `hex(hmac_sha256(secret, raw_body)) == header_value.removeprefix("sha256=")`

Full event catalogue (message types, reactions, edits, receipts, group/newsletter/call events) and exact
payload shapes for each: [docs/webhook-payload.md](docs/webhook-payload.md).

Event filtering:
- `WHATSAPP_WEBHOOK_EVENTS` — comma list of event names to forward (empty = all).
- `WHATSAPP_WEBHOOK_IGNORE_JIDS` — mute specific chats/senders, e.g. `@g.us` to drop all group traffic.

## Reading history / chat state (pull model, if you need it instead of/alongside webhooks)

| Endpoint | Purpose |
|---|---|
| `GET /chats` | list chats |
| `GET /chat/:chat_jid/messages` | message history for one chat |
| `GET /user/check?phone=...` | verify a number has WhatsApp before sending |
| `GET /user/my/contacts` | contact list |
| `GET /user/my/groups` | group list (capped at 500 by WhatsApp itself) |

## Alternative: MCP instead of raw REST

If the calling backend is an AI agent framework rather than plain application code, use the MCP endpoint
instead of hand-rolling REST calls:

- URL: `https://gowatokenzrey.my.id/mcp` (streamable HTTP)
- Auth: same Basic Auth header as REST
- Tools: `whatsapp_send`, `whatsapp_message`, `whatsapp_chat`, `whatsapp_group`, `whatsapp_app` — each takes
  a `type`/`action` argument selecting the operation (mirrors the REST table above).

```json
{
  "mcpServers": {
    "whatsapp": {
      "url": "https://gowatokenzrey.my.id/mcp",
      "headers": { "Authorization": "Basic <base64 admin:password>" }
    }
  }
}
```

## Limits / gotchas worth knowing before integrating

- `GET /app/info` returns current version and media size limits — check it at integration time rather than
  hardcoding limits.
- WhatsApp rate-limits aggressive/bulk sending; this is not a bulk-broadcast API — build backoff/queueing in
  the calling service, don't fire messages in a tight loop.
- `/user/my/groups` caps at 500 (WhatsApp protocol limit, not this app).
- Media messages: `download_media` / `GET /message/:message_id/download` only works while the media is still
  on WhatsApp's servers / locally cached — don't assume it's available indefinitely.
- One WhatsApp account = one number. This is not a multi-tenant SaaS; if the plan is "many businesses, many
  numbers," that means many devices under `/devices`, each with its own webhook via
  `PATCH /devices/:device_id/webhook`.
- Full endpoint list/schemas: [docs/openapi.yaml](docs/openapi.yaml) (open in https://editor.swagger.io for a
  browsable UI).
