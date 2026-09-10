"""Real browser/API acceptance against an SSH target on loopback.

The SSH target is still a real transport.  Its HOME, tmux socket, repository,
and known-hosts file are all fixture-owned, so this test cannot attach to or
clean up an operator's normal tmux sessions.
"""

import hashlib
import json
from pathlib import Path
import shlex
import subprocess
import uuid

import pytest
from playwright.sync_api import expect

from conftest import PHONE, DESKTOP
from test_terminal_workspace import real_terminal


SSH_KEY = "/home/admin/.ssh/id_ed25519"


def _ssh_args(known_hosts: Path):
    return [
        "ssh", "-F", "/dev/null", "-i", SSH_KEY,
        "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new",
        "-o", f"UserKnownHostsFile={known_hosts}",
        "root@127.0.0.1",
    ]


def _ssh(known_hosts: Path, command: str):
    return subprocess.run(_ssh_args(known_hosts) + [command], check=True,
                          capture_output=True, text=True, timeout=20)


def _open_remote_terminal(page, t):
    page.goto(f"{t['url']}/terminal/session/{t['id']}")
    expect(page.locator("#connection")).to_have_text("Connected", timeout=20000)
    # The isolated SSH fixture deliberately logs in as root, whose bash prompt
    # ends in '#'; the normal local fixture uses '$'.
    expect(page.locator("#agent-terminal .xterm-screen")).to_contain_text("#", timeout=10000)
    page.locator("#agent-terminal").click()


@pytest.fixture()
def remote_terminal(real_terminal, tmp_path):
    """Add an actual SSH target to the already isolated real server."""
    t = real_terminal
    root = tmp_path / "remote-repo"
    home = tmp_path / "remote-home"
    tmux_dir = tmp_path / "remote-tmux"
    known_hosts = tmp_path / "known_hosts"
    root.mkdir()
    home.mkdir(mode=0o700)
    tmux_dir.mkdir(mode=0o700)
    # All repository setup goes through SSH, as it would for another host.
    _ssh(known_hosts, "mkdir -p %s %s %s && git init -q -b main %s" % tuple(
        shlex.quote(str(p)) for p in (root, home, tmux_dir, root)))
    name = "remote-accept-%s" % uuid.uuid4().hex[:10]
    env = "env HOME=%s TMUX_TMPDIR=%s" % (shlex.quote(str(home)), shlex.quote(str(tmux_dir)))
    _ssh(known_hosts, "%s tmux -f /dev/null new-session -d -s %s -c %s bash --norc" % (
        env, shlex.quote(name), shlex.quote(str(root))))
    prefix = "%s sh -c" % env
    target = t["api"]("/targets", {
        "name": "loopback-ssh-acceptance",
        "kind": "ssh", "host": "127.0.0.1", "port": 22, "user": "root",
        "key_path": SSH_KEY, "command_prefix": prefix,
    })
    session = t["api"]("/sessions/adopt", {
        "target_id": target["id"], "tmux_session": name,
        "workdir": str(root), "name": "Remote acceptance", "agent": "claude",
    })
    result = dict(t, remote_root=root, remote_home=home, remote_tmux=tmux_dir,
                  known_hosts=known_hosts, remote_target=target,
                  remote_session=session, id=session["id"])
    try:
        yield result
    finally:
        # The exact fixture-owned session is the only process this cleanup can kill.
        _ssh(known_hosts, "%s tmux -f /dev/null kill-session -t =%s || true" %
             (env, shlex.quote(name)))


def _pdf_bytes():
    objects = [
        b"<< /Type /Catalog /Pages 2 0 R >>",
        b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
        b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
    ]
    stream = b"BT /F1 24 Tf 30 100 Td (Remote PDF context proof) Tj ET"
    objects += [
        b"<< /Length " + str(len(stream)).encode() + b" >>\nstream\n" + stream + b"\nendstream",
        b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    ]
    pdf = b"%PDF-1.4\n"
    offsets = [0]
    for i, obj in enumerate(objects, 1):
        offsets.append(len(pdf))
        pdf += f"{i} 0 obj\n".encode() + obj + b"\nendobj\n"
    xref = len(pdf)
    pdf += b"xref\n0 6\n0000000000 65535 f \n"
    pdf += b"".join(f"{v:010d} 00000 n \n".encode() for v in offsets[1:])
    pdf += f"trailer\n<< /Root 1 0 R /Size 6 >>\nstartxref\n{xref}\n%%EOF\n".encode()
    return pdf


@pytest.mark.parametrize("page", [PHONE, DESKTOP], indirect=True)
def test_remote_pdf_context_is_uploaded_and_usable(page, remote_terminal):
    """Drop a PDF in the browser, send its remote path through the API, and
    use the real file drawer to preview/download the resulting remote file."""
    t = remote_terminal
    pdf = _pdf_bytes()
    digest = hashlib.sha256(pdf).hexdigest()
    _open_remote_terminal(page, {"url": t["url"], "id": t["id"]})

    page.keyboard.type("sha256sum ")
    with page.expect_response(lambda r: r.request.method == "POST" and r.url.endswith("/attachments")) as response:
        page.locator("#agent-terminal").evaluate(
            """(el, p) => { const d = new DataTransfer();
            d.items.add(new File([new Uint8Array(p.bytes)], p.name, {type:'application/pdf'}));
            el.dispatchEvent(new DragEvent('drop', {bubbles:true, cancelable:true, dataTransfer:d})); }""",
            {"bytes": list(pdf), "name": "context proof.pdf"})
    attachment = response.value.json()
    remote_path = attachment["path"]
    assert remote_path.startswith(str(t["remote_root"] / ".agentdeck" / "context") + "/")
    page.keyboard.press("Enter")
    expect(page.locator("#agent-terminal .xterm-screen")).to_contain_text(digest, timeout=10000)

    # This is the operator's context/send action: the browser calls the real
    # session API with the exact path returned by the remote upload.
    received = t["remote_root"] / "received-from-context.pdf"
    send = page.request.post(
        f"{t['url']}/api/sessions/{t['id']}/send",
        data=json.dumps({"text": "cat -- %s > %s" %
                         (shlex.quote(remote_path), shlex.quote(str(received)))}),
        headers={"Content-Type": "application/json"},
    )
    assert send.ok, send.text()
    file_url = f"{t['url']}/api/term/session/{t['id']}/file"
    for _ in range(50):
        got = page.request.get(file_url, params={"path": received.name})
        if got.ok and got.body() == pdf:
            break
        page.wait_for_timeout(100)
    else:
        pytest.fail("the remote context path was not readable through the file API")

    # Preview and download exercise the same browser controls users use for a
    # shared artifact, while the bytes still come from the SSH target.
    page.locator("#files").click()
    page.get_by_role("button", name=received.name, exact=True).click()
    expect(page.locator("#pdf-page")).to_have_text("Page 1 of 1", timeout=20000)
    with page.expect_download() as download:
        page.locator("#preview-download").click()
    assert Path(download.value.path()).read_bytes() == pdf
