mod handler;
mod nats;
mod setting;

use actix_files as fs;
use actix_web::{guard, web, App, HttpResponse, HttpServer, Result};
use std::collections::HashMap;

use setting::Settings;

async fn index() -> Result<fs::NamedFile> {
    Ok(fs::NamedFile::open(
        "../frontend/dist/cheshmhayash/index.html",
    )?)
}

#[actix_web::main]
async fn main() {
    let setting = Settings::new().expect("loading configuration failed");
    println!("settings: {:?}", setting);
    let setting_clone = setting.clone();

    HttpServer::new(move || {
        let clients: HashMap<String, nats::Client> = setting_clone
            .nats()
            .iter()
            .map(|s| (s.name().to_string(), nats::Client::new(s.monitoring())))
            .collect();

        let nats_handler = handler::Nats::new(clients);

        App::new()
            .service(nats_handler.register(web::scope("/api")))
            .service(handler::Healthz::register(web::scope("/healthz")))
            .service(fs::Files::new("/", "../frontend/dist/cheshmhayash/").index_file("index.html"))
            .default_service(
                web::resource("").route(web::get().to(index)).route(
                    web::route()
                        .guard(guard::Not(guard::Get()))
                        .to(HttpResponse::MethodNotAllowed),
                ),
            )
    })
    .workers(12)
    .bind(format!(
        "{}:{}",
        setting.server().host(),
        setting.server().port()
    ))
    .expect("http server failed to bind")
    .run()
    .await
    .expect("http server failed to run");
}
