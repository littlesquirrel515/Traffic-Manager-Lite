#!/bin/sh
# Read-only version acquisition on the Docker HOST, never inside the manager.
# Usage: sh scripts/core-version-evidence.sh hysteria2 hy-instance http://hy-instance:9999 /host/hysteria.yaml
set -eu
core=${1:?core required}
container=${2:?container required}
endpoint=${3:?API endpoint required}
config_file=${4:?host config path required}
case "$core" in
 xray) exec sh "$(dirname "$0")/xray-version-evidence.sh" "$container" "$endpoint" "$config_file";;
 hysteria2) binaries="/usr/local/bin/hysteria /usr/bin/hysteria /hysteria hysteria";;
 singbox) binaries="/usr/local/bin/sing-box /usr/bin/sing-box /sing-box sing-box";;
 v2fly) binaries="/usr/local/bin/v2ray /usr/bin/v2ray /v2ray v2ray";;
 *) echo "Unknown core" >&2; exit 1;;
esac
case "$container" in -*|*[!a-zA-Z0-9_.-]*) echo 'Invalid container name' >&2; exit 1;; esac
test -f "$config_file"
output=''
for binary in $binaries; do
 if output=$(docker exec "$container" "$binary" version 2>/dev/null); then break; fi
done
test -n "$output"
# Python serializes JSON safely; no credentials or full config are written.
python3 - "$core" "$container" "$endpoint" "$config_file" "$output" <<'PY'
import datetime, hashlib, json, pathlib, re, subprocess, sys
core, container, endpoint, config, output = sys.argv[1:]
from urllib.parse import urlparse
patterns={'hysteria2':r'(?m)^Version:\s+v?[0-9]+\.[0-9]+\.[0-9]+', 'singbox':r'^sing-box version [0-9]+\.[0-9]+\.[0-9]+', 'v2fly':r'^V2Ray [0-9]+\.[0-9]+\.[0-9]+'}
if not re.search(patterns[core], output):
    raise SystemExit('Unexpected fixed-command version output')
if len(output)>4096:
    raise SystemExit('Version output too large')
path = pathlib.Path(config)
inspection=json.loads(subprocess.check_output(['docker','inspect',container]))[0]
aliases={inspection['Name'].lstrip('/')}
for network in inspection['NetworkSettings']['Networks'].values():
    aliases.update(network.get('Aliases') or [])
    aliases.add(network.get('IPAddress'))
host=urlparse(endpoint).hostname if '://' in endpoint else endpoint.rsplit(':',1)[0].strip('[]')
if host not in aliases:
    raise SystemExit('Endpoint host does not match this container network identity')
container_paths=[]
for mount in inspection.get('Mounts',[]):
    source=pathlib.Path(mount['Source']).resolve()
    try:
        relative=path.resolve().relative_to(source)
    except ValueError:
        continue
    container_paths.append(mount['Destination'].rstrip('/')+('/'+relative.as_posix() if str(relative)!='.' else ''))
arguments=inspection.get('Args',[])
loaded=[]
for index,arg in enumerate(arguments):
    if arg in ('-config','--config','-c') and index+1<len(arguments):
        loaded.append(arguments[index+1])
    elif arg.startswith(('-config=','--config=','-c=')):
        loaded.append(arg.split('=',1)[1])
if not set(loaded).intersection(container_paths):
    raise SystemExit('Cannot verify loaded config from container arguments/mounts; no evidence written')
evidence = dict(core_type=core,api_endpoint=endpoint, method='docker_exec_core_version', output=output,
    observed_at=datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='seconds'),
    config_sha256=hashlib.sha256(path.read_bytes()).hexdigest())
target=pathlib.Path(str(path)+'.version.json')
temporary=pathlib.Path(str(target)+'.tmp')
temporary.write_text(json.dumps(evidence, ensure_ascii=False)+'\n')
temporary.replace(target)
print('Version evidence:', target)
PY
