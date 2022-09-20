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
	Labels map[string]string
	Env map[string]string
	Entrypoint []string
	Cmd []string
}
type VolumeMapping struct {
	Name string
	Dest string
}
type Service struct {
	Name  string
	Image string `json:"$image"`
	Volumes []VolumeMapping `json:"$volumes"`
	Ports []PortBinding `json:"$ports"`
	Networks []string `json:"$networks"`
	Wants []string `json:"$wants"`
	Requires []string `json:"$requires"`
	After []string `json:"$after"`
}
type Config struct {
	Volumes map[string]Volume `json:"$volumes"`
	Networks map[string]Network `json:"$networks"`
	Images map[string]Image `json:"$images"`
	Services map[string]Service `json:"$services"`
}

