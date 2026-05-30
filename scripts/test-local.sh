#!/usr/bin/env bash
# Local CI test script - simpler alternative to act
# Runs the same steps as the GitHub Actions workflow
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "🧪 Local CI Test (act alternative)"
echo "=================================="
echo ""

# Check prerequisites
echo "📋 Checking prerequisites..."
command -v docker >/dev/null || { echo "❌ docker not found"; exit 1; }
command -v go >/dev/null || { echo "❌ go not found"; exit 1; }
command -v bats >/dev/null || { echo "⚠️  bats not found (optional)"; }
echo "✅ Prerequisites OK"
echo ""

# Set environment
export SKIP_SIM=true
export IMAGES=release

# Build/pull images
echo "📦 Step 1: Build/pull images..."
bash scripts/build-images.sh
echo "✅ Images ready"
echo ""

# Start infrastructure
echo "🚀 Step 2: Start infrastructure..."
docker compose -f compose/infra.yaml up -d
echo "✅ Infrastructure started"
echo ""

# Wait for health
echo "⏳ Step 3: Wait for infrastructure health..."
max_attempts=20
attempt=0
while [ $attempt -lt $max_attempts ]; do
    vault_status=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8200/v1/sys/health || echo "000")
    localstack_status=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:4566/_localstack/health || echo "000")
    
    if [[ "$vault_status" =~ ^(200|429)$ ]] && [[ "$localstack_status" == "200" ]]; then
        echo "✅ Infrastructure healthy (vault: $vault_status, localstack: $localstack_status)"
        break
    fi
    
    attempt=$((attempt + 1))
    echo "   Waiting for services... (attempt $attempt/$max_attempts)"
    sleep 3
done

if [ $attempt -eq $max_attempts ]; then
    echo "❌ Infrastructure failed to become healthy"
    docker compose -f compose/infra.yaml ps
    docker compose -f compose/infra.yaml logs --tail=50
    docker compose -f compose/infra.yaml down
    exit 1
fi
echo ""

# Start core services
echo "🚀 Step 4: Start core services..."
docker compose -f compose/infra.yaml -f compose/core.yaml up -d
echo "✅ Core services started"
echo ""

# Wait for core health
echo "⏳ Step 5: Wait for core services health..."
max_attempts=40
attempt=0
while [ $attempt -lt $max_attempts ]; do
    smd_status=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:27779/hsm/v2/service/ready || echo "000")
    tokensmith_status=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:27780/health || echo "000")
    boot_status=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:27791/health || echo "000")
    
    if [[ "$smd_status" == "200" ]] && [[ "$tokensmith_status" == "200" ]] && [[ "$boot_status" == "200" ]]; then
        echo "✅ Core services healthy (smd: $smd_status, tokensmith: $tokensmith_status, boot: $boot_status)"
        break
    fi
    
    attempt=$((attempt + 1))
    echo "   Waiting for core services... (attempt $attempt/$max_attempts) smd:$smd_status token:$tokensmith_status boot:$boot_status"
    sleep 3
done

if [ $attempt -eq $max_attempts ]; then
    echo "❌ Core services failed to become healthy"
    docker compose -f compose/core.yaml ps
    docker compose -f compose/core.yaml logs --tail=50
    docker compose -f compose/infra.yaml -f compose/core.yaml down
    exit 1
fi
echo ""

# Seed fixtures
echo "🌱 Step 6: Seed fixtures..."
export VAULT_ADDR=http://127.0.0.1:8200
export VAULT_TOKEN=dev-root-token
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1
export AWS_ENDPOINT_URL=http://127.0.0.1:4566

bash fixtures/vault-seed.sh
bash fixtures/s3-buckets.sh
bash fixtures/seed-smd.sh
echo "✅ Fixtures seeded"
echo ""

# Run BATS tests (optional)
if command -v bats >/dev/null; then
    echo "🧪 Step 7: Run BATS smoke tests..."
    bats tests/bats/ || {
        echo "⚠️  BATS tests failed (non-fatal)"
    }
    echo ""
fi

# Run UC1 test
echo "🧪 Step 8: Run UC1 (Node Visibility)..."
cd tests
go test -tags integration -count=1 -v -timeout 10m -run '^TestUC1_' ./integration/... || {
    echo "❌ UC1 failed"
    cd ..
    docker compose -f compose/infra.yaml -f compose/core.yaml down
    exit 1
}
cd ..
echo "✅ UC1 passed"
echo ""

# Cleanup
echo "🧹 Step 9: Cleanup..."
docker compose -f compose/infra.yaml -f compose/core.yaml down
echo "✅ Cleanup complete"
echo ""

echo "🎉 Local CI test PASSED!"
echo ""
echo "To run other use cases:"
echo "  make uc2  # Multi-cluster"
echo "  make uc3  # Restart resilience"
echo "  make uc4  # Tokensmith + SMD"
echo "  make uc5  # Magellan scan"
echo "  make uc6  # Power control"
echo "  make uc7  # FRU discovery"
