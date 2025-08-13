# Security Policy

## Security Announcements

- **Project-Specific Security**: Subscribe to [GitHub Security Advisories](https://github.com/startappdev/karpenter/security/advisories) for this repository
- **Kubernetes Security**: Join the [kubernetes-security-announce] group for Kubernetes security announcements
- **OCI Security**: Monitor [Oracle Security Alerts](https://www.oracle.com/security-alerts/) for OCI platform updates

## Reporting a Vulnerability

### For Karpenter OCI Provider Issues

**Report directly to our security team:**
- 📧 **Email**: [security@startapp.com](mailto:security@startapp.com)
- 🔒 **Encrypted**: Use our [PGP key](https://keybase.io/startapp) for sensitive reports
- ⏱️ **Response Time**: We aim to respond within 24 hours

**Include in your report:**
- Vulnerability description and potential impact
- Steps to reproduce the issue
- OCI environment details (region, OKE version)
- Affected Karpenter OCI Provider versions

### For Kubernetes Core Issues

Instructions for reporting Kubernetes core vulnerabilities can be found on the
[Kubernetes Security and Disclosure Information] page.

## 🛡️ OCI-Specific Security Considerations

### **Instance Principal Authentication (Recommended)**

```yaml
# Secure configuration
oci:
  useInstancePrincipal: true  # Preferred - no secrets needed
  existingSecret: "oci-config"  # Alternative - use sealed secrets
```

**Benefits:**
- ✅ No credential storage in cluster
- ✅ Automatic credential rotation by OCI
- ✅ Fine-grained IAM policies
- ✅ Audit trail through OCI logging

### **IAM Policy Best Practices**

**Minimum Required Permissions:**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "compute:Instance*",
        "compute:Image*",
        "compute:Subnet*",
        "compute:Vnic*",
        "identity:AvailabilityDomain*",
        "containerengine:Cluster*"
      ],
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "oci:RequestedRegion": "us-ashburn-1"
        }
      }
    }
  ]
}
```

**Security Hardening:**
- 🔒 Use compartment-scoped policies
- ⏰ Implement time-based access restrictions  
- 🌍 Restrict to specific regions/availability domains
- 📊 Enable detailed audit logging

### **Network Security**

**Private Endpoint Access:**
```yaml
# Private OKE cluster configuration
spec:
  template:
    spec:
      requirements:
        - key: "karpenter.sh/subnet-type"
          operator: In
          values: ["private"]
```

**Security Groups:**
- Restrict outbound internet access where possible
- Use OCI Network Security Groups (NSGs) for fine-grained control
- Enable OCI Flow Logs for network monitoring

### **Container Security**

**Image Security:**
```yaml
# Use official OKE node images
spec:
  template:
    spec:
      imageID: "ocid1.image.oc1.iad.aaaaaaaavxqdkuyamlnrdo5q2qaqdw..."  # Official OKE image
```

**Runtime Security:**
- Enable OCI Container Runtime Security
- Use OCI Vulnerability Scanning for node images
- Configure Pod Security Standards
- Enable OCI Audit logging for container events

## 🔐 Secrets Management

### **Sealed Secrets Integration**

```bash
# Create sealed secret for OCI credentials
echo -n 'your-config' | kubectl create secret generic oci-config \
  --dry-run=client --from-file=config=/dev/stdin -o yaml | \
  kubeseal -o yaml > sealed-secret-oci-config.yaml
```

### **Secret Rotation**

**Automated Rotation:**
- Instance Principal: Automatic by OCI
- API Keys: Implement 90-day rotation policy
- Sealed Secrets: Update and re-seal quarterly

## 🔍 Security Monitoring

### **OCI Native Monitoring**

**Enable These Services:**
- **OCI Audit**: All API calls logged
- **OCI Logging**: Application and system logs  
- **OCI Monitoring**: Resource and security metrics
- **OCI Security Advisor**: Security posture assessment

**Key Metrics to Monitor:**
```yaml
# Example monitoring configuration
monitoring:
  metrics:
    - name: "oci_api_rate_limits"
      description: "Track rate limiting events"
    - name: "instance_provisioning_time" 
      description: "Monitor provisioning performance"
    - name: "security_policy_violations"
      description: "IAM policy violations"
```

### **Security Event Response**

**Incident Response Plan:**
1. **Detection**: Automated alerts via OCI Monitoring
2. **Isolation**: Immediate instance termination capability  
3. **Investigation**: Complete audit trail analysis
4. **Recovery**: Automated redeployment with security patches
5. **Prevention**: Policy updates and hardening

## 📊 Compliance & Audit

### **Audit Logging**

**OCI Audit Integration:**
```yaml
# Enable comprehensive audit logging
spec:
  template:
    metadata:
      annotations:
        oci.audit.enabled: "true"
        oci.audit.retention: "365d"
```

**Audit Trail Includes:**
- All instance lifecycle events
- IAM policy evaluations  
- Network security group changes
- Secret access attempts

### **Compliance Standards**

**Supported Frameworks:**
- **SOC 2 Type II**: OCI compliance inherited
- **ISO 27001**: Security management system
- **PCI DSS**: Payment card industry standards
- **HIPAA**: Healthcare compliance (where applicable)

## Supported Versions

### **Karpenter OCI Provider**

| Version | Supported          | Security Updates |
| ------- | ------------------ | ---------------- |
| 0.1.47+ | ✅ Yes             | ✅ Active        |
| 0.1.46  | ⚠️ Legacy          | 🔄 Critical only |
| < 0.1.46| ❌ End of Life     | ❌ None          |

### **Dependencies**

**Kubernetes Compatibility:**
- **Supported**: 1.28, 1.29, 1.30, 1.31
- **OKE Versions**: Compatible with all supported OKE releases
- **Go Runtime**: 1.24+ (security patches included)

**OCI SDK:**
- **Current**: v65.97.0+ (actively maintained)
- **Security**: Automatic dependency updates via Dependabot

## 🚨 Emergency Response

### **Security Incident Contacts**

**Immediate Response (24/7):**
- 📧 **Email**: [security@startapp.com](mailto:security@startapp.com)
- 📱 **Phone**: Available upon request for verified security researchers
- 🔐 **Encrypted Chat**: Matrix room for verified researchers

### **Disclosure Timeline**

**Coordinated Disclosure Process:**
1. **Day 0**: Vulnerability reported
2. **Day 1**: Initial response and triage  
3. **Day 7**: Severity assessment and patch development
4. **Day 14**: Testing and validation
5. **Day 21**: Public disclosure and patch release

**Emergency Response:**
- **Critical vulnerabilities**: 24-hour patch timeline
- **High severity**: 72-hour response
- **Medium/Low**: Standard timeline applies

---

## 📚 Additional Resources

- 🔒 [OCI Security Best Practices](https://docs.oracle.com/en-us/iaas/Content/Security/Reference/security_guidance.htm)
- 🛡️ [Kubernetes Security Documentation](https://kubernetes.io/docs/concepts/security/)
- 📋 [OCI Compliance Documentation](https://docs.oracle.com/en-us/iaas/Content/General/Reference/compliance.htm)
- 🔍 [Container Security Guide](https://kubernetes.io/docs/concepts/security/pod-security-standards/)

[kubernetes-security-announce]: https://groups.google.com/forum/#!forum/kubernetes-security-announce
[Kubernetes version and version skew support policy]: https://kubernetes.io/docs/setup/release/version-skew-policy/#supported-versions  
[Kubernetes Security and Disclosure Information]: https://kubernetes.io/docs/reference/issues-security/security/#report-a-vulnerability