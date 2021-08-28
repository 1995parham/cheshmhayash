use super::connz::Connz;
use super::varz::Varz;

use actix_web::client;

#[derive(Clone)]
pub struct Client {
    url: String,
}

impl Client {
    pub fn new(url: &str) -> Client {
        Client {
            url: String::from(url),
        }
    }

    pub async fn connz(&self, offset: u64, limit: u64) -> Result<Connz, actix_web::Error> {
        let client = client::Client::new();

        let connz = client
            .get(format!("{}/connz", self.url).as_str())
            .query(&[("offset", offset), ("limit", limit), ("subs", 1)])?
            .send()
            .await?
            .json::<Connz>()
            .await?;
        Ok(connz)
    }

    pub async fn varz(&self) -> Result<Varz, actix_web::Error> {
        let client = client::Client::new();

        let connz = client
            .get(format!("{}/varz", self.url).as_str())
            .send()
            .await?
            .json::<Varz>()
            .await?;
        Ok(connz)
    }
}
