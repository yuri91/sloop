package defs

image: #Image & {
	from: "docker.io/miniflux/miniflux"
}
let _image = image
service: #Service & {
	image: _image
	volumes: {
		"/etc/miniflux.db": #Volume
	}
	wants: [
		"network.target",
	]
}
