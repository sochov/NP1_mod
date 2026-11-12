"""Generate a source/build dependency inventory with no local paths or credentials."""
import json
import pathlib
root=pathlib.Path(__file__).resolve().parents[1]
lock=json.loads((root/'frontend/package-lock.json').read_text(encoding='utf8'))
components=[]
for name,value in lock['packages'].items():
    if not name or 'version' not in value:
        continue
    package=name.rsplit('node_modules/',1)[-1]
    components.append({'ecosystem':'npm','name':package,'version':value['version'],'license':value.get('license','see package'),'buildOnly':value.get('dev',False),'integrity':value.get('integrity')})
for line in (root/'backend/go.sum').read_text().splitlines():
    name,version,checksum=line.split()
    if not version.endswith('/go.mod'):
        components.append({'ecosystem':'go','name':name,'version':version,'checksum':checksum})
destination=root/'artifacts/ncc'
destination.mkdir(parents=True,exist_ok=True)
(destination/'dependency-inventory.json').write_bytes(json.dumps({'schemaVersion':1,'application':'NP1 Control Center','applicationVersion':lock['version'],'components':components},indent=2,ensure_ascii=False).encode('utf8'))
print('Dependency inventory:',len(components),'components')
