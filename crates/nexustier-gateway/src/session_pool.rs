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
