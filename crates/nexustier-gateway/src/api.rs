use std::{net::SocketAddr, time::Duration};

use axum::{
    Json, Router,
    extract::State,
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::get,
};
use serde::Serialize;
use tokio::sync::watch;

use crate::{session_pool::SessionPool, telemetry::TelemetryCollector};

#[derive(Clone)]
pub struct ApiState {
    sessions: SessionPool,
    telemetry: TelemetryCollector,
}

#[derive(Debug, Serialize)]
struct HealthResponse {
    status: &'static str,
    active_sessions: usize,
}

#[derive(Debug, Serialize)]
struct ErrorResponse {
    error: ApiErrorBody,
}

#[derive(Debug, Serialize)]
struct ApiErrorBody {
    code: &'static str,
    message: String,
}

pub async fn serve(
    bind_addr: SocketAddr,
    state: ApiState,
    mut shutdown_rx: watch::Receiver<bool>,
) -> anyhow::Result<()> {
    let listener = tokio::net::TcpListener::bind(bind_addr).await?;
    tracing::info!(listen_addr = %listener.local_addr()?, "gateway API is listening");

    axum::serve(listener, router(state))
        .with_graceful_shutdown(async move {
            let _ = shutdown_rx.changed().await;
        })
        .await?;
    Ok(())
}

impl ApiState {
    pub fn new(sessions: SessionPool, rpc_timeout: Duration) -> Self {
        Self {
            telemetry: TelemetryCollector::new(sessions.clone(), rpc_timeout),
            sessions,
        }
    }
}

pub fn router(state: ApiState) -> Router {
    Router::new()
        .route("/healthz", get(health))
        .route("/readyz", get(ready))
        .route("/v1/sessions", get(list_sessions))
        .route("/v1/topology", get(topology))
        .fallback(not_found)
        .with_state(state)
}

async fn health(State(state): State<ApiState>) -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "ok",
        active_sessions: state.sessions.len(),
    })
}

async fn ready(State(state): State<ApiState>) -> Response {
    if state.sessions.is_empty() {
        return ApiError::new(
            StatusCode::SERVICE_UNAVAILABLE,
            "no_active_sessions",
            "no EasyTier clients have registered",
        )
        .into_response();
    }

    Json(HealthResponse {
        status: "ready",
        active_sessions: state.sessions.len(),
    })
    .into_response()
}

async fn list_sessions(State(state): State<ApiState>) -> impl IntoResponse {
    Json(state.telemetry.sessions().await)
}

async fn topology(State(state): State<ApiState>) -> impl IntoResponse {
    Json(state.telemetry.topology().await)
}

async fn not_found() -> ApiError {
    ApiError::new(
        StatusCode::NOT_FOUND,
        "route_not_found",
        "the requested gateway API route does not exist",
    )
}

struct ApiError {
    status: StatusCode,
    code: &'static str,
    message: String,
}

impl ApiError {
    fn new(status: StatusCode, code: &'static str, message: impl Into<String>) -> Self {
        Self {
            status,
            code,
            message: message.into(),
        }
    }
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        (
            self.status,
            Json(ErrorResponse {
                error: ApiErrorBody {
                    code: self.code,
                    message: self.message,
                },
            }),
        )
            .into_response()
    }
}

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use axum::{
        body::{Body, to_bytes},
        http::{Request, StatusCode},
    };
    use tower::ServiceExt;

    use super::{ApiState, router};
    use crate::session_pool::SessionPool;

    fn empty_api() -> axum::Router {
        router(ApiState::new(
            SessionPool::default(),
            Duration::from_millis(100),
        ))
    }

    #[tokio::test]
    async fn health_is_ok_without_clients() {
        let response = empty_api()
            .oneshot(Request::get("/healthz").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), 16 * 1024).await.unwrap();
        let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert_eq!(json["status"], "ok");
        assert_eq!(json["active_sessions"], 0);
    }

    #[tokio::test]
    async fn readiness_requires_an_active_client() {
        let response = empty_api()
            .oneshot(Request::get("/readyz").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);
        let body = to_bytes(response.into_body(), 16 * 1024).await.unwrap();
        let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert_eq!(json["error"]["code"], "no_active_sessions");
    }

    #[tokio::test]
    async fn unknown_routes_use_the_versioned_error_envelope() {
        let response = empty_api()
            .oneshot(Request::get("/missing").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::NOT_FOUND);
        let body = to_bytes(response.into_body(), 16 * 1024).await.unwrap();
        let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert_eq!(json["error"]["code"], "route_not_found");
    }
}
