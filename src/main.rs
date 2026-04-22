mod handler;
mod nats;
mod setting;

use std::collections::HashMap;
use std::sync::Arc;

use actix_cors::Cors;
use actix_files as fs;
use actix_web::http::Method;
use actix_web::{web, App, HttpRequest, HttpResponse, HttpServer};
use anyhow::Context;
use tracing_actix_web::TracingLogger;
use tracing_subscriber::EnvFilter;

use handler::Clusters;
use setting::Settings;

const FRONTEND_DIR: &str = "web/dist/cheshmhayash";

async fn spa_fallback(req: HttpRequest) -> actix_web::Result<HttpResponse> {
    if req.method() == Method::GET {
        let file = fs::NamedFile::open(format!("{FRONTEND_DIR}/index.html"))?;
        Ok(file.into_response(&req))
    } else {
        Ok(HttpResponse::MethodNotAllowed().finish())
    }
}

#[actix_web::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .init();

    let settings = Settings::new().context("loading configuration failed")?;

    let bind = format!("{}:{}", settings.server().host(), settings.server().port());

    let mut clusters_map: HashMap<String, nats::Cluster> =
        HashMap::with_capacity(settings.nats().len());
    for cfg in settings.nats() {
        tracing::info!(cluster = cfg.name(), url = cfg.url(), "connecting");
        let cluster = nats::Cluster::connect(cfg)
            .await
            .with_context(|| format!("connect to cluster '{}' failed", cfg.name()))?;
        clusters_map.insert(cfg.name().to_string(), cluster);
    }
    let clusters: Clusters = Arc::new(clusters_map);
    let clusters_data = web::Data::new(clusters);

    tracing::info!(
        address = %bind,
        clusters = clusters_data.len(),
        "starting cheshmhayash"
    );

    HttpServer::new(move || {
        App::new()
            .wrap(TracingLogger::default())
            .wrap(
                Cors::default()
                    .allow_any_origin()
                    .allow_any_method()
                    .allow_any_header(),
            )
            .app_data(clusters_data.clone())
            .service(handler::Admin::register(web::scope("/api/admin")))
            .service(handler::Jsm::register(web::scope("/api/jsm")))
            .service(handler::Healthz::register(web::scope("/healthz")))
            .service(fs::Files::new("/", FRONTEND_DIR).index_file("index.html"))
            .default_service(web::to(spa_fallback))
    })
    .workers(num_workers())
    .bind(&bind)
    .with_context(|| format!("binding to {bind} failed"))?
    .run()
    .await
    .context("http server failed")?;

    Ok(())
}

fn num_workers() -> usize {
    std::thread::available_parallelism()
        .map(|n| n.get())
        .unwrap_or(4)
}
