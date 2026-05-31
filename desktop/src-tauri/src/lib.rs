// Tauri shell for Tomo. The Rust side is thin: it drives the bundled `tomo` CLI
// (declared as a sidecar in tauri.conf.json) and relays its JSON output to the
// frontend — search returns the parsed result, download streams NDJSON progress
// events as `tomo://progress`.
use serde_json::Value;
use tauri::{AppHandle, Emitter, Manager};
use tauri_plugin_shell::{process::CommandEvent, ShellExt};

/// Search every searchable source (or just `source`) and return the parsed JSON.
#[tauri::command]
async fn search(
    app: AppHandle,
    query: String,
    source: String,
    limit: u32,
) -> Result<Value, String> {
    let lim = if limit == 0 { 10 } else { limit };
    let mut args: Vec<String> = vec![
        "search".into(),
        "--json".into(),
        "--limit".into(),
        lim.to_string(),
    ];
    if !source.is_empty() {
        args.push("--source".into());
        args.push(source);
    }
    args.push(query);

    let output = app
        .shell()
        .sidecar("tomo")
        .map_err(|e| e.to_string())?
        .args(args)
        .output()
        .await
        .map_err(|e| e.to_string())?;

    if !output.status.success() {
        return Err(String::from_utf8_lossy(&output.stderr).trim().to_string());
    }
    serde_json::from_slice(&output.stdout).map_err(|e| e.to_string())
}

/// Download a series, emitting each NDJSON progress line as a `tomo://progress`
/// event. Resolves when the sidecar exits.
#[tauri::command]
async fn download(
    app: AppHandle,
    url: String,
    chapters: String,
    out: String,
) -> Result<(), String> {
    let mut args: Vec<String> = vec!["--json".into(), "--out".into(), out];
    if !chapters.trim().is_empty() {
        args.push("--chapters".into());
        args.push(chapters);
    }
    args.push(url);

    let (mut rx, _child) = app
        .shell()
        .sidecar("tomo")
        .map_err(|e| e.to_string())?
        .args(args)
        .spawn()
        .map_err(|e| e.to_string())?;

    while let Some(event) = rx.recv().await {
        match event {
            CommandEvent::Stdout(line) => {
                if let Ok(v) = serde_json::from_slice::<Value>(&line) {
                    let _ = app.emit("tomo://progress", v);
                }
            }
            CommandEvent::Error(err) => {
                return Err(err);
            }
            _ => {}
        }
    }
    Ok(())
}

/// A sensible default output directory (~/Downloads/Tomo, falling back to home).
#[tauri::command]
fn default_output_dir(app: AppHandle) -> String {
    let base = app
        .path()
        .download_dir()
        .or_else(|_| app.path().home_dir());
    match base {
        Ok(p) => p.join("Tomo").to_string_lossy().to_string(),
        Err(_) => "Tomo".to_string(),
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(tauri::generate_handler![search, download, default_output_dir])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
