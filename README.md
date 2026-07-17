# cloakenv

Pluggable secret orchestrator and dynamic runtime environment injector. It wraps application binaries, resolves secret URIs, and injects them strictly into temporary execution memory.

## Prerequisites

- [Go](https://go.dev/) 1.26.2 or higher.
- A native keyring store (macOS Keychain, Linux D-Bus, Windows Credential Manager).

## Project Structure

```
├── main.go                 # CLI Entrypoint
├── examples/
│   ├── providers/          # Example databases for vaults (JSON, YAML, KeePass)
│   └── config.yaml         # Fully documented example config file
├── internal/
│   ├── config/             # YAML Config parser
│   ├── engine/             # Orchestrator core
│   └── provider/           # Built-in & Custom Secret providers
├── testdata/
│   └── testDB.kdbx         # Test KeePass database
├── Makefile
├── go.mod
├── README.md
```
## Installation & Execution

You can run `cloakenv` directly or install it using the Go toolchain, or build it locally.

### Direct Execution (go run)

To run the orchestrator directly without manual compilation, use `go run`:
```bash
go run github.com/warpcode/cloakenv@latest [args]
```
For example, to display the help menu:
```bash
go run github.com/warpcode/cloakenv@latest --help
```

### Installation via Go Toolchain

You can install the binary directly to your `$GOPATH/bin` directory using:
```bash
go install github.com/warpcode/cloakenv@latest
```

## Development Tasks

The project uses a `Makefile` to simplify common development commands:

### Building the Binary

To compile the application to a local binary in `bin/`:
```bash
make build
```
This produces a binary executable at `bin/cloakenv`.

### Installing the Binary

To install the binary using the Go toolchain (by default to `$GOBIN` or `$GOPATH/bin`):
```bash
make install
```

If you prefer to install it to a custom directory, set the `GOBIN` environment variable:
```bash
GOBIN=$HOME/.local/bin make install
```

### Uninstalling the Binary

To uninstall/remove the installed binary:
```bash
make uninstall
```

If you installed to a custom directory, specify the same `GOBIN`:
```bash
GOBIN=$HOME/.local/bin make uninstall
```

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

   vaults:
     testdb:
       provider: "keepass"
       vault_path: "./testdata/testDB.kdbx"
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

4. **Verify accessibility status**:
   ```bash
   ./bin/cloakenv -c testdata/test_config.yaml auth status
   ```

5. **Clean up master password from keyring**:
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
  cloakenv show <entry-uri> [-o yaml | json | env | keys]
  ```
- **Search entries using expression querying**:
  ```bash
  cloakenv search "[query_expression]" [--vault <vault_name>] [-o yaml | json]
  ```

---

### Query Syntax Reference (Go `expr` syntax)

All queries are evaluated against a flattened environment containing `title`, `tags`, `path`, and all custom attributes of each entry.

#### 1. Numeric/Integer Operations
Compare integers (e.g., bit sizes, counts):
```bash
# Exact match
cloakenv search "bit_strength == 4096"

# Inequalities
cloakenv search "bit_strength > 2048"
```

#### 2. Array Containment (Positive & Negative Checking)
Query membership within array fields (like `tags` or `public_keys` lists) using `in` and `not`:
```bash
# Positive array check: must have 'auth:ssh' tag
cloakenv search '"auth:ssh" in tags'

# Negative array check: must not have 'deprecated' tag
cloakenv search 'not ("deprecated" in tags)'

# Combined array check
cloakenv search '"auth:ssh" in tags and not ("deprecated" in tags)'
```

#### 3. Boolean Operators
Combine conditions using logical operators (`and`, `or`, `not` / `&&`, `||`, `!`):
```bash
# Scope to vaults and match bit strengths
cloakenv search '"auth:ssh" in tags and (bit_strength == 4096 or bit_strength == 2048)'
```

#### 4. Positive and Negative Matching
Check exact equality or inequality of strings, numbers, or boolean attributes:
```bash
# Positive check
cloakenv search 'username == "admin"'

# Negative check
cloakenv search 'username != "stage_user"'
```

#### 5. String Partials and Wildcards
Use built-in string functions (`contains`, `startsWith`, `endsWith`, `matches`):
```bash
# Substring search (case-sensitive)
cloakenv search 'title contains "Production"'

# Case-insensitive substring search (converting to lowercase first)
cloakenv search 'lower(title) contains "ssh"'

# Prefix match
cloakenv search 'title startsWith "Staging"'

# Suffix match
cloakenv search 'hostname endsWith ".com"'

# Regular expression match
cloakenv search 'hostname matches "bastion\\..*\\.example\\.com"'
```

#### 6. Graceful Missing Field Handling
If you search on a property (like `bit_strength`) that doesn't exist on all entries, the query engine will **gracefully skip** matching entries that lack that property, rather than throwing a runtime error.


## Explicit Expansion Syntax (`${...}`)

`cloakenv` uses a secure, explicit syntax for resolving and injecting secrets. Any configuration value or template environment string containing `${scheme://...}` expressions will have those expressions dynamically replaced with their resolved secret values at runtime.

### Key Rules & Behavior
1. **Explicit Wrapping**: Secrets are only resolved when enclosed in `${...}`. Raw URIs (such as `env://DB_USER`) without `${...}` are treated as literal strings.
2. **CLI Convenience**: For CLI commands such as `cloakenv get`, raw URIs are automatically wrapped in `${...}` transparently to save typing.
3. **Multiple Expansions**: You can mix literal text and multiple secret expansions in a single configuration value:
   ```yaml
   connection_string: "mysql://${env://DB_USER}:${keyring://mysql/password}@localhost:3306/db"
   ```
4. **Escaping**: Use `$$` to escape the expansion syntax and output literal `$` or `${...}` sequences:
   - `$$` becomes `$`
   - `$${env://USER}` becomes `${env://USER}`
5. **No Nesting**: Nested expansions (e.g., `${env://${USER}}`) are not supported and will return a validation error.

## Vaults & URI Schemes

`cloakenv` supports configured **Vaults**. Each vault manages a specific URI scheme (acting as its vault reference):

### 1. OS Keyring (`keyring://`)
* **Type**: Built-in, read/write, multiple-entity
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
* **Type**: Built-in, read-only, single-entity
* **Description**: Accesses values from the current process's environment variables.
* **Usage**:
  ```bash
  # Retrieve the value of $USER
  cloakenv get env://USER
  ```
  *(Note: Does not support entry structured schemas or search operations.)*

### 3. Encrypted Cache (`cache://`)
* **Type**: Built-in, read/write, single-entity
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
* **Type**: Configured vault, read-only, multiple-entity
* **Description**: Integrates with KeePass database (`.kdbx`) files. Requires authentication using `cloakenv auth login`.
* **Configuration Settings**:
  - `provider` (required): Must be set to `"keepass"`.
  - `vault_path` (required): File path to the KeePass `.kdbx` file. Supports `~/` expansion.
  - `searchable` (optional, defaults to `true`): If set to `false`, excludes this vault from dynamic queries.
* **Configuration** (`config.yaml`):
  ```yaml
  vaults:
    my_vault:
      provider: "keepass"
      vault_path: "~/secrets/personal.kdbx"
      searchable: true
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
  cloakenv search '"auth:ssh" in tags' --vault my_vault
  ```

### 5. YAML (`yaml`)
* **Type**: Configured vault, read-only
* **Description**: Reads static YAML files containing entries. Can be configured as a single-entity or multiple-entity vault.
* **Configuration Settings**:
  - `provider` (required): Must be set to `"yaml"`.
  - `vault_path` (required): File path to the YAML database file.
  - `single_entity` (optional, defaults to `false`): If `true`, the YAML file is parsed as a flat key-value map representing a single entity.
  - `entity_name` (optional): The title of the entity in search results (if `single_entity` is `true`).
  - `tags` (optional): List of tag strings applied to the single entity (if `single_entity` is `true`).
  - `entities_root_key` (optional, defaults to `"entities"` or `"entries"`): Root dictionary key where entities are listed when `single_entity` is `false`. Use `"."` to parse directly from the document root.
  - `searchable` (optional, defaults to `true`): If set to `false`, excludes this vault from dynamic searches.
  - **JSON/YAML Serialization**: If a resolved value is a structured map/list, it is returned as a formatted YAML string.
* **Configuration** (`config.yaml`):
  ```yaml
  vaults:
    yaml_db:
      provider: "yaml"
      vault_path: "./testdata/test_entries.yaml"
      entities_root_key: "entries"
  ```
* **Usage**:
  ```bash
  # Single value lookup using dot-separated paths from root (ignores entities_root_key)
  cloakenv get "yaml_db://entries.ssh_key_prod.hostname"

  # Array index traversal
  cloakenv get "yaml_db://entries.ssh_key_prod.public_keys.0"

  # Search for entries with 4096-bit strength in this database
  cloakenv search 'bit_strength == 4096' --vault yaml_db
  ```

### 6. JSON (`json`)
* **Type**: Configured vault, read-only
* **Description**: Reads static JSON files containing entries. Can be configured as a single-entity or multiple-entity vault.
* **Configuration Settings**:
  - `provider` (required): Must be set to `"json"`.
  - `vault_path` (required): File path to the JSON database file.
  - `single_entity` (optional, defaults to `false`): If `true`, the JSON file is parsed as a flat key-value map representing a single entity.
  - `entity_name` (optional): The title of the entity in search results (if `single_entity` is `true`).
  - `tags` (optional): List of tag strings applied to the single entity (if `single_entity` is `true`).
  - `entities_root_key` (optional, defaults to `"entities"` or `"entries"`): Root dictionary key where entities are listed when `single_entity` is `false`. Use `"."` to parse directly from the document root.
  - `searchable` (optional, defaults to `true`): If set to `false`, excludes this vault from dynamic searches.
  - **JSON Serialization**: Structured values resolved from `GetSecret` are returned as compact JSON strings.
* **Configuration** (`config.yaml`):
  ```yaml
  vaults:
    json_db:
      provider: "json"
      vault_path: "./testdata/test_hosts.json"
      entities_root_key: "hosts"
  ```
* **Usage**:
  ```bash
  # Single value lookup using dot-separated paths from root (ignores entities_root_key)
  cloakenv get "json_db://hosts.ssh_host.hostname"

  # Search for entries in this database matching tag "auth:ssh"
  cloakenv search '"auth:ssh" in tags' --vault json_db
  ```

### 7. Custom Static Vault (`custom_vault`)
* **Type**: Configured vault, inline, read-only
* **Description**: Configured completely inside the `config.yaml` file without any external database dependencies. Excellent for static key lists.
* **Configuration Settings**:
  - `provider` (required): Must be set to `"custom_vault"`.
  - `entities` (optional): Inline map of named entities to their attribute maps.
  - `resolve_values` (optional, defaults to `false`): Enables recursive URI resolution for values inside this vault.
  - `searchable` (optional, defaults to `true`): If set to `false`, excludes this vault from dynamic searches.
* **Configuration** (`config.yaml`):
  ```yaml
  vaults:
    custom_multiple:
      provider: "custom_vault"
      entities:
        custom_entry:
          username: "admin"
          Password: "inline_password"
  ```
* **Usage**:
  ```bash
  # Retrieve from multiple-entity custom vault
  cloakenv get "custom_multiple://custom_entry"
  ```
