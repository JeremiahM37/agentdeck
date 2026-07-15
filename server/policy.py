"""Per-project approval policy: 'always allow' rules evaluated server-side
before a human is bothered. Rules live in projects.policy_json:
  {"allow": [{"tool": "Bash", "prefix": "pytest"}, {"tool": "Edit"}]}
Bash rules match on the command's first token; other tools match wholesale.
"""


def pattern_for(tool_name: str, tool_input: dict) -> dict:
    if tool_name == "Bash":
        cmd = (tool_input.get("command") or "").strip()
        return {"tool": "Bash", "prefix": cmd.split()[0] if cmd else ""}
    return {"tool": tool_name}


def matches(policy: dict, tool_name: str, tool_input: dict) -> bool:
    from fnmatch import fnmatch
    for rule in (policy or {}).get("allow", []):
        if rule.get("tool") != tool_name:
            continue
        if tool_name == "Bash":
            cmd = (tool_input.get("command") or "").strip()
            prefix = rule.get("prefix", "")
            if prefix and cmd.split()[:1] == [prefix]:
                return True
            glob = rule.get("glob", "")
            if glob and fnmatch(cmd, glob):
                return True
        else:
            return True
    return False


def add_rule(policy: dict, rule: dict) -> dict:
    policy = policy or {}
    allow = policy.setdefault("allow", [])
    if rule not in allow:
        allow.append(rule)
    return policy
