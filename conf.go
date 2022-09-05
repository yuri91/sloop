package main

type Volume struct {
	Name string
}
type Network struct {
	Name string
}
type File struct {
	Content string
	Permissions uint16
}
type PortBinding struct {
	Host uint16
	Service uint16
}
type Image struct {
	Name string
	From string
	Files map[string]File `json:"$files"`
	labels map[string]string
	env map[string]string
	entrypoint []string
	cmd []string
}
type Service struct {
	Name  string
	Image string `json:"$image"`
	Volumes []string `json:"$volumes"`
	Ports []PortBinding `json:"$ports"`
	Networks []string `json:"$networks"`
	Wants []string `json:"$wants"`
	Requires []string `json:"$requires"`
	After []string `json:"$after"`
}
type Config struct {
	Volumes map[string]Volume `json:"$volume"`
	Networks map[string]Network `json:"$network"`
	Images map[string]Image `json:"$images"`
	Services map[string]Service `json:"$services"`
}

