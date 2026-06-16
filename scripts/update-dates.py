#!/usr/bin/env python3
"""Replace <!-- DATE:key --> markers in HTML with git log or file mtime dates."""

import re
import subprocess
import os
from datetime import datetime

SITE = os.path.join(os.path.dirname(os.path.dirname(__file__)), "site")

FILE_MAP = {
    "agents": "notes/agents.html",
    "architecture": "notes/architecture.html",
    "local-first": "notes/local-first.html",
    "edits": "edits/index.html",
}

def git_date(path):
    abs_path = os.path.join(SITE, path)
    try:
        result = subprocess.run(
            ["git", "log", "-1", "--format=%ad", "--date=format:%-d %b %Y", "--", abs_path],
            capture_output=True, text=True, cwd=SITE, timeout=10,
        )
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
    except Exception:
        pass
    return None

def mtime_date(path):
    abs_path = os.path.join(SITE, path)
    try:
        mtime = os.path.getmtime(abs_path)
        return datetime.fromtimestamp(mtime).strftime("%-d %b %Y")
    except Exception:
        return None

def get_date(path):
    return git_date(path) or mtime_date(path)

def process_file(html_path):
    abs_path = os.path.join(SITE, html_path)
    with open(abs_path) as f:
        content = f.read()

    changed = False
    def replacer(m):
        nonlocal changed
        key = m.group(1)
        path = FILE_MAP.get(key)
        if not path:
            return m.group(0)
        date = get_date(path)
        if not date:
            return m.group(0)
        changed = True
        return date

    new_content = re.sub(r"<!--\s*DATE:([\w-]+)\s*-->", replacer, content)

    def self_replacer(m):
        nonlocal changed
        date = get_date(html_path)
        if not date:
            return m.group(0)
        changed = True
        return date

    new_content = re.sub(r"<!--\s*DATE:self\s*-->", self_replacer, new_content)

    if changed:
        with open(abs_path, "w") as f:
            f.write(new_content)
        print(f"  updated {html_path}")
    else:
        print(f"  unchanged {html_path}")

def main():
    print("Updating dates...")
    for root, dirs, files in os.walk(SITE):
        for f in files:
            if f.endswith(".html"):
                rel = os.path.relpath(os.path.join(root, f), SITE)
                process_file(rel)
    print("Done.")

if __name__ == "__main__":
    main()
