import requests

release = requests.get("https://api.github.com/repos/bjia56/scrypted-arlo-go/releases/latest").json()
for asset in release["assets"]:
    url = asset["browser_download_url"]
    filename = url.split("/")[-1]
    print(f'   <a href="{url}">')
    print(f'    {filename}')
    print(f'   </a><br/>')