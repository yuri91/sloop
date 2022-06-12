function volume(name)
	local self = {
		name = name,
	}
	if string.sub(name,1,1) ~= "/" then
		table.insert(context.volumes, self)
	end
	local getName = function ()
		return self.name
	end
	return {
		name = getName,
	}
end

function network(name)
	local self = {
		name = name,
	}
	table.insert(context.networks, self)
	local getName = function ()
		return self.name
	end
	return {
		name = getName,
	}
end

function image(name)
	local self = {
		name = name,
		from = "scratch",
		files = {},
		labels= {},
		env= {},
		entrypoint = {},
		cmd = {},
	}
	local pub = {}
	table.insert(context.images, self)
	local getName = function ()
		return self.name
	end
	local from = function (base)
		self.from = base
		return pub
	end
	local file = function (path, content, perm)
		table.insert(self.files, {
			path = path,
			content = content,
			perm = perm or 0x666,
		})
		return pub
	end
	local label = function (k, v)
		table.insert(self.labels, {
			key = k,
			value = v,
		})
		return pub
	end
	local env = function (k, v)
		table.insert(self.env, {
			key = k,
			value = v,
		})
		return pub
	end
	local entrypoint = function (...)
		self.entrypoint = {...}
		return pub
	end
	local cmd = function (...)
		self.cmd = {...}
		return pub
	end
	pub.name = getName
	pub.from = from
	pub.file = file
	pub.label = label
	pub.env = env
	pub.entrypoint = entrypoint
	pub.cmd = cmd
	return pub
end

function unit(name)
	local self = {
		name = name,
		volumes = {},
		ports = {},
		networks = {},
		after = {},
		wants = {},
		requires = {},
		labels = {},
	}
	local pub = {}
	table.insert(context.units, self)
	local getName = function ()
		return self.name
	end
	local image = function (im)
		self.image = im.name()
		return pub
	end
	local volume = function (v, path)
		table.insert(self.volumes, {
			volume = v.name(),
			path = path,
		})
		return pub
	end
	local network = function (n)
		table.insert(self.networks, n.name())
		return pub
	end
	local port = function (host, unit)
		table.insert(self.ports, {
			host = host,
			unit = unit,
		})
		return pub
	end
	local after = function (unit)
		table.insert(self.after, unit.name())
		return pub
	end
	local wants = function (unit)
		table.insert(self.wants, unit.name())
		return pub
	end
	local requires = function (unit)
		table.insert(self.requires, unit.name())
		return pub
	end
	local label = function (k, v)
		table.insert(self.labels, {
			key = k,
			value = v,
		})
		return pub
	end
	pub.name = getName
	pub.image = image
	pub.volume = volume
	pub.network = network
	pub.port = port
	pub.after = after
	pub.wants = wants
	pub.requires = requires
	pub.label = label
	return pub;
end

function system_unit(name)
	local self = {
		name = name
	}
	local name getName = function(n)
		return self.name
	end
	return {
		name = getName
	}
end

function traefik_image(name, net, acme_email)
	local name = name or "traefik"
	local conf = string.format(
[[
api = true↲
[providers.docker]↲
exposedByDefault = false↲
network = "%s"↲
↲
[entryPoints.web]↲
address = ":80"↲
[entryPoints.web.http.redirections.entryPoint]↲
to = "websecure"↲
scheme = "https"↲
↲
[entryPoints.websecure]↲
address = ":443"↲
↲
[entryPoints.matrixfed]↲
address = ":8448"↲
↲
[certificatesResolvers.my.acme]↲
tlsChallenge = true↲
email = "%s"↲
storage = "/certificates/acme.json"↲
↲
[accessLog]↲
]]
	, net, acme_email)

	local image = image(name)
		.from("docker.io/traefik:v2.5")
		.file("/etc/traefik/traefik.conf", conf, 0x666)
		.label("traefik.enable", "true")
		.label("traefik.http.routers.api.rule", "Host(`traefik.yuri.space`)")
		.label("traefik.http.routers.api.entrypoints", "websecure")
		.label("traefik.http.routers.api.service", "api@internal")
		.label("traefik.http.routers.api.tls", "true")
		.label("traefik.http.routers.api.tls.certresolver", "my")
	return image
end

function traefik_unit(name, net, vol, acme_email)
	local name = name or "traefik"
	local image = traefik_image(name, net, acme_email)
	local podman_sock = volume("/run/podman/podman.sock")
	local podman_sock_unit = system_unit("podman.socket")
	local unit = unit(name)
		.image(image)
		.network(net)
		.volume(vol, "/volume")
		.volume(podman_sock, "/var/run/docker.sock")
		.port(80, 80)
		.port(443, 443)
		.port(8448, 8448)
		.requires(podman_sock_unit)
		.after(podman_sock_unit)
	return unit
end

net = network("public")
certs_vol = volume("certs")

traefik_unit("traefik", net, certs_vol, "y.iozzelli@gmail.com")
	.label("traefik.http.routers.api.middlewares", "authelia@docker")
