# Helm Chart Secret Management

The `ai-edge` Helm chart ships everything needed to install the EdgeAI
Runtime Platform on a Kubernetes cluster. Several components consume
secrets — database credentials, the apiserver's private CA, the gateway's
mTLS server certificate, and bundled PostgreSQL / MinIO passwords. Until
recently, the chart did not materialise any of these, which meant
`helm install` failed (or, worse, installed pods that crashed because
their `secretKeyRef` pointed at a Secret that did not exist).

This document explains how the chart now handles secrets end-to-end, and
how to point it at secrets you already own.

## TL;DR

1. By default, the chart generates a random password for whatever it
   owns (bundled Postgres, bundled MinIO, the control-plane DB) and
   publishes it in a `Secret` whose name is documented in `helm install`
   output (`NOTES.txt`).
2. Every secret has three resolution paths, in priority order:
   1. `*.existingSecret` — use a Secret you created yourself.
   2. `*.generate=true` / `*.createSecret=true` — let the chart
      generate a Secret (and a random password / self-signed cert).
   3. Auto-derived default name — useful when the parent component is
      enabled (e.g. enabling `postgresql.enabled=true` implies DB Secret
      creation).
3. All secret names are resolved through helpers in
   [`templates/_helpers.tpl`](../manifests/helm/ai-edge/templates/_helpers.tpl).
   Component templates never read secret names directly from
   `.Values`; they always go through a helper so the precedence rules
   stay consistent.

## Secret inventory

| Secret (default name)        | Consumed by                          | Auto-generated when                                                | Keys                                        |
|------------------------------|--------------------------------------|--------------------------------------------------------------------|---------------------------------------------|
| `edgeai-db`                  | apiserver, controller, gateway, **bundled Postgres** | `db.createSecret=true` **or** `postgresql.enabled=true`            | `password`, `username`, `database`          |
| `<release>-minio-secret`     | bundled MinIO pod                    | `minio.enabled=true` (and no `existingSecret`)                     | `rootuser`, `rootpassword`                  |
| `<fullname>-apiserver-ca`    | apiserver                            | `apiserver.ca.generate=true`                                       | `ca.crt`, `ca.key`, `tls.crt`, `tls.key`    |
| `<fullname>-gateway-tls`     | gateway-runtime                      | `gatewayRuntime.tls.generate=true`                                 | `tls.crt`, `tls.key`, (`ca.crt` if chained) |
| `<fullname>-gateway-ca`      | gateway-runtime                      | `gatewayRuntime.ca.generate=true` (and no apiserver CA generated)  | `ca.crt`                                    |

## Prerequisites

- **Kubernetes 1.24+**. The chart no longer reads K8s Node
  annotations via the Downward API — `gateway-runtime` registers
  itself on first boot and only uses `fieldRef: spec.nodeName`,
  which has been GA since 1.24. The earlier K8s 1.27+ requirement
  (raised for `metadata.annotations['...']` Downward API) has
  been removed; see `manifests/README.md` for the rationale.
- **Helm 3.10+**. Required for `hook-delete-policy: before-hook-creation`
  and `helm.sh/hook-weight`.
- **kubectl 1.24+** for routine cluster operations
  (`kubectl get pod -o jsonpath='{.spec.nodeName}'` etc).

`<release>` is the Helm release name (`{{ .Release.Name }}`).
`<fullname>` is `<release>-<chart-name>` (`{{ include "ai-edge.fullname" . }}`, so for release `edgeai` it is `edgeai-ai-edge`).

All Secrets are tagged with
`app.kubernetes.io/component: <apiserver|gateway|database|minio|postgresql>`
plus the standard chart labels, so you can find them with:

```sh
kubectl get secrets -n <namespace> -l app.kubernetes.io/part-of=ai-edge
```

## Resolution rules

For every secret, the chart exposes three knobs and always picks the
highest-precedence one:

| Knob                       | Meaning                                                                 |
|----------------------------|-------------------------------------------------------------------------|
| `*.existingSecret`         | Name of a Secret you manage. The chart will not create or read it.      |
| `*.generate` / `*.createSecret` | Render a Secret with random password / self-signed cert.          |
| Default name (`*.secretName`) | Used as the Secret name when neither of the above is set.            |

When you set `*.existingSecret`, all the other knobs for that secret are
ignored — the chart never reads the contents of the Secret you supply
(at render time), so the `username` / `database` / `password` literals
in `values.yaml` are also ignored.

### Precedence summary

```
existingSecret  >  generate/createSecret  >  bundled-component-enabled  >  default
```

For the **DB Secret** specifically, the chart also auto-enables
`db.createSecret=true` when `postgresql.enabled=true`, so the bundled
Postgres' password can flow into the apiserver / controller / gateway
without any extra config.

## Auto-generated passwords

`templates/secrets.yaml` uses Helm's `randAlphaNum 24` to produce a
24-character alphanumeric password when one is needed. Passwords are
regenerated on every `helm template` / `helm install`, so the Secret
content is **not deterministic** between runs. Once installed, the
password is stable for the life of the Secret — `helm upgrade` will
update the Secret manifest but Helm never edits Secret data in place.

To retrieve an auto-generated password:

```sh
kubectl get secret -n <namespace> <secret-name> -o jsonpath='{.data.<key>}' | base64 -d
```

Examples:

```sh
# Bundled Postgres password — same Secret as the control plane (apiserver / controller / gateway)
kubectl get secret -n edgeai-system edgeai-db \
    -o jsonpath='{.data.password}' | base64 -d

# Bundled MinIO root password
kubectl get secret -n edgeai-system test-minio-secret \
    -o jsonpath='{.data.rootpassword}' | base64 -d

# DB credentials consumed by apiserver / controller / gateway
kubectl get secret -n edgeai-system edgeai-db \
    -o jsonpath='{.data.password}' | base64 -d
```

## Auto-generated TLS material

The chart can produce three TLS artefacts:

1. **apiserver CA** — a self-signed root CA used by the apiserver to
   sign bootstrap / leaf certs. Validity: 10 years.
2. **gateway mTLS server cert** — a server certificate the gateway
   presents to edge-agents.
3. **gateway CA bundle** — a CA the gateway uses to verify peer
   (apiserver / edge-agent) chains.

When both the apiserver CA and the gateway mTLS cert are auto-generated,
the chart signs the gateway cert with the apiserver CA so the same
`ca.crt` embedded in the gateway TLS Secret also validates the gateway
server cert. This avoids the chicken-and-egg of "who signs the signer".

The cert / key pairs are generated with Helm's `genCA` / `genSignedCert`
/ `genSelfSignedCert` helpers at template render time and live in
`kubernetes.io/tls` Secrets.

## Database migrations

Schema migrations are not a Secret but they are part of the same
"helm install must be self-sufficient" guarantee. The chart ships every
file under `migrations/*.up.sql` / `migrations/*.down.sql` as a
ConfigMap, and renders a pre-install,pre-upgrade Helm hook Job that runs
`migrate/migrate` against the same database the control plane points
at. The Job is gated on `.Values.migration.enabled` (default `true`).

```yaml
# In manifests/helm/ai-edge/values.yaml
migration:
  enabled: true                # master switch
  image:
    repository: migrate/migrate
    tag: v4.17.1
  activeDeadlineSeconds: 600
  backoffLimit: 5
  resources:
    requests: { cpu: 50m,  memory: 64Mi }
    limits:   { cpu: 200m, memory: 128Mi }
```

Lifecycle:

1. `helm install` / `helm upgrade` enters the `pre-install,pre-upgrade`
   phase. The chart renders a ConfigMap (`<fullname>-migrations`) and a
   Job (`<fullname>-migrate`) with the same hooks.
2. The Job's container runs
   `migrate -path /migrations -database $DATABASE_URL up`. The
   `DATABASE_URL` is composed from `.Values.db.username` /
   `.Values.db.database` / `.Values.db.sslmode` plus the password
   sourced from the Secret resolved by `ai-edge.dbSecretName`. The host
   is `.Values.db.host`, or the in-chart PostgreSQL Service FQDN when
   `postgresql.enabled=true`.
3. `golang-migrate` is idempotent: an already up-to-date database
   reports "no change" and exits 0.
4. Helm blocks the rest of the release until the Job reports
   `Complete`. If the Job fails, the install / upgrade rolls back.
5. The previous run's ConfigMap and Job are cleaned up by
   `helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded`
   before the next hook fires.

Inspect what the chart will do without installing:

```sh
helm template ./manifests/helm/ai-edge \
  --set postgresql.enabled=true \
  --set minio.enabled=true \
  --set apiserver.ca.generate=true \
  --set gatewayRuntime.tls.generate=true \
  | grep -A 30 'kind: Job' | head -60
```

Inspect logs after a real install:

```sh
kubectl logs -n edgeai-system -l app.kubernetes.io/component=migration --tail=200
```

When to opt out (`migration.enabled=false`):

- The database is managed by a DBA team with its own change-window
  policy.
- You want to run the migration from a CI pipeline / dedicated job
  runner before the helm release, then let `helm install` skip the hook.
- You are upgrading from a chart version that did not ship the
  migration hook and want to keep the old `make migrate-up` flow for
  one cycle.

If you opt out, the apiserver / controller / gateway pods will still
start (the chart does not gate them on the migration Job), so make
sure the schema is at the version expected by the deployed image
**before** rolling out an upgrade.

## Deployment scenarios

The chart supports four common scenarios; pick the one that matches
your environment and copy the matching `helm install` command.

### 1. All-in-one dev cluster

Self-contained: bundled Postgres + MinIO + auto-generated TLS. Good for
local development and CI smoke tests.

```sh
helm install edgeai ./manifests/helm/ai-edge \
  --create-namespace --namespace edgeai-system \
  --set postgresql.enabled=true \
  --set minio.enabled=true \
  --set apiserver.ca.generate=true \
  --set gatewayRuntime.tls.generate=true
```

> Helm will print a `== Secrets ==` block listing the auto-generated
> Secret names and the keys you need. Save the DB password — it is
> the only credential that is not retrievable from any other
> Kubernetes object after install.

### 2. Production with an external managed Postgres

Use a managed Postgres (RDS, Cloud SQL, …), supply a Secret that
contains the password, and disable the bundled Postgres.

```sh
kubectl create secret generic corp-db \
  --namespace edgeai-system \
  --from-literal=password='REPLACE_ME' \
  --from-literal=username=edgeai \
  --from-literal=database=edgeai

helm install edgeai ./manifests/helm/ai-edge \
  --create-namespace --namespace edgeai-system \
  --set db.host=edgeai-prod.cluster-abc123.us-east-1.rds.amazonaws.com \
  --set db.existingSecret=corp-db \
  --set minio.enabled=true \
  --set apiserver.ca.generate=true \
  --set gatewayRuntime.tls.generate=true
```

> The chart still auto-generates a bundled MinIO Secret and TLS
> material. Disable them too by setting `apiserver.ca.existingSecret`,
> `gatewayRuntime.tls.existingSecret`, and `minio.auth.existingSecret`.

### 3. Production with bring-your-own TLS

Most production clusters already have a corporate CA and cert manager
(HashiCorp Vault, cert-manager, internal PKI). Hand the chart a
pre-issued Secret for each.

```sh
# apiserver CA bundle (must include tls.crt, tls.key, ca.crt)
kubectl create secret generic corp-apiserver-ca \
  --namespace edgeai-system \
  --from-file=tls.crt=./apiserver-ca.crt \
  --from-file=tls.key=./apiserver-ca.key \
  --from-file=ca.crt=./apiserver-ca.crt

# gateway mTLS cert (must include tls.crt, tls.key; optional ca.crt)
kubectl create secret generic corp-gateway-tls \
  --namespace edgeai-system \
  --from-file=tls.crt=./gateway.crt \
  --from-file=tls.key=./gateway.key

helm install edgeai ./manifests/helm/ai-edge \
  --create-namespace --namespace edgeai-system \
  --set postgresql.enabled=true \
  --set minio.enabled=true \
  --set apiserver.ca.existingSecret=corp-apiserver-ca \
  --set gatewayRuntime.tls.existingSecret=corp-gateway-tls
```

### 4. Component-by-component enable

Disable any component you do not need:

```sh
helm install edgeai ./manifests/helm/ai-edge \
  --set controller.enabled=false \
  --set minio.enabled=true \
  --set postgresql.enabled=true \
  --set apiserver.ca.generate=true \
  --set gatewayRuntime.tls.generate=true \
  --set gatewayRuntime.tls.commonName=edge-gw.example.com
```

> When a component is disabled, none of its related Secrets are
> generated. For example, with `minio.enabled=false` no MinIO Secret
> is created and `gatewayRuntime.minioBucket` is ignored.

## How the helpers work

The chart centralises secret-name resolution in
[`templates/_helpers.tpl`](../manifests/helm/ai-edge/templates/_helpers.tpl).
Component templates call helpers like `ai-edge.dbSecretName` rather than
reading `.Values.db.existingSecret` directly. This guarantees:

- The same precedence rules apply everywhere.
- A new resolution path (e.g. "look up the Secret by label") can be
  added in one place.
- The templates stay declarative and easy to test with `helm template`.

| Helper                       | Resolves to                                                       |
|------------------------------|-------------------------------------------------------------------|
| `ai-edge.fullname`           | `<release>-<chart>` (drops the redundant prefix when the release name is the same as the chart name) |
| `ai-edge.apiserverAddr`      | `<fullname>-apiserver.<ns>.svc.cluster.local:<grpcPort>` — used by gateway-runtime's `CONTROL_PLANE_ADDR` |
| `ai-edge.gatewayRuntimeAddr` | `<fullname>-gateway-runtime.<ns>.svc.cluster.local:<grpcPort>` — reserved for in-cluster callers |
| `ai-edge.dbSecretName`       | DB credentials consumed by apiserver / controller / gateway / bundled Postgres |
| `ai-edge.postgresPasswordSecretName` | Source of `POSTGRES_PASSWORD` for the bundled Postgres pod (`postgresql.auth.existingSecret` → falls back to `dbSecretName`) |
| `ai-edge.minioSecretName`    | Bundled MinIO root credentials                                    |
| `ai-edge.apiserverCaSecretName` | CA used by the apiserver to sign leaf certs                    |
| `ai-edge.gatewayTlsSecretName` | mTLS server cert presented by gateway-runtime                  |
| `ai-edge.gatewayCaSecretName` | CA bundle used by gateway-runtime to verify peers               |
| `ai-edge.dbCreateSecret`     | Boolean — should the chart render a DB Secret?                   |

## Operational notes

- **Rotation.** To rotate a chart-generated password, delete the
  Secret and run `helm upgrade` — `randAlphaNum` will produce a new
  value and the Secret will be replaced. There is a single source of
  truth (`edgeai-db`); rotating it propagates to the bundled
  PostgreSQL pod automatically.
- **Backup.** The chart never deletes Secrets on `helm uninstall`
  (Helm's default behaviour) so uninstalling and reinstalling keeps
  the credentials stable. Use `helm uninstall ... --keep-history` or
  delete the Secrets manually if you want a clean slate.
- **Upgrading from chart versions that auto-generated
  `<release>-postgresql-secret`.** The bundled PostgreSQL used to read
  its password from a dedicated Secret that was randomly generated
  independently of `edgeai-db`, which made the three control-plane
  components unable to log in to the database. After upgrading to the
  current chart, the Postgres pod now reads `edgeai-db.password`. The
  old `<release>-postgresql-secret` becomes an orphan — the chart no
  longer renders it but Kubernetes will not garbage-collect it. Delete
  the orphan manually (`kubectl delete secret
  <release>-postgresql-secret -n <namespace>`) or run `helm uninstall
  ... --keep-history` first. The bundled PostgreSQL PVC (which holds
  the database files) is **not** affected by this change because the
  password it is initialised with lives in PG's own catalog, not in
  the Secret — but the password PG accepts for new connections now
  matches `edgeai-db`.
- **Namespaces.** The chart installs into a single namespace. Cross-
  namespace Secret references (e.g. DB in `db-team`, control plane
  in `edgeai-system`) are not natively supported; bring the Secret
  into the chart's namespace or use `Secret` mirroring.
- **External Secrets Operator.** If you use ESO or a similar
  controller, leave the chart's `existingSecret` keys pointing at
  the Secret name ESO produces, and the chart will use it.

## Troubleshooting

| Symptom                                                              | Likely cause / fix                                                                     |
|----------------------------------------------------------------------|----------------------------------------------------------------------------------------|
| Pod stuck in `CreateContainerConfigError`, `secret "X" not found`   | The Secret name resolved by a helper does not exist. See the [Secret inventory](#secret-inventory) and either let the chart generate it or create the Secret yourself. |
| `helm install` fails with `wrong number of args for genSignedCert`  | Helm version < 3.7. Upgrade to Helm 3.13+.                                            |
| `helm install` fails with `nil pointer evaluating interface{}`      | A required `*.existingSecret` is set but the Secret has the wrong key. Inspect the Secret with `kubectl get secret -o yaml` and compare to the [key list](#secret-inventory). |
| mTLS handshake fails between gateway and edge-agents                 | The CA in the apiserver / gateway Secrets does not match. When bringing your own CA, both `apiserver.ca` and `gatewayRuntime.tls` must be issued (or chained) by the same root. |
| DB connection refused after install                                  | The bundled Postgres takes 20–30s to become ready. `apiserver` / `controller` / `gateway` retry on backoff; check `kubectl get pods -n edgeai-system`. |
| `apiserver` / `controller` / `gateway` log `password authentication failed for user "postgres"` | Two Secrets (`edgeai-db` and `<release>-postgresql-secret`) drifted; restore a single source of truth as described in [Upgrading from chart versions that auto-generated `<release>-postgresql-secret`](#operational-notes). |
| `apiserver` logs `init signer: open /etc/edgeai/pki/ca.key: no such file or directory` and exits | The mounted CA Secret is missing a `ca.key` key. With `apiserver.ca.generate=true` the chart writes both `ca.crt`/`ca.key` (consumed by the apiserver) and `tls.crt`/`tls.key` (consumed as `kubernetes.io/tls`); if you bring your own Secret, ensure it contains `ca.crt` **and** `ca.key` at the root — the apiserver reads `CA_CERT_PATH=/etc/edgeai/pki/ca.crt` and `CA_KEY_PATH=/etc/edgeai/pki/ca.key` from the mounted volume. |
| `helm install` rolls back; `<fullname>-migrate` Job is `Error` with `connection refused` | Most common when `postgresql.enabled=true`: the bundled Postgres pod was not yet ready when the hook ran. The chart's `backoffLimit=5` + `activeDeadlineSeconds=600` usually self-heals; if it doesn't, inspect the Postgres pod (`kubectl get pods -n <ns> -l app.kubernetes.io/name=postgresql`) and rerun `helm upgrade`. |
| Migration Job `Error`: `dialect postgres: write tcp ...: connection reset by peer` against an external managed Postgres | A network policy / SG / VPC peering is blocking the hook Pod from reaching the database. Run `kubectl describe job <fullname>-migrate -n <ns>` and check the cluster's egress rules for the hook Job's ServiceAccount. The Job runs in the same namespace as the rest of the chart and uses the cluster's default ServiceAccount. |
| Migration Job reports `no migration found` | The chart's `migrations/` directory is empty or the files were excluded by `.helmignore`. Inspect with `helm template ... | grep -A 30 'kind: ConfigMap'`; the ConfigMap should contain one key per `*.up.sql` / `*.down.sql`. |

For deeper background on the secrets that flow through the platform,
see [`docs/design/02-node-onboarding-security.md`](../design/02-node-onboarding-security.md)
and [`docs/design/11-database-schema.md`](../design/11-database-schema.md).
