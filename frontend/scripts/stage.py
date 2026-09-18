"""Stage all built React entries into Go embeds; does not deploy the binary."""
from pathlib import Path
import shutil
root=Path(__file__).resolve().parents[1]
dist=root/'dist'
web=root.parent/'web'
for entry in ['index.html','terminal.html','sw.js']:
 if not (dist/entry).is_file(): raise SystemExit('Build the complete frontend before staging: missing '+entry)
assets=web/'static/react/assets'
shutil.copytree(dist/'assets',assets,dirs_exist_ok=True)
# Bundle names are content hashes, so every build adds files and none replace
# the last. The whole directory is embedded in the binary: without pruning, each
# rebuild makes the binary and the repository permanently larger. A full build
# is the complete set, so anything it did not produce is dead.
built={item.name for item in (dist/'assets').iterdir()}
for stale in [item for item in assets.iterdir() if item.is_file() and item.name not in built]:
 stale.unlink()
for source,target in [('terminal.html',web/'static/terminal.html'),('index.html',web/'index.html'),('sw.js',web/'static/sw.js')]:
 temporary=target.with_name(target.name+'.new')
 shutil.copy2(dist/source,temporary)
 temporary.replace(target)
print('Staged React app, terminal and service worker for next Go build; live binary unchanged')
