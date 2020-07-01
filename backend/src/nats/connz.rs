use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize, Deserialize)]
pub struct Connz {
    server_id: String,
    now: DateTime<Utc>,
    num_connections: u64,
    total: u64,
    offset: u64,
    limit: u64,
}
