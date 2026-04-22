mod admin;
mod healthz;
mod jsm;

pub use admin::*;
pub use healthz::*;
pub use jsm::*;

use serde::Serialize;

#[derive(Serialize)]
pub struct ApiError {
    message: String,
}

impl ApiError {
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
        }
    }
}
