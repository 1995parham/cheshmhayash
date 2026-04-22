use actix_web::{web, HttpResponse, Scope};
use serde::Deserialize;

use super::admin::Clusters;
use super::ApiError;
use crate::nats::{self, AdminError, Cluster};

#[derive(Deserialize)]
pub struct ConfirmQuery {
    #[serde(default)]
    pub confirm: bool,
}

#[derive(Deserialize)]
pub struct OffsetQuery {
    #[serde(default)]
    pub offset: u64,
}

pub struct Jsm;

impl Jsm {
    pub fn register(scope: Scope) -> Scope {
        scope
            .route(
                "/clusters/{cluster}/streams",
                web::get().to(Self::list_streams),
            )
            .route(
                "/clusters/{cluster}/streams/{stream}",
                web::get().to(Self::stream_info),
            )
            .route(
                "/clusters/{cluster}/streams/{stream}",
                web::delete().to(Self::delete_stream),
            )
            .route(
                "/clusters/{cluster}/streams/{stream}/purge",
                web::post().to(Self::purge_stream),
            )
            .route(
                "/clusters/{cluster}/streams/{stream}/consumers",
                web::get().to(Self::list_consumers),
            )
            .route(
                "/clusters/{cluster}/streams/{stream}/consumers/{consumer}",
                web::get().to(Self::consumer_info),
            )
            .route(
                "/clusters/{cluster}/streams/{stream}/consumers/{consumer}",
                web::delete().to(Self::delete_consumer),
            )
    }

    async fn list_streams(
        data: web::Data<Clusters>,
        path: web::Path<String>,
        query: web::Query<OffsetQuery>,
    ) -> actix_web::Result<HttpResponse> {
        let cluster = resolve(&data, &path)?;
        Ok(match nats::list_streams(cluster, query.offset).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn stream_info(
        data: web::Data<Clusters>,
        path: web::Path<(String, String)>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, stream) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(match nats::stream_info(cluster, &stream).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn purge_stream(
        data: web::Data<Clusters>,
        path: web::Path<(String, String)>,
        query: web::Query<ConfirmQuery>,
    ) -> actix_web::Result<HttpResponse> {
        if !query.confirm {
            return Ok(require_confirm());
        }
        let (cluster_name, stream) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(match nats::purge_stream(cluster, &stream).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn delete_stream(
        data: web::Data<Clusters>,
        path: web::Path<(String, String)>,
        query: web::Query<ConfirmQuery>,
    ) -> actix_web::Result<HttpResponse> {
        if !query.confirm {
            return Ok(require_confirm());
        }
        let (cluster_name, stream) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(match nats::delete_stream(cluster, &stream).await {
            Ok(v) => HttpResponse::Ok().json(v),
            Err(err) => upstream_error(err),
        })
    }

    async fn list_consumers(
        data: web::Data<Clusters>,
        path: web::Path<(String, String)>,
        query: web::Query<OffsetQuery>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, stream) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(
            match nats::list_consumers(cluster, &stream, query.offset).await {
                Ok(v) => HttpResponse::Ok().json(v),
                Err(err) => upstream_error(err),
            },
        )
    }

    async fn consumer_info(
        data: web::Data<Clusters>,
        path: web::Path<(String, String, String)>,
    ) -> actix_web::Result<HttpResponse> {
        let (cluster_name, stream, consumer) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(
            match nats::consumer_info(cluster, &stream, &consumer).await {
                Ok(v) => HttpResponse::Ok().json(v),
                Err(err) => upstream_error(err),
            },
        )
    }

    async fn delete_consumer(
        data: web::Data<Clusters>,
        path: web::Path<(String, String, String)>,
        query: web::Query<ConfirmQuery>,
    ) -> actix_web::Result<HttpResponse> {
        if !query.confirm {
            return Ok(require_confirm());
        }
        let (cluster_name, stream, consumer) = path.into_inner();
        let cluster = resolve(&data, &cluster_name)?;
        Ok(
            match nats::delete_consumer(cluster, &stream, &consumer).await {
                Ok(v) => HttpResponse::Ok().json(v),
                Err(err) => upstream_error(err),
            },
        )
    }
}

fn resolve<'a>(clusters: &'a Clusters, name: &str) -> Result<&'a Cluster, actix_web::error::Error> {
    clusters
        .get(name)
        .ok_or_else(|| actix_web::error::ErrorNotFound(format!("cluster '{name}' not configured")))
}

fn require_confirm() -> HttpResponse {
    HttpResponse::PreconditionRequired()
        .json(ApiError::new("destructive action requires ?confirm=true"))
}

fn upstream_error(err: AdminError) -> HttpResponse {
    tracing::warn!(error = %err, "jsm request failed");
    HttpResponse::BadGateway().json(ApiError::new(err.to_string()))
}
