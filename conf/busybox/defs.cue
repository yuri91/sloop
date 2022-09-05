package defs

image: #Image & {
	from: "docker.io/busybox:latest"
	files: {
		"/hello.txt": {
			content: """
				I am a file
				"""
			permissions: 0o666
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

let _image = image
service: #Service & {
	image: _image
	volumes: {
		"\(image.#mounts.data)": #Volume
	}
}
