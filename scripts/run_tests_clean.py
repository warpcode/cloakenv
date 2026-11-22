#!/usr/bin/env python3
import os
env = dict(os.environ)
if "MISE_SHELL" in env:
    del env["MISE_SHELL"]
go_path = "/home/jase/.local/share/mise/installs/go/1.27.3/bin/go"
os.execve(go_path, ["go", "test", "-race", "./internal/runner", "./internal/cmd"], env)
