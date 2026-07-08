# Hosting your Tomo library with Komga + Tailscale

This folder turns the CBZ files Tomo produces into a **private, read-anywhere
manga server**. [Komga](https://komga.org/) serves your library; **Tailscale**
makes it reachable from your iPad (or anywhere) without exposing anything to the
public internet; **Tachimanga** reads it.

Nothing here is public — traffic only flows between your own devices over
Tailscale's encrypted WireGuard tunnel.

```
  Tomo ──writes CBZ──▶ /srv/manga ──served by──▶ Komga ──Tailscale──▶ Tachimanga (iPad)
```

## Before you start

- **Docker + Docker Compose** on the host.
- **An always-on machine** — a Raspberry Pi 4/5, a mini-PC, or a NAS. A laptop
  you close the lid on will sleep and take the server down with it, so don't use
  one as the host.
- A free **[Tailscale](https://tailscale.com/)** account.
- **Tachimanga** on the iPad.

Under ~50 GB the library fits comfortably on almost any disk or microSD.

## 1. Start Komga

```bash
cd deploy/komga
cp .env.example .env
# edit .env so MANGA_LIBRARY points at your CBZ folder (the same path you give `tomo -out`)
docker compose up -d
```

Check it's healthy:

```bash
docker compose logs -f komga    # Ctrl-C to stop tailing
```

> The compose file mounts your library read-only and runs as `user: "1000:1000"`.
> If your files are owned by a different user, run `id` on the host and update
> that line so Komga can read them.

## 2. First-run setup

Open `http://localhost:25600` (or `http://<host-ip>:25600` from another machine
on your LAN):

1. Create the **admin account**.
2. **Add Library** → set the root to `/data` (that's your `MANGA_LIBRARY`
   mounted inside the container). Name it e.g. *Manga*.
3. Komga scans and your series appear. Tomo's
   `<Series>/<Series> - Chapter NNNN[ - title].cbz` layout is parsed natively —
   correct series grouping, chapter order, and titles out of the box.

## 3. Reach it from anywhere with Tailscale

On the host:

```bash
# install per https://tailscale.com/download, then:
sudo tailscale up

# front Komga with a real HTTPS cert on your tailnet (recommended):
sudo tailscale serve --bg 25600
```

`tailscale serve` publishes Komga at `https://<host>.<your-tailnet>.ts.net`
with a valid TLS certificate. HTTPS matters because iOS prefers it (App
Transport Security) — this saves you fighting plain-HTTP warnings on the iPad.

Install Tailscale on the iPad and sign into the same account. Find your server's
name under `tailscale status` (or in the admin console). From here on, use that
`https://…ts.net` URL — it works on cellular, other Wi-Fi, anywhere.

## 4. Connect Tachimanga

Tachimanga is a Tachiyomi/Mihon fork, so you have two ways in — the extension is
the nicer one:

**A) Komga source extension (recommended)** — native browsing plus read-progress
sync back to the server.

1. Tachimanga → **Browse → Extensions** → install **Komga**.
2. Configure it with your server URL (`https://<host>.<your-tailnet>.ts.net`)
   and your Komga username/password.
3. Komga now shows up as a source you can browse, read, and track in.

**B) OPDS (simple fallback)** — Komga exposes an OPDS feed at:

```
https://<host>.<your-tailnet>.ts.net/opds/v1.2/catalog
```

Add that as an OPDS catalog in Tachimanga with your Komga credentials.

## 5. Keep the library fed with Tomo

Just point Tomo's output at the same folder Komga watches:

```bash
./tomo -out /srv/manga -chapters 1-20 <series-url>
```

Tomo is resumable (it skips CBZs that already exist), so re-running a series only
adds new chapters. Komga picks them up on its next scan — or trigger one from
**Server Settings → Scan library**.

Want it hands-off? List the ongoing series you want to track in `series.txt`
(one URL per line) and run [`refresh-library.sh`](refresh-library.sh) from cron —
it fetches new chapters for each into your library. Tomo writes new chapters
only, so every pass is cheap and idempotent.

If you know which day of the week a series updates, tag the URL with it and the
daily run will only check that series on its day. Untagged series are checked
every run — right for irregular schedules.

```
https://mangadex.org/title/<id>/shiba-inu-rooms     wed        # weekly, Wednesdays
https://weebcentral.com/series/<id>/Blue-Lock       tue        # weekly, Tuesdays
https://mangadex.org/title/<id>/gakuen-babysitters             # irregular → every run
https://weebcentral.com/series/<id>/Sousou-no-Frieren  tue  Sousou no Frieren
```

The last line shows the optional third field: a series-name override (passed to
`tomo -name`), for when a source titles the series differently from the folder
your library already uses — without it you'd get a second folder and a full
re-download. Tomo also matches existing chapters by number, not exact file
name, so a library whose history came from one source refreshes cleanly from
another even when their file naming differs.

A good way to find a series' day: look at the timestamps of its last few
chapters on the source site — weekly series land on the same weekday like
clockwork. Mind timezones: schedule the cron run *after* the usual release hour
in your local time, or a Tuesday chapter that drops at 15:00 UTC will be missed
by a Tuesday-morning run and wait a whole week.

```bash
cp series.txt.example series.txt
# add your series URLs, then try a full pass by hand (ignores day tags):
MANGA_LIBRARY=/srv/manga FORCE_ALL=1 ./refresh-library.sh
```

Once that works, schedule it daily — e.g. every evening at 9pm, logging to a file:

```cron
0 21 * * *  MANGA_LIBRARY=/srv/manga LOG_FILE=/var/log/tomo-refresh.log /path/to/Tomo/deploy/komga/refresh-library.sh
```

The script also reads its config (`MANGA_LIBRARY`, `TOMO_BIN`, `SERIES_FILE`,
`LOG_FILE`) from the `.env` file in this folder, so if you set them there you can
drop them from the cron line. It exits non-zero if any series failed, so cron
mail / your monitoring will surface a bad run. Each run logs how many chapters
each series gained and a summary line (`N checked, M skipped (not their day),
F failed, T new chapters`), so the log doubles as an audit trail of what arrived
when.

## Maintenance

- **Update Komga:** `docker compose pull && docker compose up -d`.
- **Back up** the `config/` folder — it holds Komga's database, accounts, and
  read progress. Your CBZs are already your library's source of truth.
- **Logs:** `docker compose logs -f komga`.
- **Stop / start:** `docker compose down` / `docker compose up -d`.

## Prefer Kavita?

[Kavita](https://www.kavitareader.com/) is an equally good alternative with its
own reader and OPDS support; Tachimanga has a Kavita extension too. Swap the
`image:` and port in `docker-compose.yml` if you'd rather use it — the Tailscale
and Tomo-feeding steps are identical.
