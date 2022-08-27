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

tool: create: exec.Run & {
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
tool: create_kind: [Kind=string]: {
	objects: [...{name: string}]
	do_list: exec.Run & {
		cmd:    "podman \(Kind) ls --format \"{{.Name}}\" --filter label=sloop"
		stdout: string
		quoted: strings.Split(stdout, "\n")
		result: [ for v in quoted {strings.Trim(v, "\"")}]
	}
	for v in objects {
		"create_\(Kind)_\(v.name)": tool.create & {
			kind:   Kind
			name:   v.name
			exists: list.Contains(do_list.result, v.name)
		}
	}
}

tool: create_kind: volume: {
	objects: volumes
}
tool: create_kind: network: {
	objects: networks
}
tool: create_tempdir: file.MkdirTemp & {
	name:    string
	pattern: "files_\(name)"
}

tool: write_file: file.Create & {
	tempdir:   string
	path:      string
	path_conv: strings.Replace(path, "/", "_", -1)
	filename:  "\(tempdir)/\(path_conv)"
}
tool: create_dockerfile: file.Create & {
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
tool: create_image: exec.Run & {
	name: string
	cmd:  "buildah bud --layers -t sloop/\(name)"
}
tool: clean_tempdir: exec.Run & {
	path: string
	cmd:  "rm -rfv  \(path)"
}

task: create_volumes:  tool.create_kind.volume
task: create_networks: tool.create_kind.network
task: create_images: {
	group: {
		for i in images {
			"create_tempdir_\(i.name)": tool.create_tempdir & {
				name: i.name
			}
			for p, f in i.files {
				"write_file_\(i.name)_\(p)": tool.write_file & {
					path:        p
					contents:    f.content
					permissions: f.permissions
					tempdir:     group["create_tempdir_\(i.name)"].path
				}
			}
			"create_dockerfile_\(i.name)": tool.create_dockerfile & {
				tempdir: group["create_tempdir_\(i.name)"].path
				image:   i
			}
			"create_image_\(i.name)": tool.create_image & {
				name: i.name
				dir:  group["create_tempdir_\(i.name)"].path
			}
			"clean_tempdir_\(i.name)": tool.clean_tempdir & {
				path:   group["create_tempdir_\(i.name)"].path
				$after: group["create_image_\(i.name)"]
			}
		}
	}
}

command: list: {
	task: print: cli.Print & {
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

command: create_volumes:  task.create_volumes
command: create_networks: task.create_networks
command: create_images:   task.create_images

service_path: "/etc/systemd/system/"

tool: is_service_active: exec.Run & {
	name: string
	cmd: ["bash", "-c", "systemctl is-active \(name).service | cat"]
	stdout: string
	active: stdout == "active\n"
}
tool: stop_service: exec.Run & {
	name:   string
	active: bool
	cmd:    [
		if active == false {
			{"true"}
		},
		{"systemctl stop \(name).service"},
	][0]
}
tool: start_service: exec.Run & {
	name: string
	cmd:  "systemctl enable --now \(name).service"
}
tool: create_container: exec.Run & {
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
tool: remove_container: exec.Run & {
	name: string
	cmd:  "podman container rm \(name)"
}
tool: gen_service: exec.Run & {
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

tool: write_service: file.Create & {
	name:     string
	filename: "\(service_path)\(name).service"
}
task: create_services: {
	group: {
		for s in services {
			"is_service_active_\(s.name)": tool.is_service_active & {
				name: s.name
			}
			"stop_service_\(s.name)": tool.stop_service & {
				name:   s.name
				active: group["is_service_active_\(s.name)"].active
			}
			"create_container_\(s.name)": tool.create_container & {
				service: s
				$after:  group["stop_service_\(s.name)"]
			}
			"gen_service_\(s.name)": tool.gen_service & {
				service: s
				$after:  group["create_container_\(s.name)"]
			}
			"remove_container_\(s.name)": tool.remove_container & {
				name:   s.name
				$after: group["gen_service_\(s.name)"]
			}
			"write_service_\(s.name)": tool.write_service & {
				name:     s.name
				contents: group["gen_service_\(s.name)"].stdout
				$after:   group["remove_container_\(s.name)"]
			}
			"start_service_\(s.name)": tool.start_service & {
				name:   s.name
				$after: group.daemon_reload
			}
		}
		daemon_reload: {
			$after: [ for s in services {group["write_service_\(s.name)"]}]
			cmd: "systemctl daemon-reload"
		}
	}
}
command: create_services: task.create_services
