use chrono::{DateTime, Utc};

#[derive(Debug)]
pub struct Connz {
    server_id: String,
    now: DateTime<Utc>,
    num_connections: u64,
    total: u64,
    offset: u64,
    limit: u64,
}
