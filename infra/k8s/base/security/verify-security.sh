#!/bin/bash
# Security Verification Script for PravaraMES
# Verifies that all security controls are properly deployed

set -e

NAMESPACE="pravara-mes"

echo "========================================="
echo "PravaraMES Security Verification"
echo "========================================="
echo ""

# Check if namespace exists
echo "1. Checking namespace..."
if kubectl get namespace $NAMESPACE &>/dev/null; then
    echo "✓ Namespace $NAMESPACE exists"

    # Check Pod Security Standards labels
    echo ""
    echo "2. Checking Pod Security Standards..."
    kubectl get namespace $NAMESPACE -o jsonpath='{.metadata.labels}' | grep -q "pod-security.kubernetes.io/enforce" && \
        echo "✓ Pod Security Standards labels configured" || \
        echo "✗ Pod Security Standards labels missing"
else
    echo "✗ Namespace $NAMESPACE does not exist"
    exit 1
fi

# Check Network Policies
echo ""
echo "3. Checking Network Policies..."
NP_COUNT=$(kubectl get networkpolicies -n $NAMESPACE --no-headers 2>/dev/null | wc -l)
echo "   Found $NP_COUNT network policies"

REQUIRED_NPS=(
    "default-deny-all"
    "allow-dns"
    "pravara-api-ingress"
    "pravara-api-egress"
    "pravara-ui-ingress"
    "pravara-ui-egress"
    "telemetry-worker-egress"
    "centrifugo-ingress"
    "centrifugo-egress"
    "postgres-ingress"
    "redis-ingress"
    "emqx-ingress"
)

for np in "${REQUIRED_NPS[@]}"; do
    if kubectl get networkpolicy $np -n $NAMESPACE &>/dev/null; then
        echo "   ✓ $np"
    else
        echo "   ✗ $np (missing)"
    fi
done

# Check Service Accounts
echo ""
echo "4. Checking Service Accounts..."
SA_COUNT=$(kubectl get serviceaccounts -n $NAMESPACE --no-headers 2>/dev/null | wc -l)
echo "   Found $SA_COUNT service accounts"

REQUIRED_SAS=(
    "pravara-api-sa"
    "pravara-ui-sa"
    "telemetry-worker-sa"
    "centrifugo-sa"
    "postgres-sa"
    "redis-sa"
    "emqx-sa"
)

for sa in "${REQUIRED_SAS[@]}"; do
    if kubectl get serviceaccount $sa -n $NAMESPACE &>/dev/null; then
        echo "   ✓ $sa"
    else
        echo "   ✗ $sa (missing)"
    fi
done

# Check Roles and RoleBindings
echo ""
echo "5. Checking RBAC..."
ROLE_COUNT=$(kubectl get roles -n $NAMESPACE --no-headers 2>/dev/null | wc -l)
RB_COUNT=$(kubectl get rolebindings -n $NAMESPACE --no-headers 2>/dev/null | wc -l)
echo "   Found $ROLE_COUNT roles and $RB_COUNT rolebindings"

if kubectl get role pravara-app-role -n $NAMESPACE &>/dev/null; then
    echo "   ✓ pravara-app-role"
else
    echo "   ✗ pravara-app-role (missing)"
fi

if kubectl get role pravara-infra-role -n $NAMESPACE &>/dev/null; then
    echo "   ✓ pravara-infra-role"
else
    echo "   ✗ pravara-infra-role (missing)"
fi

# Check for ClusterRoles/ClusterRoleBindings (should be none)
echo ""
echo "6. Checking for unauthorized cluster-wide permissions..."
CR_COUNT=$(kubectl get clusterroles 2>/dev/null | grep -c "pravara" || true)
CRB_COUNT=$(kubectl get clusterrolebindings 2>/dev/null | grep -c "pravara" || true)

if [ $CR_COUNT -eq 0 ] && [ $CRB_COUNT -eq 0 ]; then
    echo "   ✓ No cluster-wide permissions (good - namespace-scoped only)"
else
    echo "   ✗ Found $CR_COUNT ClusterRoles and $CRB_COUNT ClusterRoleBindings"
    echo "   WARNING: PravaraMES should not have cluster-wide permissions"
fi

# Check Resource Quotas and Limits
echo ""
echo "7. Checking Resource Quotas..."
if kubectl get resourcequota pravara-quota -n $NAMESPACE &>/dev/null; then
    echo "   ✓ Resource quota configured"
    kubectl get resourcequota pravara-quota -n $NAMESPACE -o jsonpath='{.spec.hard}' | grep -q "requests.cpu" && \
        echo "   ✓ CPU limits defined"
    kubectl get resourcequota pravara-quota -n $NAMESPACE -o jsonpath='{.spec.hard}' | grep -q "requests.memory" && \
        echo "   ✓ Memory limits defined"
else
    echo "   ✗ Resource quota not configured"
fi

# Summary
echo ""
echo "========================================="
echo "Verification Complete"
echo "========================================="
echo ""
echo "To test network policies:"
echo "  kubectl run -it --rm debug --image=nicolaka/netshoot -n $NAMESPACE -- curl http://pravara-api:4500/health"
echo ""
echo "To verify RBAC permissions:"
echo "  kubectl auth can-i get configmaps --as=system:serviceaccount:$NAMESPACE:pravara-api-sa -n $NAMESPACE"
echo ""
echo "To check Pod Security Standards enforcement:"
echo "  kubectl run -it --rm privileged-test --image=nginx --privileged -n $NAMESPACE"
echo "  (should fail with PSS enforcement)"
echo ""
