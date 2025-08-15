# Cluster Autoscaler on Amazon EKS (from scratch, 2025)

This lab builds an **EKS** cluster from zero, installs **Cluster Autoscaler (CA)** using **IRSA**, and proves scale-up via logs. All files are created in the **current folder** and every command is **copy-pasteable**.

> [!note]
> Shell: bash on macOS/Linux (or WSL). Make sure `aws`, `eksctl`, `kubectl`, and `helm` are installed and authenticated.

---

## Step 0 — Prep variables & detect latest EKS version

```bash
set -Eeuo pipefail

# Basic context
export CLUSTER_NAME="interview-ca-cluster"
export AWS_REGION="us-east-1"
export AWS_ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"

# Discover latest supported Kubernetes version for EKS in your region
export EKS_LATEST_VERSION="$(
  aws eks describe-addon-versions     --region "$AWS_REGION"     --query 'addons[].compatibilities[].clusterVersion'     --output text | tr '\t' '\n' | sort -uV | tail -1 || true
)"

# Fallback to 1.33 if discovery yields nothing
: "${EKS_LATEST_VERSION:=1.33}"

echo "Using: Account=$AWS_ACCOUNT_ID  Region=$AWS_REGION  Cluster=$CLUSTER_NAME  K8s=$EKS_LATEST_VERSION"
```

> [!tip]
> If your org pins versions, set `export EKS_LATEST_VERSION="1.33"` explicitly before continuing.

---

## Step 1 — Create the EKS cluster (CA tags included)

Create the eksctl config (in the **current folder**):

```bash
cat > ./demo-cluster.yaml <<EOF
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: ${CLUSTER_NAME}
  region: ${AWS_REGION}
  version: "${EKS_LATEST_VERSION}"

managedNodeGroups:
  - name: managed-ng-1
    minSize: 1
    desiredCapacity: 1
    maxSize: 5
    instanceType: t3.medium
    volumeSize: 20
    labels: { role: worker }
    # Required tags for Cluster Autoscaler autodiscovery
    tags:
      k8s.io/cluster-autoscaler/enabled: "true"
      k8s.io/cluster-autoscaler/${CLUSTER_NAME}: "owned"
EOF
```

Create the cluster:

```bash
eksctl create cluster -f ./demo-cluster.yaml
```

Authenticate kubectl to your new cluster:

```bash
aws eks update-kubeconfig --region "$AWS_REGION" --name "$CLUSTER_NAME"
kubectl cluster-info
```

---

## Step 2 — Enable IRSA (OIDC provider)

```bash
eksctl utils associate-iam-oidc-provider   --region "$AWS_REGION"   --cluster "$CLUSTER_NAME"   --approve
```

---

## Step 3 — Create the minimal IAM policy for CA

Write the policy file (saved **here**):

```bash
cat > ./cluster-autoscaler-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow",
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "autoscaling:DescribeAutoScalingInstances",
        "autoscaling:DescribeLaunchConfigurations",
        "autoscaling:DescribeScalingActivities",
        "autoscaling:DescribeTags",
        "ec2:DescribeImages",
        "ec2:DescribeInstanceTypes",
        "ec2:DescribeLaunchTemplateVersions",
        "ec2:GetInstanceTypesFromInstanceRequirements",
        "eks:DescribeNodegroup"
      ],
      "Resource": "*" },
    { "Effect": "Allow",
      "Action": [
        "autoscaling:SetDesiredCapacity",
        "autoscaling:TerminateInstanceInAutoScalingGroup",
        "autoscaling:UpdateAutoScalingGroup"
      ],
      "Resource": "*",
      "Condition": {
        "StringEquals": { "aws:ResourceTag/k8s.io/cluster-autoscaler/enabled": "true" },
        "StringLike":   { "aws:ResourceTag/k8s.io/cluster-autoscaler/*": "owned" }
      }
    }
  ]
}
EOF
```

Create the policy and capture its ARN:

```bash
# Try to create; if it already exists, we'll fetch its ARN
export CA_POLICY_ARN="$(
  aws iam create-policy     --policy-name AmazonEKSClusterAutoscalerPolicy     --policy-document file://./cluster-autoscaler-policy.json     --query "Policy.Arn" --output text 2>/dev/null || true
)"

if [ -z "${CA_POLICY_ARN:-}" ] || [ "$CA_POLICY_ARN" = "None" ]; then
  export CA_POLICY_ARN="$(
    aws iam list-policies --scope Local       --query "Policies[?PolicyName=='AmazonEKSClusterAutoscalerPolicy'].Arn | [0]"       --output text
  )"
fi

echo "CA policy ARN: $CA_POLICY_ARN"
```

---

## Step 4 — Create the CA service account (IRSA role binding)

```bash
eksctl create iamserviceaccount   --cluster "$CLUSTER_NAME"   --region "$AWS_REGION"   --namespace kube-system   --name cluster-autoscaler   --attach-policy-arn "$CA_POLICY_ARN"   --approve   --override-existing-serviceaccounts
```

---

## Step 5 — Install Cluster Autoscaler (Helm)

We’ll make the resource names/labels match the pattern you saw:

- **Release name:** `cluster-autoscaler`
- **nameOverride:** `aws-cluster-autoscaler`

This yields a deployment named `cluster-autoscaler-aws-cluster-autoscaler` and pod labels:
`app.kubernetes.io/instance=cluster-autoscaler`, `app.kubernetes.io/name=aws-cluster-autoscaler`.

```bash
helm repo add autoscaler https://kubernetes.github.io/autoscaler
helm repo update

helm upgrade --install cluster-autoscaler autoscaler/cluster-autoscaler   --namespace kube-system   --set nameOverride=aws-cluster-autoscaler   --set cloudProvider=aws   --set awsRegion="$AWS_REGION"   --set autoDiscovery.clusterName="$CLUSTER_NAME"   --set expander=least-waste   --set rbac.serviceAccount.create=false   --set rbac.serviceAccount.name=cluster-autoscaler   --set extraArgs.balance-similar-node-groups=true   --set extraArgs.scale-down-unneeded-time=5m   --set extraArgs.skip-nodes-with-local-storage=false   --set extraArgs.skip-nodes-with-system-pods=false
```

> [!note]
> If you must pin the CA image: add `--set image.tag=v1.33.x` (match your cluster minor).

---

## Step 6 — Validate (names, labels, status)

Use labels (robust across chart changes):

```bash
# Helm release present?
helm -n kube-system list | grep -i cluster-autoscaler || true

# Deployment by instance label
kubectl -n kube-system get deploy   -l app.kubernetes.io/instance=cluster-autoscaler

# Pods by chart name label (matches your output)
kubectl -n kube-system get pods -o wide   -l app.kubernetes.io/name=aws-cluster-autoscaler

# Details (first 120 lines)
kubectl -n kube-system describe deploy   -l app.kubernetes.io/instance=cluster-autoscaler | sed -n '1,120p'
```

Expected examples:

```
NAME                                        READY   UP-TO-DATE   AVAILABLE   AGE
cluster-autoscaler-aws-cluster-autoscaler   1/1     1            1           3m

cluster-autoscaler-aws-cluster-autoscaler-5ff6bcbc4b-2gg22   1/1   Running   0   101s
Labels: app.kubernetes.io/instance=cluster-autoscaler, app.kubernetes.io/name=aws-cluster-autoscaler
```

---

## Step 7 — Smoke test scaling (and watch CA ask for nodes)

Create load to force scale-up:

```bash
cat > ./scale-test.yaml <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: scale-test
  namespace: default
spec:
  replicas: 10
  selector:
    matchLabels: { app: scale-test }
  template:
    metadata:
      labels: { app: scale-test }
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
          resources:
            requests: { cpu: "200m", memory: "256Mi" }
            limits:   { cpu: "200m", memory: "256Mi" }
EOF

kubectl apply -f ./scale-test.yaml
```

Confirm some pods are **Pending** (unschedulable):

```bash
kubectl get pods -l app=scale-test -o wide
kubectl describe pods -l app=scale-test | grep -iE "unschedulable|Insufficient|0/|node(s)"
```

Tail **Cluster Autoscaler** logs and look for scale-up decisions:

```bash
# Full logs
kubectl -n kube-system logs -f deploy/cluster-autoscaler-aws-cluster-autoscaler

# (Optional) Filtered view for key phrases
# kubectl -n kube-system logs -f deploy/cluster-autoscaler-aws-cluster-autoscaler #   | grep -iE "pending|unschedulable|scale-up|expanding|increase size|upcoming|best option|backoff"
```

Typical messages you’ll see:
- `Pod ... is unschedulable`
- `Scale-up: setting group managed-ng-1 size to 2`
- `Expanding Node Group managed-ng-1 from 1 to 2`
- `Upcoming 1 nodes`

Watch nodes join and pods schedule:

```bash
kubectl get nodes -w
# In another tab:
kubectl get pods -l app=scale-test -w
```

**Scale-in (optional):** remove load and observe scale-down later:

```bash
kubectl delete -f ./scale-test.yaml
# Watch CA logs for "removing node"/"scale down" after its delay.
```

---

## Step 8 — Cleanup (optional)

```bash
helm -n kube-system uninstall cluster-autoscaler || true
eksctl delete cluster --name "$CLUSTER_NAME" --region "$AWS_REGION"
# Optionally remove the IAM policy after the cluster is gone:
# aws iam delete-policy --policy-arn "$CA_POLICY_ARN"
```

---

## Troubleshooting quick hits

- **ASG not discovered?** Ensure the two tags exist on the node group’s Auto Scaling Group:
  - `k8s.io/cluster-autoscaler/enabled=true`
  - `k8s.io/cluster-autoscaler/${CLUSTER_NAME}=owned`

- **IRSA not applied?** `kubectl -n kube-system get sa cluster-autoscaler -o yaml`  
  should show an `eks.amazonaws.com/role-arn` annotation.

- **No scale-down?** Check node group **minSize**, PodDisruptionBudgets, and `safe-to-evict` annotations.

> [!tip]
> Don’t run **two** node autoscalers (e.g., CA and Karpenter/EKS Auto Mode) on the same capacity.
