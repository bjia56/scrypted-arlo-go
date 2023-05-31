#!/bin/bash

set -e

build_wheel() (
    PY_VER=$1
    mkdir build$PY_VER
    cd build$PY_VER
    pip$PY_VER wheel ..
)

test_wheel() (
    PY_VER=$1
    cd build$PY_VER
    pip$PY_VER install *armv7l.whl
    python$PY_VER -c "import scrypted_arlo_go; print(scrypted_arlo_go)"
)

repair_wheel() (
    PY_VER=$1
    cd build$PY_VER
    auditwheel repair *armv7l.whl
)

build_wheel 3.7
build_wheel 3.8
build_wheel 3.9
build_wheel 3.10
build_wheel 3.11

repair_wheel 3.7
repair_wheel 3.8
repair_wheel 3.9
repair_wheel 3.10
repair_wheel 3.11

test_wheel 3.7
test_wheel 3.8
test_wheel 3.9
test_wheel 3.10
test_wheel 3.11