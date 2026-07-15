from server.policy import add_rule, matches, pattern_for


def test_pattern_for_bash_takes_first_token():
    assert pattern_for("Bash", {"command": "pytest -x tests/"}) == \
        {"tool": "Bash", "prefix": "pytest"}
    assert pattern_for("Bash", {"command": ""}) == {"tool": "Bash", "prefix": ""}
    assert pattern_for("Edit", {"file_path": "a.py"}) == {"tool": "Edit"}


def test_matches_bash_prefix_only():
    pol = {"allow": [{"tool": "Bash", "prefix": "pytest"}]}
    assert matches(pol, "Bash", {"command": "pytest -q"})
    assert not matches(pol, "Bash", {"command": "rm -rf /"})
    # substring is not a token match
    assert not matches(pol, "Bash", {"command": "pytest-cov run"})
    assert not matches(pol, "Edit", {"file_path": "x"})


def test_matches_whole_tool():
    pol = {"allow": [{"tool": "Edit"}]}
    assert matches(pol, "Edit", {"file_path": "x"})
    assert not matches(pol, "Write", {})


def test_empty_prefix_never_matches():
    pol = {"allow": [{"tool": "Bash", "prefix": ""}]}
    assert not matches(pol, "Bash", {"command": "anything"})


def test_add_rule_dedupes():
    pol = add_rule({}, {"tool": "Bash", "prefix": "ls"})
    pol = add_rule(pol, {"tool": "Bash", "prefix": "ls"})
    assert pol == {"allow": [{"tool": "Bash", "prefix": "ls"}]}


def test_glob_rules():
    pol = {"allow": [{"tool": "Bash", "glob": "git commit *"}]}
    assert matches(pol, "Bash", {"command": "git commit -m 'x'"})
    assert not matches(pol, "Bash", {"command": "git push origin main"})
    assert not matches(pol, "Bash", {"command": "rm -rf / && git commit -m x"})


def test_glob_and_prefix_combined():
    pol = {"allow": [{"tool": "Bash", "prefix": "pytest"},
                     {"tool": "Bash", "glob": "npm run *"}]}
    assert matches(pol, "Bash", {"command": "pytest -q"})
    assert matches(pol, "Bash", {"command": "npm run build"})
    assert not matches(pol, "Bash", {"command": "npm install left-pad"})


def test_matches_handles_none_policy():
    assert not matches(None, "Bash", {"command": "ls"})
    assert not matches({}, "Bash", {"command": "ls"})
