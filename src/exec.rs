use crate::objects::*;
use crate::plan;

static NAME_PLACEHOLDER: &'static str = "SLOOP_PLACEHOLDER";

//fn create_network(net: &Network, dirty: &mut DirtyState) -> anyhow::Result<()> {
//    info!("new network: {}", net.name);
//    if podman::network_exists(&net.name) {
//        dirty.networks.keep(&net.name);
//        return Ok(())
//    }
//    dirty.networks.update(&net.name);
//    cmd(&["podman", "network", "create", "--label", "sloop", &net.name], None)?;
//    Ok(())
//}
//
//fn create_volume(vol: &Volume, dirty: &mut DirtyState) -> anyhow::Result<()> {
//    info!("new volume: {}", vol.name);
//    if podman::volume_exists(&vol.name) {
//        dirty.volumes.keep(&vol.name);
//        return Ok(())
//    }
//    dirty.volumes.update(&vol.name);
//    cmd(&["podman", "volume", "create", "--label", "sloop", &vol.name], None)?;
//    Ok(())
//}
//
//fn create_image(img: &Image, dirty: &mut DirtyState) -> anyhow::Result<()> {
//    info!("new image: {}", img.name);
//    let tag_latest = format!("sloop/{}:latest", img.name);
//    let mut script = format!("FROM {}\n", img.from);
//    let mut tmps = Vec::new();
//    for file in &img.files {
//        let mut f = tempfile::NamedTempFile::new_in(".")?;
//        f.write_all(&file.content.as_bytes())?;
//        f.flush()?;
//        let fname = f.path().file_name().unwrap().to_string_lossy();
//        script.push_str(&format!("COPY --chmod={:03x} {} {}\n", file.perm, fname, file.path));
//        tmps.push(f);
//    }
//    script.push_str("LABEL \"sloop\"=\"\"\n");
//    for l in &img.labels {
//        script.push_str(&format!("LABEL \"{}\"=\"{}\"\n", l.key, l.value));
//    }
//    for e in &img.env {
//        script.push_str(&format!("ENV \"{}\"=\"{}\"\n", e.key, e.value));
//    }
//    if !img.entrypoint.is_empty() {
//        script.push_str(&format!("ENTRYPOINT {:?}\n", img.entrypoint));
//    }
//    if !img.cmd.is_empty() {
//        script.push_str(&format!("CMD {:?}\n", img.cmd));
//    }
//    let prev_id = cmd(&["buildah", "inspect", "--format='{{.FromImageID}}'", &tag_latest], None).ok();
//    debug!("Dockerfile:\n{}", script);
//    let res = cmd(&["buildah", "bud", "--layers", "-t", &tag_latest, "-f", "-"], Some(&script))?;
//    let new_id = cmd(&["buildah", "inspect", "--format='{{.FromImageID}}'", &tag_latest], None).ok();
//    if new_id != prev_id {
//        dirty.images.update(&img.name);
//    } else {
//        dirty.images.keep(&img.name);
//    }
//    debug!("build output:\n{}", res);
//    Ok(())
//}
//
//mod podman2 {
//    use super::*;
//
//    static NAME_PLACEHOLDER: &'static str = "SLOOP_PLACEHOLDER";
//    pub fn create(u: &Unit, dirty: &mut DirtyState) -> anyhow::Result<()> {
//        let mut args: Vec<_> = vec!["podman", "container", "create", "--init", "--name", &u.name, "--label", "sloop"].into_iter().map(str::to_owned).collect();
//        for v in &u.volumes {
//            if v.volume.starts_with('/') {
//                continue;
//            }
//            if !dirty.volumes.is_unchanged(&v.volume) {
//                dirty.units.update(&u.name);
//            }
//            args.extend(["-v".to_owned(), format!("{}:{}", v.volume, v.path)]);
//        }
//        for n in &u.networks {
//            if !dirty.networks.is_unchanged(n) {
//                dirty.units.update(&u.name);
//            }
//            args.extend(["--net".to_owned(), n.to_owned()]);
//        }
//        for p in &u.ports {
//            args.extend(["-p".to_owned(), p.host.to_string(), p.unit.to_string()]);
//        }
//        for l in &u.labels {
//            args.extend(["-l".to_owned(), format!("{}={}",l.key, l.value)]);
//        }
//        let name_ver = format!("sloop/{}:latest", u.name);
//        args.push(name_ver);
//        cmd(&args, None)?;
//        Ok(())
//    }
//}
//fn create_unit(u: &Unit, dirty: &mut DirtyState) -> anyhow::Result<()> {
//    info!("new unit: {}", u.name);
//    if systemd::is_active(&u.name) {
//        systemd::stop(&u.name)?;
//    }
//    if podman::container_exists(&u.name) {
//        podman::container_remove(&u.name)?;
//    }
//    podman::create(u, dirty)?;
//    let s = podman::generate(u)?;
//    let old = systemd::read(&u.name);
//    if Some(&s) == old.as_ref() {
//        dirty.units.keep(&u.name);
//    } else {
//        dirty.units.update(&u.name);
//        systemd::install(&u.name, &s)?;
//    }
//    Ok(())
//}
//

pub fn dry_run(plan: &plan::Plan) {}
pub fn exec(plan: &mut plan::Plan) -> anyhow::Result<()> {
    Ok(())
}
