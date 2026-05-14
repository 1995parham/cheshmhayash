//! JetStream management over `$JS.API.*` — raw subject request/reply so
//! the response payload flows through unchanged to the HTTP client.

use bytes::Bytes;
use serde_json::Value;

use super::admin::{discover, request_json, AdminError};
use super::Cluster;

/// Cluster-wide JetStream snapshot via `$SYS.REQ.SERVER.PING.JSZ`. With a
/// system-account connection this returns per-server JS state including
/// every account, stream, and consumer the server is aware of.
pub async fn jsm_overview(cluster: &Cluster) -> Result<Vec<Value>, AdminError> {
    let payload = serde_json::to_vec(&serde_json::json!({
        "accounts": true,
        "streams": true,
        "consumer": true,
        "config": true,
        "raft": true,
    }))?;
    discover(
        cluster,
        "$SYS.REQ.SERVER.PING.JSZ".to_string(),
        Bytes::from(payload),
        cluster.discovery_timeout(),
    )
    .await
}

pub async fn list_streams(cluster: &Cluster, offset: u64) -> Result<Value, AdminError> {
    let payload = serde_json::to_vec(&serde_json::json!({ "offset": offset }))?;
    request_json(
        cluster,
        "$JS.API.STREAM.LIST".to_string(),
        Bytes::from(payload),
    )
    .await
}

pub async fn stream_info(cluster: &Cluster, name: &str) -> Result<Value, AdminError> {
    request_json(cluster, format!("$JS.API.STREAM.INFO.{name}"), Bytes::new()).await
}

/// Update an existing stream's configuration. The payload must be a full
/// `StreamConfig` JSON (NATS rejects partial updates). Only certain fields
/// can change without recreate — see NATS docs.
pub async fn update_stream(cluster: &Cluster, name: &str, config: Value) -> Result<Value, AdminError> {
    let payload = serde_json::to_vec(&config)?;
    request_json(
        cluster,
        format!("$JS.API.STREAM.UPDATE.{name}"),
        Bytes::from(payload),
    )
    .await
}

pub async fn purge_stream(cluster: &Cluster, name: &str) -> Result<Value, AdminError> {
    request_json(
        cluster,
        format!("$JS.API.STREAM.PURGE.{name}"),
        Bytes::new(),
    )
    .await
}

pub async fn delete_stream(cluster: &Cluster, name: &str) -> Result<Value, AdminError> {
    request_json(
        cluster,
        format!("$JS.API.STREAM.DELETE.{name}"),
        Bytes::new(),
    )
    .await
}

pub async fn list_consumers(
    cluster: &Cluster,
    stream: &str,
    offset: u64,
) -> Result<Value, AdminError> {
    let payload = serde_json::to_vec(&serde_json::json!({ "offset": offset }))?;
    request_json(
        cluster,
        format!("$JS.API.CONSUMER.LIST.{stream}"),
        Bytes::from(payload),
    )
    .await
}

pub async fn consumer_info(
    cluster: &Cluster,
    stream: &str,
    consumer: &str,
) -> Result<Value, AdminError> {
    request_json(
        cluster,
        format!("$JS.API.CONSUMER.INFO.{stream}.{consumer}"),
        Bytes::new(),
    )
    .await
}

pub async fn delete_consumer(
    cluster: &Cluster,
    stream: &str,
    consumer: &str,
) -> Result<Value, AdminError> {
    request_json(
        cluster,
        format!("$JS.API.CONSUMER.DELETE.{stream}.{consumer}"),
        Bytes::new(),
    )
    .await
}
