#!/bin/bash

START_GROUP=0

trap _end_group

_end_group() (
    if [ "$START_GROUP" == "1" ]
    then
        echo "::endgroup::"
        START_GROUP=0
    fi
)

_start_group() (
    _end_group
    echo "::group::$1"
    START_GROUP=1
)

install_python() (
    PY_MINOR=$1
    brew install python@3.$PY_MINOR
)

build_wheel() (
    PY_MINOR=$1
    ln -s /usr/local/opt/python@3.$PY_MINOR/bin/python3.$PY_MINOR  /usr/local/bin/python_for_build
    /usr/local/bin/python_for_build --version
    /usr/local/bin/python_for_build -m pip install cibuildwheel==2.11.2 pybindgen
    CIBW_BUILD="cp3$PY_MINOR-*" /usr/local/bin/python_for_build -m cibuildwheel --output-dir wheelhouse
)

if [ "$CIBW_ARCHS" == "arm64" ]
then
    _start_group "Python 3.7"
    install_python 7
    build_wheel 7
fi

_start_group "Python 3.8"
install_python 8
build_wheel 8

_start_group "Python 3.9"
install_python 9
build_wheel 9

_start_group "Python 3.10"
install_python 10
build_wheel 10

_start_group "Python 3.11"
install_python 11
build_wheel 11