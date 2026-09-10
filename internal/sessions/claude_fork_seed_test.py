import importlib.util
import json
from pathlib import Path
import re
import tempfile
import unittest
from unittest.mock import patch
import uuid

spec=importlib.util.spec_from_file_location('seed',Path(__file__).with_name('claude_fork_seed.py'))
seed=importlib.util.module_from_spec(spec);spec.loader.exec_module(seed)


class ForkSnapshotTests(unittest.TestCase):
    def test_snapshot_preserves_transcript_and_nested_assets_without_publishing(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);profile=root/'profile';source=root/'parent';dest=root/'child'
            cid=str(uuid.uuid4());folder=profile/'projects'/re.sub(r'[^a-zA-Z0-9]','-',str(source));folder.mkdir(parents=True)
            transcript=folder/(cid+'.jsonl');body=(json.dumps({'sessionId':cid,'cwd':str(source),'type':'user','message':{'content':'history'}})+'\n').encode();transcript.write_bytes(body)
            sidecar=folder/cid/'subagents';sidecar.mkdir(parents=True);(sidecar/'worker.jsonl').write_bytes(b'worker context\n')
            destination=profile/'projects'/re.sub(r'[^a-zA-Z0-9]','-',str(dest));destination.mkdir()
            existing=destination/(cid+'.jsonl');existing.write_bytes(b'existing destination history')
            stage,manifest=seed.snapshot(profile,source,dest,cid)
            self.assertEqual((stage/transcript.name).read_bytes(),body)
            self.assertEqual((stage/cid/'subagents/worker.jsonl').read_bytes(),b'worker context\n')
            self.assertEqual(transcript.read_bytes(),body)
            self.assertEqual(existing.read_bytes(),b'existing destination history')
            self.assertEqual(len(manifest['files']),2)
            self.assertEqual((stage/transcript.name).stat().st_mode & 0o777,0o600)
            original_copy=seed.copy_file
            def changing_copy(source_path,destination_path,budget):
                result=original_copy(source_path,destination_path,budget)
                if source_path.name=='worker.jsonl':transcript.write_bytes(body+body)
                return result
            before=set(destination.iterdir())
            with patch.object(seed,'copy_file',side_effect=changing_copy):
                with self.assertRaisesRegex(ValueError,'changed'):seed.snapshot(profile,source,dest,cid)
            self.assertEqual(set(destination.iterdir()),before)
            transcript.write_bytes(body)
            with patch.object(seed,'MAX_TOTAL',1):
                with self.assertRaisesRegex(ValueError,'limit'):seed.snapshot(profile,source,dest,cid)
            self.assertEqual(set(destination.iterdir()),before)
            for invalid in ('partial','foreign','symlink'):
                transcript.write_bytes(body)
                if invalid=='partial':transcript.write_bytes(body+b'{')
                if invalid=='foreign':transcript.write_text(json.dumps({'sessionId':cid,'cwd':'/elsewhere'})+'\n')
                if invalid=='symlink':(sidecar/'link').symlink_to(transcript)
                before=set(destination.iterdir())
                with self.assertRaises((ValueError,OSError)):seed.snapshot(profile,source,dest,cid)
                self.assertEqual(set(destination.iterdir()),before)
                self.assertEqual(existing.read_bytes(),b'existing destination history')

if __name__=='__main__':unittest.main()
