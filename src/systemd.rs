use crate::utils::cmd;
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
    std::fs::write(&format!("/etc/systemd/system/{}.service", service), content)?;
    Ok(())
}
pub fn uninstall(service: &str) -> anyhow::Result<()> {
    std::fs::remove_file(&format!("/etc/systemd/system/{}.service", service))?;
    Ok(())
}
