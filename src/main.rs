use serde::Deserialize;
use std::collections::HashMap;
use std::convert::AsRef;
use std::path::{Path, PathBuf};
use std::io::Write;
use structopt::StructOpt;
use log::{info, debug};

#[derive(Debug, StructOpt)]
#[structopt(name = "sloop", about = "minimal container manager")]
struct Opt {
    #[structopt(parse(from_os_str))]
    confs: Vec<PathBuf>,
    #[structopt(short, long)]
    start: bool,
    #[structopt(short, long)]
    enable: bool,
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
pub struct VolumeMapping {
    pair: String,
}
impl VolumeMapping {
    pub fn new(pair: String) -> anyhow::Result<VolumeMapping> {
        if pair.find(':').is_none() {
            anyhow::bail!("Missing ':' separator in volume definition: {}", pair);
        }
        Ok(VolumeMapping { pair })
    }
}

#[derive(Debug)]
pub struct PortMapping {
    pair: String,
}
impl PortMapping {
    pub fn new(pair: String) -> anyhow::Result<PortMapping> {
        if pair.find(':').is_none() {
            anyhow::bail!("Missing ':' separator in port definition: {}", pair);
        }
        Ok(PortMapping { pair })
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
        let tag = format!("sloop/{}:{}", self.name, self.version);
        let tag_latest = format!("sloop/{}:latest", self.name);
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
            script.push_str(&format!("LABEL \"{}\"=\"{}\"\n", k, v));
        }
        for (k, v) in self.env {
            script.push_str(&format!("ENV \"{}\"=\"{}\"\n", k, v));
        }
        if !self.entrypoint.is_empty() {
            script.push_str(&format!("ENTRYPOINT {:?}\n", self.entrypoint));
        }
        if !self.cmd.is_empty() {
            script.push_str(&format!("CMD {:?}\n", self.cmd));
        }
        debug!("Dockerfile:\n{}", script);
        let res = cmd(&["buildah", "bud", "--layers", "-t", &tag_latest, "-t", &tag, "-f", "-"], Some(&script))?;
        debug!("build output:\n{}", res);
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
    volumes: Vec<VolumeMapping>,
    ports: Vec<PortMapping>,
    networks: Vec<Network>,
    dependencies: Vec<Dependency>,
}

static NAME_PLACEHOLDER: &'static str = "SLOOP_PLACEHOLDER";

impl Service {
    fn new(name: String, version: String, image: Image, volumes: Vec<VolumeMapping>, ports: Vec<PortMapping>, networks: Vec<Network>, dependencies: Vec<Dependency>) -> anyhow::Result<Service> {
        Ok(Service {
            name,
            version,
            image,
            volumes,
            ports,
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
            conf.volumes.into_iter().map(VolumeMapping::new).collect::<anyhow::Result<_>>()?,
            conf.ports.into_iter().map(PortMapping::new).collect::<anyhow::Result<_>>()?,
            conf.networks.into_iter().map(Network::new).collect::<anyhow::Result<_>>()?,
            dependencies,
        )?)
    }
    fn podman_create(&self) -> anyhow::Result<()> {
        let mut args: Vec<_> = vec!["podman", "container", "create", "--init", "--name", NAME_PLACEHOLDER];
        for v in &self.volumes {
            args.extend(["-v", &v.pair]);
        }
        for n in &self.networks {
            args.extend(["--net", &n.name]);
        }
        for p in &self.ports {
            args.extend(["-p", &p.pair]);
        }
        let name_ver = format!("sloop/{}:latest", self.name);
        args.push(&name_ver);
        cmd(&args, None)?;
        Ok(())
    }
    fn podman_remove(&self) -> anyhow::Result<()> {
        cmd(&["podman","container","rm", NAME_PLACEHOLDER], None)?;
        Ok(())
    }
    fn podman_generate(&self) -> anyhow::Result<String> {
        let out = cmd(&["podman","generate","systemd", "--name", "--new", NAME_PLACEHOLDER, "--container-prefix", "", "--separator", ""], None)?;
        let out = out.replace(NAME_PLACEHOLDER, &self.name);
        Ok(out)
    }
    fn add_dependencies(&self, out: &mut String) {
        let mut requirements = String::new();
        for d in &self.dependencies {
            let verb = match d.kind {
                DependencyKind::Wants => {
                    "Wants"
                },
                DependencyKind::Requires => {
                    "Requires"
                },
                DependencyKind::After => {
                    "After"
                },
            };
            requirements.push_str(&format!("{}={}\n", verb, d.name));
        }
        let insert_pt = out.find("Documentation").unwrap();
        out.insert_str(insert_pt, &requirements);
    }
    fn remove_comments(&self, out: &mut String) {
        let unit_start = out.find("[Unit]").unwrap();
        out.replace_range(..unit_start, "");
    }
    fn gen_service(&self) -> anyhow::Result<String> {
        self.podman_create()?;
        let mut out = self.podman_generate()?;
        self.podman_remove()?;
        self.add_dependencies(&mut out);
        self.remove_comments(&mut out);
        Ok(out)
    }
}

mod systemctl {
    use super::cmd;
    use log::info;
    pub fn start(service: &str) -> anyhow::Result<()> {
        info!("starting {}", service);
        cmd(&["systemctl", "start", service], None)?;
        Ok(())
    }
    pub fn stop(service: &str) -> anyhow::Result<()> {
        info!("stopping {}", service);
        cmd(&["systemctl", "stop", service], None)?;
        Ok(())
    }
    pub fn enable(service: &str) -> anyhow::Result<()> {
        info!("enabling {}", service);
        cmd(&["systemctl", "enable", service], None)?;
        Ok(())
    }
    pub fn is_active(service: &str) -> bool {
        cmd(&["systemctl", "is-active", service], None).is_ok()
    }
    pub fn daemon_reload() -> anyhow::Result<()> {
        cmd(&["systemctl", "daemon-reload"], None)?;
        Ok(())
    }
    pub fn install(service: &str, content: &str) -> anyhow::Result<()> {
        info!("installing {}", service);
        let mut path = std::path::PathBuf::new();
        path.push("/etc/systemd/system/");
        path.push(service);
        std::fs::write(path, content)?;
        Ok(())
    }
}

fn process<P: AsRef<Path>>(conf_path: P, start: bool, enable: bool) -> anyhow::Result<()> {
    let conf_str = std::fs::read_to_string(conf_path)?;
    let conf: Conf = toml::from_str(&conf_str)?;
    let service_name = format!("{}.service", conf.name);
    let service = Service::from_conf(conf)?;
    let service_content = service.gen_service()?;
    debug!("service: {:?}", service_content);
    if systemctl::is_active(&service_name) {
        systemctl::stop(&service_name)?;
    }
    systemctl::install(&service_name, &service_content)?;
    systemctl::daemon_reload()?;
    if start {
        systemctl::start(&service_name)?;
    }
    if enable {
        systemctl::enable(&service_name)?;
    }
    Ok(())
}

fn cmd<T: AsRef<str>>(args: &[T], stdin: Option<&str>) -> anyhow::Result<String> {
    info!("+ {:?}", args.iter().map(|a| a.as_ref()).collect::<Vec<&str>>());
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
    let mut ret = String::from_utf8(out.stdout)?;
    ret.push_str(&String::from_utf8(out.stderr)?);
    debug!("output: -----------------------\n{}\n--------------------------------\n", ret);
    if !out.status.success() {
        anyhow::bail!("non-zero exit status for {}", args[0].as_ref());
    }
    Ok(ret)
}

fn main() -> anyhow::Result<()> {
    sudo::with_env(&["RUST_BACKTRACE", "RUST_LOG"]).expect("Cannot gain root privilege");
    pretty_env_logger::init();
    let opt = Opt::from_args();
    for conf in opt.confs {
        info!("processing {:?}", conf);
        process(&conf, opt.start, opt.enable)?;
    }
    Ok(())
}
