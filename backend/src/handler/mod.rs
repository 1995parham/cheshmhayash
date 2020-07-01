mod healthz;
mod nats;

pub use healthz::*;
pub use nats::*;
use serde::Serialize;

#[derive(Serialize)]
pub struct Error {
    message: String,
}
