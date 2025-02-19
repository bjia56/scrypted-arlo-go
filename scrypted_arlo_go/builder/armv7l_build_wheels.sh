#!/bin/bash

PYTHON3_VERSION=$1

set -e

build_wheel() (
    PY_VER=$1
    mkdir build$PY_VER
    cd build$PY_VER
    pip3 wheel ..
)

test_wheel() (
    PY_VER=$1
    cd build$PY_VER
    pip3 install *armv7l.whl
    python3 -c "import scrypted_arlo_go; print(scrypted_arlo_go)"
)

repair_wheel() (
    PY_VER=$1
    cd build$PY_VER
    auditwheel repair *armv7l.whl
)

select_python 3.$PYTHON3_VERSION
build_wheel 3.$PYTHON3_VERSION
repair_wheel 3.$PYTHON3_VERSION
test_wheel 3.$PYTHON3_VERSION