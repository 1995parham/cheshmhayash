mod nats;

use nats::client;

#[tokio::main]
async fn main() {
    let client = client::Client::new("http://127.0.0.1:8222");
    let connz = client.connz(0, 0).await.expect("fetching connz failed");
    println!("{:?}", connz);
}
