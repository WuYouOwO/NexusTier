use std::sync::Arc;

use dashmap::DashMap;
use uuid::Uuid;

use crate::session::GatewaySession;

#[derive(Clone, Default)]
pub struct SessionPool {
    sessions: Arc<DashMap<Uuid, Arc<GatewaySession>>>,
}

impl SessionPool {
    pub fn insert(
        &self,
        machine_id: Uuid,
        session: Arc<GatewaySession>,
    ) -> Option<Arc<GatewaySession>> {
        self.sessions.insert(machine_id, session)
    }

    pub fn remove_if_current(&self, machine_id: &Uuid, session: &Arc<GatewaySession>) -> bool {
        self.sessions
            .remove_if(machine_id, |_, current| Arc::ptr_eq(current, session))
            .is_some()
    }

    pub fn sessions(&self) -> Vec<Arc<GatewaySession>> {
        self.sessions
            .iter()
            .map(|entry| entry.value().clone())
            .collect()
    }

    pub fn len(&self) -> usize {
        self.sessions.len()
    }

    pub fn is_empty(&self) -> bool {
        self.sessions.is_empty()
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use super::SessionPool;
    use crate::session::GatewaySession;

    #[tokio::test]
    async fn stale_disconnect_cannot_remove_a_replacement_session() {
        let pool = SessionPool::default();
        let machine_id = uuid::Uuid::new_v4();
        let old_session = Arc::new(GatewaySession::new(
            "udp://127.0.0.1:10001".parse().unwrap(),
        ));
        let new_session = Arc::new(GatewaySession::new(
            "udp://127.0.0.1:10002".parse().unwrap(),
        ));

        assert!(pool.insert(machine_id, old_session.clone()).is_none());
        let replaced = pool
            .insert(machine_id, new_session.clone())
            .expect("old session should be replaced");
        assert!(Arc::ptr_eq(&replaced, &old_session));
        assert!(!pool.remove_if_current(&machine_id, &old_session));
        assert_eq!(pool.len(), 1);
        assert!(pool.remove_if_current(&machine_id, &new_session));
        assert!(pool.is_empty());
    }
}
