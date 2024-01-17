#!/bin/bash
PY_VER=$1
if [ "$PY_VER" == "" ]
then
    PY_VER=39
fi
rm -rf wheelhouse
export CIBW_BUILD_VERBOSITY=3
export CIBW_BUILD="cp$PY_VER*_x86_64"
export CIBW_SKIP="cp36-* *-musllinux_x86_64"
export CIBW_ARCHS="native"
export CIBW_ENVIRONMENT="CGO_ENABLED=1 PATH=\$PATH:/usr/local/go/bin"
export CIBW_BEFORE_ALL_LINUX="curl -o go.tar.gz https://dl.google.com/go/go1.21.6.linux-amd64.tar.gz; tar -C /usr/local -xzf go.tar.gz; go install github.com/go-python/gopy@v0.4.8; go install golang.org/x/tools/cmd/goimports@latest"
python3 -m cibuildwheel --output-dir wheelhouse --platform linux ./scrypted_arlo_go