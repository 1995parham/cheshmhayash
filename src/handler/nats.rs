use actix_web::{web, HttpResponse, Responder, Scope};
use serde::Deserialize;

use crate::nats;
use super::Error;
use std::collections::HashMap;

#[derive(Clone)]
pub struct NATS {
    clients: HashMap<String, nats::Client>,
}

#[derive(Deserialize)]
pub struct ConnzQuery {
    offset: Option<u64>,
    limit: Option<u64>,
    name: String,
}

#[derive(Deserialize)]
pub struct VarzQuery {
    name: String,
}

impl NATS {
    pub fn new(clients: HashMap<String, nats::Client>) -> NATS {
        NATS { clients }
    }

    async fn list(data: web::Data<Self>) -> impl Responder {
        let names: Vec<&String> = data.as_ref().clients.iter().map(|(name, _)| name).collect();
        HttpResponse::Ok().json(names)
    }

    async fn varz(data: web::Data<Self>, query: web::Query<VarzQuery>) -> impl Responder {
        match data.as_ref().clients.get(&query.name) {
            Some(client) => {
                let res = client.varz().await;

                match res {
                    Ok(varz) => HttpResponse::Ok().json(varz),
                    Err(err) => HttpResponse::InternalServerError().json(
                        Error{message: err.to_string()}
                        ),
                }
        },
        None => HttpResponse::NotFound().json("nats server not found"),
        }
    }

    async fn connz(data: web::Data<Self>, query: web::Query<ConnzQuery>) -> impl Responder {
        match data.as_ref().clients.get(&query.name) {
            Some(client) => {
                let res = client.connz(query.offset.unwrap_or(0), query.limit.unwrap_or(1024)).await;

                match res {
                    Ok(connz) => HttpResponse::Ok().json(connz),
                    Err(err) => HttpResponse::InternalServerError().json(
                        Error{message: err.to_string()}
                        ),
                }
        },
        None => HttpResponse::NotFound().json("nats server not found"),
        }
    }

    pub fn register(self, scope: Scope) -> Scope {
        let data = web::Data::new(self);
        scope
            .app_data(data)
            .route("/connz", web::get().to(Self::connz))
            .route("/varz", web::get().to(Self::varz))
            .route("/list", web::get().to(Self::list))
    }
}
