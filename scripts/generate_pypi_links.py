#!/usr/bin/env python3
import os
import copy
import re
import requests
import pathlib
import sys
from urllib.parse import urlparse

from bs4 import BeautifulSoup


INDEX_FILE = "index.html"
CWD = pathlib.Path(__file__).parent.resolve()
PYPI_DIR = os.path.join(pathlib.Path(CWD).parent.resolve(), "pypi")


def normalize(name):
    """ From PEP503 : https://www.python.org/dev/peps/pep-0503/ """
    return re.sub(r"[-_.]+", "-", name).lower()


def package_exists(soup, package_name):
    package_ref = package_name + "/"
    for anchor in soup.find_all('a'):
        if anchor['href'] == package_ref:
            return True
    return False


def update(pkg_name, link):
    norm_pkg_name = normalize(pkg_name)

    # Change the package page
    index_file = os.path.join(PYPI_DIR, norm_pkg_name, INDEX_FILE)
    with open(index_file) as html_file:
        soup = BeautifulSoup(html_file, "html.parser")

    anchors = soup.find_all('a')
    for anchor in anchors:
        if anchor['href'] == link:
            return

    # Create a new anchor element for our new version
    last_anchor = anchors[-1]  # Copy the last anchor element
    new_anchor = copy.copy(last_anchor)
    new_anchor['href'] = link
    url = urlparse(link)
    filename = url.path.split('/')[-1]
    new_anchor.contents[0].replace_with(filename)

    # Add it to our index
    br = soup.new_tag("br")
    last_anchor.insert_after(br)
    br.insert_after(new_anchor)

    # Save it
    with open(index_file, 'wb') as index:
        index.write(soup.prettify("utf-8"))


def main():
    api = "https://api.github.com/repos/bjia56/scrypted-arlo-go/releases/latest"
    filter = None

    if len(sys.argv) > 1:
        api = f"https://api.github.com/repos/bjia56/scrypted-arlo-go/releases/tags/{sys.argv[1]}"

    release = requests.get(api).json()
    for asset in release["assets"]:
        url = asset["browser_download_url"]
        update("scrypted-arlo-go", url)


if __name__ == "__main__":
    main()
