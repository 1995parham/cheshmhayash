use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct Server {
    host: String,
    port: u32,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Nats {
    monitoring: String,
    name: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Settings {
    server: Server,
    nats: Vec<Nats>,
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

    pub fn nats(&self) -> &[Nats] {
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

impl Nats {
    pub fn monitoring(&self) -> &str {
        self.monitoring.as_str()
    }

    pub fn name(&self) -> &str {
        self.name.as_str()
    }
}
