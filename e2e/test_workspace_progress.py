"""Read target workspace progress through the terminal actions menu."""
from test_terminal_workspace import real_terminal
from test_terminal_dashboard import Dashboard
from test_multi_workspace import grouped
from test_session_restore import request


def test_terminal_reads_workspace_setup_progress(real_terminal):
    t=real_terminal;row=grouped(t)
    assert request(t,'DELETE',f"/sessions/{row['id']}")[0]==200
    d=Dashboard(t)
    try:
        d.wait('Real terminal');d.send('z');d.wait('Grouped review')
        d.send('/Grouped review\r');d.send('m');d.wait('Workspace setup progress')
        lines=[line.strip() for line in d.text.splitlines() if line.strip()]
        steps=lines.index('Workspace setup progress')-lines.index('Saved conversations')
        d.send('j'*steps+'\r')
        d.wait('Recorded workspace state: ready');d.wait('Second repository: ready')
        d.send('\x1b');d.quit()
    finally:d.close()
