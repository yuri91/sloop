.DEFAULT_GOAL := all
.PHONY: build/sloop

build:
	mkdir -p build

clean:
	rm -rf build

build/sloop: catatonit/catatonit | build
	go build -ldflags="-s -w" -o build/sloop

catatonit/catatonit:
	curl -JL -o catatonit/catatonit https://github.com/openSUSE/catatonit/releases/download/v0.1.7/catatonit.x86_64

all: build/sloop
