mod nats;
mod handler;
mod setting;

use actix_web::{web, App, HttpServer};

use setting::Settings;

#[actix_rt::main]
async fn main() {
    let setting = Settings::new().expect("loading configuration failed");
    let setting_clone = setting.clone();

    HttpServer::new(move || {
        let client = nats::Client::new(setting_clone.nats().monitoring());
        let nats_handler = handler::NATS::new(client);

        App::new()
            .service(
               nats_handler.register(web::scope("/api"))
            )
            .service(
                handler::Healthz::register(web::scope("/"))
            )
    })
    .workers(12)
    .bind(
        format!("{}:{}", setting.server().host(), setting.server().port())
        ).expect("http server failed to bind")
    .run()
    .await.expect("http server failed to run");
}
