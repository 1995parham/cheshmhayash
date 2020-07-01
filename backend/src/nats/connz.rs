use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

// use std::time::Duration;

#[derive(Debug, Serialize, Deserialize)]
pub struct Connz {
    server_id: String,
    now: DateTime<Utc>,
    num_connections: u64,
    total: u64,
    offset: u64,
    limit: u64,
    connections: Vec<Connection>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct Connection {
    cid: u64,
    ip: String,
    port: u16,
    start: DateTime<Utc>,
    last_activity: DateTime<Utc>,
    // rtt: Duration,
    // uptime: Duration,
    // idle: Duration,
    pending_bytes: u64,
    in_msgs: u64,
    out_msgs: u64,
    in_bytes: u64,
    out_bytes: u64,
    subscriptions: u64,
    lang: String,
    version: String,
    subscriptions_list: Vec<String>,
}
