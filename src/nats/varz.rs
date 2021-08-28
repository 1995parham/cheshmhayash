use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize, Deserialize)]
pub struct Varz {
    server_id: String,
    server_name: String,
    version: String,
    git_commit: String,
    go: String,
    proto: u64,
    host: String,
    port: u16,
    http_host: String,
    http_port: u16,
}
