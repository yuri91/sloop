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

_tool: create: exec.Run & {
	name:   string
	exists: bool
	kind:   string
	cmd:    [
		if exists == false {
			{"podman \(kind) create --label sloop \(name)"}
		},
		{"echo \(kind) \(name) exists"},
	][0]
}
_tool: create_kind: [Kind=string]: {
	objects: [...{name: string}]
	group: {
		do_list: exec.Run & {
			cmd:    "podman \(Kind) ls --format \"{{.Name}}\" --filter label=sloop"
			stdout: string
			quoted: strings.Split(stdout, "\n")
			result: [ for v in quoted {strings.Trim(v, "\"")}]
			$after: in_deps
		}
		for v in objects {
			"create_\(Kind)_\(v.name)": _tool.create & {
				kind:   Kind
				name:   v.name
				exists: list.Contains(do_list.result, v.name)
			}
		}
	}
	out_deps: [ for k, v in group {v}]
	in_deps: [...] | *[]
}

_tool: create_kind: volume: {
	objects: volumes
}
_tool: create_kind: network: {
	objects: networks
}
_tool: create_tempdir: file.MkdirTemp & {
	name:    string
	pattern: "files_\(name)"
}

_tool: write_file: file.Create & {
	tempdir:   string | *"a"
	path:      string | *"puppa"
	contents:  string | *"puppa"
	path_conv: strings.Replace(path, "/", "_", -1)
	filename:  "\(tempdir)/\(path_conv)"
}
_tool: create_dockerfile: file.Create & {
	tempdir:  string
	filename: "\(tempdir)/Dockerfile"
	image:    #Image
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
}
_tool: create_image: exec.Run & {
	name: string
	cmd:  "buildah bud --layers -t sloop/\(name)"
}
_tool: clean_tempdir: exec.Run & {
	path: string
	cmd:  "rm -rfv  \(path)"
}

_task: create_volumes:  _tool.create_kind.volume
_task: create_networks: _tool.create_kind.network
_task: create_images: {
	for i in images {
		"\(i.name)": {
			create_tempdir: _tool.create_tempdir & {
				name:   i.name
				$after: in_deps
			}
			write_files: {
				for p, f in i.files {
					"\(p)": _tool.write_file & {
						path:        p
						contents:    f.content
						permissions: f.permissions
						tempdir:     create_tempdir.path
					}
				}
			}
			create_dockerfile: _tool.create_dockerfile & {
				tempdir: create_tempdir.path
				image:   i
				$after: [ for k, v in write_files {v}]
			}
			create_image: _tool.create_image & {
				name:   i.name
				dir:    create_tempdir.path
				$after: create_dockerfile
			}
			clean_tempdir: _tool.clean_tempdir & {
				path:   create_tempdir.path
				$after: create_image
			}
		}
	}
	out_deps: [ for i in images {create_images[i.name].clean_tempdir}]
	in_deps: [...] | *[]
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

service_path: "/etc/systemd/system/"

_tool: is_service_active: exec.Run & {
	name: string
	cmd: ["bash", "-c", "systemctl is-active \(name).service | cat"]
	stdout: string
	active: stdout == "active\n"
}
_tool: stop_service: exec.Run & {
	name:   string
	active: bool
	cmd:    [
		if active == false {
			{"true"}
		},
		{"systemctl stop \(name).service"},
	][0]
}
_tool: start_service: exec.Run & {
	name: string
	cmd:  "systemctl enable --now \(name).service"
}
_tool: create_container: exec.Run & {
	service:  #Service
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
	ports: strings.Join([ for p in _ports {" -p \(p.host):\(p.service)"}], " ")
	cmd:   "podman container create --init --name \(service.name) --label sloop \(volumes) \(networks) \(ports) sloop/\(service.image.name):latest"
}
_tool: remove_container: exec.Run & {
	name: string
	cmd:  "podman container rm \(name)"
}
_tool: gen_service: exec.Run & {
	service: #Service
	dir:     service_path
	_wants: [ for w in service.wants {(w & string) | (w.name + ".service")}]
	wants: list.Concat([ for w in _wants {["--wants", w]}])
	_requires: [ for r in service.requires {(r & string) | (r.name + ".service")}]
	requires: list.Concat([ for r in _requires {["--requires", r]}])
	_after: [ for a in service.after {(a & string) | (a.name + ".service")}]
	after:  list.Concat([ for a in _after {["--after", a]}])
	cmd:    ["podman", "generate", "systemd", "--name", "--new", service.name, "--container-prefix", "", "--separator", ""] + wants + after + requires
	stdout: string
}

_tool: write_service: file.Create & {
	name:     string
	filename: "\(service_path)\(name).service"
}
_task: create_services: {
	group: {
		for s in services {
			"is_service_active_\(s.name)": _tool.is_service_active & {
				name:   s.name
				$after: in_deps
			}
			"stop_service_\(s.name)": _tool.stop_service & {
				name:   s.name
				active: group["is_service_active_\(s.name)"].active
			}
			"create_container_\(s.name)": _tool.create_container & {
				service: s
				$after:  group["stop_service_\(s.name)"]
			}
			"gen_service_\(s.name)": _tool.gen_service & {
				service: s
				$after:  group["create_container_\(s.name)"]
			}
			"remove_container_\(s.name)": _tool.remove_container & {
				name:   s.name
				$after: group["gen_service_\(s.name)"]
			}
			"write_service_\(s.name)": _tool.write_service & {
				name:     s.name
				contents: group["gen_service_\(s.name)"].stdout
				$after:   group["remove_container_\(s.name)"]
			}
		}
		daemon_reload: {
			$after: [ for s in services {group["write_service_\(s.name)"]}]
			cmd: "systemctl daemon-reload"
		}
	}
	out_deps: [group.daemon_reload]
	in_deps: [...] | *[]
}
_task: start_services: {
	group: {
		for s in services {
			"start_service_\(s.name)": _tool.start_service & {
				name: s.name
			}
		}
	}
	out_deps: [ for k, v in group {v}]
	in_deps: [...] | *[]
}
command: create_services: _task.create_services
command: start_services:  _task.start_services
command: create_all: {
	create_networks: _task.create_networks
	create_volumes:  _task.create_volumes & {
		in_deps: create_networks.out_deps
	}
	create_images: _task.create_images & {
		in_deps: create_volumes.out_deps
	}
}
