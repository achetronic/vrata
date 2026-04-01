#!/usr/bin/env bash
# Creates a TLS k8s Secret with self-signed certs for the existingSecret e2e test.
# Usage: create-tls-secret.sh <kind-cluster> <namespace> <secret-name>
set -euo pipefail

CLUSTER="${1:?usage: create-tls-secret.sh <kind-cluster> <namespace> <secret-name>}"
NAMESPACE="${2:?}"
SECRET_NAME="${3:?}"
CONTEXT="kind-${CLUSTER}"
TMPDIR="$(mktemp -d)"

trap 'rm -rf "$TMPDIR"' EXIT

# Generate CA
openssl genrsa -out "$TMPDIR/ca.key" 4096 2>/dev/null
openssl req -new -x509 -key "$TMPDIR/ca.key" -out "$TMPDIR/ca.crt" \
  -days 1 -subj "/CN=Vrata E2E CA" 2>/dev/null

# Generate server/client cert
openssl genrsa -out "$TMPDIR/tls.key" 4096 2>/dev/null
openssl req -new -key "$TMPDIR/tls.key" -out "$TMPDIR/tls.csr" \
  -subj "/CN=vrata-tls-vrata-cp" \
  -addext "subjectAltName=DNS:vrata-tls-vrata-cp,DNS:localhost,IP:127.0.0.1" 2>/dev/null
openssl x509 -req -in "$TMPDIR/tls.csr" -CA "$TMPDIR/ca.crt" -CAkey "$TMPDIR/ca.key" \
  -CAcreateserial -out "$TMPDIR/tls.crt" -days 1 \
  -extfile <(printf "subjectAltName=DNS:vrata-tls-vrata-cp,DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth,clientAuth") 2>/dev/null

# Create namespace if needed
kubectl --context "$CONTEXT" create namespace "$NAMESPACE" 2>/dev/null || true

# Create or replace the Secret
kubectl --context "$CONTEXT" -n "$NAMESPACE" create secret generic "$SECRET_NAME" \
  --from-file=tls.crt="$TMPDIR/tls.crt" \
  --from-file=tls.key="$TMPDIR/tls.key" \
  --from-file=ca.crt="$TMPDIR/ca.crt" \
  --dry-run=client -o yaml | kubectl --context "$CONTEXT" apply -f -

echo "✓ TLS Secret $SECRET_NAME created in $NAMESPACE"
