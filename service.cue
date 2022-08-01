package sloop

#Volume: {
	name: string
}
#Network: {
	name: string
}
#File: {
	content:     string
	permissions: uint16
} | string

#PortBinding: {
	host:    uint16
	service: uint16
} | uint16

#Image: {
	name: string
	from: string
	files: [string]:  #File
	labels: [string]: string
	env: [string]:    string
	entrypoint: [...string]
	cmd: [...string]
	#mounts: [string]: string
}
#Service: {
	name:  string
	image: #Image
	volumes: [string]: #Volume
	ports: [...#PortBinding]
	networks: [...#Network]
	wants: [...#Dependency]
	requires: [...#Dependency]
	after: [...#Dependency]
	...
}
#UnitName:   =~"^(\\.service)|(\\.target)$"
#Dependency: #Service | #UnitName

$volume: [Name=_]: #Volume & {name: string | *Name}

$network: [Name=_]: #Network & {name: string | *Name}

$image: [Name=_]: #Image & {name: string | *Name}

$service: [Name=_]: S=#Service & {
	name: string | *Name
	_volumeCheck: {
		for k, v in S.volumes {
			"\(v.name).is_in_$volume": [ for k1, v1 in $volume if v1.name == v.name {v1}] & [v]
		}
	}
	_networkCheck: {
		for n in S.networks {
			"\(n.name).is_in_$network": [ for k, v in $network if v.name == n.name {v}] & [n]
		}
	}
	mountCheck: [
		for k, v in S.image.#mounts {
			"\(k):\(v).is_mounted": [ for k1, v1 in S.volumes if k1 == v {v}] & [v]
		},
	]

}

$volume: miniflux_db: {
}
$network: public: {
}
$image: miniflux: {
	from: "docker.io/miniflux"
}
$service: feeder: {
	image: $image.miniflux
	volumes: {
		"/etc/miniflux.db": $volume.miniflux_db
	}
	networks: [
		$network.public,
	]
	wants: [
		"network.target",
	]
}

#busybox_image: #Image & {
	from: "docker.io/busybox:latest"
	files: {
		"/hello.txt": {
			content: """
				I am a file
				"""
			permissions: 0x666
		}
	}
	labels: {
		mylabel: "value"
	}
	env: {
		FILE: "/hello.txt"
	}
	entrypoint: ["/bin/busybox"]
	cmd: ["sh", "-c", "cat $FILE"]
	#mounts: {
		data: "/volume"
	}
}

#busybox_service: #Service & {
	image: #busybox_image
	volumes: {
		"\(image.#mounts.data)": #Volume
	}
}

$image: busybox: #busybox_image
$volume: busybox: {}

$service: busybox: #busybox_service & {
	image: $image.busybox
	volumes: {
		"\(image.#mounts.data)": $volume.busybox
	}
}
