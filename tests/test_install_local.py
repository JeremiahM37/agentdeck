"""Installer checks that never modify the host installation or user config."""

import os
from pathlib import Path
import shutil
import subprocess


ROOT = Path(__file__).resolve().parents[1]
INSTALLER = ROOT / "tools" / "install-local.sh"


def run_installer(tmp_path, *args, extra_path=()):
    home = tmp_path / "home"
    home.mkdir()
    path = [str(p) for p in extra_path]
    path.extend(p for p in (shutil.which("git"), shutil.which("tmux"), "/usr/bin", "/bin") if p)
    env = {**os.environ, "HOME": str(home), "PATH": os.pathsep.join(path)}
    return subprocess.run(
        ["bash", str(INSTALLER), *map(str, args)],
        env=env,
        text=True,
        capture_output=True,
        check=False,
    ), home


def fake_binary(path):
    path.write_text("#!/bin/sh\n[ \"$1\" = version ] && echo local-test-1\n")
    path.chmod(0o755)


def test_binary_install_does_not_clobber_remote_client(tmp_path):
    binary = tmp_path / "agentdeck"
    fake_binary(binary)
    prefix = tmp_path / "bin"
    remote = prefix / "agentdeck"
    prefix.mkdir()
    remote.write_text("remote launcher\n")

    result, home = run_installer(tmp_path, "--binary", binary, "--prefix", prefix)

    assert result.returncode == 0, result.stderr
    assert (prefix / "agentdeck-local").is_file()
    assert remote.read_text() == "remote launcher\n"
    assert "agentdeck-local local" in result.stdout
    assert home.exists()


def test_source_build_works_when_called_outside_checkout(tmp_path):
    fake_go = tmp_path / "go"
    fake_go.write_text(
        "#!/bin/sh\n"
        "while [ $# -gt 0 ]; do [ \"$1\" = -o ] && { out=$2; shift 2; continue; }; shift; done\n"
        "printf '#!/bin/sh\\n[ \"$1\" = version ] && echo source-test-1\\n' > \"$out\"\n"
        "chmod +x \"$out\"\n"
    )
    fake_go.chmod(0o755)
    source = ROOT
    prefix = tmp_path / "bin"

    result, _ = run_installer(tmp_path, "--source", source, "--prefix", prefix, extra_path=(tmp_path,))

    assert result.returncode == 0, result.stderr
    installed = prefix / "agentdeck-local"
    assert installed.is_file() and os.access(installed, os.X_OK)
    assert subprocess.run([installed, "version"], text=True, capture_output=True, check=True).stdout.strip() == "source-test-1"


def test_windows_shell_is_directed_to_wsl(tmp_path):
    fake_uname = tmp_path / "uname"
    fake_uname.write_text("#!/bin/sh\nprintf 'MINGW64_NT\\n'\n")
    fake_uname.chmod(0o755)

    # Exercise the Windows guard without requiring a Windows machine.
    result = subprocess.run(
        ["bash", str(INSTALLER), "--binary", tmp_path / "missing"],
        env={**os.environ, "HOME": str(tmp_path / "home"), "PATH": os.pathsep.join((str(tmp_path), "/usr/bin", "/bin"))},
        text=True,
        capture_output=True,
        check=False,
    )
    assert result.returncode == 1
    assert "WSL2" in result.stderr
