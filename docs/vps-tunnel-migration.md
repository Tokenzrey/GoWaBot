# Migrasi GOWA: Local → VPS (Cloudflare Tunnel)

Panduan pindahin GOWA dari mesin lokal (WSL) ke VPS kamu, pakai domain yang sama
(`gowatokenzrey.my.id`), tunnel Cloudflare yang sama (ID tunnel tidak berubah, jadi **tidak perlu
ubah DNS sama sekali**). VPS kamu udah punya `cloudflared` terpasang dan repo ini udah di-clone —
tinggal sambungin.

## Yang dipindah vs yang tidak

| Item | Dipindah? | Kenapa |
|---|---|---|
| Tunnel ID `d0cfe822-70b4-4466-9d2d-f8efa1250f2a` | **Tetap dipakai** | DNS CNAME `gowatokenzrey.my.id` sudah nunjuk ke ID ini — origin-nya bisa pindah tempat asal file kredensialnya ikut pindah, tidak perlu `route dns` ulang |
| File kredensial tunnel (`d0cfe822-....json`) | **Copy ke VPS** | Ini yang membuktikan ke Cloudflare bahwa VPS berhak run tunnel itu |
| `src/.env` | **Copy ke VPS** | Tidak ikut git (`.gitignore`), harus dipindah manual |
| `storages/`, `statics/` | **Tidak perlu** (saat ini) | Belum ada device yang login (`/devices` masih kosong) — VPS mulai dari kondisi bersih dan login ulang lewat QR di sana. Kalau kamu sempat scan QR duluan di lokal sebelum migrasi, baru copy dua folder ini juga. |

## 0. Prasyarat di VPS

Cek Docker sudah ada (repo ini jalan via `docker compose`, sama seperti di lokal):

```bash
docker --version && docker compose version
```

Kalau belum ada, install dulu (Ubuntu/Debian):

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER   # logout/login lagi biar grup berlaku
```

`cloudflared` di VPS kamu sudah ada di `/usr/local/bin/cloudflared` (kebukti dari tunnel-tunnel lain
yang sudah jalan) — tidak perlu install ulang.

## 1. Copy file kredensial tunnel dari lokal ke VPS

Di **lokal** (WSL), file kredensialnya ada di:

```text
~/.cloudflared/d0cfe822-70b4-4466-9d2d-f8efa1250f2a.json
```

Copy ke VPS pakai `scp` (jalankan dari WSL lokal):

```bash
scp ~/.cloudflared/d0cfe822-70b4-4466-9d2d-f8efa1250f2a.json <user>@<vps-ip>:~/.cloudflared/gowa/
```

Kalau folder `~/.cloudflared/gowa/` belum ada di VPS, bikin dulu:

```bash
mkdir -p ~/.cloudflared/gowa
```

> File ini setara private key — jangan pernah taruh isinya di git, chat, atau markdown manapun.
> Cukup pindah file-nya langsung lewat `scp`.

## 2. Copy `src/.env` dari lokal ke VPS

```bash
scp /path/ke/repo/src/.env <user>@<vps-ip>:~/path/ke/repo/src/.env
```

File ini isinya basic-auth production dan config lain — sama seperti di lokal, jangan commit ke git.

## 3. Buat config tunnel di VPS

Ikutin pola yang sudah kamu pakai buat `koperasi`, `sonarqube`, dll — satu folder per app di bawah
`~/.cloudflared/`.

`~/.cloudflared/gowa/config.yml`:

```yaml
tunnel: d0cfe822-70b4-4466-9d2d-f8efa1250f2a
credentials-file: /home/<user>/.cloudflared/gowa/d0cfe822-70b4-4466-9d2d-f8efa1250f2a.json

ingress:
  - hostname: gowatokenzrey.my.id
    service: http://localhost:3000
  - service: http_status:404
```

Ganti `<user>` sesuai username VPS kamu (path harus absolut, sama kayak `credentials-file` di
`koperasi/config.yml` punyamu).

## 4. Daftarin sebagai systemd service

Sama persis pola `koperasi/setup-service.sh` kamu:

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

Jangan `start` dulu — nyalain GOWA-nya dulu di langkah berikut, biar pas tunnel connect,
`localhost:3000` sudah ada yang jawab.

## 5. Jalankan GOWA di VPS

Masuk ke folder repo di VPS, lalu **jalankan cuma service app-nya** — `cloudflared` biar systemd
yang pegang (bukan sidecar di `docker-compose.yml`, itu dipakai buat setup lokal doang):

```bash
cd ~/path/ke/repo
docker compose up -d --build whatsapp_go
```

Cek jalan:

```bash
curl -s http://localhost:3000/health
# harus: OK
```

## 6. Nyalain tunnel

```bash
sudo systemctl start cloudflared-gowa
sudo systemctl status cloudflared-gowa --no-pager
```

Log harus nunjukin `Registered tunnel connection` beberapa kali (mirip yang muncul pas kamu setup
di lokal kemarin).

## 7. Verifikasi dari luar

```bash
curl -s https://gowatokenzrey.my.id/health
# harus: OK
```

Kalau ini sukses **sambil tunnel lokal masih nyala**, dua origin (lokal + VPS) sama-sama ngelayanin
domain yang sama untuk sementara (Cloudflare load-balance otomatis antar koneksi tunnel yang sama).
Aman — tidak akan bentrok, tinggal matiin yang lokal kapan saja di langkah berikut.

## 8. Login WhatsApp (kalau belum pernah login sama sekali)

```bash
curl -u admin:<password> -X POST https://gowatokenzrey.my.id/devices
curl -u admin:<password> https://gowatokenzrey.my.id/devices/<device_id>/login
# scan QR dari WhatsApp: Linked Devices > Link a Device
```

Kalau kamu sudah pernah login di lokal dan copy folder `storages/` + `statics/` ke VPS (lihat tabel
di atas), lewati langkah ini — sesi WhatsApp-nya udah ikut kepindah.

## 9. Matiin yang di lokal

Setelah VPS kebukti jalan dan `https://gowatokenzrey.my.id/health` sukses lewat VPS:

```bash
# di WSL lokal
cd /path/ke/repo
docker compose down
```

Ini matiin dua-duanya sekaligus — container app (`whatsapp_go`) dan sidecar `cloudflared` lokal
(karena keduanya didefinisikan di [docker-compose.yml](../docker-compose.yml) yang sama).

Selesai — semua traffic sekarang lewat VPS, domain tidak berubah, tidak ada downtime kalau urutan
di atas diikuti (nyalain VPS dulu, baru matiin lokal).

## Troubleshooting cepat

| Gejala | Penyebab umum | Fix |
|---|---|---|
| `cloudflared-gowa` gagal start, log bilang credentials file not found | Path di `config.yml` salah / file belum ke-copy | Cek `ls ~/.cloudflared/gowa/`, pastikan nama file cocok persis dengan `credentials-file` |
| Tunnel connect tapi `curl https://gowatokenzrey.my.id/health` timeout/502 | `whatsapp_go` container belum jalan atau port beda | `docker compose ps`, `curl localhost:3000/health` di VPS dulu |
| `docker compose up` gagal build, error `exec /entrypoint.sh: no such file` | Repo di-clone/di-copy dengan line ending CRLF (biasanya kalau di-zip dari Windows) | Pastikan clone lewat `git clone` langsung di VPS (Linux), bukan copy folder dari Windows — `.gitattributes` di repo ini sudah handle ini otomatis kalau lewat git |
| Basic Auth ditolak di VPS padahal password sama | `src/.env` belum ke-copy atau beda isi | Diff `src/.env` lokal vs VPS, pastikan `APP_BASIC_AUTH` sama |
