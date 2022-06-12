use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct Volume {
    pub name: String,
}
#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct VolumeMapping {
    pub volume: String,
    pub path: String,
}
#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct Network {
    pub name: String,
}
#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct PortMapping {
    pub host: u16,
    pub unit: u16,
}
#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct File {
    pub path: String,
    pub content: String,
    pub perm: u32,
}
#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct Label {
    pub key: String,
    pub value: String,
}
#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct Env {
    pub key: String,
    pub value: String,
}
#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct Image {
    pub name: String,
    pub from: String,
    pub files: Vec<File>,
    pub labels: Vec<Label>,
    pub env: Vec<Env>,
    pub entrypoint: Vec<String>,
    pub cmd: Vec<String>,
}
#[derive(Debug, Deserialize, Serialize, Hash, Clone)]
pub struct Unit {
    pub name: String,
    pub image: String,
    pub volumes: Vec<VolumeMapping>,
    pub ports: Vec<PortMapping>,
    pub networks: Vec<String>,
    pub after: Vec<String>,
    pub wants: Vec<String>,
    pub requires: Vec<String>,
    pub labels: Vec<Label>,
}
#[derive(Debug, Deserialize, Serialize)]
pub struct Context {
    pub units: Vec<Unit>,
    pub images: Vec<Image>,
    pub volumes: Vec<Volume>,
    pub networks: Vec<Network>,
}
