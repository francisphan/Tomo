# Tomo Desktop

A small Tauri desktop app for [Tomo](../README.md): search for a series, pick
it, choose chapters, and watch them download as CBZ — no command line needed.

The heavy lifting is still the Go `tomo` CLI, which is **bundled as a Tauri
sidecar**. The Rust shell just spawns it (`tomo search --json …` /
`tomo --json …`) and relays its JSON output to the React UI. No logic is
duplicated in Rust.

```
desktop/
  src/                 React UI (App.tsx, api.ts, styles.css)
  src-tauri/           Rust shell
    src/lib.rs         search / download / default_output_dir commands
    tauri.conf.json    bundles binaries/tomo as a sidecar
    capabilities/      allows running the tomo sidecar
    binaries/          tomo-<target-triple>  (built from ../.. Go source)
  scripts/
    build-sidecar.sh   builds the Go CLI into src-tauri/binaries/
```

## Develop / build

Requires Node, Rust, the Go toolchain, and the platform's Tauri prerequisites
(on Linux: `libwebkit2gtk-4.1-dev`, `librsvg2-dev`, `patchelf`,
`build-essential`).

```bash
cd desktop
npm install
npm run tauri:dev      # builds the Go sidecar, then runs the app with hot reload
npm run tauri:build    # builds the Go sidecar, then produces installers
```

Both scripts run `build-sidecar.sh` first, which compiles the Go CLI for the
host platform and places it at `src-tauri/binaries/tomo-<target-triple>` so
Tauri can bundle it.

## How the pieces talk

- **search** → `tomo search --json <query>` → parsed JSON returned to the UI.
- **download** → `tomo --json --out <dir> [--chapters <spec>] <url>` → each
  NDJSON progress line is re-emitted to the frontend as a `tomo://progress`
  event (series info, per-chapter status, final summary) to drive the progress
  bar.

## Releases & signing

`.github/workflows/release-desktop.yml` builds installers for macOS
(Intel + Apple Silicon), Windows and Linux on a version tag. Builds are
**unsigned** — macOS Gatekeeper / Windows SmartScreen will warn until signing
certificates are configured. Add the relevant secrets and Tauri signing config
when you're ready to distribute widely.

## Icons

The icons under `src-tauri/icons/` are placeholders copied from Peyto. Replace
them (e.g. `npm run tauri icon path/to/tomo-logo.png`) before a real release.
