#!/bin/bash

set -e

export HOMEBREW_NO_INSTALLED_DEPENDENTS_CHECK=1
export HOMEBREW_NO_INSTALL_UPGRADE=1

START_GROUP=0

trap _end_group EXIT

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
    brew install --force python@3.$PY_MINOR
)

uninstall_python() (
    PY_MINOR=$1
    brew uninstall --ignore-dependencies --force python@3.$PY_MINOR
)

build_wheel() (
    PY_MINOR=$1
    ln -sf $(brew --prefix python@3.$PY_MINOR)/libexec/bin/python /usr/local/bin/python_for_build
    /usr/local/bin/python_for_build --version
    /usr/local/bin/python_for_build -m pip install cibuildwheel==2.16.2 setuptools git+https://github.com/bjia56/pybindgen.git@pypy-fixes --break-system-packages
    CIBW_BUILD="cp3$PY_MINOR-*" /usr/local/bin/python_for_build -m cibuildwheel --output-dir wheelhouse scrypted_arlo_go
)

test_wheel() (
    PY_MINOR=$1
    if [ "$CIBW_ARCHS" == "x86_64" ]
    then
        ln -sf $(brew --prefix python@3.$PY_MINOR)/libexec/bin/python /usr/local/bin/python_for_build
        /usr/local/bin/python_for_build -m pip install wheelhouse/*cp3$PY_MINOR*.whl --break-system-packages
        /usr/local/bin/python_for_build -c "import scrypted_arlo_go; print(scrypted_arlo_go)"
    fi
)

brew update

_start_group "Python 3.9"
install_python 9
build_wheel 9
test_wheel 9
uninstall_python 9

_start_group "Python 3.10"
install_python 10
build_wheel 10
test_wheel 10
uninstall_python 10

_start_group "Python 3.11"
install_python 11
build_wheel 11
test_wheel 11
uninstall_python 11

_start_group "Python 3.12"
install_python 12
build_wheel 12
test_wheel 12
uninstall_python 12
