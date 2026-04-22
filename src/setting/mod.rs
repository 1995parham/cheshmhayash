use std::time::Duration;

use config::{Config, ConfigError, Environment, File};
use serde::Deserialize;

const DEFAULT_REQUEST_TIMEOUT_MS: u64 = 2_000;
const DEFAULT_DISCOVERY_TIMEOUT_MS: u64 = 500;

#[derive(Debug, Clone, Deserialize)]
pub struct Server {
    host: String,
    port: u16,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Nats {
    name: String,
    url: String,
    #[serde(default)]
    creds_file: Option<String>,
    #[serde(default)]
    user: Option<String>,
    #[serde(default)]
    password: Option<String>,
    #[serde(default = "default_request_timeout_ms")]
    request_timeout_ms: u64,
    #[serde(default = "default_discovery_timeout_ms")]
    discovery_timeout_ms: u64,
}

fn default_request_timeout_ms() -> u64 {
    DEFAULT_REQUEST_TIMEOUT_MS
}

fn default_discovery_timeout_ms() -> u64 {
    DEFAULT_DISCOVERY_TIMEOUT_MS
}

#[derive(Debug, Clone, Deserialize)]
pub struct Settings {
    server: Server,
    nats: Vec<Nats>,
}

impl Settings {
    pub fn new() -> Result<Self, ConfigError> {
        Config::builder()
            .add_source(File::with_name("config/default"))
            .add_source(File::with_name("settings").required(false))
            .add_source(
                Environment::with_prefix("CHESHMHAYASH")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()?
            .try_deserialize()
    }

    pub fn server(&self) -> &Server {
        &self.server
    }

    pub fn nats(&self) -> &[Nats] {
        &self.nats
    }
}

impl Server {
    pub fn port(&self) -> u16 {
        self.port
    }

    pub fn host(&self) -> &str {
        &self.host
    }
}

impl Nats {
    pub fn name(&self) -> &str {
        &self.name
    }

    pub fn url(&self) -> &str {
        &self.url
    }

    pub fn creds_file(&self) -> Option<&str> {
        self.creds_file.as_deref()
    }

    pub fn user_password(&self) -> Option<(&str, &str)> {
        match (self.user.as_deref(), self.password.as_deref()) {
            (Some(u), Some(p)) => Some((u, p)),
            _ => None,
        }
    }

    pub fn request_timeout(&self) -> Duration {
        Duration::from_millis(self.request_timeout_ms)
    }

    pub fn discovery_timeout(&self) -> Duration {
        Duration::from_millis(self.discovery_timeout_ms)
    }
}
