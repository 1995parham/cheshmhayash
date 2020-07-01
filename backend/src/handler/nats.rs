use actix_web::{web, HttpResponse, Responder, Scope};
use serde::Deserialize;

use crate::nats;
use super::Error;

#[derive(Clone)]
pub struct NATS {
    client: nats::Client,
}

#[derive(Deserialize)]
pub struct Page {
    offset: Option<u64>,
    limit: Option<u64>,
}

impl NATS {
    pub fn new(client: nats::Client) -> NATS {
        NATS { client }
    }

    async fn connz(data: web::Data<Self>, page: web::Query<Page>) -> impl Responder {
        let res = data.as_ref().client.connz(page.offset.unwrap_or(0), page.limit.unwrap_or(1024)).await;

        match res {
            Ok(connz) => HttpResponse::Ok().json(connz),
            Err(err) => HttpResponse::InternalServerError().json(
                Error{message: err.to_string()}
                ),
        }
    }

    pub fn register(self, scope: Scope) -> Scope {
        let data = web::Data::new(self);
        scope
            .app_data(data)
            .route("/connz", web::get().to(Self::connz))
    }
}
