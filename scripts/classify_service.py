"""Maps E2E API operations onto module names.

myaccount_bucket(path) mirrors aws-sdk-go-v2's real per-service module
boundaries (e.g. EBS and VPC operations live in aws-sdk-go-v2's ec2
module, not their own, because that's how the actual AWS API groups
them -- so we do the same here).

tir_bucket(tags) uses TIR's own tag groups (Compute, Storage,
Models & Inference, Clusters, Network, RAG, Pipelines & Runs, Other)
since TIR is E2E's own product with no direct AWS analog to mirror.

This is the single source of truth for "which module does this
endpoint belong to" -- split_spec.py imports it, and it's meant to be
re-run, not re-derived, when E2E adds new endpoints.
"""

MYACCOUNT_PATH_RULES = [
    ("/kubernetes", "eks"),
    ("/storage/", "s3"),  # covers bucket*, storage/core/*, storage/objects, storage/urls
    ("/rds/", "rds"),
    ("/vpc", "ec2"),
    ("/block_storage", "ec2"),
    ("/schedule-snapshot", "ec2"),
    ("/scaler/", "autoscaling"),
    ("/reserve_ips", "ec2"),
    ("floating-ip", "ec2"),
    ("/faas", "lambda"),
    ("/cdn", "cloudfront"),
    ("/efs", "efs"),
    ("/epfs", "fsx"),
    ("/container_registry", "ecr"),
    ("/sso", "ssoadmin"),
    ("/vault", "secretsmanager"),
    ("/monitoring", "cloudwatch"),
    ("/monitor/", "cloudwatch"),
    ("/cdpbackup", "backup"),
    ("/draas", "drs"),
    ("/license", "licensemanager"),
    ("terminate-request", "licensemanager"),
    ("/label", "resourcegroupstaggingapi"),
    ("/billing", "costexplorer"),
    ("billing-history", "costexplorer"),
    ("ledger-statement", "costexplorer"),
    ("coupons", "costexplorer"),
    ("autopay", "costexplorer"),
    ("get_estimates", "costexplorer"),
    ("monthly-report", "costexplorer"),
    ("pre-paid-date-range-usage", "costexplorer"),
    ("pre-transaction", "costexplorer"),
    ("prepaid/cost_usage", "costexplorer"),
    ("transaction-history", "costexplorer"),
    ("/e2e_dns", "route53"),
    ("/fortigate", "networkfirewall"),
    ("/apis/token", "iam"),
    ("/iam/", "iam"),
    ("/images", "ec2"),
    ("/snapshot", "ec2"),
    ("/security_group", "ec2"),
    ("/appliances", "elasticloadbalancingv2"),
    ("load-balancer", "elasticloadbalancingv2"),
    ("load_balancer", "elasticloadbalancingv2"),
    ("appliance-type", "elasticloadbalancingv2"),
    ("/persistent_volume", "ec2"),
    ("/ssh_keys", "ec2"),
    ("/start-script", "ec2"),
    ("resource-quota-details", "servicequotas"),
    ("customer-resource-limit", "servicequotas"),
    ("accounts/profile", "iam"),
    ("profile-settings", "iam"),
    ("pbac/projects-header", "iam"),
    ("abuse-detail", "securityhub"),
    ("compliance-security", "securityhub"),
    ("resource-transfer", "ec2"),
    ("update_committed_node_status", "ec2"),
    ("/nodes", "ec2"),
    ("/scheduled_actions", "ec2"),
    ("plans-and-pricing", "servicecatalog"),
]


def myaccount_bucket(path):
    p = path.lower()
    for needle, bucket in MYACCOUNT_PATH_RULES:
        if needle in p:
            return bucket
    return "other"


TIR_TAG_TO_BUCKET = {
    "Instance/VM": "compute",
    "Dataset": "storage",
    "SFS": "storage",
    "PFS": "storage",
    "Container Registry": "storage",
    "Vector Database": "storage",
    "Model Repository": "models",
    "Model Endpoints": "models",
    "Playground API": "models",
    "Model Evaluation": "models",
    "GenAI API": "models",
    "Fine Tune Models": "models",
    "Training Cluster": "clusters",
    "Private Cluster": "clusters",
    "Reserve IP": "network",
    "Security Groups": "network",
    "Security": "network",
    "Gateway": "network",
    "RAG (Knowledge Base)": "rag",
    "RAG (Chat Assistant)": "rag",
    "Pipeline": "pipelines",
    "Run": "pipelines",
    "Schedule Run": "pipelines",
    "IAM Users": "other",
    "Plans & Pricing": "other",
    "SKU": "other",
    "Data Syncer": "other",
    "AI Labs": "other",
    "Alerts Management": "other",
    "External Integration": "other",
}


def tir_bucket(tags):
    for tag in tags or []:
        if tag in TIR_TAG_TO_BUCKET:
            return TIR_TAG_TO_BUCKET[tag]
    return "other"
