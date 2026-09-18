"""Stage the built React terminal into Go's embedded assets (no deployment)."""
from pathlib import Path
import shutil
root=Path(__file__).resolve().parents[1];target=root.parent/'web/static'
shutil.copytree(root/'dist/assets',target/'react/assets',dirs_exist_ok=True)
staged=target/'terminal.html.new';shutil.copy2(root/'dist/terminal.html',staged);staged.replace(target/'terminal.html')
print('Staged React terminal for next Go build; live binary is unchanged')
