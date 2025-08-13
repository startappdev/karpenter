# Contributing to Karpenter OCI Provider

Welcome to the Karpenter OCI Provider community! We're excited about your interest in contributing to this **production-ready Oracle Cloud Infrastructure implementation** of Karpenter.

## 🌟 **About This Project**

The Karpenter OCI Provider brings Karpenter's intelligent node provisioning to Oracle Kubernetes Engine (OKE) with enterprise-grade features including:
- Dynamic flexible shape provisioning  
- Comprehensive rate limiting protection
- Cost optimization with smart shape filtering
- Production-validated performance under extreme load

## 🚀 **Quick Start for Contributors**

### **Development Environment Setup**

```bash
# 1. Fork and clone the repository
git clone https://github.com/your-username/karpenter.git
cd karpenter

# 2. Set up Go environment (1.24+)
go version  # Ensure Go 1.24+

# 3. Install dependencies
make setup

# 4. Run tests
make test

# 5. Run OCI-specific tests (requires OCI credentials)
make test-oci
```

### **OCI Development Requirements**

**Required OCI Setup:**
- OCI CLI configured or Instance Principal access
- Access to OKE cluster for testing
- OCI compartment with compute permissions
- Test environment in us-ashburn-1 (recommended)

**Environment Variables:**
```bash
export OCI_REGION="us-ashburn-1"
export OCI_COMPARTMENT_ID="ocid1.compartment.oc1..."
export OCI_CLUSTER_ID="ocid1.cluster.oc1..."
export OCI_SUBNET_IDS="ocid1.subnet.oc1...,ocid1.subnet.oc1..."
```

## 🎯 **Contribution Areas**

### **1. Core OCI Provider Development**
- **Location**: `pkg/providers/oci/`
- **Focus**: Instance provisioning, flexible shapes, rate limiting
- **Expertise**: Go, OCI SDK, Kubernetes controllers

### **2. Performance & Optimization** 
- **Location**: `pkg/providers/oci/client.go`, `pkg/providers/oci/instancetypes.go`
- **Focus**: Rate limiting protection, cost optimization, shape selection
- **Expertise**: Performance tuning, OCI APIs, concurrent programming

### **3. Documentation & Guides**
- **Location**: `docs/`
- **Focus**: Installation guides, troubleshooting, best practices
- **Expertise**: Technical writing, OCI/OKE experience

### **4. Testing & Validation**
- **Location**: `test/`, `pkg/providers/oci/*_test.go`
- **Focus**: Integration tests, performance benchmarks, chaos testing
- **Expertise**: Go testing, OCI environments, load testing

## 📋 **Contribution Process**

### **1. Before You Start**
- 🔍 **Check existing issues**: Browse [GitHub Issues](https://github.com/startappdev/karpenter/issues)
- 💬 **Join discussions**: Start a [GitHub Discussion](https://github.com/startappdev/karpenter/discussions) for major changes
- 📧 **Contact maintainers**: Email [support@startapp.com](mailto:support@startapp.com) for complex contributions

### **2. Development Workflow**

```bash
# 1. Create feature branch
git checkout -b feature/your-feature-name

# 2. Make changes following our coding standards
# See "Coding Standards" section below

# 3. Test your changes
make test
make test-oci  # If OCI changes
make lint

# 4. Commit with clear messages
git commit -m "feat: add flexible shape optimization for E5 instances

- Implement dynamic OCPU/memory calculation for E5 shapes  
- Add cost comparison logic for E4 vs E5 selection
- Update integration tests for new shape selection logic

Fixes #123"

# 5. Push and create pull request
git push origin feature/your-feature-name
```

### **3. Pull Request Requirements**

**✅ Required Checklist:**
- [ ] **Tests pass**: `make test` and `make test-oci` (if applicable)
- [ ] **Linting passes**: `make lint` 
- [ ] **Documentation updated**: For user-facing changes
- [ ] **Performance validated**: For performance-related changes
- [ ] **OCI testing**: For OCI provider changes
- [ ] **Changelog entry**: For significant changes

**📝 PR Description Template:**
```markdown
## Summary
Brief description of changes

## Changes Made
- List key changes
- Include any breaking changes

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated  
- [ ] OCI environment tested
- [ ] Performance impact assessed

## Documentation
- [ ] Code comments added
- [ ] User documentation updated
- [ ] API documentation updated

Fixes #issue_number
```

## 🔧 **Coding Standards**

### **Go Code Style**
```go
// ✅ Good: Clear function naming and documentation
// CalculateFlexibleShapeConfig determines optimal OCPU/memory configuration
// for the given pod requirements using OCI flexible shapes
func (p *Provider) CalculateFlexibleShapeConfig(
    ctx context.Context, 
    requirements []v1.NodeSelectorRequirementWithMinValues,
) (*ShapeConfig, error) {
    // Implementation with clear logic and error handling
}

// ✅ Good: Proper error handling
instance, err := p.client.LaunchFlexibleInstance(ctx, nodeClaim, nodeClass, shapeConfig)
if err != nil {
    return nil, fmt.Errorf("launching flexible instance: %w", err)
}

// ❌ Avoid: Generic error messages
if err != nil {
    return nil, err  // Not descriptive
}
```

### **OCI-Specific Conventions**
```go
// ✅ Good: OCI resource naming
type OCINodeClass struct {
    Spec OCINodeClassSpec `json:"spec"`
}

// ✅ Good: Shape naming consistency  
const (
    ShapeE4Flex = "VM.Standard.E4.Flex"
    ShapeE5Flex = "VM.Standard.E5.Flex"
)

// ✅ Good: Rate limiting protection
func (c *Client) TerminateInstance(ctx context.Context, instanceID string) error {
    logger := log.FromContext(ctx)
    logger.Info("terminating OCI instance with rate limiting protection",
        "instanceID", instanceID)
    
    // Apply rate limiting protection
    // Implementation...
}
```

### **Testing Standards**
```go
// ✅ Good: Comprehensive test coverage
func TestFlexibleShapeSelection(t *testing.T) {
    tests := []struct {
        name         string
        requirements []v1.NodeSelectorRequirementWithMinValues  
        expected     *ShapeConfig
        expectError  bool
    }{
        {
            name: "should select E4.Flex for standard workload",
            requirements: []v1.NodeSelectorRequirementWithMinValues{
                {Key: corev1.ResourceCPU, Values: []string{"4000m"}},
                {Key: corev1.ResourceMemory, Values: []string{"16Gi"}},
            },
            expected: &ShapeConfig{
                OCPUs: lo.ToPtr(int32(2)),
                MemoryInGBs: lo.ToPtr(int32(16)),
            },
        },
        // More test cases...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

## 🧪 **Testing Guidelines**

### **Test Categories**

1. **Unit Tests** (`*_test.go`)
   - Test individual functions and methods
   - Mock OCI API calls
   - Focus on business logic

2. **Integration Tests** (`test/`)
   - Test against real OCI environment
   - Validate end-to-end workflows
   - Test rate limiting protection

3. **Performance Tests** 
   - Benchmark shape selection algorithms
   - Test under load conditions
   - Validate rate limiting effectiveness

### **OCI Testing Best Practices**
```go
// ✅ Good: Mock OCI clients for unit tests
func TestInstanceProvisioning(t *testing.T) {
    mockClient := &MockOCIClient{}
    mockClient.On("LaunchInstance").Return(&Instance{
        ID: "test-instance-id",
        Shape: "VM.Standard.E4.Flex",
    }, nil)
    
    provider := &Provider{client: mockClient}
    // Test implementation
}

// ✅ Good: Integration test with proper cleanup
func TestRealOCIIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    
    // Create test resources
    defer func() {
        // Cleanup test resources
    }()
    
    // Test against real OCI
}
```

## 📖 **Documentation Standards**

### **Code Documentation**
- **All public functions** must have clear godoc comments
- **Complex algorithms** need inline comments explaining logic
- **OCI-specific behavior** should be documented with examples

### **User Documentation**
- **Installation guides** must be tested on clean environments
- **Troubleshooting docs** should include common error messages and solutions
- **Configuration examples** must be complete and working

## 🏷️ **Issue and PR Labels**

### **Issue Labels**
- `good-first-issue`: Perfect for new contributors
- `help-wanted`: Community help needed
- `bug`: Something isn't working
- `enhancement`: New feature or improvement
- `documentation`: Documentation needs
- `oci-specific`: OCI provider specific issues
- `performance`: Performance related
- `security`: Security implications

### **Priority Labels**
- `priority/critical`: Critical production issues
- `priority/high`: Important features/fixes
- `priority/medium`: Standard priority
- `priority/low`: Nice to have

## 👥 **Community Guidelines**

### **Code of Conduct**
This project follows the [Kubernetes Code of Conduct](code-of-conduct.md). Key principles:
- **Be respectful** and inclusive
- **Be collaborative** and constructive
- **Be responsible** for your contributions

### **Communication Channels**
- 🐛 **Bug Reports**: [GitHub Issues](https://github.com/startappdev/karpenter/issues)
- 💬 **Feature Discussions**: [GitHub Discussions](https://github.com/startappdev/karpenter/discussions)
- 🏠 **General Karpenter**: [#karpenter](https://kubernetes.slack.com/archives/C02SFFZSA2K) in Kubernetes Slack
- 🔧 **Development**: [#karpenter-dev](https://kubernetes.slack.com/archives/C04JW2J5J5P) in Kubernetes Slack

### **Getting Help**
- 📧 **Direct Support**: [support@startapp.com](mailto:support@startapp.com)
- 📚 **Documentation**: Browse our [comprehensive docs](docs/)
- 🤝 **Mentoring**: Available for significant contributions

## 🏆 **Recognition**

### **Contributors**
All contributors are recognized in:
- GitHub contributor graphs
- Release notes acknowledgments  
- Special recognition for major contributions

### **Maintainer Path**
Regular contributors may be invited to become maintainers based on:
- **Code quality** and consistency
- **Community involvement** and helpfulness
- **OCI/OKE expertise** and knowledge sharing
- **Long-term commitment** to the project

## 🚀 **Advanced Contribution Areas**

### **For OCI Experts**
- Implement advanced OCI features (spot instances, preemptible instances)
- Optimize OCI SDK usage and performance
- Develop OCI-specific monitoring and alerting
- Enhance security with OCI native features

### **For Kubernetes Experts**  
- Improve Kubernetes controller performance
- Enhance scheduling and provisioning logic
- Develop advanced autoscaling strategies
- Implement multi-zone and multi-region support

### **For Performance Engineers**
- Optimize rate limiting algorithms
- Benchmark and improve provisioning speed  
- Develop chaos engineering tests
- Create performance regression detection

## 📞 **Contact & Support**

### **Maintainer Team**
- **Lead Maintainer**: Available via [support@startapp.com](mailto:support@startapp.com)
- **OCI Expert**: For OCI-specific questions and reviews
- **Performance Expert**: For performance and optimization reviews

### **Response Times**
- **Bug reports**: 24-48 hours
- **Feature requests**: 1-2 weeks  
- **Documentation**: 48-72 hours
- **Security issues**: 24 hours (see [SECURITY.md](SECURITY.md))

---

## 🎯 **Ready to Contribute?**

1. 🍴 **Fork the repository**
2. 📋 **Choose an issue** or propose a feature
3. 🔧 **Set up your development environment** 
4. 💻 **Start coding** following our guidelines
5. 🧪 **Test thoroughly** including OCI integration
6. 📝 **Submit a pull request** with clear description

**Thank you for contributing to the future of Kubernetes autoscaling on Oracle Cloud!** 🚀

---

**Questions?** Don't hesitate to reach out via [GitHub Discussions](https://github.com/startappdev/karpenter/discussions) or [email us](mailto:support@startapp.com). 