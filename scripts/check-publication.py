#!/usr/bin/env python3
"""Targeted public-snapshot regression guard; not a complete secret scanner."""
from pathlib import Path
import re
import subprocess

root = Path(__file__).resolve().parents[1]
errors = []
for name in ("connect_prod_db.bat", "database_metadata.json"):
    if (root / name).exists():
        errors.append(f"operational/stale export must not be published: {name}")

allowed_workflows = {"ci.yml", "branch-guard.yml", "publication.yml"}
for path in (root / ".github/workflows").glob("*"):
    if path.is_file() and path.name not in allowed_workflows:
        errors.append(f"unreviewed active workflow: {path.name}")

for name in (
    "internal/auth/service/auth_service_v2.go",
    "internal/auth/handler/auth_handler_v2.go",
):
    text = (root / name).read_text()
    if "reviewer-test-verification-id" in text or re.search(r'phone\s*==\s*"\+', text):
        errors.append(f"phone-specific authentication bypass: {name}")

paths = subprocess.check_output(["git", "ls-files", "-z"], cwd=root).decode().split("\0")
private_key = re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----")
for name in filter(None, paths):
    path = root / name
    if not path.is_file():
        continue
    text = path.read_bytes().decode("utf-8", errors="replace")
    if private_key.search(text):
        errors.append(f"literal private key: {name}")

if errors:
    raise SystemExit("\n".join(errors))
print("publication regression checks passed (not a full secret/history audit)")
