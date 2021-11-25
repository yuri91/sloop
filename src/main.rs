use serde::Deserialize;
use std::collections::HashMap;
use std::convert::AsRef;
use std::path::{Path, PathBuf};
use std::io::Write;
use structopt::StructOpt;

#[derive(Debug, StructOpt)]
#[structopt(name = "sloop", about = "minimal container manager")]
struct Opt {
    #[structopt(parse(from_os_str))]
    confs: Vec<PathBuf>,
}

#[derive(Debug, Deserialize)]
struct Conf {
    name: String,
    image: String,
    #[serde(default)]
    volumes: Vec<String>,
    #[serde(default)]
    networks: Vec<String>,
    #[serde(default)]
    ports: Vec<String>,
    #[serde(default)]
    files: HashMap<String, String>,
    #[serde(default)]
    labels: HashMap<String, String>,
    #[serde(default)]
    env: HashMap<String, String>,
    #[serde(default)]
    entrypoint: Vec<String>,
    #[serde(default)]
    cmd: Vec<String>,
    #[serde(default)]
    requires: Vec<String>,
    #[serde(default)]
    wants: Vec<String>,
    #[serde(default)]
    after: Vec<String>,
}

#[derive(Debug)]
pub struct Volume {
    name: String,
}
impl Volume {
    pub fn new(name: String) -> Volume {
        Volume { name }
    }
    pub fn name(&self) -> &str {
        &self.name
    }
}

#[derive(Debug)]
pub struct Network {
    name: String,
}
impl Network {
    pub fn new(name: String) -> anyhow::Result<Network> {
        let net = Network { name };
        if !net.exists() {
            net.create()?;
        }
        Ok(net)
    }
    pub fn remove(self) -> anyhow::Result<()> {
        cmd(&["podman", "network", "remove", &self.name], None)?;
        Ok(())
    }
    pub fn name(&self) -> &str {
        &self.name
    }
    fn create(&self) -> anyhow::Result<()> {
        cmd(&["podman", "network", "create", &self.name], None)?;
        Ok(())
    }
    fn exists(&self) -> bool {
        cmd(&["podman", "network", "exists", &self.name], None).is_ok()
    }
}

#[derive(Debug, Copy, Clone)]
pub enum DependencyKind {
    Wants,
    Requires,
    After,
}
#[derive(Debug)]
pub struct Dependency {
    kind: DependencyKind,
    name: String,
}
impl Dependency {
    pub fn new(kind: DependencyKind, name: String) -> Dependency {
        Dependency { kind, name }
    }
    fn kind(&self) -> DependencyKind {
        self.kind
    }
    fn name(&self) -> &str {
        &self.name
    }
}

#[derive(Debug)]
pub struct ImageBuilder {
    name: String,
    base_url: String,
    cmd: Vec<String>,
    entrypoint: Vec<String>,
    files: HashMap<String, String>,
    labels: HashMap<String, String>,
    env: HashMap<String, String>,
    version: String,
}

#[derive(Debug)]
pub struct Image {
    name: String,
    version: String,
}

impl ImageBuilder {
    fn build(self) -> anyhow::Result<Image> {
        let tag = format!("{}:{}", self.name, self.version);
        let tag_latest = format!("{}:latest", self.name);
        let mut script = format!("FROM {}\n", self.base_url);
        let mut tmps = Vec::new();
        for (path, content) in self.files {
            let mut f = tempfile::NamedTempFile::new_in(".")?;
            f.write_all(&content.as_bytes())?;
            f.flush()?;
            let fname = f.path().file_name().unwrap().to_string_lossy();
            script.push_str(&format!("COPY --chmod=666 {} {}\n", fname, path));
            tmps.push(f);
        }
        for (k, v) in self.labels {
            script.push_str(&format!("LABEL {}={}\n", k, v));
        }
        for (k, v) in self.env {
            script.push_str(&format!("ENV {}={}\n", k, v));
        }
        if !self.entrypoint.is_empty() {
            script.push_str(&format!("ENTRYPOINT {:?}\n", self.entrypoint));
        }
        if !self.cmd.is_empty() {
            script.push_str(&format!("CMD {:?}\n", self.cmd));
        }
        cmd(&["buildah", "bud", "-t", &tag_latest, "-t", &tag, "-f", "-"], Some(&script))?;
        Ok(Image {
            name: self.name,
            version: self.version,
        })
    }
}


#[derive(Debug)]
pub struct Service {
    name: String,
    version: String,
    image: Image,
    volumes: Vec<Volume>,
    networks: Vec<Network>,
    dependencies: Vec<Dependency>,
}

impl Service {
    fn new(name: String, version: String, image: Image, volumes: Vec<Volume>, networks: Vec<Network>, dependencies: Vec<Dependency>) -> anyhow::Result<Service> {
        Ok(Service {
            name,
            version,
            image,
            volumes,
            networks,
            dependencies,
        })
    }

    fn from_conf(conf: Conf) -> anyhow::Result<Service> {
        let mut dependencies = Vec::new();
        dependencies.extend(
            conf.after
                .into_iter()
                .map(|i| Dependency::new(DependencyKind::After, i)),
        );
        dependencies.extend(
            conf.requires
                .into_iter()
                .map(|i| Dependency::new(DependencyKind::Requires, i)),
        );
        dependencies.extend(
            conf.wants
                .into_iter()
                .map(|i| Dependency::new(DependencyKind::Wants, i)),
        );
        let version = chrono::Utc::now().format("%Y-%m-%d_%H-%M-%S").to_string();
        let image_builder = ImageBuilder {
            name: conf.name.clone(),
            version: version.clone(),
            base_url: conf.image,
            cmd: conf.cmd,
            entrypoint: conf.entrypoint,
            files: conf.files,
            labels: conf.labels,
            env: conf.env,
        };
        Ok(Service::new(
            conf.name,
            version,
            image_builder.build()?,
            conf.volumes.into_iter().map(Volume::new).collect(),
            conf.networks.into_iter().map(Network::new).collect::<anyhow::Result<_>>()?,
            dependencies,
        )?)
    }
}

fn process<P: AsRef<Path>>(conf_path: P) -> anyhow::Result<()> {
    let conf_str = std::fs::read_to_string(conf_path)?;
    let conf: Conf = toml::from_str(&conf_str)?;
    let service = Service::from_conf(conf);
    println!("{:?}", service);
    Ok(())
}

fn cmd<T: AsRef<str>>(args: &[T], stdin: Option<&str>) -> anyhow::Result<String> {
    let mut child = std::process::Command::new(args[0].as_ref())
        .args(args[1..].iter().map(|a|a.as_ref()))
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()?;
    if let Some(s) = stdin {
        child.stdin.take().unwrap().write_all(s.as_bytes())?;
    }
    let out = child.wait_with_output()?;
    if !out.status.success() {
        anyhow::bail!("non-zero exit status for {}", args[0].as_ref());
    }
    let mut ret = String::from_utf8(out.stdout)?;
    ret.push_str(&String::from_utf8(out.stderr)?);
    Ok(ret)
}

fn main() -> anyhow::Result<()> {
    let opt = Opt::from_args();
    for conf in opt.confs {
        process(&conf)?;
    }
    Ok(())
}
