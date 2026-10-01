#!/bin/bash
cat << 'REPLACE' > patch.diff
--- internal/provider/keepass.go
+++ internal/provider/keepass.go
@@ -387,14 +387,7 @@
				s = s[idx+1:]
			}

-			// Trim leading and trailing spaces without allocating
-			start := 0
-			for start < len(tag) && (tag[start] == ' ' || tag[start] == '\t' || tag[start] == '\n' || tag[start] == '\r' || tag[start] == '\f' || tag[start] == '\v') {
-				start++
-			}
-			end := len(tag)
-			for end > start && (tag[end-1] == ' ' || tag[end-1] == '\t' || tag[end-1] == '\n' || tag[end-1] == '\r' || tag[end-1] == '\f' || tag[end-1] == '\v') {
-				end--
-			}
-			tag = tag[start:end]
+			tag = strings.TrimSpace(tag)

			if tag == "" {
REPLACE
patch internal/provider/keepass.go patch.diff
