# Migrasi GOWA: Local → VPS (clone, deploy Docker, Cloudflare Tunnel)

Panduan lengkap mindahin GOWA dari mesin lokal (WSL) ke VPS: dari `git clone` di VPS, setup repo,
deploy via Docker, sambungin ke Cloudflare Tunnel pakai domain yang sama (`gowatokenzrey.my.id`),
sampai bener-bener bisa diakses publik — lalu matiin yang lokal supaya tidak tabrakan.

Domain dan tunnel ID **tidak berubah**. CNAME `gowatokenzrey.my.id` sudah nunjuk ke tunnel ID
`d0cfe822-70b4-4466-9d2d-f8efa1250f2a`; origin-nya (yang jalanin tunnel) tinggal pindah ke VPS asal
file kredensialnya ikut. **Tidak perlu `cloudflared tunnel login` / `route dns` ulang.**

---

## Prinsip cutover: matiin lokal DULUAN

GOWA nyimpen **state** di `storages/` — sesi login WhatsApp (device sudah ke-pair), history chat,
dan **config webhook per-device** (URL + secret financetrack yang sudah dibenerin). SQLite ini harus
di-copy dalam keadaan proses **berhenti** biar konsisten.

Kalau tunnel lokal + VPS nyala barengan dengan tunnel ID yang sama, Cloudflare anggap 2 replika dan
load-balance random — sebagian request kehandle instance lokal (punya sesi), sebagian ke VPS (masih
kosong). Split-brain. Jadi urutannya: **stop lokal → copy state → start VPS**. Ada downtime beberapa
menit, tapi bersih dan sesi WhatsApp tidak perlu scan QR ulang.

---

## Apa yang pindah

| Item | Cara | Kenapa |
|---|---|---|
| Kode repo | `git clone` fresh di VPS | Line ending otomatis LF di Linux (`.gitattributes` sudah ngurus) |
| `src/.env` | `scp` manual | Di-`.gitignore`, isinya basic-auth + config |
| `storages/` | `scp -r` manual (setelah lokal mati) | Sesi login WA, chat DB, webhook per-device. Di-`.gitignore` |
| `statics/` | `scp -r` manual | Media/QR yang sudah ke-download. Di-`.gitignore` |
| Kredensial tunnel (`d0cfe822-….json`) | `scp` manual ke `~/.cloudflared/gowa/` | Bukti VPS berhak jalanin tunnel ini. **Setara private key** |
| Tunnel ID + DNS | — | Dipakai apa adanya, nol perubahan |

---

## FASE A — Siapin VPS (belum matiin lokal)

### A1. Clone repo di VPS

```bash
ssh <user>@<vps-ip>
git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice.git ~/gowa
cd ~/gowa
```

> Ganti URL kalau kamu pakai fork sendiri. Sisa panduan pakai `~/gowa` sebagai folder repo.

### A2. Pastikan Docker ada

```bash
docker --version && docker compose version
```

Kalau belum ada (Ubuntu/Debian):

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER
# logout lalu ssh lagi biar grup docker kebaca
```

`cloudflared` di VPS sudah terpasang di `/usr/local/bin/cloudflared` (kebukti dari tunnel `koperasi`,
`sonarqube`, dll yang sudah jalan) — tidak usah install ulang.

### A3. Copy kredensial tunnel ke VPS

Di **WSL lokal**:

```bash
ssh <user>@<vps-ip> 'mkdir -p ~/.cloudflared/gowa'
scp ~/.cloudflared/d0cfe822-70b4-4466-9d2d-f8efa1250f2a.json \
    <user>@<vps-ip>:~/.cloudflared/gowa/
```

> Jangan pernah taruh isi file ini di git / chat / markdown. Pindahin file-nya langsung aja.

### A4. Buat config tunnel di VPS

`~/.cloudflared/gowa/config.yml` (ikutin pola `koperasi/config.yml` punyamu — path **absolut**).
Di VPS ini GOWA di-publish ke **port host 4719** (bukan 3000), jadi `service` nunjuk ke situ:

```yaml
tunnel: d0cfe822-70b4-4466-9d2d-f8efa1250f2a
credentials-file: /home/<user>/.cloudflared/gowa/d0cfe822-70b4-4466-9d2d-f8efa1250f2a.json

ingress:
  - hostname: gowatokenzrey.my.id
    service: http://localhost:4719
  - service: http_status:404
```

> **Port forwarding**: container GOWA tetap listen di `3000` di dalamnya. `docker-compose.yml`
> mem-publish `${GOWA_HOST_PORT:-3000}:3000`, jadi set `GOWA_HOST_PORT=4719` (langkah C1) bikin
> host jadi `4719 → 3000`. Mau port lain tinggal ganti angkanya di dua tempat: root `.env` dan
> `config.yml` ini.

### A5. Daftarin systemd service (belum di-`start`)

```bash
sudo tee /etc/systemd/system/cloudflared-gowa.service > /dev/null <<EOF
[Unit]
Description=Cloudflare Tunnel - GOWA WhatsApp API
After=network.target docker.service

[Service]
Type=simple
User=$USER
ExecStart=/usr/local/bin/cloudflared tunnel --config /home/$USER/.cloudflared/gowa/config.yml run
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable cloudflared-gowa
```

Jangan `start` dulu — nanti setelah GOWA-nya nyala di VPS.

---

## FASE B — Matiin lokal

### B1. Stop stack Docker lokal

Di **WSL lokal**, di folder repo:

```bash
docker compose down
```

Ini matiin container app (`whatsapp_go`) **dan** sidecar `cloudflared` lokal sekaligus (dua-duanya
di [docker-compose.yml](../docker-compose.yml) yang sama). Sesi SQLite di `storages/` sekarang bebas
lock.

### B2. Bersihin sisa proses lokal (kalau ada)

Selama sesi setup kemarin sempat ada yang jalan manual. Pastikan port 3000 bener-bener kosong dan
tidak ada `cloudflared` nyangkut:

```bash
# di WSL lokal
docker compose ps                 # harus kosong
ss -ltnp | grep ':3000' || echo "port 3000 bebas"
pgrep -af cloudflared || echo "tidak ada cloudflared jalan"
pgrep -af 'whatsapp rest|go run' || echo "tidak ada gowa native jalan"
```

Kalau masih ada yang nyangkut, matiin (`kill <pid>` atau `docker rm -f <name>`).

### B3. Verifikasi domain sudah "mati" sementara

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://gowatokenzrey.my.id/health
```

Harus `530` / `502` / timeout — artinya betul tidak ada origin lagi. (Ini window downtime, lanjut
cepat ke Fase C.)

---

## FASE C — Nyalain di VPS

### C1. Copy `src/.env` + state ke VPS, lalu set port host

Di **WSL lokal**, di folder repo:

```bash
scp src/.env <user>@<vps-ip>:~/gowa/src/.env
scp -r storages <user>@<vps-ip>:~/gowa/
scp -r statics  <user>@<vps-ip>:~/gowa/
```

> `storages/` bawa sesi login WA + webhook per-device. Kalau di-skip, di VPS kamu harus scan QR ulang
> **dan** set ulang webhook device (`PATCH /devices/:id/webhook`).

Di **VPS**, set port host jadi 4719 lewat root `.env` repo (dibaca otomatis oleh `docker compose`
buat interpolasi; file ini di-`.gitignore`):

```bash
cd ~/gowa
echo 'GOWA_HOST_PORT=4719' > .env
```

> `.env` (root repo) ≠ `src/.env`. Yang root cuma buat variabel `docker compose`; `src/.env` yang
> dibaca aplikasi GOWA. `APP_PORT` di `src/.env` biarin `3000` — itu port **di dalam** container.

### C2. Build + jalanin GOWA di VPS

Di VPS, di `~/gowa`:

```bash
docker compose up -d --build whatsapp_go
```

> **Wajib sebut `whatsapp_go`.** Jangan `docker compose up -d` polos — itu ikut nyalain service
> `cloudflared` sidecar dari compose file, yang bakal bentrok sama systemd `cloudflared-gowa`.
> Alternatif: hapus blok `cloudflared:` dari `docker-compose.yml` di VPS.

Cek app (port host **4719**):

```bash
curl -s http://localhost:4719/health          # harus: OK
curl -s -u admin:<password> http://localhost:4719/devices
# device "Njay Mabar" harus muncul dengan "state":"logged_in"

docker compose ps
# PORTS harus: 0.0.0.0:4719->3000/tcp
```

Kalau device-nya `logged_in` → sesi kepindah mulus, tidak perlu scan QR.

### C3. Nyalain tunnel

```bash
sudo systemctl start cloudflared-gowa
sudo systemctl status cloudflared-gowa --no-pager
journalctl -u cloudflared-gowa -n 20 --no-pager
```

Log harus ada `Registered tunnel connection` beberapa kali.

### C4. Verifikasi publik

Dari mana aja (bukan VPS):

```bash
curl -s https://gowatokenzrey.my.id/health                         # OK
curl -s -u admin:<password> https://gowatokenzrey.my.id/devices    # device logged_in
```

Kirim 1 pesan WA ke nomor bot dari HP lain → cek log webhook:

```bash
docker compose logs whatsapp_go --since=2m | grep -i webhook
# harus: "Successfully submitted webhook on attempt 1"
```

---

## FASE D — Pastikan tidak tabrakan

Checklist final, jalanin **di WSL lokal**:

```bash
docker compose ps                                # KOSONG
ss -ltnp | grep -E ':3000|:4719' || echo ok      # port bebas
pgrep -af cloudflared || echo ok                 # tidak ada tunnel lokal
```

Lalu pastikan `.env` lokal tidak ke-`docker compose up` lagi tanpa sengaja. Kalau mau,
disable Docker Desktop autostart di Windows.

Di **VPS**, konfirmasi cuma ada 1 origin:

```bash
docker compose ps                       # whatsapp_go Up
systemctl is-active cloudflared-gowa     # active
systemctl is-enabled cloudflared-gowa    # enabled (auto-start on boot)
```

Selesai. Semua traffic `gowatokenzrey.my.id` lewat VPS. Domain, tunnel ID, dan sesi WhatsApp tidak
berubah.

---

## Operasional harian di VPS

```bash
cd ~/gowa
docker compose logs -f whatsapp_go          # lihat log app
docker compose restart whatsapp_go          # restart app (env-only change cukup ini)
docker compose up -d --build whatsapp_go    # rebuild setelah git pull
sudo systemctl restart cloudflared-gowa     # restart tunnel
```

Ganti password / config: edit `~/gowa/src/.env` → `docker compose up -d whatsapp_go`.

Ganti port host (mis. 4719 → port lain): edit `~/gowa/.env` (`GOWA_HOST_PORT=`), edit
`~/.cloudflared/gowa/config.yml` (`service: http://localhost:<port>`), lalu
`docker compose up -d whatsapp_go && sudo systemctl restart cloudflared-gowa`.

---

## Troubleshooting

| Gejala | Penyebab | Fix |
|---|---|---|
| `cloudflared-gowa` gagal start: `credentials file not found` | Path di `config.yml` salah / file belum ke-scp | `ls ~/.cloudflared/gowa/`, samakan nama file dengan field `credentials-file` (path absolut) |
| Tunnel connect, tapi `curl https://gowatokenzrey.my.id/health` → 502 | Container belum jalan / port `service` di `config.yml` tidak cocok sama `GOWA_HOST_PORT` | `docker compose ps` (lihat kolom PORTS `->3000`); `curl localhost:4719/health` di VPS; samakan angka port di `~/gowa/.env` dan `~/.cloudflared/gowa/config.yml` |
| `docker compose ps` PORTS masih `3000->3000` padahal sudah set `GOWA_HOST_PORT` | Root `.env` di folder salah / belum `up` ulang | Pastikan `~/gowa/.env` (bukan `src/.env`); `docker compose up -d whatsapp_go` lagi |
| `docker compose up` gagal build: `exec /entrypoint.sh: no such file` | File `.sh` ke-checkout CRLF | Pastikan **`git clone`** langsung di VPS (Linux), bukan copy folder dari Windows. `.gitattributes` repo sudah paksa LF via git |
| `/devices` kosong di VPS padahal `storages/` sudah di-copy | Copy sebelum `docker compose down` di lokal (DB ke-lock / korup), atau kepatch di path salah | Ulang: `down` lokal → `scp -r storages` → `up` VPS. Pastikan mendarat di `~/gowa/storages` |
| Basic Auth ditolak di VPS | `src/.env` belum di-copy / beda | `diff` `src/.env` lokal vs VPS; cek `APP_BASIC_AUTH` |
| Webhook `403` setelah pindah | `webhook_secret` per-device salah (mis. ke-isi seluruh baris `WHATSAPP_WEBHOOK_SECRET=...`) | `curl -u admin:<pw> -X PATCH .../devices/<id>/webhook -d '{"webhook_url":"<url>","webhook_secret":"<hex-doang>"}'` — `webhook_url` wajib ikut tiap PATCH |
| Request kadang jalan kadang gagal setelah cutover | Tunnel lokal masih nyala (split-brain) | `docker compose down` + `pkill cloudflared` di lokal; jalanin checklist Fase D |
