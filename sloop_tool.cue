package sloop

import (
	"strings"
	"list"
	"encoding/json"
	"text/tabwriter"
	"text/template"
	"tool/cli"
	"tool/exec"
	"tool/file"
	"strconv"
)

volumes: [ for k, v in $volume {v}]
networks: [ for k, v in $network {v}]
images: [ for k, v in $image {v}]
services: [ for k, v in $service {v}]

_task: create_volumes: {
	group: {
		do_list: exec.Run & {
			cmd:    "podman volume ls --format \"{{.Name}}\" --filter label=sloop"
			stdout: string
			quoted: strings.Split(stdout, "\n")
			result: [ for v in quoted {strings.Trim(v, "\"")}]
		}
		for v in volumes {
			"create_volume_\(v.name)": exec.Run & {
				name:   v.name
				exists: list.Contains(do_list.result, v.name)
				cmd:    [
					if exists == false {
						{"podman volume create --label sloop \(name)"}
					},
					{"echo volume \(name) exists"},
				][0]
			}
		}
	}
}
_task: create_networks: {
	group: {
		do_list: exec.Run & {
			cmd:    "podman network ls --format \"{{.Name}}\" --filter label=sloop"
			stdout: string
			quoted: strings.Split(stdout, "\n")
			result: [ for v in quoted {strings.Trim(v, "\"")}]
		}
		for n in networks {
			"create_network_\(n.name)": exec.Run & {
				name:   n.name
				exists: list.Contains(do_list.result, n.name)
				cmd:    [
					if exists == false {
						{"podman network create --label sloop \(name)"}
					},
					{"echo network \(name) exists"},
				][0]
			}
		}
	}
}
_task: create_images: {
	for i in images {
		"\(i.name)": {
			create_tempdir: file.MkdirTemp & {
				path:    string
				pattern: "files_\(i.name)"
			}
			write_files: {
				for p, f in i.files {
					"\(p)": file.Create & {
						tempdir:     create_tempdir.path
						path_conv:   strings.Replace(p, "/", "_", -1)
						filename:    "\(tempdir)/\(path_conv)"
						contents:    f.content
						permissions: f.permissions
					}
				}
			}
			create_dockerfile: file.Create & {
				tempdir:  create_tempdir.path
				filename: "\(tempdir)/Dockerfile"
				image:    i
				data: {
					from: image.from
					files: [
						for p, f in image.files {
							permissions: strconv.FormatInt(f.permissions, 8)
							path:        p
							filename:    strings.Replace(path, "/", "_", -1)
						},
					]
					labels: [
						for k, v in image.labels {
							"\"\(k)\"=\"\(v)\""
						},
					]
					env: [
						for k, v in image.env {
							"\"\(k)\"=\"\(v)\""
						},
					]
					if len(image.entrypoint) != 0 {
						entrypoint: json.Marshal(image.entrypoint)
					}
					if len(image.cmd) != 0 {
						cmd: json.Marshal(image.cmd)
					}
				}
				contents: template.Execute("""
					FROM {{ .from }}
					{{ range .files}}
					COPY --chmod={{.permissions}} {{.filename}} {{.path}}
					{{end}}
					LABEL "sloop"=""
					{{ range .labels}}
					LABEL {{.}}
					{{end}}
					{{ range .env}}
					ENV {{.}}
					{{end}}
					{{if ne .entrypoint nil}}
					ENTRYPOINT {{.entrypoint}}
					{{end}}
					{{if ne .cmd nil}}
					CMD {{.cmd}}
					{{end}}
					""", data)
				$after: [ for k, v in write_files {v}]
			}
			create_image: exec.Run & {
				cmd:    "buildah bud --layers -t sloop/\(i.name)"
				dir:    create_tempdir.path
				$after: create_dockerfile
			}
			clean_tempdir: exec.Run & {
				path:   create_tempdir.path
				cmd:    "rm -rfv  \(path)"
				$after: create_image
			}

		}
	}
}

service_path: "/etc/systemd/system/"

_task: create_services: {
	group: {
		for s in services {
			"is_service_active_\(s.name)": exec.Run & {
				cmd: ["bash", "-c", "systemctl is-active \(s.name).service | cat"]
				stdout: string
				active: stdout == "active\n"
			}
			"stop_service_\(s.name)": exec.Run & {
				active: group["is_service_active_\(s.name)"].active
				cmd:    [
					if active == false {
						{"true"}
					},
					{"systemctl stop \(s.name).service"},
				][0]
			}
			"create_container_\(s.name)": exec.Run & {
				service:  s
				volumes:  strings.Join([ for p, v in service.volumes {"-v \(v.name):\(p)"}], " ")
				networks: strings.Join([ for n in service.networks {"--net \(n.name)"}], " ")
				_ports: [
					for p in service.ports {
						{
							host:  p.host | p
							guest: p.guest | p
						}
					},
				]
				ports:  strings.Join([ for p in _ports {" -p \(p.host):\(p.service)"}], " ")
				cmd:    "podman container create --init --name \(service.name) --label sloop \(volumes) \(networks) \(ports) sloop/\(service.image.name):latest"
				$after: group["stop_service_\(s.name)"]
			}
			"gen_service_\(s.name)": exec.Run & {
				service: s
				dir:     service_path
				_wants: [ for w in service.wants {(w & string) | (w.name + ".service")}]
				wants: list.Concat([ for w in _wants {["--wants", w]}])
				_requires: [ for r in service.requires {(r & string) | (r.name + ".service")}]
				requires: list.Concat([ for r in _requires {["--requires", r]}])
				_after: [ for a in service.after {(a & string) | (a.name + ".service")}]
				after:  list.Concat([ for a in _after {["--after", a]}])
				cmd:    ["podman", "generate", "systemd", "--name", "--new", service.name, "--container-prefix", "", "--separator", ""] + wants + after + requires
				stdout: string
				$after: group["create_container_\(s.name)"]
			}
			"remove_container_\(s.name)": exec.Run & {
				cmd:    "podman container rm \(s.name)"
				$after: group["gen_service_\(s.name)"]
			}
			"write_service_\(s.name)": file.Create & {
				filename: "\(service_path)\(s.name).service"
				contents: group["gen_service_\(s.name)"].stdout
				$after:   group["remove_container_\(s.name)"]
			}
		}
		daemon_reload: {
			$after: [ for s in services {group["write_service_\(s.name)"]}]
			cmd: "systemctl daemon-reload"
		}
	}
}
_task: start_services: {
	group: {
		for s in services {
			"start_service_\(s.name)": exec.Run & {
				name: s.name
				cmd:  "systemctl enable --now \(name).service"
			}
		}
	}
}

command: list: {
	print: cli.Print & {
		text: tabwriter.Write([
			"volumes:",
			for v in volumes {
				"\t\(v.name)"
			},
			"images:",
			for i in images {
				"\t\(i.name)"
			},
			"services:",
			for s in services {
				"\t\(s.name)"
			},
		])
	}
}

command: create_volumes:  _task.create_volumes
command: create_networks: _task.create_networks
command: create_images:   _task.create_images

command: create_services: _task.create_services
command: start_services:  _task.start_services
command: create_all: {
	create_networks: _task.create_networks
	create_volumes:  _task.create_volumes
	create_images:   _task.create_images & {
		$after: [create_volumes, create_networks]
	}
	create_services: _task.create_services & {
		$after: create_images
	}
}
