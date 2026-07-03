# cloakenv

Pluggable secret orchestrator and dynamic runtime environment injector. It wraps application binaries, resolves secret URIs, and injects them strictly into temporary execution memory.

## Prerequisites

- [Go](https://go.dev/) 1.24 or higher.
- A native keyring store (macOS Keychain, Linux D-Bus, Windows Credential Manager).

## Project Structure

```
├── cmd/
│   └── cloakenv/
│       └── main.go         # CLI Entrypoint
├── examples/
│   ├── providers/          # Example databases for providers (JSON, YAML, KeePass)
│   └── config.yaml         # Fully documented example config file
├── internal/
│   ├── config/             # YAML Config parser
│   ├── engine/             # Orchestrator core
│   └── provider/           # Built-in & Remote Secret providers
├── testdata/
│   └── testDB.kdbx         # Test KeePass database
├── Makefile
├── go.mod
└── README.md
```

## Development Tasks

The project uses a `Makefile` to simplify common development commands:

### Building the Binary

To compile the application to a local binary in `bin/`:
```bash
make build
```
This produces a binary executable at `bin/cloakenv`.

### Linting and Formatting

To format the Go files and run static analysis checks:
```bash
make fmt
make vet
```

---

## Testing & Integration

A test KeePass database is provided for verification at `testdata/testDB.kdbx`.

* **Master Password**: `password123`
* **Vault Structure**:
  * **Group**: `website`
    * **Entry**: `Test Website`
      * **Password** (default): `testPassword123!`
      * **UserName**: `user@email.com`
      * **File Attachment**: `hello.txt`

### Integration Verification Steps

1. **Create a test configuration file** (`testdata/test_config.yaml`):
   ```yaml
   cache:
     default_ttl: "10m"

   keyring:
     prefix: "cloakenv_test"

   providers:
     testdb:
       provider: "keepass"
       database_path: "./testdata/testDB.kdbx"
   ```

2. **Authenticate with the test database**:
   ```bash
   echo "password123" | ./bin/cloakenv -c testdata/test_config.yaml auth login testdb
   ```

3. **Query attributes and extract file attachments**:
   ```bash
   # Retrieve default password attribute
   ./bin/cloakenv -c testdata/test_config.yaml get "testdb://website/Test Website"

   # Retrieve username attribute
   ./bin/cloakenv -c testdata/test_config.yaml get "testdb://website/Test Website:UserName"

   # Extract and stream file attachment content
   ./bin/cloakenv -c testdata/test_config.yaml get "testdb://website/Test Website:hello.txt"
   ```

4. **Clean up master password from keyring**:
   ```bash
   ./bin/cloakenv -c testdata/test_config.yaml auth forget testdb
   ```

---

## Structured Entries & Advanced Querying

`cloakenv` supports querying and managing multi-value secret records called **Entries**. 

Unlike simple Key-Value secrets, an entry has:
- `title` (entry name)
- `tags` (a list of tag strings)
- `path` (database path/folder)
- Arbitrary custom schema properties (e.g. `bit_strength`, `username`, `password`, `hostname`, `public_keys`).

### CLI Subcommands

- **Show an entry**:
  ```bash
  cloakenv entry show <entry-uri> [--yaml | --json]
  ```
- **Search entries using expression querying**:
  ```bash
  cloakenv entry search "[query_expression]" [--repo <repo_name>] [--yaml | --json]
  ```

---

### Query Syntax Reference (Go `expr` syntax)

All queries are evaluated against a flattened environment containing `title`, `tags`, `path`, and all custom attributes of each entry.

#### 1. Numeric/Integer Operations
Compare integers (e.g., bit sizes, counts):
```bash
# Exact match
cloakenv entry search "bit_strength == 4096"

# Inequalities
cloakenv entry search "bit_strength > 2048"
```

#### 2. Array Containment (Positive & Negative Checking)
Query membership within array fields (like `tags` or `public_keys` lists) using `in` and `not`:
```bash
# Positive array check: must have 'auth:ssh' tag
cloakenv entry search '"auth:ssh" in tags'

# Negative array check: must not have 'deprecated' tag
cloakenv entry search 'not ("deprecated" in tags)'

# Combined array check
cloakenv entry search '"auth:ssh" in tags and not ("deprecated" in tags)'
```

#### 3. Boolean Operators
Combine conditions using logical operators (`and`, `or`, `not` / `&&`, `||`, `!`):
```bash
cloakenv entry search '"auth:ssh" in tags and (bit_strength == 4096 or bit_strength == 2048)'
```

#### 4. Positive and Negative Matching
Check exact equality or inequality of strings, numbers, or boolean attributes:
```bash
# Positive check
cloakenv entry search 'username == "admin"'

# Negative check
cloakenv entry search 'username != "stage_user"'
```

#### 5. String Partials and Wildcards
Use built-in string functions (`contains`, `startsWith`, `endsWith`, `matches`):
```bash
# Substring search (case-sensitive)
cloakenv entry search 'title contains "Production"'

# Case-insensitive substring search (converting to lowercase first)
cloakenv entry search 'lower(title) contains "ssh"'

# Prefix match
cloakenv entry search 'title startsWith "Staging"'

# Suffix match
cloakenv entry search 'hostname endsWith ".com"'

# Regular expression match
cloakenv entry search 'hostname matches "bastion\\..*\\.example\\.com"'
```

#### 6. Graceful Missing Field Handling
If you search on a property (like `bit_strength`) that doesn't exist on all entries, the query engine will **gracefully skip** matching entries that lack that property, rather than throwing a runtime error.


## Pluggable Providers & URI Schemes

`cloakenv` supports pluggable providers. Each provider manages a specific URI scheme:

### 1. OS Keyring (`keyring://`)
* **Type**: Built-in, read/write
* **Description**: Securely stores/retrieves credential secrets in the operating system's native secure keyring (macOS Keychain, Linux Secret Service via D-Bus, Windows Credential Manager).
* **Usage**:
  ```bash
  # Store a secret value
  cloakenv set keyring://my_service/my_account "supersecretvalue"

  # Retrieve a secret value
  cloakenv get keyring://my_service/my_account
  ```
  *(Note: Does not support entry structured schemas or search operations.)*

### 2. Environment (`env://`)
* **Type**: Built-in, read-only
* **Description**: Accesses values from the current process's environment variables.
* **Usage**:
  ```bash
  # Retrieve the value of $USER
  cloakenv get env://USER
  ```
  *(Note: Does not support entry structured schemas or search operations.)*

### 3. Encrypted Cache (`cache://`)
* **Type**: Built-in, read/write
* **Description**: Stores values in a local, AES-GCM encrypted filesystem cache. The encryption key itself is safely stored in the OS keyring. Supports Time-To-Live (TTL) expiration.
* **Usage**:
  ```bash
  # Store a cached value with a 5-minute TTL
  cloakenv set cache://db_session_token "some_token" --ttl 5m

  # Retrieve a cached value
  cloakenv get cache://db_session_token

  # Clear all cached entries
  cloakenv cache clear
  ```
  *(Note: Does not support entry structured schemas or search operations.)*

### 4. KeePass (`keepass`)
* **Type**: Configured remote, read-only
* **Description**: Integrates with KeePass database (`.kdbx`) files. Requires authentication using `cloakenv auth login`.
* **Configuration** (`config.yaml`):
  ```yaml
  providers:
    my_vault:
      provider: "keepass"
      database_path: "~/secrets/personal.kdbx"
  ```
* **Usage**:
  ```bash
  # Authenticate with the database
  cloakenv auth login my_vault

  # Retrieve the password of an entry (default)
  cloakenv get "my_vault://Server/BastionHost"

  # Retrieve a specific attribute
  cloakenv get "my_vault://Server/BastionHost:UserName"

  # Search for entries in this database matching tag "auth:ssh"
  cloakenv entry search '"auth:ssh" in tags' --repo my_vault
  ```

### 5. YAML (`yaml`)
* **Type**: Configured remote, read-only
* **Description**: Reads static YAML files containing entries. Supports configuring the schema entry location using `entries_key`.
* **Configuration Settings**:
  - `database_path` (required): File path to the YAML registry.
  - `entries_key` (optional, defaults to `"entries"`): The dictionary key under which structured entries are defined. Set to `"."` to map entries directly to the root of the YAML document. If the specified key is not present, the repository is gracefully ignored during searches rather than throwing an error.
  - **Important**: `entries_key` is only used to locate entries for group commands (`entry search`, `entry show`). Single-value selector lookups (`cloakenv get`) ignore this parameter and always query starting directly from the root of the document using dot-path selectors.
* **Configuration** (`config.yaml`):
  ```yaml
  providers:
    yaml_db:
      provider: "yaml"
      database_path: "./testdata/test_entries.yaml"
      entries_key: "entries"
  ```
* **Usage**:
  ```bash
  # Single value lookup using dot-separated paths from root (ignores entries_key)
  cloakenv get "yaml_db://entries.ssh_key_prod.hostname"

  # Array index traversal
  cloakenv get "yaml_db://entries.ssh_key_prod.public_keys.0"

  # Search for entries with 4096-bit strength in this database
  cloakenv entry search 'bit_strength == 4096' --repo yaml_db
  ```

### 6. JSON (`json`)
* **Type**: Configured remote, read-only
* **Description**: Reads static JSON files containing entries. Supports configuring the schema entry location using `entries_key`.
* **Configuration Settings**:
  - `database_path` (required): File path to the JSON registry.
  - `entries_key` (optional, defaults to `"entries"`): The dictionary key under which structured entries are defined. Set to `"."` to map entries directly to the root of the JSON document. If the specified key is not present, the repository is gracefully ignored during searches rather than throwing an error.
  - **Important**: `entries_key` is only used to locate entries for group commands (`entry search`, `entry show`). Single-value selector lookups (`cloakenv get`) ignore this parameter and always query starting directly from the root of the document using dot-path selectors.
* **Configuration** (`config.yaml`):
  ```yaml
  providers:
    json_db:
      provider: "json"
      database_path: "./testdata/test_hosts.json"
      entries_key: "hosts"
  ```
* **Usage**:
  ```bash
  # Single value lookup using dot-separated paths from root (ignores entries_key)
  cloakenv get "json_db://hosts.ssh_host.hostname"

  # Search for entries in this database matching tag "auth:ssh"
  cloakenv entry search '"auth:ssh" in tags' --repo json_db
  ```

