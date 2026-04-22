use async_nats::ConnectOptions;
use thiserror::Error;

use crate::setting::Nats as NatsConfig;

#[derive(Debug, Error)]
pub enum ConnectError {
    #[error("connect to {url} failed: {source}")]
    Connect {
        url: String,
        #[source]
        source: async_nats::ConnectError,
    },
    #[error("reading credentials file {path} failed: {source}")]
    Credentials {
        path: String,
        #[source]
        source: std::io::Error,
    },
}

/// A live connection to a single NATS cluster, reused across all requests.
#[derive(Clone)]
pub struct Cluster {
    client: async_nats::Client,
    discovery_timeout: std::time::Duration,
}

impl Cluster {
    pub async fn connect(cfg: &NatsConfig) -> Result<Self, ConnectError> {
        let mut opts = ConnectOptions::new()
            .name(format!("cheshmhayash/{}", cfg.name()))
            .request_timeout(Some(cfg.request_timeout()))
            .retry_on_initial_connect();

        if let Some(path) = cfg.creds_file() {
            opts =
                opts.credentials_file(path)
                    .await
                    .map_err(|source| ConnectError::Credentials {
                        path: path.to_string(),
                        source,
                    })?;
        }
        if let Some((user, pass)) = cfg.user_password() {
            opts = opts.user_and_password(user.to_string(), pass.to_string());
        }

        let client = opts
            .connect(cfg.url())
            .await
            .map_err(|source| ConnectError::Connect {
                url: cfg.url().to_string(),
                source,
            })?;

        Ok(Self {
            client,
            discovery_timeout: cfg.discovery_timeout(),
        })
    }

    pub fn client(&self) -> &async_nats::Client {
        &self.client
    }

    pub fn discovery_timeout(&self) -> std::time::Duration {
        self.discovery_timeout
    }
}
