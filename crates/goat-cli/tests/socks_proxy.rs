//! goat-cli's HTTP clients take their proxy from the environment, and the
//! zagros-dev recipe sets `ALL_PROXY=socks5h://...`. reqwest only understands a
//! SOCKS proxy URL when built with its `socks` feature (#1942). This test
//! drives a request through a minimal in-process SOCKS5 listener and checks
//! that the client speaks SOCKS5 and leaves name resolution to the proxy
//! (the `h` in `socks5h`), which is what reaching a goatnet over a jump host
//! needs.

use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::mpsc;
use std::thread;
use std::time::Duration;

/// Accept one SOCKS5 client, complete the no-auth greeting, read the CONNECT
/// request, and report the destination host the client asked for. Replies
/// with a general failure, so the client request itself errors.
fn fake_socks5_proxy() -> (String, mpsc::Receiver<Result<String, String>>) {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind fake proxy");
    let addr = listener.local_addr().expect("proxy addr");
    let (tx, rx) = mpsc::channel();
    thread::spawn(move || {
        let result = (|| -> Result<String, String> {
            let (mut conn, _) = listener.accept().map_err(|e| e.to_string())?;
            conn.set_read_timeout(Some(Duration::from_secs(5)))
                .map_err(|e| e.to_string())?;
            let mut head = [0u8; 2];
            conn.read_exact(&mut head).map_err(|e| e.to_string())?;
            if head[0] != 0x05 {
                return Err(format!("greeting version {:#x}, want SOCKS5", head[0]));
            }
            let mut methods = vec![0u8; usize::from(head[1])];
            conn.read_exact(&mut methods).map_err(|e| e.to_string())?;
            conn.write_all(&[0x05, 0x00]).map_err(|e| e.to_string())?;
            let mut req = [0u8; 4];
            conn.read_exact(&mut req).map_err(|e| e.to_string())?;
            if req[3] != 0x03 {
                return Err(format!(
                    "address type {:#x}: the client resolved the name itself instead of sending it to the proxy",
                    req[3]
                ));
            }
            let mut len = [0u8; 1];
            conn.read_exact(&mut len).map_err(|e| e.to_string())?;
            let mut host = vec![0u8; usize::from(len[0])];
            conn.read_exact(&mut host).map_err(|e| e.to_string())?;
            let _ = conn.write_all(&[0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0]);
            String::from_utf8(host).map_err(|e| e.to_string())
        })();
        let _ = tx.send(result);
    });
    (format!("socks5h://{addr}"), rx)
}

#[tokio::test(flavor = "multi_thread")]
async fn socks5h_proxy_url_is_supported_and_resolves_remotely() {
    let (proxy_url, seen) = fake_socks5_proxy();
    let proxy = reqwest::Proxy::all(&proxy_url)
        .expect("reqwest rejected a socks5h:// proxy URL; is the `socks` feature enabled?");
    let client = reqwest::Client::builder()
        .proxy(proxy)
        .timeout(Duration::from_secs(5))
        .build()
        .expect("build client");

    // The proxy refuses the CONNECT, so the request fails; what matters is
    // what reached the proxy.
    let _ = client.get("http://goatnet-target.invalid/").send().await;

    let host = seen
        .recv_timeout(Duration::from_secs(5))
        .expect("the client never reached the SOCKS proxy")
        .expect("SOCKS handshake");
    assert_eq!(host, "goatnet-target.invalid");
}
