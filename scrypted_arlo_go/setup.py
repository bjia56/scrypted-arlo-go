import json
import os
import shutil
import subprocess
import sys
try:
    from distutils.core import Extension
except:
    from setuptools import Extension

import setuptools
from setuptools.command.build_ext import build_ext


PACKAGE_PATH="scrypted_arlo_go"
PACKAGE_NAME=PACKAGE_PATH.split("/")[-1]

if sys.platform == 'darwin':
    # PYTHON_BINARY_PATH is setting explicitly for 310 and 311, see build_wheel.yml
    # on macos PYTHON_BINARY_PATH must be python bin installed from python.org
    PYTHON_BINARY = os.getenv("PYTHON_BINARY_PATH", sys.executable)
    if PYTHON_BINARY == sys.executable:
        subprocess.check_call([sys.executable, '-m', 'pip', 'install', 'git+https://github.com/bjia56/pybindgen.git@pypy-fixes'])
else:
    # linux & windows
    PYTHON_BINARY = sys.executable
    subprocess.check_call([sys.executable, '-m', 'pip', 'install', 'git+https://github.com/bjia56/pybindgen.git@pypy-fixes'])

def _generate_path_with_gopath() -> str:
    go_path = subprocess.check_output(["go", "env", "GOPATH"]).decode("utf-8").strip()
    path_val = f'{os.getenv("PATH")}:{go_path}/bin'
    return path_val


class CustomBuildExt(build_ext):
    def build_extension(self, ext: Extension):
        bin_path = _generate_path_with_gopath()
        go_env = json.loads(subprocess.check_output(["go", "env", "-json"]).decode("utf-8").strip())

        destination = os.path.dirname(os.path.abspath(self.get_ext_fullpath(ext.name))) + f"/{PACKAGE_NAME}"
        if os.path.isdir(destination):
            # clean up destination in case it has existing build artifacts
            shutil.rmtree(destination)

        env = {
            "PATH": bin_path,
            **go_env,
            "CGO_LDFLAGS_ALLOW": ".*",
            "GOWORK": "off",
        }

        # https://stackoverflow.com/a/64706392
        if sys.platform == "win32":
            env["SYSTEMROOT"] = os.environ.get("SYSTEMROOT", "")

        if sys.platform == "darwin":
            min_ver = os.environ.get("MACOSX_DEPLOYMENT_TARGET", "")
            env["MACOSX_DEPLOYMENT_TARGET"] = min_ver
            env["CGO_LDFLAGS"] = "-mmacosx-version-min=" + min_ver
            env["CGO_CFLAGS"] = "-mmacosx-version-min=" + min_ver

        if sys.platform == "win32":
            import sysconfig
            include_path = sysconfig.get_paths()['include']
            lib_path = sysconfig.get_config_var("LIBDIR")
            lib = sysconfig.get_config_var("LIBRARY").replace(".dll", ".lib")
            env["CGO_CFLAGS"] = f"-I{include_path}"
            env["CGO_LDFLAGS"] = f"-L{lib_path} -l:{lib}"
            env["GOPY_INCLUDE"] = include_path
            env["GOPY_LIBDIR"] = lib_path
            env["GOPY_PYLIB"] = f":{lib}"

        subprocess.check_call(["go", "generate"], env=env)
        subprocess.check_call(
            [
                "gopy",
                "build",
                "-no-make",
                "-dynamic-link=True",
                "-symbols=False",
                "-output",
                destination,
                "-vm",
                PYTHON_BINARY,
                *ext.sources,
            ],
            env=env,
        )

        # dirty hack to avoid "from pkg import pkg", remove if needed
        with open(f"{destination}/__init__.py", "w") as f:
            f.write(f"from .{PACKAGE_NAME} import *")


setuptools.setup(
    name=PACKAGE_NAME,
    version="0.7.11",
    author="Brett Jia",
    author_email="dev.bjia56@gmail.com",
    description="Go extensions for @scrypted/arlo",
    url="https://github.com/bjia56/scrypted-arlo-go",
    classifiers=[
        "Programming Language :: Python :: 3",
        "License :: OSI Approved :: MIT License",
        "Operating System :: OS Independent",
    ],
    include_package_data=True,
    cmdclass={
        "build_ext": CustomBuildExt,
    },
    ext_modules=[
        Extension(PACKAGE_NAME, [PACKAGE_PATH],)
    ],
)
