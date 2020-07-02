mod nats;
mod handler;
mod setting;

use actix_web::{web, App, HttpServer};
use actix_files as fs;
use std::collections::HashMap;

use setting::Settings;

#[actix_rt::main]
async fn main() {
    let setting = Settings::new().expect("loading configuration failed");
    let setting_clone = setting.clone();

    HttpServer::new(move || {
        let clients: HashMap<String, nats::Client> =
            setting_clone.nats()
                .iter()
                .map(|s| (s.name().to_string(), nats::Client::new(s.monitoring())))
                .collect();

        let nats_handler = handler::NATS::new(clients);

        App::new()
            .service(
               nats_handler.register(web::scope("/api"))
            )
            .service(
                handler::Healthz::register(web::scope("/healthz"))
            )
            .service(fs::Files::new("/", "../frontend/dist/cheshmhayash/").index_file("index.html"))
    })
    .workers(12)
    .bind(
        format!("{}:{}", setting.server().host(), setting.server().port())
        ).expect("http server failed to bind")
    .run()
    .await.expect("http server failed to run");
}
