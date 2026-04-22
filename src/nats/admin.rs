//! Administrative operations over `$SYS.REQ.*` — the same channels natscli
//! uses. Each response is pass-through JSON so the client sees the same
//! payload the NATS server emits from its HTTP monitoring endpoints.

use std::time::Duration;

use bytes::Bytes;
use futures_util::StreamExt;
use serde_json::Value;
use thiserror::Error;
use tokio::time::Instant;

use super::Cluster;

#[derive(Debug, Error)]
pub enum AdminError {
    #[error("nats request failed: {0}")]
    Request(#[from] async_nats::RequestError),
    #[error("subscribing to reply inbox failed: {0}")]
    Subscribe(#[from] async_nats::SubscribeError),
    #[error("publishing discovery request failed: {0}")]
    Publish(#[from] async_nats::PublishError),
    #[error("decoding reply payload failed: {0}")]
    Decode(#[from] serde_json::Error),
}

/// Endpoint names valid under `$SYS.REQ.SERVER.<id>.<endpoint>` and
/// `$SYS.REQ.SERVER.PING.<endpoint>`.
pub const SERVER_ENDPOINTS: &[&str] = &[
    "VARZ", "CONNZ", "ROUTEZ", "GATEWAYZ", "LEAFZ", "SUBSZ", "JSZ", "ACCOUNTZ", "HEALTHZ", "STATSZ",
];

/// Endpoint names valid under `$SYS.REQ.ACCOUNT.<account-id>.<endpoint>`.
pub const ACCOUNT_ENDPOINTS: &[&str] = &["CONNZ", "LEAFZ", "SUBSZ", "JSZ", "INFO"];

/// Discover every server in the cluster. Publishes `$SYS.REQ.SERVER.PING`
/// and collects replies until the cluster's discovery timeout elapses.
pub async fn ping(cluster: &Cluster) -> Result<Vec<Value>, AdminError> {
    discover(
        cluster,
        "$SYS.REQ.SERVER.PING".to_string(),
        Bytes::new(),
        cluster.discovery_timeout(),
    )
    .await
}

/// Ping every server for the given endpoint (e.g. `VARZ`, `JSZ`).
pub async fn ping_endpoint(cluster: &Cluster, endpoint: &str) -> Result<Vec<Value>, AdminError> {
    let endpoint = endpoint.to_ascii_uppercase();
    discover(
        cluster,
        format!("$SYS.REQ.SERVER.PING.{endpoint}"),
        Bytes::new(),
        cluster.discovery_timeout(),
    )
    .await
}

/// Call a targeted server endpoint: `$SYS.REQ.SERVER.<id>.<endpoint>`.
pub async fn server_endpoint(
    cluster: &Cluster,
    id: &str,
    endpoint: &str,
) -> Result<Value, AdminError> {
    let endpoint = endpoint.to_ascii_uppercase();
    request_json(
        cluster,
        format!("$SYS.REQ.SERVER.{id}.{endpoint}"),
        Bytes::new(),
    )
    .await
}

/// Query an account-level endpoint. Multi-reply by design — every server in
/// the cluster answers with its view of the account.
pub async fn account_endpoint(
    cluster: &Cluster,
    account: &str,
    endpoint: &str,
) -> Result<Vec<Value>, AdminError> {
    let endpoint = endpoint.to_ascii_uppercase();
    discover(
        cluster,
        format!("$SYS.REQ.ACCOUNT.{account}.{endpoint}"),
        Bytes::new(),
        cluster.discovery_timeout(),
    )
    .await
}

/// Trigger a config reload on a specific server.
pub async fn reload(cluster: &Cluster, id: &str) -> Result<Value, AdminError> {
    request_json(
        cluster,
        format!("$SYS.REQ.SERVER.{id}.RELOAD"),
        Bytes::new(),
    )
    .await
}

/// Place a server into lame-duck mode (graceful drain of clients).
pub async fn lame_duck(cluster: &Cluster, id: &str) -> Result<Value, AdminError> {
    request_json(cluster, format!("$SYS.REQ.SERVER.{id}.LDM"), Bytes::new()).await
}

/// Kick (forcibly disconnect) a client connection by CID on the given
/// server. Payload shape follows the server's KICK handler:
/// `{"cid": <u64>}`.
pub async fn kick(cluster: &Cluster, id: &str, cid: u64) -> Result<Value, AdminError> {
    let payload = serde_json::to_vec(&serde_json::json!({ "cid": cid }))?;
    request_json(
        cluster,
        format!("$SYS.REQ.SERVER.{id}.KICK"),
        Bytes::from(payload),
    )
    .await
}

pub(crate) async fn request_json(
    cluster: &Cluster,
    subject: String,
    payload: Bytes,
) -> Result<Value, AdminError> {
    let reply = cluster.client().request(subject, payload).await?;
    let value: Value = serde_json::from_slice(&reply.payload)?;
    Ok(value)
}

async fn discover(
    cluster: &Cluster,
    subject: String,
    payload: Bytes,
    window: Duration,
) -> Result<Vec<Value>, AdminError> {
    let client = cluster.client();
    let inbox = client.new_inbox();
    let mut subscriber = client.subscribe(inbox.clone()).await?;

    client.publish_with_reply(subject, inbox, payload).await?;
    client.flush().await.ok();

    let deadline = Instant::now() + window;
    let mut replies = Vec::new();
    while let Ok(Some(msg)) = tokio::time::timeout_at(deadline, subscriber.next()).await {
        match serde_json::from_slice::<Value>(&msg.payload) {
            Ok(v) => replies.push(v),
            Err(err) => {
                tracing::warn!(%err, "discarding malformed sys reply");
            }
        }
    }
    Ok(replies)
}
