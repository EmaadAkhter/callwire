use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use crate::client::Client;
use crate::errors::{Result, Error};
use crate::server::{ServerHandle, serve_on};

#[derive(Default)]
pub struct RegistryServer {
    services: Mutex<HashMap<String, Vec<String>>>,
}

impl RegistryServer {
    pub fn new() -> Self {
        Self {
            services: Mutex::new(HashMap::new()),
        }
    }

    pub fn register(&self, service_name: String, addr: String) -> std::result::Result<(), String> {
        let mut services = self.services.lock().unwrap();
        let addrs = services.entry(service_name).or_default();
        if !addrs.contains(&addr) {
            addrs.push(addr);
        }
        Ok(())
    }

    pub fn discover(&self, service_name: String) -> std::result::Result<Vec<String>, String> {
        let services = self.services.lock().unwrap();
        if let Some(addrs) = services.get(&service_name) {
            Ok(addrs.clone())
        } else {
            Err(format!("service not found: {}", service_name))
        }
    }
}

pub async fn serve_registry(addr: &str) -> Result<ServerHandle> {
    let reg = Arc::new(RegistryServer::new());
    
    // We register the unary RPC endpoints on the registry.
    // To keep it simple and match Go/Python, we can register them as "register" and "discover".
    let reg_clone = reg.clone();
    crate::server::register_unary("callwire.register", move |(service_name, addr): (String, String)| {
        reg_clone.register(service_name, addr)
    });

    let reg_clone = reg.clone();
    crate::server::register_unary("callwire.discover", move |(service_name,): (String,)| {
        reg_clone.discover(service_name)
    });

    serve_on(addr).await
}

/// Register `callwire.register` and `callwire.discover` into the global
/// REGISTRY so an existing server socket can handle them.  Used by the
/// orchestrator which manages the socket itself.
pub(crate) async fn start_embedded_registry() {
    let reg = Arc::new(RegistryServer::new());

    let reg_clone = reg.clone();
    crate::server::register_unary("callwire.register", move |(service_name, addr): (String, String)| {
        reg_clone.register(service_name, addr)
    });

    let reg_clone = reg.clone();
    crate::server::register_unary("callwire.discover", move |(service_name,): (String,)| {
        reg_clone.discover(service_name)
    });
}


pub struct DiscoverPool {
    registry_addr: String,
    service_name: String,
    clients: Mutex<Vec<Client>>,
}

impl DiscoverPool {
    pub async fn new(registry_addr: &str, service_name: &str) -> Result<Self> {
        let pool = Self {
            registry_addr: registry_addr.to_string(),
            service_name: service_name.to_string(),
            clients: Mutex::new(Vec::new()),
        };
        pool.refresh().await?;
        Ok(pool)
    }

    pub async fn refresh(&self) -> Result<()> {
        let reg_client = Client::connect(&self.registry_addr).await?;
        let addrs: Vec<String> = reg_client.import("callwire.discover", &(self.service_name.clone(),)).await?;
        
        let mut new_clients = Vec::new();
        for addr in addrs {
            if let Ok(c) = Client::connect(&addr).await {
                new_clients.push(c);
            }
        }

        if new_clients.is_empty() {
            return Err(Error::Internal(format!("no healthy service instances found for {}", self.service_name)));
        }

        let mut clients = self.clients.lock().unwrap();
        *clients = new_clients;
        Ok(())
    }

    pub fn get(&self) -> Result<Client> {
        let clients = self.clients.lock().unwrap();
        if clients.is_empty() {
            return Err(Error::Internal("no clients available in pool".to_string()));
        }
        Ok(clients[0].clone())
    }
}
