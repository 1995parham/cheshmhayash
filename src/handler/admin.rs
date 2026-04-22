use std::collections::HashMap;
use std::sync::Arc;

use actix_web::{web, HttpResponse, Scope};
use serde::Deserialize;

use super::ApiError;
use crate::nats::{self, Cluster};

pub type Clusters = Arc<HashMap<String, Cluster>>;

#[derive(Deserialize)]
pub struct KickBody {
    pub cid: u64,
}

pub struct Admin;

impl Admin {
    pub fn register(scope: Scope) -> Scope {
        scope
            .route("/clusters", web::get().to(Self::list_clusters))
            .route("/clusters/{cluster}/servers", web::get().to(Self::ping))
            .route(
                "/clusters/{cluster}/servers/{endpoint}",
                web::get().to(Self::ping_endpoint),
            )
            .route(
                "/clusters/{cluster}/servers/{id}/{endpoint}",
                web::get().to(Self::server_endpoint),
            )
            .route(
                "/clusters/{cluster}/accounts/{account}/{endpoint}",
                web::get().to(Self::account_endpoint),
            )
            .route(
                "/clusters/{cluster}/servers/{id}/actions/reload",
                web::post().to(Self::reload),
            )
            .route(
                "/clusters/{cluster}/servers/{id}/actions/lame-duck",
                web::post().to(Self::lame_duck),
            )
            .route(
                "/clusters/{cluster}/servers/{id}/actions/kick",
                web::post().to(Self::kick),
            )
    }

    async fn list_clusters(data: web::Data<Clusters>) -> HttpResponse {
        let names: Vec<&String> = data.keys().collect();
        HttpResponse::Ok().json(names)
    }

    async fn ping(
        data: web::Data<Clusters>,
        path: web::Path<String>,
    ) -> actix_web::Result<HttpResponse> {
        let cluster = resolve(&data, &path)?;
        Ok(match nats::ping(cluster).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn ping_endpoint(
        data: web::Data<Clusters>,
        path: web::Path<(String, String)>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, endpoint) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        if !is_server_endpoint(&endpoint) {
            return Ok(unknown_endpoint(&endpoint, nats::SERVER_ENDPOINTS));
        }
        Ok(match nats::ping_endpoint(cluster, &endpoint).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn server_endpoint(
        data: web::Data<Clusters>,
        path: web::Path<(String, String, String)>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, id, endpoint) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        if !is_server_endpoint(&endpoint) {
            return Ok(unknown_endpoint(&endpoint, nats::SERVER_ENDPOINTS));
        }
        Ok(match nats::server_endpoint(cluster, &id, &endpoint).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn account_endpoint(
        data: web::Data<Clusters>,
        path: web::Path<(String, String, String)>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, account, endpoint) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        if !is_account_endpoint(&endpoint) {
            return Ok(unknown_endpoint(&endpoint, nats::ACCOUNT_ENDPOINTS));
        }
        Ok(
            match nats::account_endpoint(cluster, &account, &endpoint).await {
                Ok(v) => HttpResponse::Ok().json(v),
                Err(err) => upstream_error(err),
            },
        )
    }

    async fn reload(
        data: web::Data<Clusters>,
        path: web::Path<(String, String)>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, id) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(match nats::reload(cluster, &id).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn lame_duck(
        data: web::Data<Clusters>,
        path: web::Path<(String, String)>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, id) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(match nats::lame_duck(cluster, &id).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn kick(
        data: web::Data<Clusters>,
        path: web::Path<(String, String)>,
        body: web::Json<KickBody>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, id) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(match nats::kick(cluster, &id, body.cid).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }
}

fn resolve<'a>(clusters: &'a Clusters, name: &str) -> Result<&'a Cluster, actix_web::error::Error> {
    clusters
        .get(name)
        .ok_or_else(|| actix_web::error::ErrorNotFound(format!("cluster '{name}' not configured")))
}

fn is_server_endpoint(endpoint: &str) -> bool {
    let upper = endpoint.to_ascii_uppercase();
    nats::SERVER_ENDPOINTS.contains(&upper.as_str())
}

fn is_account_endpoint(endpoint: &str) -> bool {
    let upper = endpoint.to_ascii_uppercase();
    nats::ACCOUNT_ENDPOINTS.contains(&upper.as_str())
}

fn unknown_endpoint(endpoint: &str, allowed: &[&str]) -> HttpResponse {
    HttpResponse::BadRequest().json(ApiError::new(format!(
        "unknown endpoint '{endpoint}'; valid: {}",
        allowed.join(", ")
    )))
}

fn upstream_error(err: nats::AdminError) -> HttpResponse {
    tracing::warn!(error = %err, "sys request failed");
    HttpResponse::BadGateway().json(ApiError::new(err.to_string()))
}
