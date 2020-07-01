use config;
use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct Server {
    host: String,
    port: u32,
}

#[derive(Debug, Clone, Deserialize)]
pub struct NATS {
    monitoring: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Settings {
    server: Server,
    nats: NATS,
}

impl Settings {
    pub fn new() -> Result<Self, config::ConfigError> {
        let mut settings = config::Config::new();

        settings
            .merge(config::File::with_name("config/default"))?
            .merge(config::Environment::with_prefix("CHESHMHAYASH"))?;

        settings.merge(config::File::with_name("settings")).ok();

        settings.try_into()
    }

    pub fn server(&self) -> &Server {
        &self.server
    }

    pub fn nats(&self) -> &NATS {
        &self.nats
    }
}

impl Server {
    pub fn port(&self) -> u32 {
        self.port
    }

    pub fn host(&self) -> &str {
        self.host.as_str()
    }
}

impl NATS {
    pub fn monitoring(&self) -> &str {
        self.monitoring.as_str()
    }
}
