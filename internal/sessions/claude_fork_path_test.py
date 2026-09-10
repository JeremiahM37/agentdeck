import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import uuid

spec = importlib.util.spec_from_file_location('fork_path', Path(__file__).with_name('claude_fork_path.py'))
fork_path = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fork_path)


class NativeForkPathTests(unittest.TestCase):
    def test_exact_profile_workspace_and_identity_without_writes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            profile = root / 'profile with spaces'
            workspace = root / "repo's workspace"
            cid = str(uuid.uuid4())
            folder = profile / 'projects' / 'custom-or-hashed-name'
            folder.mkdir(parents=True)
            transcript = folder / (cid + '.jsonl')
            body = (json.dumps({'sessionId': cid, 'cwd': str(workspace)}) + '\n').encode()
            transcript.write_bytes(body)
            before = transcript.stat()
            self.assertEqual(fork_path.resolve(profile, workspace, cid), str(transcript))
            self.assertEqual(transcript.read_bytes(), body)
            self.assertEqual(transcript.stat().st_mtime_ns, before.st_mtime_ns)
            for chosen_profile, chosen_workspace, chosen_cid in (
                (root / 'other-profile', workspace, cid),
                (profile, root / 'foreign-workspace', cid),
                (profile, workspace, str(uuid.uuid4())),
                (profile, workspace, '../' + cid),
            ):
                with self.assertRaises(ValueError):
                    fork_path.resolve(chosen_profile, chosen_workspace, chosen_cid)
            other = profile / 'projects' / 'second-project'
            other.mkdir()
            duplicate = other / transcript.name
            duplicate.write_bytes(body)
            with self.assertRaisesRegex(ValueError, 'ambiguous'):
                fork_path.resolve(profile, workspace, cid)
            duplicate.unlink()
            duplicate.symlink_to(root / 'outside.jsonl')
            (root / 'outside.jsonl').write_bytes(body)
            self.assertEqual(fork_path.resolve(profile, workspace, cid), str(transcript))
            transcript.write_text(json.dumps({'sessionId': str(uuid.uuid4()), 'cwd': str(workspace)}) + '\n')
            with self.assertRaises(ValueError):
                fork_path.resolve(profile, workspace, cid)


if __name__ == '__main__':
    unittest.main()
