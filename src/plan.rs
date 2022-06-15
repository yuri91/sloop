use std::collections::hash_map::DefaultHasher;
use std::collections::{HashMap, HashSet};
use std::hash::{Hash, Hasher};

use crate::exec;
use crate::objects::*;
use crate::podman;

pub enum Action {
    AddVolume(Volume),
    AddNetwork(Network),
    AddImage(Image),
    AddUnit(Unit),
    RemoveVolume(String),
    RemoveNetwork(String),
    RemoveImage(String),
    RemoveUnit(String),
}
pub struct Plan {
    actions: Vec<Action>,
}
impl Plan {
    pub fn new() -> Plan {
        Plan {
            actions: Vec::new(),
        }
    }
    pub fn queue(&mut self, a: Action) {
        self.actions.push(a);
    }
    pub fn iter(&self) -> impl Iterator<Item=&Action> {
        self.actions.iter()
    }
}

#[derive(Debug, PartialEq, Eq, Hash, Clone)]
struct EntityId {
    name: String,
    hash: u64,
}

#[derive(Debug, Default)]
struct ContextState {
    units: HashSet<EntityId>,
    images: HashSet<EntityId>,
    volumes: HashSet<EntityId>,
    networks: HashSet<EntityId>,
}

impl ContextState {
    fn from_context(ctx: &Context) -> ContextState {
        fn get_entity_id<H: Hash>(name: &str, h: &H) -> EntityId {
            let mut hasher = DefaultHasher::new();
            h.hash(&mut hasher);
            EntityId {
                name: name.to_owned(),
                hash: hasher.finish(),
            }
        }
        fn get_entity_id_units(name: &str, u: &Unit, ims: &HashSet<EntityId>) -> EntityId {
            let mut hasher = DefaultHasher::new();
            u.hash(&mut hasher);
            for i in ims {
                i.hash(&mut hasher);
            }
            EntityId {
                name: name.to_owned(),
                hash: hasher.finish(),
            }
        }
        let volumes = ctx
            .volumes
            .iter()
            .map(|v| get_entity_id(&v.name, &v))
            .collect();
        let networks = ctx
            .networks
            .iter()
            .map(|n| get_entity_id(&n.name, &n))
            .collect();
        let images = ctx
            .images
            .iter()
            .map(|i| get_entity_id(&i.name, &i))
            .collect();
        let units = ctx
            .units
            .iter()
            .map(|u| get_entity_id_units(&u.name, &u, &images))
            .collect();

        ContextState {
            units,
            images,
            volumes,
            networks,
        }
    }
    fn from_podman() -> anyhow::Result<ContextState> {
        fn entity_mapper<F: Fn(&str) -> anyhow::Result<HashMap<String, String>>>(
            u: &str,
            lister: F,
        ) -> anyhow::Result<EntityId> {
            let hash = lister(u)?
                .get("sloop_hash")
                .ok_or_else(|| anyhow::anyhow!("missing hash label"))?
                .parse()?;
            Ok(EntityId {
                name: u.to_owned(),
                hash,
            })
        }

        let units = podman::container::all()?
            .into_iter()
            .map(|u| entity_mapper(&u, podman::container::labels))
            .collect::<anyhow::Result<_>>()?;
        let images = podman::image::all()?
            .into_iter()
            .map(|i| entity_mapper(&i, podman::image::labels))
            .collect::<anyhow::Result<_>>()?;
        let volumes = podman::volume::all()?
            .into_iter()
            .map(|v| entity_mapper(&v, podman::volume::labels))
            .collect::<anyhow::Result<_>>()?;
        let networks = podman::network::all()?
            .into_iter()
            .map(|v| entity_mapper(&v, podman::network::labels))
            .collect::<anyhow::Result<_>>()?;
        Ok(ContextState {
            units,
            images,
            volumes,
            networks,
        })
    }
}

pub fn plan(ctx: &Context) -> anyhow::Result<Plan> {
    fn removed<'a>(
        new: &'a HashSet<EntityId>,
        old: &'a HashSet<EntityId>,
    ) -> impl Iterator<Item = &'a str> {
        old.difference(new).map(|e| e.name.as_str())
    }
    fn added<'a>(
        new: &'a HashSet<EntityId>,
        old: &'a HashSet<EntityId>,
    ) -> impl Iterator<Item = &'a str> {
        new.difference(old).map(|e| e.name.as_str())
    }
    let old_ctx = ContextState::from_podman()?;
    let new_ctx = ContextState::from_context(ctx);
    //let volumes_removed  = removed(&old_ctx.volumes, &new_ctx.volumes);
    let volumes_added = added(&old_ctx.volumes, &new_ctx.volumes);
    let networks_removed = removed(&old_ctx.networks, &new_ctx.networks);
    let networks_added = added(&old_ctx.networks, &new_ctx.networks);
    let images_removed = removed(&old_ctx.images, &new_ctx.images);
    let images_added = added(&old_ctx.images, &new_ctx.images);
    let units_removed = removed(&old_ctx.units, &new_ctx.units);
    let units_added = added(&old_ctx.units, &new_ctx.units);

    let mut plan = Plan::new();

    for i in images_added {
        let img = ctx
            .images
            .iter()
            .find(|img| &img.name == i)
            .expect("cannot find image by name");
        plan.queue(Action::AddImage(img.clone()));
    }
    for v in volumes_added {
        let vol = ctx
            .volumes
            .iter()
            .find(|vol| &vol.name == v)
            .expect("cannot find volume by name");
        plan.queue(Action::AddVolume(vol.clone()));
    }
    for n in networks_added {
        let net = ctx
            .networks
            .iter()
            .find(|net| &net.name == n)
            .expect("cannot find network by name");
        plan.queue(Action::AddNetwork(net.clone()));
    }

    for u in units_removed {
        plan.queue(Action::RemoveUnit(u.to_owned()));
    }
    for i in images_removed {
        plan.queue(Action::RemoveImage(i.to_owned()));
    }
    for n in networks_removed {
        plan.queue(Action::RemoveNetwork(n.to_owned()));
    }

    for u in units_added {
        let unit = ctx
            .units
            .iter()
            .find(|unit| &unit.name == u)
            .expect("cannot find unit by name");
        plan.queue(Action::AddUnit(unit.clone()));
    }

    Ok(plan)
}
