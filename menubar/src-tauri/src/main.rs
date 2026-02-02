//! Meept menubar application.
//!
//! A macOS system-tray (menubar) app built with Tauri 2.x that communicates
//! with the meept daemon over a Unix domain socket using length-prefixed
//! JSON-RPC 2.0 framing.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use tauri::{
    image::Image,
    menu::{MenuBuilder, MenuItemBuilder, PredefinedMenuItem},
    tray::TrayIconEvent,
    Manager, WebviewUrl, WebviewWindowBuilder,
};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::UnixStream;

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 types
// ---------------------------------------------------------------------------

/// Monotonic counter for JSON-RPC request IDs.
static REQUEST_ID: AtomicU64 = AtomicU64::new(1);

#[derive(Serialize)]
struct JsonRpcRequest<'a> {
    jsonrpc: &'a str,
    method: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    params: Option<serde_json::Value>,
    id: u64,
}

#[derive(Deserialize, Debug)]
struct JsonRpcResponse {
    #[allow(dead_code)]
    jsonrpc: Option<String>,
    result: Option<serde_json::Value>,
    error: Option<JsonRpcError>,
    #[allow(dead_code)]
    id: Option<serde_json::Value>,
}

#[derive(Deserialize, Debug)]
struct JsonRpcError {
    #[allow(dead_code)]
    code: i64,
    message: String,
    #[allow(dead_code)]
    data: Option<serde_json::Value>,
}

// ---------------------------------------------------------------------------
// Socket path helper
// ---------------------------------------------------------------------------

/// Return the path to the meept daemon Unix socket.
fn socket_path() -> PathBuf {
    dirs::home_dir()
        .unwrap_or_else(|| PathBuf::from("/tmp"))
        .join(".meept")
        .join("meept.sock")
}

// ---------------------------------------------------------------------------
// Wire-level I/O helpers  (length-prefixed framing)
//
// Protocol:  {byte_length}\n{json_payload}
// ---------------------------------------------------------------------------

/// Send a length-prefixed JSON-RPC frame and read the response.
async fn rpc_call(
    method: &str,
    params: Option<serde_json::Value>,
) -> Result<serde_json::Value, String> {
    let sock = socket_path();

    let mut stream = UnixStream::connect(&sock)
        .await
        .map_err(|e| format!("Failed to connect to daemon at {}: {}", sock.display(), e))?;

    let id = REQUEST_ID.fetch_add(1, Ordering::Relaxed);
    let request = JsonRpcRequest {
        jsonrpc: "2.0",
        method,
        params,
        id,
    };

    let payload = serde_json::to_string(&request)
        .map_err(|e| format!("Failed to serialise request: {e}"))?;
    let payload_bytes = payload.as_bytes();

    // Write: {length}\n{json}
    let header = format!("{}\n", payload_bytes.len());
    stream
        .write_all(header.as_bytes())
        .await
        .map_err(|e| format!("Failed to write header: {e}"))?;
    stream
        .write_all(payload_bytes)
        .await
        .map_err(|e| format!("Failed to write payload: {e}"))?;
    stream
        .flush()
        .await
        .map_err(|e| format!("Failed to flush: {e}"))?;

    // Read response: {length}\n{json}
    let mut length_buf = Vec::new();
    loop {
        let mut byte = [0u8; 1];
        let n = stream
            .read(&mut byte)
            .await
            .map_err(|e| format!("Failed to read response length: {e}"))?;
        if n == 0 {
            return Err("Connection closed before response length received".into());
        }
        if byte[0] == b'\n' {
            break;
        }
        length_buf.push(byte[0]);
    }

    let length_str = String::from_utf8(length_buf)
        .map_err(|e| format!("Response length is not valid UTF-8: {e}"))?;
    let length: usize = length_str
        .trim()
        .parse()
        .map_err(|e| format!("Invalid response length '{}': {}", length_str, e))?;

    if length == 0 || length > 10 * 1024 * 1024 {
        return Err(format!("Response length out of range: {length}"));
    }

    let mut response_buf = vec![0u8; length];
    stream
        .read_exact(&mut response_buf)
        .await
        .map_err(|e| format!("Failed to read response payload: {e}"))?;

    let response_str = String::from_utf8(response_buf)
        .map_err(|e| format!("Response payload is not valid UTF-8: {e}"))?;

    let response: JsonRpcResponse = serde_json::from_str(&response_str)
        .map_err(|e| format!("Failed to parse response JSON: {e}"))?;

    if let Some(err) = response.error {
        return Err(format!("Daemon error: {}", err.message));
    }

    Ok(response.result.unwrap_or(serde_json::Value::Null))
}

// ---------------------------------------------------------------------------
// Tauri commands (invocable from the frontend)
// ---------------------------------------------------------------------------

/// Query the daemon for its current status.
#[tauri::command]
async fn get_status() -> Result<String, String> {
    let result = rpc_call("status", None).await?;
    serde_json::to_string_pretty(&result).map_err(|e| format!("JSON format error: {e}"))
}

/// Send a chat message to the daemon and return the reply.
#[tauri::command]
async fn send_chat(message: String) -> Result<String, String> {
    if message.trim().is_empty() {
        return Err("Message cannot be empty".into());
    }

    let params = serde_json::json!({
        "message": message,
    });

    let result = rpc_call("chat", Some(params)).await?;

    // Extract the reply text; fall back to the full JSON blob.
    if let Some(reply) = result.get("reply").and_then(|v| v.as_str()) {
        Ok(reply.to_string())
    } else {
        serde_json::to_string_pretty(&result).map_err(|e| format!("JSON format error: {e}"))
    }
}

/// Check whether the daemon socket is reachable.
#[tauri::command]
async fn get_daemon_connected() -> Result<bool, String> {
    let sock = socket_path();
    match UnixStream::connect(&sock).await {
        Ok(_) => Ok(true),
        Err(_) => Ok(false),
    }
}

// ---------------------------------------------------------------------------
// Tray icon colour helpers (green = connected, orange = disconnected)
// ---------------------------------------------------------------------------

/// 22x22 green circle PNG (pre-rendered at compile time).
fn green_icon_bytes() -> Vec<u8> {
    generate_circle_png(22, [0x34, 0xC7, 0x59, 0xFF])
}

/// 22x22 orange circle PNG (pre-rendered at compile time).
fn orange_icon_bytes() -> Vec<u8> {
    generate_circle_png(22, [0xFF, 0x9F, 0x0A, 0xFF])
}

/// Generate a minimal RGBA image of a filled circle.
/// Returns raw RGBA pixel data suitable for `Image::from_rgba`.
fn generate_circle_rgba(size: u32, color: [u8; 4]) -> Vec<u8> {
    let mut pixels = vec![0u8; (size * size * 4) as usize];
    let center = size as f64 / 2.0;
    let radius = center - 1.0;

    for y in 0..size {
        for x in 0..size {
            let dx = x as f64 - center + 0.5;
            let dy = y as f64 - center + 0.5;
            let dist = (dx * dx + dy * dy).sqrt();
            let offset = ((y * size + x) * 4) as usize;
            if dist <= radius {
                pixels[offset] = color[0];
                pixels[offset + 1] = color[1];
                pixels[offset + 2] = color[2];
                pixels[offset + 3] = color[3];
            }
            // else stays transparent (0,0,0,0)
        }
    }
    pixels
}

/// Generate a tiny uncompressed PNG from RGBA data.
/// This avoids needing the `png` crate for a simple icon.
fn generate_circle_png(size: u32, color: [u8; 4]) -> Vec<u8> {
    let rgba = generate_circle_rgba(size, color);

    // We produce a minimal valid PNG file by hand.
    // For simplicity, use uncompressed DEFLATE blocks (store).
    let mut png = Vec::new();

    // PNG signature
    png.extend_from_slice(&[137, 80, 78, 71, 13, 10, 26, 10]);

    // IHDR chunk
    let mut ihdr_data = Vec::new();
    ihdr_data.extend_from_slice(&size.to_be_bytes()); // width
    ihdr_data.extend_from_slice(&size.to_be_bytes()); // height
    ihdr_data.push(8); // bit depth
    ihdr_data.push(6); // color type: RGBA
    ihdr_data.push(0); // compression
    ihdr_data.push(0); // filter
    ihdr_data.push(0); // interlace
    write_png_chunk(&mut png, b"IHDR", &ihdr_data);

    // IDAT chunk: raw image data, each row prefixed with filter byte 0 (None)
    let mut raw_rows = Vec::new();
    for y in 0..size {
        raw_rows.push(0u8); // filter: None
        let row_start = (y * size * 4) as usize;
        let row_end = row_start + (size * 4) as usize;
        raw_rows.extend_from_slice(&rgba[row_start..row_end]);
    }

    // Wrap in a zlib stream using store (no compression)
    let idat_data = zlib_store(&raw_rows);
    write_png_chunk(&mut png, b"IDAT", &idat_data);

    // IEND chunk
    write_png_chunk(&mut png, b"IEND", &[]);

    png
}

fn write_png_chunk(out: &mut Vec<u8>, chunk_type: &[u8; 4], data: &[u8]) {
    let len = data.len() as u32;
    out.extend_from_slice(&len.to_be_bytes());
    out.extend_from_slice(chunk_type);
    out.extend_from_slice(data);
    let mut crc_input = Vec::with_capacity(4 + data.len());
    crc_input.extend_from_slice(chunk_type);
    crc_input.extend_from_slice(data);
    let crc = crc32(&crc_input);
    out.extend_from_slice(&crc.to_be_bytes());
}

/// CRC-32 as required by PNG (ISO 3309 / ITU-T V.42).
fn crc32(data: &[u8]) -> u32 {
    let mut crc: u32 = 0xFFFF_FFFF;
    for &byte in data {
        crc ^= byte as u32;
        for _ in 0..8 {
            if crc & 1 != 0 {
                crc = (crc >> 1) ^ 0xEDB8_8320;
            } else {
                crc >>= 1;
            }
        }
    }
    crc ^ 0xFFFF_FFFF
}

/// Minimal zlib wrapper using DEFLATE "store" (no compression) blocks.
fn zlib_store(data: &[u8]) -> Vec<u8> {
    let mut out = Vec::new();

    // zlib header: CM=8 (deflate), CINFO=7 (32K window), FCHECK to make it valid
    // CMF = 0x78, FLG = 0x01 (no dict, FLEVEL=0, FCHECK=1 => 0x7801 % 31 == 0)
    out.push(0x78);
    out.push(0x01);

    // DEFLATE stored blocks. Each block can hold at most 65535 bytes.
    let mut remaining = data;
    while !remaining.is_empty() {
        let block_size = remaining.len().min(65535);
        let is_last = block_size == remaining.len();
        out.push(if is_last { 0x01 } else { 0x00 }); // BFINAL + BTYPE=00 (stored)
        let len = block_size as u16;
        let nlen = !len;
        out.extend_from_slice(&len.to_le_bytes());
        out.extend_from_slice(&nlen.to_le_bytes());
        out.extend_from_slice(&remaining[..block_size]);
        remaining = &remaining[block_size..];
    }

    // Adler-32 checksum of the original data
    let adler = adler32(data);
    out.extend_from_slice(&adler.to_be_bytes());

    out
}

fn adler32(data: &[u8]) -> u32 {
    let mut a: u32 = 1;
    let mut b: u32 = 0;
    for &byte in data {
        a = (a + byte as u32) % 65521;
        b = (b + a) % 65521;
    }
    (b << 16) | a
}

// ---------------------------------------------------------------------------
// Application entry point
// ---------------------------------------------------------------------------

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            get_status,
            send_chat,
            get_daemon_connected,
        ])
        .setup(|app| {
            // ---------------------------------------------------------------
            // Build tray menu
            // ---------------------------------------------------------------
            let status_item = MenuItemBuilder::with_id("status", "Status: checking...")
                .enabled(false)
                .build(app)?;

            let open_chat = MenuItemBuilder::with_id("open_chat", "Open Chat").build(app)?;

            let separator = PredefinedMenuItem::separator(app)?;

            let quit = MenuItemBuilder::with_id("quit", "Quit meept").build(app)?;

            let menu = MenuBuilder::new(app)
                .item(&status_item)
                .item(&separator)
                .item(&open_chat)
                .item(&separator)
                .item(&quit)
                .build()?;

            // ---------------------------------------------------------------
            // Set up tray icon with initial (orange / disconnected) icon
            // ---------------------------------------------------------------
            let tray = app.tray_by_id("meept-tray").expect("tray icon not found");
            tray.set_menu(Some(menu))?;
            tray.set_tooltip(Some("meept - disconnected"))?;

            // Set initial tray icon to orange (disconnected)
            let orange_png = orange_icon_bytes();
            if let Ok(icon) = Image::from_png_bytes(&orange_png) {
                let _ = tray.set_icon(Some(icon));
            }

            // ---------------------------------------------------------------
            // Handle tray menu events
            // ---------------------------------------------------------------
            let app_handle = app.handle().clone();
            tray.on_menu_event(move |tray_handle, event| {
                match event.id().as_ref() {
                    "open_chat" => {
                        // Show or focus the chat window.
                        if let Some(window) = tray_handle.app_handle().get_webview_window("chat") {
                            let _ = window.show();
                            let _ = window.set_focus();
                        } else {
                            let _ = WebviewWindowBuilder::new(
                                tray_handle.app_handle(),
                                "chat",
                                WebviewUrl::App("index.html".into()),
                            )
                            .title("meept")
                            .inner_size(360.0, 480.0)
                            .min_inner_size(300.0, 400.0)
                            .resizable(true)
                            .decorations(true)
                            .build();
                        }
                    }
                    "quit" => {
                        tray_handle.app_handle().exit(0);
                    }
                    _ => {}
                }
            });

            // Handle tray icon click (left click opens chat)
            let app_handle2 = app_handle.clone();
            tray.on_tray_icon_event(move |_tray, event| {
                if let TrayIconEvent::Click { button, .. } = &event {
                    if *button == tauri::tray::MouseButton::Left {
                        if let Some(window) = app_handle2.get_webview_window("chat") {
                            let _ = window.show();
                            let _ = window.set_focus();
                        } else {
                            let _ = WebviewWindowBuilder::new(
                                &app_handle2,
                                "chat",
                                WebviewUrl::App("index.html".into()),
                            )
                            .title("meept")
                            .inner_size(360.0, 480.0)
                            .min_inner_size(300.0, 400.0)
                            .resizable(true)
                            .decorations(true)
                            .build();
                        }
                    }
                }
            });

            // ---------------------------------------------------------------
            // Background task: poll daemon connectivity and update tray icon
            // ---------------------------------------------------------------
            let app_handle3 = app_handle.clone();
            tauri::async_runtime::spawn(async move {
                let mut was_connected = false;

                loop {
                    let connected = match UnixStream::connect(socket_path()).await {
                        Ok(_stream) => true,
                        Err(_) => false,
                    };

                    if connected != was_connected {
                        if let Some(tray) = app_handle3.tray_by_id("meept-tray") {
                            if connected {
                                let png = green_icon_bytes();
                                if let Ok(icon) = Image::from_png_bytes(&png) {
                                    let _ = tray.set_icon(Some(icon));
                                }
                                let _ = tray.set_tooltip(Some("meept - connected"));
                            } else {
                                let png = orange_icon_bytes();
                                if let Ok(icon) = Image::from_png_bytes(&png) {
                                    let _ = tray.set_icon(Some(icon));
                                }
                                let _ = tray.set_tooltip(Some("meept - disconnected"));
                            }

                            // Update the status menu item text
                            if let Some(menu) = tray.menu() {
                                if let Some(item) = menu.get("status") {
                                    if let Some(mi) = item.as_menuitem() {
                                        let text = if connected {
                                            "Status: connected"
                                        } else {
                                            "Status: disconnected"
                                        };
                                        let _ = mi.set_text(text);
                                    }
                                }
                            }
                        }
                        was_connected = connected;
                    }

                    tokio::time::sleep(std::time::Duration::from_secs(5)).await;
                }
            });

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running meept-menubar");
}
