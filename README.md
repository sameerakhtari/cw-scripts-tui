# cw-scripts-tui

Bubble Tea TUI wrappers for Cloudways helper scripts.

## What this is
- A tiny TUI binary (`cwbackup-tui`) that asks for:
  1) Cloudways email
  2) Cloudways API key
  3) Domains (any format: commas, spaces, newlines, with/without scheme)
- It runs your existing bash script:
  https://github.com/sameerakhtari/CW-Scripts/blob/main/domain-based-backup.sh

## One-liner for customer servers
> Works after you push this repo and publish a GitHub Release with binaries.

Latest main (auto-detect arch, falls back to non-TUI if needed):
```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sameerakhtari/cw-scripts-tui/refs/heads/main/bootstrap/domain-based-backup-tui.sh)
