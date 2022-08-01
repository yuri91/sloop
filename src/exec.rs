use crate::objects::*;
use crate::plan;
use crate::podman;
use crate::systemd;
use log::info;

use std::io::Write;


trait Runner {
    fn create_volume(&self, v: &Volume) -> anyhow::Result<()>;
    fn remove_volume(&self, v: &str) -> anyhow::Result<()>;
    fn create_network(&self, n: &Network) -> anyhow::Result<()>;
    fn remove_network(&self, n: &str) -> anyhow::Result<()>;
    fn create_image(&self, i: &Image) -> anyhow::Result<()>;
    fn remove_image(&self, i: &str) -> anyhow::Result<()>;
    fn create_unit(&self, v: &Unit) -> anyhow::Result<()>;
    fn remove_unit(&self, v: &str) -> anyhow::Result<()>;
}
struct DryRunner;
struct RealRunner;

impl Runner for RealRunner {
    fn create_volume(&self, v: &Volume) -> anyhow::Result<()> {
        podman::volume::create(&v.name)
    }
    fn remove_volume(&self, v: &str) -> anyhow::Result<()> {
        Ok(())
    }
    fn create_network(&self, n: &Network) -> anyhow::Result<()> {
        podman::network::create(&n.name)
    }
    fn remove_network(&self, n: &str) -> anyhow::Result<()> {
        podman::network::remove(n)
    }
    fn create_image(&self, i: &Image) -> anyhow::Result<()> {
        let mut script = format!("FROM {}\n", i.from);
        let mut tmps = Vec::new();
        for file in &i.files {
            let mut f = tempfile::NamedTempFile::new_in(".")?;
            f.write_all(&file.content.as_bytes())?;
            f.flush()?;
            let fname = f.path().file_name().unwrap().to_string_lossy();
            script.push_str(&format!("COPY --chmod={:03x} {} {}\n", file.perm, fname, file.path));
            tmps.push(f);
        }
        script.push_str("LABEL \"sloop\"=\"\"\n");
        for l in &i.labels {
            script.push_str(&format!("LABEL \"{}\"=\"{}\"\n", l.key, l.value));
        }
        for e in &i.env {
            script.push_str(&format!("ENV \"{}\"=\"{}\"\n", e.key, e.value));
        }
        if !i.entrypoint.is_empty() {
            script.push_str(&format!("ENTRYPOINT {:?}\n", i.entrypoint));
        }
        if !i.cmd.is_empty() {
            script.push_str(&format!("CMD {:?}\n", i.cmd));
        }
        podman::image::create(&i.name, &script, "next")?;
        Ok(())
    }
    fn remove_image(&self, i: &str) -> anyhow::Result<()> {
        podman::image::remove(i, "prev")
    }
    fn create_unit(&self, u: &Unit) -> anyhow::Result<()> {
        let s = podman::container::generate_unit(&u.name, &u.wants, &u.requires, &u.after)?;
        let volumes = u.volumes.iter().map(|v| &v.path);
        let ports = u.ports.iter().map(|p| (p.host, p.unit));
        let labels = u.labels.iter().map(|l| (&l.key, &l.value));
        podman::container::create(&u.name, volumes, &u.networks, ports, labels)?;
        systemd::install(&u.name, &s)?;
        systemd::daemon_reload()?;
        systemd::start(&u.name)?;
        Ok(())
    }
    fn remove_unit(&self, u: &str) -> anyhow::Result<()> {
        if systemd::is_active(u) {
            systemd::stop(u)?;
        }
        podman::container::remove(u)?;
        systemd::uninstall(u)?;
        Ok(())
    }
}

impl Runner for DryRunner {
    fn create_volume(&self, v: &Volume) -> anyhow::Result<()> {
        info!("CREATE volume {}", v.name);
        Ok(())
    }
    fn remove_volume(&self, v: &str) -> anyhow::Result<()> {
        info!("REMOVE volume {}", v);
        Ok(())
    }
    fn create_network(&self, n: &Network) -> anyhow::Result<()> {
        info!("CREATE network {}", n.name);
        Ok(())
    }
    fn remove_network(&self, n: &str) -> anyhow::Result<()> {
        info!("REMOVE network {}", n);
        Ok(())
    }
    fn create_image(&self, i: &Image) -> anyhow::Result<()> {
        info!("CREATE image {}", i.name);
        Ok(())
    }
    fn remove_image(&self, i: &str) -> anyhow::Result<()> {
        info!("REMOVE image {}", i);
        Ok(())
    }
    fn create_unit(&self, u: &Unit) -> anyhow::Result<()> {
        info!("CREATE unit {}", u.name);
        Ok(())
    }
    fn remove_unit(&self, u: &str) -> anyhow::Result<()> {
        info!("REMOVE unit {}", u);
        Ok(())
    }
}

fn do_run<R: Runner>(runner: R, plan: &plan::Plan) -> anyhow::Result<()> {
    for a in plan.iter() {
        match a {
            plan::Action::AddVolume(v) => {
                runner.create_volume(v)?;
            },
            plan::Action::RemoveVolume(v) => {
                runner.remove_volume(v)?;
            },
            plan::Action::AddNetwork(n) => {
                runner.create_network(n)?;
            },
            plan::Action::RemoveNetwork(n) => {
                runner.remove_network(n)?;
            },
            plan::Action::AddImage(i) => {
                runner.create_image(i)?;
            },
            plan::Action::RemoveImage(i, hash) => {
                runner.remove_image(i, hash)?;
            },
            plan::Action::AddUnit(u) => {
                runner.create_unit(u)?;
            },
            plan::Action::RemoveUnit(u) => {
                runner.remove_unit(u)?;
            },
        }
    }
    Ok(())
}

pub fn dry_run(plan: &plan::Plan) {
    do_run(DryRunner, plan).unwrap();
}
pub fn exec(plan: &mut plan::Plan) -> anyhow::Result<()> {
    do_run(RealRunner, plan)
}
