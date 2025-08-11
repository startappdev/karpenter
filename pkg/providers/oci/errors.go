/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package oci

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

// Common error types
var (
	ErrInstanceLimitExceeded = errors.New("instance limit exceeded")
	ErrShapeNotAvailable     = errors.New("shape not available")
	ErrSubnetFull            = errors.New("subnet full")
	ErrQuotaExceeded         = errors.New("quota exceeded")
	ErrInvalidConfiguration  = errors.New("invalid configuration")
)

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Factor       float64
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  2,                // Reduced attempts to prevent API storms
		InitialDelay: 5 * time.Second,  // Much longer initial delay
		MaxDelay:     60 * time.Second, // Longer max delay
		Factor:       3.0,              // More aggressive backoff
	}
}

// RateLimitRetryConfig returns a retry configuration optimized for rate limiting scenarios
func RateLimitRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,                 // Significantly reduced attempts
		InitialDelay: 30 * time.Second,  // Much longer initial delay to let rate limiting cool down
		MaxDelay:     600 * time.Second, // 10 minute max delay for severe rate limiting
		Factor:       4.0,               // Very aggressive backoff
	}
}

// WithRetry executes a function with exponential backoff retry
func WithRetry(ctx context.Context, config RetryConfig, operation string, fn func() error) error {
	logger := log.FromContext(ctx)
	
	backoff := wait.Backoff{
		Duration: config.InitialDelay,
		Factor:   config.Factor,
		Jitter:   0.1,
		Steps:    config.MaxAttempts,
		Cap:      config.MaxDelay,
	}
	
	var lastErr error
	attempt := 0
	
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attempt++
		logger.V(1).Info("attempting operation", "operation", operation, "attempt", attempt)
		
		err := fn()
		if err == nil {
			return true, nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if !isRetryableError(err) {
			logger.Info("non-retryable error encountered", "operation", operation, "error", err)
			return false, err
		}
		
		// Check if we've hit a terminal error
		if isTerminalError(err) {
			logger.Info("terminal error encountered", "operation", operation, "error", err)
			return false, err
		}
		
		logger.Info("retryable error encountered, will retry", 
			"operation", operation, 
			"attempt", attempt,
			"maxAttempts", config.MaxAttempts,
			"error", err)
		
		// Continue retrying
		return false, nil
	})
	
	if err != nil {
		if wait.Interrupted(err) && lastErr != nil {
			return fmt.Errorf("%s failed after %d attempts: %w", operation, attempt, lastErr)
		}
		return err
	}
	
	return nil
}

// isRetryableError determines if an error should be retried
func isRetryableError(err error) bool {
	// Network errors are typically retryable
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	
	// Check for specific OCI errors that are retryable
	switch {
	case errors.Is(err, ErrShapeNotAvailable):
		return true
	case errors.Is(err, ErrSubnetFull):
		return true
	case errors.Is(err, ErrInstanceLimitExceeded):
		return true
	default:
		// Check error message for known retryable patterns
		errMsg := err.Error()
		retryablePatterns := []string{
			"timeout",
			"connection refused",
			"temporary failure",
			"too many requests",
			"TooManyRequests",
			"rate limit",
			"throttled",
			"429",
		}
		
		for _, pattern := range retryablePatterns {
			if contains(errMsg, pattern) {
				return true
			}
		}
	}
	
	return false
}

// isTerminalError determines if an error should stop all retries
func isTerminalError(err error) bool {
	// Invalid configuration should not be retried
	if errors.Is(err, ErrInvalidConfiguration) {
		return true
	}
	
	// Quota exceeded is terminal until quota is increased
	if errors.Is(err, ErrQuotaExceeded) {
		return true
	}
	
	// NodeClaim not found is terminal
	if cloudprovider.IsNodeClaimNotFoundError(err) {
		return true
	}
	
	return false
}

// HandleError processes an error and determines the appropriate action
func HandleError(ctx context.Context, err error, operation string) error {
	if err == nil {
		return nil
	}
	
	logger := log.FromContext(ctx)
	
	// Log the error with appropriate level
	switch {
	case IsNotFoundError(err):
		logger.V(1).Info("resource not found", "operation", operation, "error", err)
		return cloudprovider.NewNodeClaimNotFoundError(err)
	case errors.Is(err, ErrQuotaExceeded):
		logger.Error(err, "quota exceeded", "operation", operation)
		return fmt.Errorf("quota exceeded for %s: %w", operation, err)
	case errors.Is(err, ErrInvalidConfiguration):
		logger.Error(err, "invalid configuration", "operation", operation)
		return fmt.Errorf("invalid configuration for %s: %w", operation, err)
	default:
		logger.Error(err, "operation failed", "operation", operation)
		return fmt.Errorf("%s failed: %w", operation, err)
	}
}

// WrapOCIError wraps OCI-specific errors into appropriate Karpenter errors
func WrapOCIError(err error, resourceType string) error {
	if err == nil {
		return nil
	}
	
	// In a real implementation, this would check OCI-specific error types
	// For now, we'll use string matching as an example
	errMsg := err.Error()
	
	switch {
	case contains(errMsg, "NotFound") || contains(errMsg, "404"):
		// Add more context for NotAuthorizedOrNotFound errors
		if contains(errMsg, "NotAuthorizedOrNotFound") {
			return fmt.Errorf("%s not found or not authorized: %w (Check IAM policies, resource OCIDs, and compartment access)", resourceType, err)
		}
		return &NotFoundError{
			ResourceType: resourceType,
			ResourceID:   extractResourceID(errMsg),
		}
	case contains(errMsg, "LimitExceeded"):
		return ErrInstanceLimitExceeded
	case contains(errMsg, "QuotaExceeded"):
		return ErrQuotaExceeded
	case contains(errMsg, "InvalidParameter"):
		return ErrInvalidConfiguration
	case contains(errMsg, "ShapeNotAvailable"):
		return ErrShapeNotAvailable
	default:
		return err
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || 
		   len(s) >= len(substr) && s[len(s)-len(substr):] == substr ||
		   len(substr) < len(s) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func extractResourceID(errMsg string) string {
	// Simple extraction - in real implementation would be more sophisticated
	return "unknown"
}

// IsRateLimitError checks if an error is due to rate limiting
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	
	errMsg := err.Error()
	return contains(errMsg, "TooManyRequests") || 
		   contains(errMsg, "429") || 
		   contains(errMsg, "rate limit") || 
		   contains(errMsg, "throttled") ||
		   contains(errMsg, "too many requests")
}