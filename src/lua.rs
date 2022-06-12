use mlua::prelude::*;
use std::path::Path;

use super::Context;

pub fn get_conf<P: AsRef<Path>>(dir: Option<P>, file: Option<P>) -> anyhow::Result<Context> {
    let lua = Lua::new();
    let context = lua.create_table()?;
    context.set("images", lua.create_table()?)?;
    context.set("units", lua.create_table()?)?;
    context.set("volumes", lua.create_table()?)?;
    context.set("networks", lua.create_table()?)?;
    lua.globals().set("context", context.clone())?;
    if let Some(d) = dir {
        std::env::set_current_dir(d)?;
    }
    let f = std::fs::read(
        file.map(|f| f.as_ref().to_owned())
            .unwrap_or(Path::new("conf.lua").to_owned()),
    )?;
    lua.load(&f).exec()?;
    let context: Context = lua.from_value(mlua::Value::Table(context))?;
    Ok(context)
}
