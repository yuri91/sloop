use std::path::PathBuf;
use structopt::StructOpt;

mod exec;
mod lua;
mod objects;
mod plan;
mod podman;
mod systemd;
mod utils;

use objects::*;

#[derive(Debug, StructOpt)]
#[structopt(name = "sloop", about = "minimal container manager")]
struct Opt {
    file: Option<PathBuf>,
    #[structopt(parse(from_os_str), short, long)]
    dir: Option<PathBuf>,
    #[structopt(short, long)]
    start: bool,
    #[structopt(short, long)]
    enable: bool,
}

fn main() -> anyhow::Result<()> {
    pretty_env_logger::init();
    let opts = Opt::from_args();
    if std::env::var("SLOOP_CONF").is_err() {
        let ctx = lua::get_conf(opts.dir, opts.file)?;
        std::env::set_var("SLOOP_CONF", serde_json::to_string(&ctx)?);
    }
    sudo::with_env(&["RUST_BACKTRACE", "RUST_LOG", "SLOOP_CONF"])
        .expect("Cannot gain root privilege");
    let ctx_env = std::env::var("SLOOP_CONF").expect("Missing conf variable");
    let ctx = serde_json::from_str(&ctx_env)?;
    let mut plan = plan::plan(&ctx)?;
    exec::dry_run(&plan);
    exec::exec(&mut plan)?;
    Ok(())
}
