pkgname=sloop
pkgver=v0.1.0.r0.g1fbb486
pkgrel=1
pkgdesc='Sloop generates systemd units for running docker containers, without docker'
arch=('x86_64')
url="https://github.com/yuri91/sloop"
license=('MIT')
makedepends=('go')
source=("git+https://github.com/yuri91/sloop")
sha256sums=("SKIP")

pkgver() {
  cd sloop
  git describe --long | sed 's/\([^-]*-g\)/r\1/;s/-/./g'
}

prepare(){
  cd sloop
  mkdir -p build/
}

build() {
  cd sloop
  export CGO_CPPFLAGS="${CPPFLAGS}"
  export CGO_CFLAGS="${CFLAGS}"
  export CGO_CXXFLAGS="${CXXFLAGS}"
  export CGO_LDFLAGS="${LDFLAGS}"
  export GOFLAGS="-buildmode=pie -trimpath -ldflags=-linkmode=external -mod=readonly -modcacherw"
  go build -o build .
}

package() {
  cd sloop
  install -Dm755 build/sloop "$pkgdir"/usr/bin/sloop
}
