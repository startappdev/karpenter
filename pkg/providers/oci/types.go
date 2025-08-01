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

import "time"

// Config holds OCI provider configuration
type Config struct {
	// Authentication
	AuthType      string // "instance_principal" or "user_principal"
	TenancyOCID   string
	UserOCID      string
	Fingerprint   string
	PrivateKey    string
	Passphrase    string

	// Regional settings
	Region         string
	CompartmentID  string
	ClusterID      string // OKE cluster OCID

	// Network configuration
	VCNID     string
	SubnetIDs []string

	// Image configuration
	ImageID string

	// Shape defaults
	DefaultShapes []string

	// Pricing cache settings
	PricingCacheTTL time.Duration

	// API rate limiting
	MaxConcurrentLaunches int
	LaunchRateLimit       string

	// Monitoring
	EnableDetailedMetrics bool
	MetricsPort           int
}

// Instance represents an OCI compute instance
type Instance struct {
	ID                  string
	Shape               string
	ShapeConfig         *ShapeConfig
	ImageID             string
	CompartmentID       string
	AvailabilityDomain  string
	FaultDomain         string
	State               string
	TimeCreated         time.Time
	IsPreemptible       bool
	Metadata            map[string]string
	FreeformTags        map[string]string
	DefinedTags         map[string]map[string]interface{}
}

// ShapeConfig represents flexible shape configuration
type ShapeConfig struct {
	OCPUs       *int32
	MemoryInGBs *int32
}

// Shape represents an OCI compute shape
type Shape struct {
	Name                     string
	OCPUs                    float32
	MemoryInGBs              float32
	IsFlexible               bool
	OCPUOptions              *OCPUOptions
	MemoryOptions            *MemoryOptions
	NetworkingBandwidthInGbps float32
}

// OCPUOptions represents OCPU configuration options for flexible shapes
type OCPUOptions struct {
	Min float32
	Max float32
}

// MemoryOptions represents memory configuration options for flexible shapes
type MemoryOptions struct {
	MinInGBs         float32
	MaxInGBs         float32
	DefaultPerOCPUInGBs float32
	MinPerOCPUInGBs    float32
	MaxPerOCPUInGBs    float32
}

// PricingInfo represents pricing information for shapes
type PricingInfo struct {
	OCPUPerHour   float64
	MemoryGBHour  float64
	Currency      string
}

// LaunchInstanceDetails contains the details for launching an instance
type LaunchInstanceDetails struct {
	AvailabilityDomain        string
	CompartmentID             string
	Shape                     string
	ShapeConfig               *ShapeConfig
	ImageID                   string
	SubnetID                  string
	AssignPublicIP            bool
	AssignPrivateIP           bool
	Metadata                  map[string]string
	FreeformTags              map[string]string
	DefinedTags               map[string]map[string]interface{}
	PreemptibleInstanceConfig *PreemptibleInstanceConfig
}

// PreemptibleInstanceConfig represents configuration for preemptible instances
type PreemptibleInstanceConfig struct {
	PreemptionAction string // "TERMINATE" or "PRESERVE"
}

// Error types

// NotFoundError indicates a resource was not found
type NotFoundError struct {
	ResourceType string
	ResourceID   string
}

func (e *NotFoundError) Error() string {
	return "resource not found: " + e.ResourceType + "/" + e.ResourceID
}

// IsNotFoundError checks if an error is a NotFoundError
func IsNotFoundError(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}