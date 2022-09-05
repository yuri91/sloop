package sloop

import $miniflux "starship.yuri.space/miniflux:defs"

import $busybox "starship.yuri.space/busybox:defs"

import $traefik "starship.yuri.space/traefik:defs"

$network: public: {
}

$volume: miniflux_db: {
}

$image: miniflux: $miniflux.image & {
}
$service: feeder: $miniflux.service & {
	image: $image.miniflux
	volumes: {
		"/etc/miniflux.db": $volume.miniflux_db
	}
	networks: [
		$network.public,
	]
}

$volume: busybox: {}
$image: busybox: $busybox.image

$service: busybox: $busybox.service & {
	image: $image.busybox
	volumes: {
		"\(image.#mounts.data)": $volume.busybox
	}
}

traefik_conf: $traefik.config & {
	api_url:         "traefik.yuri.space"
	network:         $network.public
	api_middlewares: "authelia@docker"
	acme_email:      "y.iozzelli@gmail.com"
	extra_entrypoints: {
		matrixfed: {
			port: 8448
		}
	}
}
$volume: cert_store: {
}
$volume: podman_sock: {
	name: "/run/podman/podman.sock"
}
$image: traefik:   traefik_conf.image
$service: traefik: traefik_conf.service & {
	image: $image.traefik
	volumes: {
		"\(image.#mounts.docker_sock)": $volume.podman_sock
		"\(image.#mounts.certs)":       $volume.cert_store
	}
	networks: [$network.public]
}
