use crate::utils::cmd;
use std::collections::HashMap;

fn find_all(kind: &str) -> anyhow::Result<Vec<String>> {
    let name_filter = if kind == "image" {
        "{{.Names}}"
    } else {
        "{{.Name}}"
    };
    let res = cmd(
        &[
            "podman",
            kind,
            "ls",
            "--format",
            name_filter,
            "--filter",
            "label=sloop",
        ],
        None,
    )?;
    let ret = res.lines().map(|s| s.to_string()).collect();
    Ok(ret)
}
fn labels(kind: &str, u: &str) -> anyhow::Result<HashMap<String, String>> {
    let out = cmd(
        &[
            "podman",
            kind,
            "inspect",
            "--format",
            "{{.Config.Labels}}",
            u,
        ],
        None,
    )?;
    let p = out
        .strip_prefix("map[")
        .ok_or_else(|| anyhow::anyhow!("error parsing podman labels"))?;
    let p = p
        .strip_suffix(']')
        .ok_or_else(|| anyhow::anyhow!("error parsing podman labels"))?;
    let mut res = HashMap::new();
    for e in p.lines() {
        let (k, v) = e
            .split_once(':')
            .ok_or_else(|| anyhow::anyhow!("error parsing podman labels"))?;
        res.insert(k.to_owned(), v.to_owned());
    }
    Ok(res)
}
fn exists(kind: &str, name: &str) -> bool {
    cmd(&["podman", kind, "exists", name], None).is_ok()
}

pub mod container {
    use std::collections::HashMap;

    use crate::utils::cmd;

    pub fn create<'a, I1, I2, I3, I4, K>(
        name: &str,
        volumes: I1,
        networks: I2,
        ports: I3,
        labels: I4,
    ) -> anyhow::Result<()>
    where
        I1: IntoIterator<Item = &'a K>,
        I2: IntoIterator<Item = &'a K>,
        I3: IntoIterator<Item = (u16, u16)>,
        I4: IntoIterator<Item = (&'a K, &'a K)>,
        K: 'a + std::borrow::ToOwned<Owned = String> + std::fmt::Display,
    {
        let mut args: Vec<_> = vec![
            "podman",
            "container",
            "create",
            "--init",
            "--name",
            &name,
            "--label",
            "sloop",
        ]
        .into_iter()
        .map(str::to_owned)
        .collect();
        for v in volumes {
            args.extend(["-v".to_owned(), v.to_owned()]);
        }
        for n in networks {
            args.extend(["--net".to_owned(), n.to_owned()]);
        }
        for p in ports {
            args.extend(["-p".to_owned(), format!("{}:{}", p.0, p.1)]);
        }
        for l in labels {
            args.extend(["-l".to_owned(), format!("{}={}", l.0, l.1)]);
        }
        let name_ver = format!("sloop/{}:latest", name);
        args.push(name_ver);
        cmd(&args, None)?;
        Ok(())
    }

    pub fn remove(name: &str) -> anyhow::Result<()> {
        cmd(&["podman", "container", "rm", name], None)?;
        Ok(())
    }

    pub fn generate_unit<'a, I, K>(
        name: &str,
        wants: I,
        requires: I,
        after: I,
    ) -> anyhow::Result<String>
    where
        I: IntoIterator<Item = &'a K>,
        K: 'a + std::borrow::ToOwned<Owned=String>,
    {
        let mut args: Vec<_> = vec![
            "podman",
            "generate",
            "systemd",
            "--name",
            "--new",
            name,
            "--container-prefix",
            "",
            "--separator",
            "",
            "--no-headers",
        ]
        .into_iter()
        .map(str::to_owned)
        .collect();
        for w in wants {
            args.push(w.to_owned());
        }
        for r in requires {
            args.push(r.to_owned());
        }
        for a in after {
            args.push(a.to_owned());
        }
        let out = cmd(&args, None)?;
        Ok(out)
    }

    pub fn all() -> anyhow::Result<Vec<String>> {
        super::find_all("container")
    }

    pub fn exists(name: &str) -> bool {
        super::exists("container", name)
    }

    pub fn labels(u: &str) -> anyhow::Result<HashMap<String, String>> {
        super::labels("container", u)
    }
}

pub mod volume {
    use crate::utils::cmd;
    use std::collections::HashMap;

    pub fn create(name: &str) -> anyhow::Result<()> {
        cmd(
            &["podman", "volume", "create", "--label", "sloop", &name],
            None,
        )?;
        Ok(())
    }

    pub fn remove(name: &str) -> anyhow::Result<()> {
        cmd(&["podman", "volume", "rm", name], None)?;
        Ok(())
    }

    pub fn all() -> anyhow::Result<Vec<String>> {
        super::find_all("volume")
    }

    pub fn exists(name: &str) -> bool {
        super::exists("volume", name)
    }

    pub fn labels(u: &str) -> anyhow::Result<HashMap<String, String>> {
        super::labels("volume", u)
    }
}

pub mod network {
    use crate::utils::cmd;
    use std::collections::HashMap;

    pub fn create(name: &str) -> anyhow::Result<()> {
        cmd(
            &["podman", "network", "create", "--label", "sloop", &name],
            None,
        )?;
        Ok(())
    }

    pub fn remove(name: &str) -> anyhow::Result<()> {
        cmd(&["podman", "network", "rm", name], None)?;
        Ok(())
    }

    pub fn all() -> anyhow::Result<Vec<String>> {
        super::find_all("network")
    }

    pub fn exists(name: &str) -> bool {
        super::exists("network", name)
    }

    pub fn labels(u: &str) -> anyhow::Result<HashMap<String, String>> {
        super::labels("network", u)
    }
}

pub mod image {
    use std::collections::HashMap;
    use crate::utils::cmd;

    pub fn all() -> anyhow::Result<Vec<String>> {
        super::find_all("image")
    }

    pub fn exists(name: &str) -> bool {
        super::exists("image", name)
    }

    pub fn labels(u: &str) -> anyhow::Result<HashMap<String, String>> {
        super::labels("image", u)
    }

    pub fn create(name: &str, dockerfile: &str, tag: &str) -> anyhow::Result<()> {
        cmd(&["buildah", "bud", "--layers", "-t", &format!("sloop/{}:{}", name, tag), "-f", "-"], Some(dockerfile))?;
        Ok(())
    }

    pub fn remove(name: &str, tag: &str) -> anyhow::Result<()> {
        cmd(&["buildah", "rmi", &format!("sloop/{}:{}", name, tag)], None)?;
        Ok(())
    }

    pub fn tag(name: &str, curtag: &str, newtag: &str) -> anyhow::Result<()> {
        cmd(&["podman", "tag", &format!("sloop/{}:{}", name, curtag), &format!("sloop/{}:{}", name, newtag)], None)?;
        Ok(())
    }

    pub fn untag(name: &str, tag: &str) -> anyhow::Result<()> {
        cmd(&["podman", "untag", &format!("sloop/{}:{}", name, tag)], None)?;
        Ok(())
    }
}
