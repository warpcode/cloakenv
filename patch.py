import re

with open("internal/engine/resolver_test.go", "r") as f:
    content = f.read()

content = content.replace('''			wantQuery: `"\"api\"" in tags`,''', '''			wantQuery: `"\\\"api\\\"" in tags`,''')

with open("internal/engine/resolver_test.go", "w") as f:
    f.write(content)
