import hashlib
from test_terminal_workspace import real_terminal
from test_terminal_dashboard import Dashboard
from test_native_search import prepare


def test_terminal_global_search_reads_rebuilds_and_returns(real_terminal):
    t=real_terminal;path=prepare(t);original=hashlib.sha256(path.read_bytes()).hexdigest()
    d=Dashboard(t)
    try:
        d.wait('Real terminal');d.send('F');d.wait('Search saved conversations')
        d.send('résumé needle\t\t\x1b[C\x1b[C\x13')
        d.wait('old résumé needle',timeout=20)
        d.send('\r');d.wait('MATCHING MESSAGE');d.wait('old résumé needle')
        assert 'PRIVATE_SENTINEL' not in d.text
        d.send('N');d.wait('Later messages');d.send('O');d.wait('MATCHING MESSAGE')
        d.send('L');d.wait('Latest indexed messages');d.send('G');d.wait('Latest saved message sentinel');d.send('M');d.wait('MATCHING MESSAGE')
        d.resize(45,20);d.wait('MATCHING MESSAGE')
        d.send('\x1b');d.wait('old résumé needle')
        d.send('p');d.wait('codex');d.send('\x1b')
        assert original==hashlib.sha256(path.read_bytes()).hexdigest()
        path.write_text(path.read_text().replace('old r','new r'));edited=hashlib.sha256(path.read_bytes()).hexdigest()
        d.send('\r');d.wait('changed');d.send('\x1b');d.send('R');d.wait('new résumé needle',timeout=20)
        d.send('\r');d.wait('MATCHING MESSAGE');d.wait('new résumé needle')
        d.send('\x1b');d.send('\x1b');d.resize(120,35);d.wait('Real terminal');d.quit()
        assert edited==hashlib.sha256(path.read_bytes()).hexdigest()
    finally:d.close()
