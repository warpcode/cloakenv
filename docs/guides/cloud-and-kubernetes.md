# Guide: Cloud, Kubernetes, Terraform & Docker

This cookbook shows how to configure `cloakenv` for `aws`, `kubectl`, `terraform`, and `docker compose` using ephemeral `0600` files and declarative rules.

---

## Complete `config.yaml` Example

```yaml
vaults:
  work:
    provider: keepass
    vault_path: ~/secrets/work.kdbx
    cache: "15m"

envs:
  aws_env:
    - vault: work
      query: '{{ hasTag "aws" .tags }}'
      rules:
        - type: pass
          include: ["AWS_*"]
          exclude: ["*_NOTES"]
          key_format: env

  tf_env:
    - vault: work
      query: '{{ hasTag "terraform" .tags }}'
      rules:
        - type: match
          match: "^(.*)$"
          key: "TF_VAR_{{ .key | snake }}"
          value: "{{ .value }}"

  app_env:
    - vault: work
      query: '{{ hasTag "backend" .tags }}'
      rules:
        - type: pass
          include: ["DB_*", "REDIS_*"]
          key_format: env

templates:
  service_json:
    driver: json
    vaults:
      - vault: work
        query: '{{ eq .title "GCP Service Account" }}'
        rules:
          - type: pass
            exclude: ["Notes"]
            key_format: snake

  kubeconfig_yaml:
    driver: yaml
    vaults:
      - vault: work
        query: '{{ hasTag "k8s" .tags }}'
        rules:
          - type: pass
            exclude: ["Notes"]
            key_format: kebab

  compose_dotenv:
    driver: dotenv
    from_env: app_env

autoload:
  # 1. AWS CLI with in-memory service JSON
  - match: '^aws(\s|$)'
    envs:
      - aws_env
      - AWS_DEFAULT_REGION: "us-east-1"
    templates:
      - template: service_json
        env: SERVICE_ACCOUNT_JSON

  # 2. kubectl with ephemeral KUBECONFIG in /dev/shm (zeroed on exit)
  - match: '^(kubectl|helm)(\s|$)'
    templates:
      - template: kubeconfig_yaml
        env_path: KUBECONFIG

  # 3. Terraform with TF_VAR_* variables
  - match: '^terraform(\s|$)'
    envs:
      - tf_env

  # 4. Docker Compose with ephemeral .env file
  - match: '^docker\s+compose(\s|$)'
    templates:
      - template: compose_dotenv
        env_path: COMPOSE_ENV_FILE
```

---

## Usage

```bash
# Inspect what kubectl will receive
cloakenv inspect run -- kubectl get pods

# Run kubectl with ephemeral KUBECONFIG
cloakenv run -- kubectl get pods

# Run terraform plan with injected TF_VAR_* secrets
cloakenv run -- terraform plan
```
