"""Stage all built React entries into Go embeds; does not deploy the binary."""
from pathlib import Path
import shutil
root=Path(__file__).resolve().parents[1]
dist=root/'dist'
web=root.parent/'web'
for entry in ['index.html','terminal.html','sw.js']:
 if not (dist/entry).is_file(): raise SystemExit('Build the complete frontend before staging: missing '+entry)
shutil.copytree(dist/'assets',web/'static/react/assets',dirs_exist_ok=True)
for source,target in [('terminal.html',web/'static/terminal.html'),('index.html',web/'index.html'),('sw.js',web/'static/sw.js')]:
 temporary=target.with_name(target.name+'.new')
 shutil.copy2(dist/source,temporary)
 temporary.replace(target)
print('Staged React app, terminal and service worker for next Go build; live binary unchanged')
