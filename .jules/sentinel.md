# Sentinel's Journal

## 2026-11-04 - Enforce User-Only Permissions on Pre-Existing Cache Directories
**Vulnerability:** Local cache directory (`~/.cache/cloakenv`) was created with `os.MkdirAll(c.cacheDir, 0700)`, but `os.MkdirAll` does not alter permissions if the directory already exists. If the directory pre-existed with permissive permissions (e.g., created by another tool or umask with 0755/0777), sensitive encrypted cache files and keys could be exposed to other local users.
**Learning:** `os.MkdirAll` is insufficient for ensuring restrictive directory permissions on pre-existing paths. Defense in depth requires explicitly invoking `os.Chmod` on sensitive data directories.
**Prevention:** Always follow `os.MkdirAll` with an explicit `os.Chmod(dir, 0700)` on directories containing credentials or secret cache files to guarantee restrictive permissions regardless of prior directory state or umask.
