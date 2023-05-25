#!/bin/bash

build_wheel() (
    PY_VER=$1
    mkdir build$PY_VER
    cd build$PY_VER
    pip$PY_VER wheel ..
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
