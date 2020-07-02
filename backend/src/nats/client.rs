use super::connz::Connz;
use super::varz::Varz;

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

    pub async fn connz(&self, offset: u64, limit: u64) -> Result<Connz, reqwest::Error> {
        let client = reqwest::Client::new();

        let connz = client
            .get(format!("{}/connz", self.url).as_str())
            .query(&[("offset", offset), ("limit", limit), ("subs", 1)])
            .send().await?.json::<Connz>().await?;
        Ok(connz)
    }

    pub async fn varz(&self) -> Result<Varz, reqwest::Error> {
        let client = reqwest::Client::new();

        let connz = client
            .get(format!("{}/varz", self.url).as_str())
            .send().await?.json::<Varz>().await?;
        Ok(connz)
    }
}
