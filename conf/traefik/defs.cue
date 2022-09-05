package defs

import "encoding/yaml"

#Entrypoint: {
	port: uint16
}
config: {
	api_url:         string
	network:         #Network
	api_middlewares: string
	acme_email:      string
	extra_entrypoints: [string]: #Entrypoint
	requires: [...#Dependency]
	after: [...#Dependency]
	wants: [...#Dependency]

	_extra_entrypoints: {
		for n, e in extra_entrypoints {
			"\(n)": {
				address: ":\(e.port)"
			}
		}
	}

	let _network = network
	_config: {
		api: true
		accessLog: {
		}

		providers: docker: {
			exposedByDefault: false
			network:          _network.name
		}

		entryPoints: {
			web: {
				address: ":80"
				http: redirections: entryPoint: {
					to:     "websecure"
					scheme: "https"
				}
			}
			websecure: {
				address: ":443"
			}
			{
				_extra_entrypoints
			}
		}
		certificatesResolvers: {
			tlsChallenge: true
			email:        acme_email
			storage:      "/certificates/acme.json"
		}

	}
	image: #Image & {
		from: "docker.io/traefik:v2.5"
		labels: {
			"traefik.enable":                            "true"
			"traefik.http.routers.api.rule":             "Host(`\(api_url)`)"
			"traefik.http.routers.api.entrypoints":      "websecure"
			"traefik.http.routers.api.service":          "api@internal"
			"traefik.http.routers.api.tls":              "true"
			"traefik.http.routers.api.tls.certresolver": "my"
			"traefik.http.routers.api.middlewares":      api_middlewares
		}
		#mounts: {
			docker_sock: "/var/run/docker.sock"
			certs:       "/certificates"
		}
		files: {
			"/etc/traefik/traefik.yaml": yaml.Marshal(_config)
		}
	}

	let _image = image
	_extra_ports: [
		for _, e in extra_entrypoints {
			e.port
		},
	]
	service: #Service & {
		image:    _image
		ports:    [80, 443] + _extra_ports
		requires: ["podman.socket"] + config.requires
		after:    ["podman.socket"] + config.after
		wants:    config.wants
	}
}
