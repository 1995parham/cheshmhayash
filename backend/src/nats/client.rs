use reqwest;

pub struct Client {
    url: String,
}

impl Client {
    pub fn new(url: &str) -> Client {
        return Client {
            url: String::from(url),
        };
    }

    pub async fn connz(&self, offset: u64, limit: u64) -> Result<(), reqwest::Error> {
        reqwest::get(self.url.as_str()).await?;
        Ok(())
    }
}
