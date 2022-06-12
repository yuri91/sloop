use log::{debug, info};
use std::io::Write;

pub fn cmd<T: AsRef<str>>(args: &[T], stdin: Option<&str>) -> anyhow::Result<String> {
    info!(
        "+ {:?}",
        args.iter().map(|a| a.as_ref()).collect::<Vec<&str>>()
    );
    let mut child = std::process::Command::new(args[0].as_ref())
        .args(args[1..].iter().map(|a| a.as_ref()))
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
    debug!(
        "output: -----------------------\n{}\n--------------------------------\n",
        ret
    );
    if !out.status.success() {
        anyhow::bail!("non-zero exit status for {}", args[0].as_ref());
    }
    Ok(ret)
}
